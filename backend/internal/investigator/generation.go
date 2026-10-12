package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

func (service *Service) runGenerated(ctx context.Context, job Job) (Job, error) {
	if service.config.Provider == nil {
		return service.failWithEvidenceFallback(job, "provider_disabled", errors.New("provider is disabled"))
	}
	if job.UsageReservationID == "" {
		reservationID, err := service.store.ReserveUsage(ctx, job.ID,
			int64(service.config.InputTokenLimit*service.config.MaxProviderRounds),
			int64(service.config.OutputTokenLimit*service.config.MaxProviderRounds))
		if err != nil {
			return service.failWithEvidenceFallback(job, "budget_exhausted", err)
		}
		job.UsageReservationID = reservationID
		job.PromptVersion = service.config.PromptVersion
		if err := service.store.SaveCheckpoint(ctx, job); err != nil {
			return job, err
		}
	}

	for job.ProviderRounds < service.config.MaxProviderRounds {
		job.ProviderRounds++
		if err := service.store.SaveCheckpoint(ctx, job); err != nil {
			return job, err
		}
		result, err := service.config.Provider.Generate(ctx, GenerationRequest{
			Question: job.Question, Evidence: *job.Snapshot, Chunks: job.Chunks,
			PromptVersion: service.config.PromptVersion, InputTokenLimit: service.config.InputTokenLimit,
			OutputTokenLimit:    service.config.OutputTokenLimit,
			ToolRoundsRemaining: service.config.MaxProviderRounds - job.ProviderRounds,
			Continuation:        append([]json.RawMessage(nil), job.ProviderContinuation...),
			ToolOutputs:         append([]ProviderToolOutput(nil), job.ProviderToolOutputs...),
		})
		if err != nil {
			return service.failWithEvidenceFallback(job, providerErrorCode(err), err)
		}
		job.ProviderRequestIDs = append(job.ProviderRequestIDs, result.ProviderRequestID)
		job.ProviderModel = result.ProviderModel
		job.ProviderContinuation = append(job.ProviderContinuation, result.Continuation...)
		accumulateUsage(&job, result.Usage)
		encodedResult, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return service.failWithEvidenceFallback(job, "provider_malformed", marshalErr)
		}
		job.SourceHashes = append(job.SourceHashes, SourceHash{ToolName: "provider_response", SHA256: "sha256:" + sha256Hex(encodedResult)})
		if err := service.store.SaveCheckpoint(ctx, job); err != nil {
			return job, err
		}

		if len(result.ToolCalls) > 0 {
			if job.ProviderRounds >= service.config.MaxProviderRounds {
				return service.failWithEvidenceFallback(job, "provider_round_limit", errors.New("provider requested tools after final round"))
			}
			if err := service.executeProviderTools(ctx, &job, result.ToolCalls); err != nil {
				return service.failWithEvidenceFallback(job, "provider_tool_rejected", err)
			}
			continue
		}
		if result.Report == nil {
			return service.failWithEvidenceFallback(job, "provider_malformed", ErrProviderMalformed)
		}
		grounded, err := GroundGeneratedReport(*result.Report, *job.Snapshot, job.Chunks, job.Question)
		if err != nil {
			return service.failWithEvidenceFallback(job, "grounding_rejected", err)
		}
		if err := service.store.CompleteUsage(ctx, job.UsageReservationID, job.Usage); err != nil {
			return service.failWithEvidenceFallback(job, "usage_commit_failed", err)
		}
		job.Report = &grounded
		job.Status = StatusCompleted
		job.CompletedAt = service.config.Now().UTC()
		job.ErrorCode = ""
		if err := service.store.SaveCheckpoint(ctx, job); err != nil {
			return job, err
		}
		service.counters.completed.Add(1)
		return job, nil
	}
	return service.failWithEvidenceFallback(job, "provider_round_limit", errors.New("provider did not return a report"))
}

