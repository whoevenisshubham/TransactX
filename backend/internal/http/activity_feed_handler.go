package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/common"
)

const (
	defaultActivityLimit = 50
	maxActivityLimit     = 200
)

// ActivityEvent is the server-owned normalized representation of one durable
// operational fact. ID is stable within the source and provides the ordering
// tie breaker when multiple facts have the same timestamp.
type ActivityEvent struct {
	ID         string         `json:"id"`
	Category   string         `json:"category"`
	EventType  string         `json:"eventType"`
	Severity   string         `json:"severity"`
	OccurredAt time.Time      `json:"occurredAt"`
	TargetID   string         `json:"targetId,omitempty"`
	Title      string         `json:"title"`
	Summary    string         `json:"summary"`
	Details    map[string]any `json:"details"`
}

type activityStore interface {
	List(ctx context.Context, limit, offset int) ([]ActivityEvent, error)
}

type postgresActivityStore struct {
	db *pgxpool.Pool
}

func newPostgresActivityStore(db *pgxpool.Pool) *postgresActivityStore {
	return &postgresActivityStore{db: db}
}

// List aggregates only durable source rows. No event is inferred from
// frontend state or generated at read time.
func (store *postgresActivityStore) List(ctx context.Context, limit, offset int) ([]ActivityEvent, error) {
	rows, err := store.db.Query(ctx, `
		WITH activity AS (
			SELECT 'circuit:' || id::text AS id,
			       'CIRCUIT'::text AS category,
			       event_type::text AS event_type,
			       CASE WHEN new_state = 'OPEN' THEN 'WARNING' ELSE 'INFO' END::text AS severity,
			       transitioned_at AS occurred_at,
			       execution_target_id::text AS target_id,
			       previous_state || ' → ' || new_state AS title,
			       reason::text AS summary,
			       (CASE
			          WHEN jsonb_typeof(details) = 'object' THEN details
			          ELSE jsonb_build_object('sourceDetails', details)
			        END) || jsonb_build_object(
			           'failureCount', failure_count,
			           'timeoutCount', timeout_count,
			           'restorationStep', restoration_step
			       ) AS details
			FROM circuit_transition_events

			UNION ALL

			SELECT 'chaos:' || id::text,
			       'CHAOS', event_type::text, 'WARNING', occurred_at,
			       target_id::text,
			       event_type::text,
			       fault_type || ' simulation for ' || scenario_id,
			       (CASE
			          WHEN jsonb_typeof(details) = 'object' THEN details
			          ELSE jsonb_build_object('sourceDetails', details)
			        END) || jsonb_build_object(
			           'scenarioId', scenario_id,
			           'faultType', fault_type,
			           'actorId', actor_id,
			           'actorRole', actor_role,
			           'parameters', parameters
			       )
			FROM chaos_events

			UNION ALL

			SELECT 'reconciliation:' || id::text || ':started',
			       'RECONCILIATION', 'RECONCILIATION_STARTED', 'INFO', started_at,
			       participant_id::text,
			       'Reconciliation started',
			       'Commitment comparison started for ' || participant_id,
			       jsonb_build_object('runId', id, 'scopeFrom', scope_from, 'scopeTo', scope_to)
			FROM recon_runs

			UNION ALL

			SELECT 'reconciliation:' || id::text || ':terminal',
			       'RECONCILIATION',
			       CASE WHEN status = 'FAILED' THEN 'RECONCILIATION_FAILED' ELSE 'RECONCILIATION_COMPLETED' END,
			       CASE WHEN status = 'FAILED' THEN 'ERROR' WHEN discrepancy_count > 0 THEN 'WARNING' ELSE 'INFO' END,
			       completed_at,
			       participant_id::text,
			       CASE WHEN status = 'FAILED' THEN 'Reconciliation failed' ELSE 'Reconciliation completed' END,
			       CASE WHEN status = 'FAILED' THEN COALESCE(error_message, 'Operational execution failed')
			            ELSE discrepancy_count::text || ' discrepancies across ' || divergent_buckets::text || ' buckets' END,
			       jsonb_build_object(
			           'runId', id,
			           'status', status,
			           'discrepancyCount', discrepancy_count,
			           'divergentBuckets', divergent_buckets,
			           'divergentRecords', divergent_records
			       )
			FROM recon_runs
			WHERE completed_at IS NOT NULL

			UNION ALL

			SELECT 'reconciliation-discrepancy:' || id::text,
			       'RECONCILIATION', 'RECONCILIATION_DISCREPANCY', 'WARNING', detected_at,
			       participant_id::text,
			       mismatch_category::text,
			       'Divergence detected in ' || bucket_key,
			       (CASE
			          WHEN jsonb_typeof(evidence) = 'object' THEN evidence
			          ELSE jsonb_build_object('sourceEvidence', evidence)
			        END) || jsonb_build_object('runId', run_id, 'bucketKey', bucket_key)
			FROM recon_discrepancies

			UNION ALL

			SELECT 'integrity:' || id::text || ':started',
			       'INTEGRITY', 'INTEGRITY_RUN_STARTED', 'INFO', started_at,
			       COALESCE(participant_id, ''),
			       'Integrity run started',
			       'Runtime financial invariant checks started',
			       jsonb_build_object('runId', id)
			FROM integrity_runs

			UNION ALL

			SELECT 'integrity:' || id::text || ':terminal',
			       'INTEGRITY',
			       CASE WHEN status = 'FAILED' THEN 'INTEGRITY_RUN_FAILED'
			            WHEN failed_checks > 0 OR error_checks > 0 THEN 'INTEGRITY_VIOLATION_DETECTED'
			            ELSE 'INTEGRITY_RUN_COMPLETED' END,
			       CASE WHEN status = 'FAILED' OR error_checks > 0 THEN 'ERROR'
			            WHEN failed_checks > 0 THEN 'WARNING' ELSE 'INFO' END,
			       completed_at,
			       COALESCE(participant_id, ''),
			       CASE WHEN status = 'FAILED' THEN 'Integrity run failed'
			            WHEN failed_checks > 0 OR error_checks > 0 THEN 'Integrity violation detected'
			            ELSE 'Integrity run completed' END,
			       CASE WHEN status = 'FAILED' THEN COALESCE(error_message, 'Integrity execution failed')
			            ELSE passed_checks::text || ' passed, ' || failed_checks::text || ' failed, ' || error_checks::text || ' errors' END,
			       jsonb_build_object(
			           'runId', id,
			           'status', status,
			           'totalChecks', total_checks,
			           'passedChecks', passed_checks,
			           'failedChecks', failed_checks,
			           'errorChecks', error_checks
			       )
			FROM integrity_runs
			WHERE completed_at IS NOT NULL

			UNION ALL

			SELECT 'route:' || id::text,
			       'ROUTING', event_type::text, 'INFO', selected_at,
			       execution_target_id::text,
			       'Payment routed',
			       reason_code || ' via ' || execution_target_id,
			       jsonb_build_object(
			           'paymentId', payment_id,
			           'candidateId', candidate_id,
			           'executionTargetId', execution_target_id,
			           'selectedScore', selected_score,
			           'selectionMode', selection_mode,
			           'reasonCode', reason_code
			       )
			FROM payment_route_decisions
		)
		SELECT id, category, event_type, severity, occurred_at, target_id, title, summary, details
		FROM activity
		ORDER BY occurred_at DESC, id DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]ActivityEvent, 0, limit)
	for rows.Next() {
		var event ActivityEvent
		var detailsJSON []byte
		if err := rows.Scan(
			&event.ID, &event.Category, &event.EventType, &event.Severity,
			&event.OccurredAt, &event.TargetID, &event.Title, &event.Summary, &detailsJSON,
		); err != nil {
			return nil, err
		}
		event.OccurredAt = event.OccurredAt.UTC()
		event.Details = make(map[string]any)
		if len(detailsJSON) > 0 {
			if err := json.Unmarshal(detailsJSON, &event.Details); err != nil {
				return nil, fmt.Errorf("decode activity details for %s: %w", event.ID, err)
			}
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func sortActivityEvents(events []ActivityEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		if !events[i].OccurredAt.Equal(events[j].OccurredAt) {
			return events[i].OccurredAt.After(events[j].OccurredAt)
		}
		return events[i].ID > events[j].ID
	})
}

func (handler *Handler) activityFeed(writer http.ResponseWriter, request *http.Request) {
	if handler.activity == nil {
		writeAPIError(writer, request, common.NewAPIError("NOT_FOUND", "operational activity is not configured", http.StatusNotFound))
		return
	}
	limit := parseIntParam(request, "limit", defaultActivityLimit)
	if limit <= 0 {
		limit = defaultActivityLimit
	}
	if limit > maxActivityLimit {
		limit = maxActivityLimit
	}
	offset := parseIntParam(request, "offset", 0)
	if offset < 0 {
		offset = 0
	}

	events, err := handler.activity.List(request.Context(), limit+1, offset)
	if err != nil {
		if handler.logger != nil {
			handler.logger.Error("list operational activity", "error", err)
		}
		writeAPIError(writer, request, common.NewAPIError("INTERNAL_ERROR", "operational activity unavailable", http.StatusInternalServerError))
		return
	}
	sortActivityEvents(events)

	var nextOffset *int
	if len(events) > limit {
		events = events[:limit]
		next := offset + limit
		nextOffset = &next
	}
	writeData(writer, http.StatusOK, request, map[string]any{
		"items":      events,
		"limit":      limit,
		"nextOffset": nextOffset,
	})
}
