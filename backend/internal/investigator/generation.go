package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrProviderUsageUncertain = errors.New("provider usage is uncertain")

func (service *Service) runGenerated(ctx context.Context, job Job) (Job, error) {
	if service.config.Provider == nil {
		if job.UsageReservationID != "" {
			return service.failGenerated(job, "provider_disabled", errors.New("provider is disabled"))
		}
		return service.failWithEvidenceFallback(job, "provider_disabled", errors.New("provider is disabled"))
	}
	if job.UsageReservationID == "" {
		reservationID, err := service.store.ReserveUsage(ctx, job, UsageAllowance{
			InputTokens:  int64(service.config.InputTokenLimit * service.config.MaxProviderRounds),
			OutputTokens: int64(service.config.OutputTokenLimit * service.config.MaxProviderRounds),
			CostMicrousd: service.config.CostLimitMicrousd,
		})
		if err != nil {
			return service.failWithEvidenceFallback(job, "budget_exhausted", err)
		}
		job.UsageReservationID = reservationID
		job.PromptVersion = service.config.PromptVersion
		if err := service.store.SaveCheckpoint(ctx, job); err != nil {
			return job, err
		}
	}
	if job.ProviderCallPending {
		return service.failGenerated(job, "provider_usage_uncertain", ErrProviderUsageUncertain)
	}
	if job.Report != nil {
		return service.completeGenerated(ctx, job)
	}
	if len(job.ProviderPendingToolCalls) > 0 {
		if err := service.executeProviderTools(ctx, &job, job.ProviderPendingToolCalls); err != nil {
			return service.failGenerated(job, "provider_tool_rejected", err)
		}
		job.ProviderPendingToolCalls = nil
		if err := service.store.SaveCheckpoint(ctx, job); err != nil {
			return service.failGenerated(job, "provider_checkpoint_failed", err)
		}
	}
	if job.ProviderPendingReport != nil {
		return service.groundAndCompleteGenerated(ctx, job)
	}

	for job.ProviderRounds < service.config.MaxProviderRounds {
		job.ProviderRounds++
		job.ProviderCallPending = true
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
			job.ProviderCallPending = false
			return service.failGenerated(job, providerErrorCode(err), err)
		}
		job.ProviderCallPending = false
		if result.ProviderRequestID == "" {
			return service.failGenerated(job, "provider_malformed", ErrProviderMalformed)
		}
		if err := validateProviderToolCallIDs(job.ProviderToolOutputs, result.ToolCalls); err != nil {
			return service.failGenerated(job, "provider_tool_rejected", err)
		}
		job.ProviderRequestIDs = append(job.ProviderRequestIDs, result.ProviderRequestID)
		job.ProviderModel = result.ProviderModel
		job.ProviderContinuation = append(job.ProviderContinuation, result.Continuation...)
		job.ProviderPendingToolCalls = cloneProviderToolCalls(result.ToolCalls)
		job.ProviderPendingReport = cloneInvestigationReport(result.Report)
		accumulateUsage(&job, result.Usage)
		encodedResult, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return service.failGenerated(job, "provider_malformed", marshalErr)
		}
		job.SourceHashes = append(job.SourceHashes, SourceHash{ToolName: "provider_response", SHA256: "sha256:" + sha256Hex(encodedResult)})
		if err := service.store.SaveCheckpoint(ctx, job); err != nil {
			return service.failGenerated(job, "provider_checkpoint_failed", err)
		}

		if len(result.ToolCalls) > 0 {
			if job.ProviderRounds >= service.config.MaxProviderRounds {
				return service.failGenerated(job, "provider_round_limit", errors.New("provider requested tools after final round"))
			}
			if err := service.executeProviderTools(ctx, &job, job.ProviderPendingToolCalls); err != nil {
				return service.failGenerated(job, "provider_tool_rejected", err)
			}
			job.ProviderPendingToolCalls = nil
			if err := service.store.SaveCheckpoint(ctx, job); err != nil {
				return service.failGenerated(job, "provider_checkpoint_failed", err)
			}
			continue
		}
		if job.ProviderPendingReport == nil {
			return service.failGenerated(job, "provider_malformed", ErrProviderMalformed)
		}
		return service.groundAndCompleteGenerated(ctx, job)
	}
	return service.failGenerated(job, "provider_round_limit", errors.New("provider did not return a report"))
}

