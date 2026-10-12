package analytics

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository provides durable storage and query methods for operational aggregate windows.
type Repository interface {
	UpsertWindow(ctx context.Context, win *AnalyticsWindow, raw map[string]any) error
	QueryWindows(ctx context.Context, filter WindowFilter) (*AnalyticsPage, error)
	GetLatestCompletedWindow(ctx context.Context, entityType, entityID string) (*AnalyticsWindow, error)
	GetCursor(ctx context.Context, cursorName string) (time.Time, error)
	SetCursor(ctx context.Context, cursorName string, lastWindowTo time.Time) error
	AggregateWindow(ctx context.Context, entityType, entityID string, from, to time.Time) (*AnalyticsWindow, map[string]any, error)
	ComputeMetricsSummary(ctx context.Context, asOf time.Time) (*MetricsSummary, error)
}

// PostgresRepository implements Repository on PostgreSQL.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository constructs a PostgresRepository.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// UpsertWindow persists or updates a completed operational aggregate window.
func (r *PostgresRepository) UpsertWindow(ctx context.Context, win *AnalyticsWindow, raw map[string]any) error {
	if win == nil {
		return errors.New("cannot upsert nil analytics window")
	}
	if win.ID == "" {
		win.ID = uuid.NewString()
	}
	if win.FeatureSchemaVersion == "" {
		win.FeatureSchemaVersion = FeatureSchemaVersionV1
	}

	featuresJSON, err := json.Marshal(win.Features)
	if err != nil {
		return fmt.Errorf("marshal features: %w", err)
	}

	var alertJSON []byte
	if win.BaselineAlert != nil {
		alertJSON, err = json.Marshal(win.BaselineAlert)
		if err != nil {
			return fmt.Errorf("marshal baseline alert: %w", err)
		}
	}

	rawJSON, err := json.Marshal(raw)
	if err != nil {
		rawJSON = []byte("{}")
	}

	query := `
		INSERT INTO operational_aggregate_windows (
			id, entity_type, entity_id, window_from, window_to, sample_count,
			feature_schema_version, features, baseline_alert, model_version,
			model_status, model_score, model_threshold, advisory, raw_aggregates,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NOW(), NOW()
		)
		ON CONFLICT (entity_type, entity_id, window_from, feature_schema_version)
		DO UPDATE SET
			sample_count = EXCLUDED.sample_count,
			features = EXCLUDED.features,
			baseline_alert = EXCLUDED.baseline_alert,
			model_version = EXCLUDED.model_version,
			model_status = EXCLUDED.model_status,
			model_score = EXCLUDED.model_score,
			model_threshold = EXCLUDED.model_threshold,
			raw_aggregates = EXCLUDED.raw_aggregates,
			updated_at = NOW()
		RETURNING id;
	`

	var returnedID string
	err = r.pool.QueryRow(ctx, query,
		win.ID,
		win.EntityType,
		win.EntityID,
		win.From,
		win.To,
		win.SampleCount,
		win.FeatureSchemaVersion,
		featuresJSON,
		alertJSON,
		win.Model.Version,
		win.Model.Status,
		win.Model.Score,
		win.Model.Threshold,
		win.Advisory,
		rawJSON,
	).Scan(&returnedID)

	if err != nil {
		return fmt.Errorf("upsert operational window: %w", err)
	}
	win.ID = returnedID
	return nil
}