func (service *Service) executeProviderTools(ctx context.Context, job *Job, calls []ProviderToolCall) error {
	session, err := NewToolSession(service.registry,
		TrustedScope{ActorID: job.ActorID, Role: job.Role, SelectedPaymentID: job.PaymentID},
		job.Snapshot, 8, job.ToolCallKeys)
	if err != nil {
		return err
	}
	seenCallIDs := make(map[string]struct{}, len(job.ProviderToolOutputs)+len(calls))
	for _, output := range job.ProviderToolOutputs {
		seenCallIDs[output.CallID] = struct{}{}
	}
	for _, providerCall := range calls {
		if _, duplicate := seenCallIDs[providerCall.CallID]; duplicate {
			return errors.New("provider repeated a tool call ID")
		}
		seenCallIDs[providerCall.CallID] = struct{}{}
		call, err := NormalizeProviderToolCall(providerCall, job.PaymentID)
		if err != nil {
			return err
		}
		started := service.config.Now().UTC()
		entry := ToolTimelineEntry{ToolName: call.Name, Arguments: append(json.RawMessage(nil), call.Arguments...), StartedAt: started.Format(time.RFC3339Nano)}
		service.counters.toolCalls.Add(1)
		result, err := session.Execute(ctx, call)
		entry.FinishedAt = service.config.Now().UTC().Format(time.RFC3339Nano)
		job.ToolCallKeys = session.Keys()
		if err != nil {
			service.counters.toolErrors.Add(1)
			entry.Status = timelineStatus(err)
			job.ToolTimeline = append(job.ToolTimeline, entry)
			_ = service.saveDetached(*job)
			return err
		}
		entry.Status = "completed"
		entry.EvidenceIds = append([]string(nil), result.EvidenceIds...)
		job.ToolTimeline = append(job.ToolTimeline, entry)
		job.SourceHashes = append(job.SourceHashes, SourceHash{ToolName: call.Name, SHA256: result.SourceHash})
		if call.Name == ToolReadPaymentEvidence {
			var refreshed EvidenceBundle
			if err := json.Unmarshal(result.Payload, &refreshed); err != nil || refreshed.SnapshotId != job.Snapshot.SnapshotId {
				return errors.New("selected evidence changed during generation")
			}
		}
		job.ProviderToolOutputs = append(job.ProviderToolOutputs, ProviderToolOutput{
			CallID: providerCall.CallID, Name: providerCall.Name, Output: append(json.RawMessage(nil), result.Payload...),
		})
		if err := service.store.SaveCheckpoint(ctx, *job); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) failWithEvidenceFallback(job Job, code string, cause error) (Job, error) {
	if job.Report == nil && job.Snapshot != nil {
		if report, err := BuildEvidenceOnlyReport(*job.Snapshot, job.Chunks, job.Question); err == nil {
			job.Report = &report
		}
	}
	return service.fail(job, code, cause)
}

func providerErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrProviderRefusal):
		return "provider_refused"
	case errors.Is(err, ErrProviderRateLimited):
		return "provider_rate_limited"
	case errors.Is(err, ErrProviderMalformed):
		return "provider_malformed"
	case errors.Is(err, context.DeadlineExceeded):
		return "provider_timeout"
	default:
		return "provider_unavailable"
	}
}

func accumulateUsage(job *Job, usage Usage) {
	if usage.InputTokens == nil || usage.OutputTokens == nil {
		job.Usage = Usage{}
		job.UsageKnown = false
		return
	}
	if job.ProviderRounds == 1 {
		input, output := *usage.InputTokens, *usage.OutputTokens
		job.Usage = Usage{InputTokens: &input, OutputTokens: &output}
		job.UsageKnown = true
		return
	}
	if !job.UsageKnown || job.Usage.InputTokens == nil || job.Usage.OutputTokens == nil {
		return
	}
	input := *job.Usage.InputTokens + *usage.InputTokens
	output := *job.Usage.OutputTokens + *usage.OutputTokens
	job.Usage = Usage{InputTokens: &input, OutputTokens: &output}
}