func (service *Service) groundAndCompleteGenerated(ctx context.Context, job Job) (Job, error) {
	grounded, err := GroundGeneratedReport(*job.ProviderPendingReport, *job.Snapshot, job.Chunks, job.Question)
	if err != nil {
		return service.failGenerated(job, "grounding_rejected", err)
	}
	job.Report = &grounded
	job.ProviderPendingReport = nil
	if err := service.store.SaveCheckpoint(ctx, job); err != nil {
		return service.failGenerated(job, "provider_checkpoint_failed", err)
	}
	return service.completeGenerated(ctx, job)
}

func (service *Service) completeGenerated(ctx context.Context, job Job) (Job, error) {
	job.Status = StatusCompleted
	job.CompletedAt = service.config.Now().UTC()
	job.ErrorCode = ""
	if err := service.store.CompleteUsage(ctx, job); err != nil {
		if errors.Is(err, ErrUsageBudget) {
			job.Status = StatusRunning
			job.CompletedAt = time.Time{}
			return service.failGenerated(job, "usage_commit_failed", err)
		}
		job.Status = StatusRunning
		job.CompletedAt = time.Time{}
		return job, err
	}
	service.counters.completed.Add(1)
	return job, nil
}

func (service *Service) executeProviderTools(ctx context.Context, job *Job, calls []ProviderToolCall) error {
	session, err := NewToolSession(service.registry,
		TrustedScope{ActorID: job.ActorID, Role: job.Role, SelectedPaymentID: job.PaymentID},
		job.Snapshot, 8, job.ToolCallKeys)
	if err != nil {
		return err
	}
	completedCalls := make(map[string]string, len(job.ProviderToolOutputs))
	for _, output := range job.ProviderToolOutputs {
		completedCalls[output.CallID] = output.Name
	}
	for _, providerCall := range calls {
		if completedName, completed := completedCalls[providerCall.CallID]; completed {
			if completedName != providerCall.Name {
				return errors.New("provider changed a checkpointed tool call")
			}
			continue
		}
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

func validateProviderToolCallIDs(outputs []ProviderToolOutput, calls []ProviderToolCall) error {
	seen := make(map[string]struct{}, len(outputs)+len(calls))
	for _, output := range outputs {
		seen[output.CallID] = struct{}{}
	}
	for _, call := range calls {
		if call.CallID == "" {
			return errors.New("provider omitted a tool call ID")
		}
		if _, duplicate := seen[call.CallID]; duplicate {
			return errors.New("provider repeated a tool call ID")
		}
		seen[call.CallID] = struct{}{}
	}
	return nil
}

func cloneProviderToolCalls(calls []ProviderToolCall) []ProviderToolCall {
	cloned := make([]ProviderToolCall, len(calls))
	for index, call := range calls {
		cloned[index] = call
		cloned[index].Arguments = append(json.RawMessage(nil), call.Arguments...)
	}
	return cloned
}

func cloneInvestigationReport(report *InvestigationReport) *InvestigationReport {
	if report == nil {
		return nil
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return nil
	}
	var cloned InvestigationReport
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return nil
	}
	return &cloned
}

func (service *Service) failWithEvidenceFallback(job Job, code string, cause error) (Job, error) {
	if job.Report == nil && job.Snapshot != nil {
		if report, err := BuildEvidenceOnlyReport(*job.Snapshot, job.Chunks, job.Question); err == nil {
			job.Report = &report
		}
	}
	return service.fail(job, code, cause)
}

func (service *Service) failGenerated(job Job, code string, cause error) (Job, error) {
	job.ProviderCallPending = false
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := service.store.MarkUsageUncertain(ctx, job); err != nil {
		cause = errors.Join(cause, err)
		code = "usage_commit_failed"
	}
	return service.failWithEvidenceFallback(job, code, cause)
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
	case errors.Is(err, ErrProviderUsageUncertain):
		return "provider_usage_uncertain"
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
		job.Usage = Usage{InputTokens: &input, OutputTokens: &output, CostMicrousd: cloneInt64Pointer(usage.CostMicrousd)}
		job.UsageKnown = true
		return
	}
	if !job.UsageKnown || job.Usage.InputTokens == nil || job.Usage.OutputTokens == nil {
		return
	}
	input := *job.Usage.InputTokens + *usage.InputTokens
	output := *job.Usage.OutputTokens + *usage.OutputTokens
	var cost *int64
	if job.Usage.CostMicrousd != nil && usage.CostMicrousd != nil {
		total := *job.Usage.CostMicrousd + *usage.CostMicrousd
		cost = &total
	}
	job.Usage = Usage{InputTokens: &input, OutputTokens: &output, CostMicrousd: cost}
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