// QueryWindows retrieves a paginated slice of operational aggregate windows.
func (r *PostgresRepository) QueryWindows(ctx context.Context, filter WindowFilter) (*AnalyticsPage, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}

	var cursorObj *WindowCursor
	if filter.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(filter.Cursor)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid encoding: %v", ErrInvalidCursor, err)
		}
		var c WindowCursor
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("%w: invalid payload: %v", ErrInvalidCursor, err)
		}
		cursorObj = &c
	}

	sqlBuilder := `
		SELECT
			id, entity_type, entity_id, window_from, window_to, sample_count,
			feature_schema_version, features, baseline_alert, model_version,
			model_status, model_score, model_threshold, advisory
		FROM operational_aggregate_windows
		WHERE 1=1
	`
	args := []any{}
	argIdx := 1

	if filter.EntityType != "" {
		sqlBuilder += fmt.Sprintf(" AND entity_type = $%d", argIdx)
		args = append(args, filter.EntityType)
		argIdx++
	}

	if filter.EntityID != "" {
		sqlBuilder += fmt.Sprintf(" AND entity_id = $%d", argIdx)
		args = append(args, filter.EntityID)
		argIdx++
	}

	if filter.From != nil {
		sqlBuilder += fmt.Sprintf(" AND window_from >= $%d", argIdx)
		args = append(args, *filter.From)
		argIdx++
	}

	if filter.To != nil {
		sqlBuilder += fmt.Sprintf(" AND window_from < $%d", argIdx)
		args = append(args, *filter.To)
		argIdx++
	}

	if cursorObj != nil {
		sqlBuilder += fmt.Sprintf(" AND (window_from < $%d OR (window_from = $%d AND id < $%d))", argIdx, argIdx, argIdx+1)
		args = append(args, cursorObj.WindowFrom, cursorObj.ID)
		argIdx += 2
	}

	sqlBuilder += fmt.Sprintf(" ORDER BY window_from DESC, id DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := r.pool.Query(ctx, sqlBuilder, args...)
	if err != nil {
		return nil, fmt.Errorf("query operational windows: %w", err)
	}
	defer rows.Close()

	var items []AnalyticsWindow
	for rows.Next() {
		var win AnalyticsWindow
		var featuresJSON, alertJSON []byte
		var modelVer sql.NullString
		var modelScore, modelThreshold sql.NullFloat64

		err := rows.Scan(
			&win.ID,
			&win.EntityType,
			&win.EntityID,
			&win.From,
			&win.To,
			&win.SampleCount,
			&win.FeatureSchemaVersion,
			&featuresJSON,
			&alertJSON,
			&modelVer,
			&win.Model.Status,
			&modelScore,
			&modelThreshold,
			&win.Advisory,
		)
		if err != nil {
			return nil, fmt.Errorf("scan operational window: %w", err)
		}

		if len(featuresJSON) > 0 {
			_ = json.Unmarshal(featuresJSON, &win.Features)
		}
		if len(alertJSON) > 0 {
			var w Warning
			if err := json.Unmarshal(alertJSON, &w); err == nil {
				win.BaselineAlert = &w
			}
		}
		if modelVer.Valid {
			win.Model.Version = &modelVer.String
		}
		if modelScore.Valid {
			win.Model.Score = &modelScore.Float64
		}
		if modelThreshold.Valid {
			win.Model.Threshold = &modelThreshold.Float64
		}

		items = append(items, win)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate operational windows: %w", err)
	}

	if items == nil {
		items = []AnalyticsWindow{}
	}

	var nextCursor *string
	if len(items) == limit {
		lastItem := items[len(items)-1]
		c := WindowCursor{
			WindowFrom: lastItem.From,
			ID:         lastItem.ID,
		}
		data, _ := json.Marshal(c)
		encoded := base64.RawURLEncoding.EncodeToString(data)
		nextCursor = &encoded
	}

	return &AnalyticsPage{
		Items:      items,
		NextCursor: nextCursor,
		AsOf:       time.Now().UTC(),
	}, nil
}

// GetLatestCompletedWindow returns the most recent aggregate window for the specified entity.
func (r *PostgresRepository) GetLatestCompletedWindow(ctx context.Context, entityType, entityID string) (*AnalyticsWindow, error) {
	page, err := r.QueryWindows(ctx, WindowFilter{
		EntityType: entityType,
		EntityID:   entityID,
		Limit:      1,
	})
	if err != nil {
		return nil, err
	}
	if len(page.Items) == 0 {
		return nil, pgx.ErrNoRows
	}
	return &page.Items[0], nil
}

// GetCursor reads the last aggregation timestamp for a given cursor name.
func (r *PostgresRepository) GetCursor(ctx context.Context, cursorName string) (time.Time, error) {
	var lastTo time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT last_window_to FROM operational_aggregate_cursors WHERE cursor_name = $1
	`, cursorName).Scan(&lastTo)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return lastTo, nil
}

// SetCursor updates the cursor boundary timestamp.
func (r *PostgresRepository) SetCursor(ctx context.Context, cursorName string, lastWindowTo time.Time) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO operational_aggregate_cursors (cursor_name, last_window_to, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (cursor_name)
		DO UPDATE SET last_window_to = EXCLUDED.last_window_to, updated_at = NOW();
	`, cursorName, lastWindowTo)
	return err
}

