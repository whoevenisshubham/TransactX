package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	traceIDRegex     = regexp.MustCompile(`^[0-9a-f]{32}$`)
	ErrInvalidTraceID = errors.New("trace ID must be a 32-character lowercase hex string")
)

// TraceResponse matches the contract defined in API_CONTRACTS.md.
type TraceResponse struct {
	TraceID      string    `json:"traceId"`
	SourceStatus string    `json:"sourceStatus"` // "available" or "not_collected"
	CollectedAt  time.Time `json:"collectedAt"`
	Truncated    bool      `json:"truncated"`
	Spans        []SpanDTO `json:"spans"`
}

// SpanDTO represents a single recorded span in the trace hierarchy.
type SpanDTO struct {
	SpanID       string         `json:"spanId"`
	ParentSpanID *string        `json:"parentSpanId"`
	Service      string         `json:"service"`
	Name         string         `json:"name"`
	Kind         string         `json:"kind"`
	StartTime    time.Time      `json:"startTime"`
	EndTime      time.Time      `json:"endTime"`
	DurationMs   int64          `json:"durationMs"`
	Status       string         `json:"status"`
	Attributes   map[string]any `json:"attributes"`
	Links        []LinkDTO      `json:"links,omitempty"`
}

// LinkDTO represents a span link (e.g. connecting a recovery attempt to an original trace).
type LinkDTO struct {
	TraceID string `json:"traceId"`
	SpanID  string `json:"spanId"`
}

// TraceStore handles read-only queries for operational trace analysis.
type TraceStore struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewTraceStore creates a new trace querying store.
func NewTraceStore(pool *pgxpool.Pool, logger *slog.Logger) *TraceStore {
	if logger == nil {
		logger = slog.Default()
	}
	return &TraceStore{
		pool:   pool,
		logger: logger,
	}
}

// ValidateTraceID verifies that a string is a 32-character lowercase hex trace ID.
func ValidateTraceID(traceID string) error {
	if !traceIDRegex.MatchString(traceID) {
		return ErrInvalidTraceID
	}
	return nil
}

// GetTrace retrieves stored spans for a given trace ID.
// If the trace does not exist or has expired, it returns sourceStatus: "not_collected" with an empty spans list.
func (s *TraceStore) GetTrace(ctx context.Context, traceID string) (*TraceResponse, error) {
	if err := ValidateTraceID(traceID); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	resp := &TraceResponse{
		TraceID:      traceID,
		SourceStatus: "not_collected",
		CollectedAt:  now,
		Truncated:    false,
		Spans:        []SpanDTO{},
	}

	if s.pool == nil {
		return resp, nil
	}

	query := `
		SELECT span_id, parent_span_id, service_name, span_name, span_kind,
		       start_time, end_time, duration_ms, status_code, attributes, links
		FROM trace_spans
		WHERE trace_id = $1
		ORDER BY start_time ASC
		LIMIT 201
	`
	rows, err := s.pool.Query(ctx, query, traceID)
	if err != nil {
		s.logger.Error("failed to query trace spans", "error", err, "trace_id", traceID)
		return resp, nil
	}
	defer rows.Close()

	spans := make([]SpanDTO, 0)
	for rows.Next() {
		var (
			spanID, serviceName, spanName, spanKind, statusCode string
			parentSpanID                                        *string
			startTime, endTime                                  time.Time
			durationMs                                          int64
			attrBytes, linksBytes                               []byte
		)
		if err := rows.Scan(
			&spanID, &parentSpanID, &serviceName, &spanName, &spanKind,
			&startTime, &endTime, &durationMs, &statusCode,
			&attrBytes, &linksBytes,
		); err != nil {
			s.logger.Error("failed to scan trace span row", "error", err)
			continue
		}

		attrs := make(map[string]any)
		if len(attrBytes) > 0 {
			_ = json.Unmarshal(attrBytes, &attrs)
		}

		var links []LinkDTO
		if len(linksBytes) > 0 {
			var rawLinks []map[string]any
			if err := json.Unmarshal(linksBytes, &rawLinks); err == nil {
				for _, rl := range rawLinks {
					tID, _ := rl["traceId"].(string)
					sID, _ := rl["spanId"].(string)
					if tID != "" && sID != "" {
						links = append(links, LinkDTO{TraceID: tID, SpanID: sID})
					}
				}
			}
		}

		spans = append(spans, SpanDTO{
			SpanID:       spanID,
			ParentSpanID: parentSpanID,
			Service:      serviceName,
			Name:         spanName,
			Kind:         spanKind,
			StartTime:    startTime.UTC(),
			EndTime:      endTime.UTC(),
			DurationMs:   durationMs,
			Status:       statusCode,
			Attributes:   attrs,
			Links:        links,
		})
	}

	if len(spans) == 0 {
		return resp, nil
	}

	truncated := false
	if len(spans) > 200 {
		truncated = true
		spans = spans[:200]
	}

	resp.SourceStatus = "available"
	resp.Truncated = truncated
	resp.Spans = spans
	return resp, nil
}

// GetTraceIDsForPayment retrieves all unique trace IDs associated with a specific payment.
func (s *TraceStore) GetTraceIDsForPayment(ctx context.Context, paymentID uuid.UUID) ([]string, error) {
	if s.pool == nil {
		return []string{}, nil
	}

	query := `
		SELECT DISTINCT trace_id
		FROM trace_spans
		WHERE payment_id = $1
		ORDER BY trace_id ASC
		LIMIT 20
	`
	rows, err := s.pool.Query(ctx, query, paymentID)
	if err != nil {
		return nil, fmt.Errorf("failed to query payment trace IDs: %w", err)
	}
	defer rows.Close()

	var traceIDs []string
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err == nil {
			traceIDs = append(traceIDs, tid)
		}
	}
	if traceIDs == nil {
		traceIDs = []string{}
	}
	return traceIDs, nil
}
