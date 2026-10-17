package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/analytics"
	"github.com/transactx/backend/internal/investigator"
	"github.com/transactx/backend/internal/operations"
	"github.com/transactx/backend/internal/telemetry"
)

type opsEvidenceSource struct {
	ops *operations.Service
}

func (s *opsEvidenceSource) ReadPaymentEvidenceJSON(ctx context.Context, scope investigator.TrustedScope, paymentID string) ([]byte, error) {
	if s.ops == nil {
		return nil, errors.New("operations service unavailable")
	}
	pid, err := uuid.Parse(paymentID)
	if err != nil {
		return nil, fmt.Errorf("invalid payment uuid: %w", err)
	}
	bundle, err := s.ops.ReadPaymentEvidence(ctx, "trusted_ops", pid)
	if err != nil {
		return nil, err
	}
	return json.Marshal(bundle)
}

type opsRecoveryReader struct {
	ops *operations.Service
}

func (r *opsRecoveryReader) ReadRecoveryAttempts(ctx context.Context, scope investigator.TrustedScope, paymentID string, limit int) ([]investigator.RecoveryAttempt, error) {
	if r.ops == nil {
		return nil, errors.New("operations service unavailable")
	}
	pid, err := uuid.Parse(paymentID)
	if err != nil {
		return nil, fmt.Errorf("invalid payment uuid: %w", err)
	}
	page, err := r.ops.GetRecoveryAttempts(ctx, pid, limit, "")
	if err != nil {
		return nil, err
	}
	var res []investigator.RecoveryAttempt
	for _, a := range page.Items {
		var finishedAt *string
		if a.FinishedAt != nil {
			s := a.FinishedAt.UTC().Format(time.RFC3339Nano)
			finishedAt = &s
		}
		var opIDs []string
		for _, opID := range a.OperationIds {
			opIDs = append(opIDs, opID.String())
		}
		res = append(res, investigator.RecoveryAttempt{
			AttemptId:    a.AttemptID.String(),
			PaymentId:    a.PaymentID.String(),
			WorkerId:     a.WorkerID,
			StartedAt:    a.StartedAt.UTC().Format(time.RFC3339Nano),
			FinishedAt:   finishedAt,
			StateBefore:  a.StateBefore,
			StateAfter:   a.StateAfter,
			Result:       a.Result,
			ReasonCode:   a.ReasonCode,
			OperationIds: opIDs,
			TraceId:      a.TraceID,
			LeaseEpoch:   a.LeaseEpoch,
		})
	}
	return res, nil
}

type opsTraceReader struct {
	traceStore *telemetry.TraceStore
}

func (r *opsTraceReader) ReadTrace(ctx context.Context, scope investigator.TrustedScope, traceID string) (investigator.Trace, error) {
	if r.traceStore == nil {
		return investigator.Trace{}, errors.New("trace store not configured")
	}
	t, err := r.traceStore.GetTrace(ctx, traceID)
	if err != nil {
		return investigator.Trace{}, err
	}
	var spans []investigator.Span
	for _, s := range t.Spans {
		spans = append(spans, investigator.Span{
			SpanId:       s.SpanID,
			ParentSpanId: s.ParentSpanID,
			Service:      s.Service,
			Name:         s.Name,
			StartedAt:    s.StartTime.UTC().Format(time.RFC3339Nano),
			Status:       s.Status,
		})
	}
	return investigator.Trace{
		TraceId:      t.TraceID,
		SourceStatus: investigator.SourceStatus(t.SourceStatus),
		CollectedAt:  t.CollectedAt.UTC().Format(time.RFC3339Nano),
		Truncated:    t.Truncated,
		Spans:        spans,
	}, nil
}

type opsAnalyticsReader struct {
	repo analytics.Repository
}

func (r *opsAnalyticsReader) ReadWindows(ctx context.Context, scope investigator.TrustedScope, entityType, entityID, from, to string, limit int) ([]investigator.AnalyticsWindow, error) {
	if r.repo == nil {
		return nil, errors.New("analytics repository not configured")
	}
	page, err := r.repo.QueryWindows(ctx, analytics.WindowFilter{
		EntityType: entityType,
		EntityID:   entityID,
		Limit:      limit,
	})
	if err != nil {
		return nil, err
	}
	var windows []investigator.AnalyticsWindow
	for _, w := range page.Items {
		windows = append(windows, investigator.AnalyticsWindow{
			Id:                   w.ID,
			EntityType:           w.EntityType,
			EntityId:             w.EntityID,
			From:                 w.From.UTC().Format(time.RFC3339Nano),
			To:                   w.To.UTC().Format(time.RFC3339Nano),
			SampleCount:          int64(w.SampleCount),
			FeatureSchemaVersion: w.FeatureSchemaVersion,
			Advisory:             w.Advisory,
		})
	}
	return windows, nil
}