// AggregateWindow computes durable features for a single 5-minute half-open window [from, to).
func (r *PostgresRepository) AggregateWindow(ctx context.Context, entityType, entityID string, from, to time.Time) (*AnalyticsWindow, map[string]any, error) {
	// 1. Authoritative payment metrics as of window_to.
	// Evaluates payment states strictly at occurred_at <= to using state transitions to ensure
	// late terminal transitions after window end do not leak into earlier window features.
	paymentStatsQuery := `
		WITH window_payments AS (
			SELECT
				p.id,
				p.created_at,
				p.failure_reason,
				p.routing_reason,
				b_src.code AS src_code,
				b_dst.code AS dst_code,
				prd.execution_target_id
			FROM payments p
			LEFT JOIN banks b_src ON p.source_bank_id = b_src.id
			LEFT JOIN banks b_dst ON p.destination_bank_id = b_dst.id
			LEFT JOIN payment_route_decisions prd ON p.id = prd.payment_id
			WHERE p.created_at >= $1 AND p.created_at < $2
			  AND (
				($3 = 'bank' AND (b_src.code = $4 OR b_dst.code = $4))
				OR ($3 = 'execution_target' AND prd.execution_target_id = $4)
			  )
		),
		effective_states AS (
			SELECT
				wp.id,
				wp.failure_reason,
				wp.routing_reason,
				COALESCE(
					(
						SELECT pst.to_state
						FROM payment_state_transitions pst
						WHERE pst.payment_id = wp.id AND pst.transitioned_at <= $2
						ORDER BY pst.sequence_number DESC, pst.transitioned_at DESC
						LIMIT 1
					),
					'CREATED'
				) AS state_at_window_end
			FROM window_payments wp
		)
		SELECT
			COUNT(*) AS total_payments,
			COUNT(CASE WHEN state_at_window_end IN ('FAILED', 'REVERSED') THEN 1 END) AS failed_payments,
			COUNT(CASE WHEN failure_reason ILIKE '%TIMEOUT%' OR routing_reason ILIKE '%TIMEOUT%' THEN 1 END) AS timeout_payments,
			COUNT(CASE WHEN state_at_window_end = 'COMPLETED' THEN 1 END) AS completed_payments,
			COUNT(CASE WHEN state_at_window_end NOT IN ('COMPLETED', 'FAILED', 'REVERSED') THEN 1 END) AS pending_payments
		FROM effective_states;
	`

	var totalPayments, failedPayments, timeoutPayments, completedPayments, pendingPayments int64
	err := r.pool.QueryRow(ctx, paymentStatsQuery, from, to, entityType, entityID).Scan(
		&totalPayments,
		&failedPayments,
		&timeoutPayments,
		&completedPayments,
		&pendingPayments,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("aggregate payment stats: %w", err)
	}

	// 2. Recovery attempts in [from, to).
	recoveryAttemptsQuery := `
		SELECT
			COUNT(*) AS attempt_count,
			MAX(EXTRACT(EPOCH FROM (ra.finished_at - ra.started_at))*1000) AS max_attempt_duration_ms
		FROM recovery_attempts ra
		JOIN payments p ON p.id = ra.payment_id
		LEFT JOIN banks b_src ON p.source_bank_id = b_src.id
		LEFT JOIN banks b_dst ON p.destination_bank_id = b_dst.id
		LEFT JOIN payment_route_decisions prd ON p.id = prd.payment_id
		WHERE ra.started_at >= $1 AND ra.started_at < $2
		  AND (
			($3 = 'bank' AND (b_src.code = $4 OR b_dst.code = $4))
			OR ($3 = 'execution_target' AND prd.execution_target_id = $4)
		  );
	`
	var attemptCount int64
	var maxAttemptDuration sql.NullFloat64
	err = r.pool.QueryRow(ctx, recoveryAttemptsQuery, from, to, entityType, entityID).Scan(
		&attemptCount,
		&maxAttemptDuration,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("aggregate recovery attempts: %w", err)
	}

	// 3. Oldest recovery age as of window_to (historical queue observation before window end).
	oldestRecoveryAgeQuery := `
		SELECT COALESCE(MAX(EXTRACT(EPOCH FROM ($1::timestamptz - rj.due_at))*1000), 0)
		FROM recovery_jobs rj
		JOIN payments p ON p.id = rj.payment_id
		LEFT JOIN banks b_src ON p.source_bank_id = b_src.id
		LEFT JOIN banks b_dst ON p.destination_bank_id = b_dst.id
		LEFT JOIN payment_route_decisions prd ON p.id = prd.payment_id
		WHERE rj.created_at <= $1
		  AND rj.due_at <= $1
		  AND (rj.status != 'resolved' OR rj.updated_at > $1)
		  AND (
			($2 = 'bank' AND (b_src.code = $3 OR b_dst.code = $3))
			OR ($2 = 'execution_target' AND prd.execution_target_id = $3)
		  );
	`
	var oldestRecoveryAgeMs float64
	err = r.pool.QueryRow(ctx, oldestRecoveryAgeQuery, to, entityType, entityID).Scan(&oldestRecoveryAgeMs)
	if err != nil {
		return nil, nil, fmt.Errorf("aggregate oldest recovery age: %w", err)
	}

	// 4. Health samples in [from, to).
	healthSamplesQuery := `
		SELECT
			COUNT(*) AS total_samples,
			COUNT(CASE WHEN available = true THEN 1 END) AS available_samples,
			PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY latency_ms) AS p95_latency_ms
		FROM health_samples
		WHERE target_id = $1
		  AND sampled_at >= $2 AND sampled_at < $3;
	`
	var totalHealthSamples, availableHealthSamples int64
	var p95Latency sql.NullFloat64
	err = r.pool.QueryRow(ctx, healthSamplesQuery, entityID, from, to).Scan(
		&totalHealthSamples,
		&availableHealthSamples,
		&p95Latency,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("aggregate health samples: %w", err)
	}

	// 5. Construct FeatureVector with strict missing-value and bound handling.
	features := FeatureVector{}
	sampleCount := int(totalPayments)

	if sampleCount > 0 {
		timeoutRate := float64(timeoutPayments) / float64(sampleCount)
		if timeoutRate > 1.0 {
			timeoutRate = 1.0
		}
		if timeoutRate < 0.0 {
			timeoutRate = 0.0
		}
		features.TimeoutRate = &timeoutRate

		failureRate := float64(failedPayments) / float64(sampleCount)
		if failureRate > 1.0 {
			failureRate = 1.0
		}
		if failureRate < 0.0 {
			failureRate = 0.0
		}
		features.FailureRate = &failureRate

		attemptsPerPayment := float64(attemptCount) / float64(sampleCount)
		if attemptsPerPayment < 0.0 {
			attemptsPerPayment = 0.0
		}
		features.RecoveryAttemptsPerPayment = &attemptsPerPayment
	}

	if oldestRecoveryAgeMs > 0 {
		features.OldestRecoveryAgeMs = &oldestRecoveryAgeMs
	} else {
		zero := 0.0
		features.OldestRecoveryAgeMs = &zero
	}

	if totalHealthSamples > 0 {
		availRate := float64(availableHealthSamples) / float64(totalHealthSamples)
		if availRate > 1.0 {
			availRate = 1.0
		}
		if availRate < 0.0 {
			availRate = 0.0
		}
		features.HealthAvailabilityRate = &availRate

		if p95Latency.Valid {
			p95 := p95Latency.Float64
			if p95 >= 0 {
				features.P95LatencyMs = &p95
			}
		}
	}

	// 6. Transparent statistical baseline check over prior completed healthy windows.
	var baselineAlert *Warning
	if sampleCount >= MinimumSampleCount {
		if features.FailureRate != nil && *features.FailureRate > 0.15 {
			baselineAlert = &Warning{
				Code:    "HIGH_FAILURE_RATE",
				Message: fmt.Sprintf("Observed failure rate %.2f exceeds baseline threshold of 0.15", *features.FailureRate),
			}
		} else if features.TimeoutRate != nil && *features.TimeoutRate > 0.10 {
			baselineAlert = &Warning{
				Code:    "HIGH_TIMEOUT_RATE",
				Message: fmt.Sprintf("Observed timeout rate %.2f exceeds baseline threshold of 0.10", *features.TimeoutRate),
			}
		}
	}

	// 7. Model status evaluation.
	modelInfo := ModelStatusInfo{
		Status: "unavailable",
	}
	if sampleCount < MinimumSampleCount {
		modelInfo.Status = "insufficient_data"
	}

	rawAggregates := map[string]any{
		"totalPayments":          totalPayments,
		"failedPayments":         failedPayments,
		"timeoutPayments":        timeoutPayments,
		"completedPayments":      completedPayments,
		"pendingPayments":        pendingPayments,
		"attemptCount":           attemptCount,
		"totalHealthSamples":     totalHealthSamples,
		"availableHealthSamples": availableHealthSamples,
	}

	win := &AnalyticsWindow{
		ID:                   uuid.NewString(),
		EntityType:           entityType,
		EntityID:             entityID,
		From:                 from,
		To:                   to,
		SampleCount:          sampleCount,
		FeatureSchemaVersion: FeatureSchemaVersionV1,
		Features:             features,
		BaselineAlert:        baselineAlert,
		Model:                modelInfo,
		Advisory:             true,
	}

	return win, rawAggregates, nil
}

// ComputeMetricsSummary aggregates durable metrics across all subsystem sources as of asOf.
func (r *PostgresRepository) ComputeMetricsSummary(ctx context.Context, asOf time.Time) (*MetricsSummary, error) {
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	} else {
		asOf = asOf.UTC()
	}

	windowTo := asOf.Truncate(WindowDuration)
	windowFrom := windowTo.Add(-WindowDuration)

	sources := []Source{}

	// 1. Payments metrics
	paymentsMetrics := PaymentMetrics{
		Attempted: "0",
		Completed: "0",
		Failed:    "0",
		Pending:   "0",
	}
	var attempted, completed, failed, pending int64
	err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) AS attempted,
			COUNT(CASE WHEN state = 'COMPLETED' THEN 1 END) AS completed,
			COUNT(CASE WHEN state IN ('FAILED', 'REVERSED') THEN 1 END) AS failed,
			COUNT(CASE WHEN state NOT IN ('COMPLETED', 'FAILED', 'REVERSED') THEN 1 END) AS pending
		FROM payments
		WHERE created_at >= $1 AND created_at < $2;
	`, windowFrom, windowTo).Scan(&attempted, &completed, &failed, &pending)

	if err != nil {
		errCode := "PAYMENTS_QUERY_FAILED"
		sources = append(sources, Source{
			Name:       "payments",
			Status:     "unavailable",
			ObservedAt: &asOf,
			ErrorCode:  &errCode,
		})
	} else {
		paymentsMetrics.Attempted = strconv.FormatInt(attempted, 10)
		paymentsMetrics.Completed = strconv.FormatInt(completed, 10)
		paymentsMetrics.Failed = strconv.FormatInt(failed, 10)
		paymentsMetrics.Pending = strconv.FormatInt(pending, 10)
		sources = append(sources, Source{
			Name:       "payments",
			Status:     "available",
			ObservedAt: &asOf,
			ErrorCode:  nil,
		})
	}

	// 2. Recovery metrics
	recoveryMetrics := RecoveryMetrics{
		Due:            "0",
		Running:        "0",
		NeedsAttention: "0",
		OldestDueAgeMs: nil,
	}
	var dueCount, runningCount, attentionCount int64
	var oldestDueMs sql.NullFloat64
	err = r.pool.QueryRow(ctx, `
		SELECT
			COUNT(CASE WHEN status = 'scheduled' AND due_at <= $1 THEN 1 END) AS due_count,
			COUNT(CASE WHEN status = 'running' THEN 1 END) AS running_count,
			COUNT(CASE WHEN status = 'needs_attention' THEN 1 END) AS attention_count,
			MAX(CASE WHEN status = 'scheduled' AND due_at <= $1 THEN EXTRACT(EPOCH FROM ($1 - due_at))*1000 END) AS oldest_due_ms
		FROM recovery_jobs;
	`, asOf).Scan(&dueCount, &runningCount, &attentionCount, &oldestDueMs)

	if err != nil {
		errCode := "RECOVERY_QUERY_FAILED"
		sources = append(sources, Source{
			Name:       "recovery",
			Status:     "unavailable",
			ObservedAt: &asOf,
			ErrorCode:  &errCode,
		})
	} else {
		recoveryMetrics.Due = strconv.FormatInt(dueCount, 10)
		recoveryMetrics.Running = strconv.FormatInt(runningCount, 10)
		recoveryMetrics.NeedsAttention = strconv.FormatInt(attentionCount, 10)
		if oldestDueMs.Valid && oldestDueMs.Float64 >= 0 {
			ageInt := int64(oldestDueMs.Float64)
			recoveryMetrics.OldestDueAgeMs = &ageInt
		}
		sources = append(sources, Source{
			Name:       "recovery",
			Status:     "available",
			ObservedAt: &asOf,
			ErrorCode:  nil,
		})
	}

	// 3. Outbox metrics
	outboxMetrics := OutboxMetrics{
		Pending:            "0",
		Quarantined:        "0",
		OldestPendingAgeMs: nil,
	}
	var pendingDeliveries, quarantinedDeliveries int64
	var oldestPendingMs sql.NullFloat64
	err = r.pool.QueryRow(ctx, `
		SELECT
			COUNT(CASE WHEN status IN ('pending', 'processing') THEN 1 END) AS pending_count,
			COUNT(CASE WHEN status = 'quarantined' THEN 1 END) AS quarantined_count,
			MAX(CASE WHEN status IN ('pending', 'processing') THEN EXTRACT(EPOCH FROM ($1 - created_at))*1000 END) AS oldest_pending_ms
		FROM outbox_deliveries;
	`, asOf).Scan(&pendingDeliveries, &quarantinedDeliveries, &oldestPendingMs)

	if err != nil {
		errCode := "OUTBOX_QUERY_FAILED"
		sources = append(sources, Source{
			Name:       "outbox",
			Status:     "unavailable",
			ObservedAt: &asOf,
			ErrorCode:  &errCode,
		})
	} else {
		outboxMetrics.Pending = strconv.FormatInt(pendingDeliveries, 10)
		outboxMetrics.Quarantined = strconv.FormatInt(quarantinedDeliveries, 10)
		if oldestPendingMs.Valid && oldestPendingMs.Float64 >= 0 {
			ageInt := int64(oldestPendingMs.Float64)
			outboxMetrics.OldestPendingAgeMs = &ageInt
		}
		sources = append(sources, Source{
			Name:       "outbox",
			Status:     "available",
			ObservedAt: &asOf,
			ErrorCode:  nil,
		})
	}

	// 4. Runtime metrics
	runtimeMetrics := RuntimeMetrics{
		WorkerLastHeartbeatAt: nil,
		PoolWaitMs:            nil,
	}
	var lastSeen sql.NullTime
	err = r.pool.QueryRow(ctx, `
		SELECT MAX(last_seen_at) FROM worker_heartbeats;
	`, ).Scan(&lastSeen)

	if err != nil {
		errCode := "RUNTIME_QUERY_FAILED"
		sources = append(sources, Source{
			Name:       "runtime",
			Status:     "unavailable",
			ObservedAt: &asOf,
			ErrorCode:  &errCode,
		})
	} else {
		if lastSeen.Valid {
			t := lastSeen.Time
			runtimeMetrics.WorkerLastHeartbeatAt = &t
		}
		sources = append(sources, Source{
			Name:       "runtime",
			Status:     "available",
			ObservedAt: &asOf,
			ErrorCode:  nil,
		})
	}

	return &MetricsSummary{
		WindowFrom: windowFrom,
		WindowTo:   windowTo,
		AsOf:       asOf,
		Payments:   paymentsMetrics,
		Recovery:   recoveryMetrics,
		Outbox:     outboxMetrics,
		Runtime:    runtimeMetrics,
		Sources:    sources,
	}, nil
}
