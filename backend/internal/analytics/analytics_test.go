package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AllowedDisposableTestDBPrefixes defines database name prefixes considered safe and isolated for automated testing.
var AllowedDisposableTestDBPrefixes = []string{
	"transactx_disposable_",
	"transactx_cb_restore_",
	"transactx_test_",
	"transactx_m2_disposable_",
}

func validateTestDatabaseName(dbName string) error {
	trimmed := strings.TrimSpace(dbName)
	if trimmed == "" {
		return fmt.Errorf("database name cannot be empty")
	}
	lower := strings.ToLower(trimmed)
	if lower == "transactx" {
		return fmt.Errorf("refusing test execution against source database %q: integration tests require an isolated disposable database", dbName)
	}
	for _, sysDB := range []string{"postgres", "template0", "template1"} {
		if lower == sysDB {
			return fmt.Errorf("refusing test execution against system database %q", dbName)
		}
	}
	for _, pfx := range AllowedDisposableTestDBPrefixes {
		if strings.HasPrefix(lower, pfx) {
			return nil
		}
	}
	return fmt.Errorf("database %q is not explicitly identified as disposable (must start with one of: %s)",
		dbName, strings.Join(AllowedDisposableTestDBPrefixes, ", "))
}

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping live PostgreSQL analytics tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("configured PostgreSQL connection failed: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("configured PostgreSQL ping failed: %v", err)
	}

	var currentDB string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&currentDB); err != nil {
		pool.Close()
		t.Fatalf("failed to query current_database(): %v", err)
	}
	if err := validateTestDatabaseName(currentDB); err != nil {
		pool.Close()
		t.Fatalf("unsafe test database connection rejected: %v", err)
	}

	return pool
}

func TestSchemaCompliance_AgainstV2Schema(t *testing.T) {
	schemaPaths := []string{
		"../../../docs/next-gen/execution-pack/docs/next-gen/contracts/v2.schema.json",
		"docs/next-gen/execution-pack/docs/next-gen/contracts/v2.schema.json",
	}

	var schemaData []byte
	var err error
	for _, p := range schemaPaths {
		schemaData, err = os.ReadFile(p)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("required v2.schema.json is not accessible: %v", err)
	}

	var schemaRoot struct {
		Defs map[string]struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		} `json:"$defs"`
	}

	if err := json.Unmarshal(schemaData, &schemaRoot); err != nil {
		t.Fatalf("failed to unmarshal v2.schema.json: %v", err)
	}

	typeMapping := map[string]any{
		"AnalyticsWindow": AnalyticsWindow{},
		"FeatureVector":   FeatureVector{},
		"AnalyticsPage":   AnalyticsPage{},
		"MetricsSummary":  MetricsSummary{},
	}

	for defName, dtoInstance := range typeMapping {
		def, ok := schemaRoot.Defs[defName]
		if !ok {
			t.Errorf("definition %s missing from schema $defs", defName)
			continue
		}

		val := reflect.TypeOf(dtoInstance)
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}

		jsonFields := make(map[string]bool)
		for i := 0; i < val.NumField(); i++ {
			tag := val.Field(i).Tag.Get("json")
			if tag != "" && tag != "-" {
				fieldName := strings.Split(tag, ",")[0]
				jsonFields[fieldName] = true
			}
		}

		for _, reqField := range def.Required {
			if !jsonFields[reqField] {
				t.Errorf("[%s] DTO struct is missing required json field: %s", defName, reqField)
			}
		}
	}
}

func TestAnalytics_WindowCutoff_LateTransitionsNotLeaked(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	repo := NewPostgresRepository(pool)
	now := time.Now().UTC().Truncate(time.Minute)
	from := now.Add(-10 * time.Minute)
	to := now.Add(-5 * time.Minute)

	// Ensure isolated test banks exist
	var bankAID, bankBID uuid.UUID
	bankACode := "BANK_CUTOFF_" + uuid.NewString()[:8]
	bankBCode := "BANK_CUTOFF_" + uuid.NewString()[:8]
	bankAID = uuid.New()
	_, _ = pool.Exec(ctx, "INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Bank Cutoff A', 'ACTIVE')", bankAID, bankACode)
	bankBID = uuid.New()
	_, _ = pool.Exec(ctx, "INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Bank Cutoff B', 'ACTIVE')", bankBID, bankBCode)

	// Create test user and accounts for payments foreign keys
	senderID := uuid.New()
	recipientID := uuid.New()
	senderPhone := "+9199" + uuid.NewString()[:8]
	recipPhone := "+9199" + uuid.NewString()[:8]
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, phone, name, upi_id, password_hash, role)
		VALUES ($1, $2, 'Analytics Sender', $3, 'hash', 'CUSTOMER')
		ON CONFLICT DO NOTHING;
	`, senderID, senderPhone, senderPhone+"@upi")
	if err != nil {
		t.Fatalf("insert sender user: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, phone, name, upi_id, password_hash, role)
		VALUES ($1, $2, 'Analytics Recipient', $3, 'hash', 'CUSTOMER')
		ON CONFLICT DO NOTHING;
	`, recipientID, recipPhone, recipPhone+"@upi")
	if err != nil {
		t.Fatalf("insert recipient user: %v", err)
	}

	senderAccID := uuid.New()
	recipAccID := uuid.New()
	_, _ = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status)
		VALUES ($1, $2, $3, $4, 5000000, 'ACTIVE')
		ON CONFLICT DO NOTHING;
	`, senderAccID, senderID, bankAID, "ACC_"+uuid.NewString()[:8])
	_, _ = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status)
		VALUES ($1, $2, $3, $4, 5000000, 'ACTIVE')
		ON CONFLICT DO NOTHING;
	`, recipAccID, recipientID, bankBID, "ACC_"+uuid.NewString()[:8])

	// Insert payment created inside window [from, to)
	payID := uuid.New()
	payCreatedAt := from.Add(1 * time.Minute)
	_, err = pool.Exec(ctx, `
		INSERT INTO payments (
			id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise,
			currency, state, source_bank_id, destination_bank_id,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, 10000, 'INR', 'COMPLETED', $5, $6, $7, $8);
	`, payID, senderID, senderAccID, recipAccID, bankAID, bankBID, payCreatedAt, to.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("insert test payment: %v", err)
	}

	// Insert initial transition inside window: CREATED -> PROCESSING at from + 2m
	_, err = pool.Exec(ctx, `
		INSERT INTO payment_state_transitions (id, payment_id, from_state, to_state, transitioned_at)
		VALUES ($1, $2, 'CREATED', 'PROCESSING', $3);
	`, uuid.New(), payID, from.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("insert state transition 1: %v", err)
	}

	// Insert late terminal transition AFTER window_to: PROCESSING -> COMPLETED at to + 1m
	_, err = pool.Exec(ctx, `
		INSERT INTO payment_state_transitions (id, payment_id, from_state, to_state, transitioned_at)
		VALUES ($1, $2, 'PROCESSING', 'COMPLETED', $3);
	`, uuid.New(), payID, to.Add(1*time.Minute))
	if err != nil {
		t.Fatalf("insert state transition 2: %v", err)
	}

	// Aggregate window [from, to)
	win, raw, err := repo.AggregateWindow(ctx, "bank", bankACode, from, to)
	if err != nil {
		t.Fatalf("AggregateWindow failed: %v", err)
	}

	if win.SampleCount != 1 {
		t.Fatalf("expected sampleCount == 1, got %d", win.SampleCount)
	}

	// Verify that the late terminal transition (COMPLETED at to + 1m) was NOT counted as completed in [from, to)
	completedInRaw := raw["completedPayments"].(int64)
	if completedInRaw != 0 {
		t.Errorf("late completed transition leaked into earlier window! expected completedPayments = 0, got %d", completedInRaw)
	}
	pendingInRaw := raw["pendingPayments"].(int64)
	if pendingInRaw != 1 {
		t.Errorf("expected pendingPayments = 1 at window_to cutoff, got %d", pendingInRaw)
	}
}

func TestAnalytics_InsufficientSamples_YieldsInsufficientData(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	repo := NewPostgresRepository(pool)
	now := time.Now().UTC().Truncate(time.Minute)
	from := now.Add(-30 * time.Minute)
	to := now.Add(-25 * time.Minute)

	win, _, err := repo.AggregateWindow(ctx, "bank", "BANK_A", from, to)
	if err != nil {
		t.Fatalf("AggregateWindow failed: %v", err)
	}

	if win.SampleCount < MinimumSampleCount {
		if win.Model.Status != "insufficient_data" {
			t.Errorf("expected model status 'insufficient_data' when samples (%d) < %d, got %s",
				win.SampleCount, MinimumSampleCount, win.Model.Status)
		}
		if win.BaselineAlert != nil {
			t.Errorf("expected nil baseline alert on insufficient samples, got %v", win.BaselineAlert)
		}
	}
}

func TestAnalytics_DuplicateWindowRetry_Idempotent(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	repo := NewPostgresRepository(pool)
	from := time.Now().UTC().Add(-60 * time.Minute).Truncate(WindowDuration)
	to := from.Add(WindowDuration)

	win, raw, err := repo.AggregateWindow(ctx, "bank", "BANK_A", from, to)
	if err != nil {
		t.Fatalf("AggregateWindow failed: %v", err)
	}

	// First upsert
	err = repo.UpsertWindow(ctx, win, raw)
	if err != nil {
		t.Fatalf("first UpsertWindow failed: %v", err)
	}
	firstID := win.ID

	// Second upsert for same window should succeed without error and update the same record
	win.SampleCount = 42
	err = repo.UpsertWindow(ctx, win, raw)
	if err != nil {
		t.Fatalf("second UpsertWindow failed: %v", err)
	}

	// Verify only 1 record exists for (bank, BANK_A, from, tx.features.v1)
	var count int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM operational_aggregate_windows
		WHERE entity_type = 'bank' AND entity_id = 'BANK_A'
		  AND window_from = $1 AND feature_schema_version = $2;
	`, from, FeatureSchemaVersionV1).Scan(&count)
	if err != nil {
		t.Fatalf("count windows failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 window row after retry, found %d", count)
	}
	_ = firstID
}

func TestAnalytics_MetricsSummary_DurableSources(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	repo := NewPostgresRepository(pool)
	asOf := time.Now().UTC()

	summary, err := repo.ComputeMetricsSummary(ctx, asOf)
	if err != nil {
		t.Fatalf("ComputeMetricsSummary failed: %v", err)
	}

	if summary.WindowFrom.IsZero() || summary.WindowTo.IsZero() {
		t.Errorf("expected non-zero window boundaries, got from=%v to=%v", summary.WindowFrom, summary.WindowTo)
	}
	if summary.WindowTo.Sub(summary.WindowFrom) != WindowDuration {
		t.Errorf("expected 5-minute window duration, got %v", summary.WindowTo.Sub(summary.WindowFrom))
	}

	// Verify payment decimal strings
	if summary.Payments.Attempted == "" || summary.Payments.Completed == "" {
		t.Errorf("expected decimal string counters for payments, got %+v", summary.Payments)
	}

	// Verify recovery decimal strings
	if summary.Recovery.Due == "" || summary.Recovery.Running == "" {
		t.Errorf("expected decimal string counters for recovery, got %+v", summary.Recovery)
	}

	// Verify outbox decimal strings
	if summary.Outbox.Pending == "" || summary.Outbox.Quarantined == "" {
		t.Errorf("expected decimal string counters for outbox, got %+v", summary.Outbox)
	}

	// Verify sources
	if len(summary.Sources) == 0 {
		t.Errorf("expected at least 1 source report")
	}
	for _, s := range summary.Sources {
		if s.Status != "available" && s.Status != "unavailable" {
			t.Errorf("source %s has invalid status: %s", s.Name, s.Status)
		}
	}
}

func TestAnalytics_CursorPagination(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	repo := NewPostgresRepository(pool)
	baseTime := time.Now().UTC().Add(-5 * time.Hour).Truncate(WindowDuration)
	testBankID := "BANK_PAGINATION_" + uuid.NewString()[:8]

	// Seed 3 sequential windows
	for i := 0; i < 3; i++ {
		wFrom := baseTime.Add(time.Duration(i) * WindowDuration)
		wTo := wFrom.Add(WindowDuration)
		win := &AnalyticsWindow{
			ID:                   uuid.NewString(),
			EntityType:           "bank",
			EntityID:             testBankID,
			From:                 wFrom,
			To:                   wTo,
			SampleCount:          i + 1,
			FeatureSchemaVersion: FeatureSchemaVersionV1,
			Features:             FeatureVector{},
			Model: ModelStatusInfo{
				Status: "insufficient_data",
			},
			Advisory: true,
		}
		if err := repo.UpsertWindow(ctx, win, nil); err != nil {
			t.Fatalf("upsert pagination test window %d: %v", i, err)
		}
	}

	// Query page 1 with limit 2
	page1, err := repo.QueryWindows(ctx, WindowFilter{
		EntityType: "bank",
		EntityID:   testBankID,
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("query page 1 failed: %v", err)
	}
	if len(page1.Items) != 2 {
		t.Fatalf("expected 2 items in page 1, got %d", len(page1.Items))
	}
	if page1.NextCursor == nil || *page1.NextCursor == "" {
		t.Fatalf("expected non-empty nextCursor on page 1")
	}

	// Query page 2 using cursor
	page2, err := repo.QueryWindows(ctx, WindowFilter{
		EntityType: "bank",
		EntityID:   testBankID,
		Limit:      2,
		Cursor:     *page1.NextCursor,
	})
	if err != nil {
		t.Fatalf("query page 2 failed: %v", err)
	}
	if len(page2.Items) != 1 {
		t.Fatalf("expected 1 item in page 2, got %d", len(page2.Items))
	}
	if page2.NextCursor != nil {
		t.Errorf("expected nil nextCursor on last page, got %v", *page2.NextCursor)
	}

	// Verify ordering: page1 items should have later timestamps than page2 items
	if page1.Items[0].From.Before(page2.Items[0].From) {
		t.Errorf("expected descending sort order by window_from")
	}
}

func TestAnalytics_WarmupWithoutBaseline_DoesNotEmitFixedAlerts(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	repo := NewPostgresRepository(pool)
	now := time.Now().UTC().Truncate(time.Minute)
	from := now.Add(-60 * time.Minute).Truncate(WindowDuration)
	to := from.Add(WindowDuration)

	bankCode := "BANK_WARMUP_" + uuid.NewString()[:8]
	bankID := uuid.New()
	_, err := pool.Exec(ctx, "INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Warmup Bank', 'ACTIVE')", bankID, bankCode)
	if err != nil {
		t.Fatalf("insert bank: %v", err)
	}

	userID := uuid.New()
	userPhone := "+9199" + uuid.NewString()[:8]
	_, err = pool.Exec(ctx, "INSERT INTO users (id, phone, name, upi_id, password_hash, role) VALUES ($1, $2, 'Warmup User', $3, 'hash', 'CUSTOMER')", userID, userPhone, userPhone+"@upi")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	accID := uuid.New()
	_, err = pool.Exec(ctx, "INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status) VALUES ($1, $2, $3, $4, 5000000, 'ACTIVE')", accID, userID, bankID, "ACC_"+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	// Insert 25 payments: 15 COMPLETED, 10 FAILED (failureRate = 10/25 = 0.40)
	for i := 0; i < 25; i++ {
		payID := uuid.New()
		state := "COMPLETED"
		if i < 10 {
			state = "FAILED"
		}
		pTime := from.Add(time.Duration(i*10) * time.Second)
		_, err := pool.Exec(ctx, `
			INSERT INTO payments (
				id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise,
				currency, state, source_bank_id, destination_bank_id,
				created_at, updated_at
			) VALUES ($1, $2, $3, $3, 1000, 'INR', $4, $5, $5, $6, $6);
		`, payID, userID, accID, state, bankID, pTime)
		if err != nil {
			t.Fatalf("insert alert test payment %d: %v", i, err)
		}
		_, _ = pool.Exec(ctx, `
			INSERT INTO payment_state_transitions (id, payment_id, from_state, to_state, transitioned_at)
			VALUES ($1, $2, 'CREATED', $3, $4);
		`, uuid.New(), payID, state, pTime)
	}

	win, raw, err := repo.AggregateWindow(ctx, "bank", bankCode, from, to)
	if err != nil {
		t.Fatalf("AggregateWindow failed: %v", err)
	}

	if win.SampleCount != 25 {
		t.Fatalf("expected sampleCount == 25, got %d", win.SampleCount)
	}
	if win.Features.FailureRate == nil {
		t.Fatalf("expected non-nil failureRate")
	}
	if *win.Features.FailureRate < 0.39 || *win.Features.FailureRate > 0.41 {
		t.Errorf("expected failureRate ~0.40, got %.2f", *win.Features.FailureRate)
	}

	// Proves Gate 6 requirement: during warmup without calibrated baseline, NO fixed fallback alert is emitted!
	if win.BaselineAlert != nil {
		t.Fatalf("expected nil baselineAlert during warmup, got %v", win.BaselineAlert)
	}
	if raw["scoring_outcome"] != "warming_up" {
		t.Errorf("expected scoring_outcome 'warming_up', got %v", raw["scoring_outcome"])
	}
	if raw["baseline_status"] != "warming_up" {
		t.Errorf("expected baseline_status 'warming_up', got %v", raw["baseline_status"])
	}

	// Persist window and inspect PostgreSQL row
	if err := repo.UpsertWindow(ctx, win, raw); err != nil {
		t.Fatalf("UpsertWindow failed: %v", err)
	}

	var persistedScoringOutcome, persistedBaselineStatus string
	var persistedBaselineAlert []byte
	err = pool.QueryRow(ctx, `
		SELECT raw_aggregates->>'scoring_outcome', raw_aggregates->>'baseline_status', baseline_alert
		FROM operational_aggregate_windows
		WHERE id = $1::uuid
	`, win.ID).Scan(&persistedScoringOutcome, &persistedBaselineStatus, &persistedBaselineAlert)
	if err != nil {
		t.Fatalf("query persisted window row: %v", err)
	}
	if persistedScoringOutcome != "warming_up" || persistedBaselineStatus != "warming_up" {
		t.Errorf("persisted outcome mismatch: scoring=%s, baseline=%s", persistedScoringOutcome, persistedBaselineStatus)
	}
	if len(persistedBaselineAlert) > 0 {
		t.Errorf("expected null baseline_alert in DB, got: %s", string(persistedBaselineAlert))
	}
}

func TestAnalytics_IneligiblePriorWindows_ExcludedFromCalibration(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	repo := NewPostgresRepository(pool)
	testBankCode := "BANK_INELIG_" + uuid.NewString()[:8]
	baseTime := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	scoreFrom := baseTime.Add(10 * WindowDuration)
	scoreTo := scoreFrom.Add(WindowDuration)

	lowFail := 0.01
	lowTimeout := 0.005
	lat := 100.0

	// Seed 8 ineligible windows with various reasons:
	// 1. Unreviewed (reviewed_healthy omitted / false)
	// 2. Anomalous (anomalous: true)
	// 3. Ineligible (ineligible: true)
	// 4. Has baseline_alert
	// 5. Sample count < MinimumSampleCount (10 < 20)
	// 6. Overlapping with scoreFrom (wTo > scoreFrom)
	// 7. Future (wFrom >= scoreFrom)
	// 8. Explicitly marked unreviewed: true
	ineligibleConfigs := []struct {
		offset       int
		sampleCount  int
		alert        *Warning
		rawOverrides map[string]any
		duration     time.Duration
	}{
		{0, 40, nil, map[string]any{"reviewed_healthy": false}, WindowDuration},
		{1, 40, nil, map[string]any{"reviewed_healthy": true, "anomalous": true}, WindowDuration},
		{2, 40, nil, map[string]any{"reviewed_healthy": true, "ineligible": true}, WindowDuration},
		{3, 40, &Warning{Code: "BASELINE_DEVIATION", Message: "dev"}, map[string]any{"reviewed_healthy": true}, WindowDuration},
		{4, 10, nil, map[string]any{"reviewed_healthy": true}, WindowDuration},                 // < 20 samples
		{5, 40, nil, map[string]any{"reviewed_healthy": true}, WindowDuration + 2*time.Minute}, // malformed duration
		{10, 40, nil, map[string]any{"reviewed_healthy": true}, WindowDuration},                // future / same as scoreFrom
		{6, 40, nil, map[string]any{"reviewed_healthy": true, "unreviewed": true}, WindowDuration},
	}

	for i, cfg := range ineligibleConfigs {
		wFrom := baseTime.Add(time.Duration(cfg.offset) * WindowDuration)
		wTo := wFrom.Add(cfg.duration)
		win := &AnalyticsWindow{
			ID:                   uuid.NewString(),
			EntityType:           "bank",
			EntityID:             testBankCode,
			From:                 wFrom,
			To:                   wTo,
			SampleCount:          cfg.sampleCount,
			FeatureSchemaVersion: FeatureSchemaVersionV1,
			Features: FeatureVector{
				FailureRate:  &lowFail,
				TimeoutRate:  &lowTimeout,
				P95LatencyMs: &lat,
			},
			BaselineAlert: cfg.alert,
			Model:         ModelStatusInfo{Status: "unavailable"},
			Advisory:      true,
		}
		if err := repo.UpsertWindow(ctx, win, cfg.rawOverrides); err != nil {
			t.Fatalf("seed ineligible window %d failed: %v", i, err)
		}
	}

	// Now aggregate window at scoreFrom with 25 payments
	bankID := uuid.New()
	_, _ = pool.Exec(ctx, "INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Inelig Bank', 'ACTIVE')", bankID, testBankCode)
	userID := uuid.New()
	userPhone := "+9199" + uuid.NewString()[:8]
	_, _ = pool.Exec(ctx, "INSERT INTO users (id, phone, name, upi_id, password_hash, role) VALUES ($1, $2, 'User', $3, 'h', 'CUSTOMER')", userID, userPhone, userPhone+"@upi")
	accID := uuid.New()
	_, _ = pool.Exec(ctx, "INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status) VALUES ($1, $2, $3, $4, 5000000, 'ACTIVE')", accID, userID, bankID, "ACC_"+uuid.NewString()[:8])

	for p := 0; p < 25; p++ {
		payID := uuid.New()
		pTime := scoreFrom.Add(time.Duration(p*5) * time.Second)
		_, _ = pool.Exec(ctx, `
			INSERT INTO payments (
				id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise,
				currency, state, source_bank_id, destination_bank_id, created_at, updated_at
			) VALUES ($1, $2, $3, $3, 1000, 'INR', 'COMPLETED', $4, $4, $5, $5);
		`, payID, userID, accID, bankID, pTime)
		_, _ = pool.Exec(ctx, `
			INSERT INTO payment_state_transitions (id, payment_id, from_state, to_state, transitioned_at)
			VALUES ($1, $2, 'CREATED', 'COMPLETED', $3);
		`, uuid.New(), payID, pTime)
	}

	win, raw, err := repo.AggregateWindow(ctx, "bank", testBankCode, scoreFrom, scoreTo)
	if err != nil {
		t.Fatalf("AggregateWindow failed: %v", err)
	}

	// Verify that 0 ineligible windows entered calibration
	calibSamples := raw["calibration_samples"].(int)
	if calibSamples != 0 {
		t.Errorf("expected 0 calibration samples (all ineligible), got %d", calibSamples)
	}
	if raw["baseline_status"] != "warming_up" {
		t.Errorf("expected baseline_status 'warming_up', got %v", raw["baseline_status"])
	}
	if win.BaselineAlert != nil {
		t.Errorf("expected nil baseline alert, got %v", win.BaselineAlert)
	}
}

func TestAnalytics_Service_AggregateCompletedWindows(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	repo := NewPostgresRepository(pool)
	now := time.Now().UTC().Truncate(WindowDuration)
	cursorStart := now.Add(-10 * time.Minute)

	testBankCode := "BANK_SVC_" + uuid.NewString()[:8]
	svc := NewService(repo, []EntityTarget{
		{Type: "bank", ID: testBankCode},
	}, nil)

	// Set cursor 10 minutes ago (meaning 2 completed windows are pending)
	err := repo.SetCursor(ctx, CursorNameFiveMinuteAggregator, cursorStart)
	if err != nil {
		t.Fatalf("set initial cursor: %v", err)
	}

	processed, err := svc.AggregateCompletedWindows(ctx)
	if err != nil {
		t.Fatalf("AggregateCompletedWindows failed: %v", err)
	}

	if processed < 2 {
		t.Errorf("expected at least 2 processed windows, got %d", processed)
	}

	// Verify cursor advanced to now
	newCursor, err := repo.GetCursor(ctx, CursorNameFiveMinuteAggregator)
	if err != nil {
		t.Fatalf("get cursor: %v", err)
	}
	if !newCursor.Equal(now) {
		t.Errorf("expected cursor to advance to %v, got %v", now, newCursor)
	}

	// Verify metrics
	age, duration, total := svc.AggregationMetrics()
	if total < 2 {
		t.Errorf("expected total >= 2, got %d", total)
	}
	if duration <= 0 {
		t.Errorf("expected positive duration, got %v", duration)
	}
	_ = age
}

func TestAnalytics_AggregateWindow_RejectsMalformedAndFutureInputs(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	repo := NewPostgresRepository(pool)
	now := time.Now().UTC().Truncate(WindowDuration)
	validFrom := now.Add(-10 * time.Minute)
	validTo := now.Add(-5 * time.Minute)

	// 1. Invalid entity type
	_, _, err := repo.AggregateWindow(ctx, "unsupported_entity", "BANK_A", validFrom, validTo)
	if err == nil || !strings.Contains(err.Error(), "invalid entity type") {
		t.Errorf("expected error for invalid entity type, got: %v", err)
	}

	// 2. Empty entity ID
	_, _, err = repo.AggregateWindow(ctx, "bank", "", validFrom, validTo)
	if err == nil || !strings.Contains(err.Error(), "entity ID cannot be empty") {
		t.Errorf("expected error for empty entity ID, got: %v", err)
	}

	// 3. Zero time boundaries
	_, _, err = repo.AggregateWindow(ctx, "bank", "BANK_A", time.Time{}, validTo)
	if err == nil || !strings.Contains(err.Error(), "window boundaries cannot be zero") {
		t.Errorf("expected error for zero from, got: %v", err)
	}

	// 4. Inverted time boundaries
	_, _, err = repo.AggregateWindow(ctx, "bank", "BANK_A", validTo, validFrom)
	if err == nil || !strings.Contains(err.Error(), "must be before to") {
		t.Errorf("expected error for inverted boundaries, got: %v", err)
	}

	// 5. Invalid duration (not 5 minutes)
	_, _, err = repo.AggregateWindow(ctx, "bank", "BANK_A", validFrom, validFrom.Add(3*time.Minute))
	if err == nil || !strings.Contains(err.Error(), "invalid window duration") {
		t.Errorf("expected error for invalid duration, got: %v", err)
	}

	// 6. Future window
	futureFrom := now.Add(1 * time.Hour)
	_, _, err = repo.AggregateWindow(ctx, "bank", "BANK_A", futureFrom, futureFrom.Add(WindowDuration))
	if err == nil || !strings.Contains(err.Error(), "future window rejected") {
		t.Errorf("expected error for future window, got: %v", err)
	}
}

func TestAnalytics_FitLiveBaseline_PriorHealthyCalibrationAndAlerts(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	repo := NewPostgresRepository(pool)
	testBankCode := "BANK_CALIB_" + uuid.NewString()[:8]
	baseTime := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)

	// Seed 8 prior completed healthy windows (sample_count >= 20, baseline_alert = NULL)
	for i := 0; i < 8; i++ {
		wFrom := baseTime.Add(time.Duration(i) * WindowDuration)
		wTo := wFrom.Add(WindowDuration)
		lowFail := 0.01
		lowTimeout := 0.005
		lat := 100.0
		recovAge := 500.0
		recovAtt := 0.1
		avail := 0.99

		healthyWin := &AnalyticsWindow{
			ID:                   uuid.NewString(),
			EntityType:           "bank",
			EntityID:             testBankCode,
			From:                 wFrom,
			To:                   wTo,
			SampleCount:          40,
			FeatureSchemaVersion: FeatureSchemaVersionV1,
			Features: FeatureVector{
				FailureRate:                &lowFail,
				TimeoutRate:                &lowTimeout,
				P95LatencyMs:               &lat,
				OldestRecoveryAgeMs:        &recovAge,
				RecoveryAttemptsPerPayment: &recovAtt,
				HealthAvailabilityRate:     &avail,
			},
			BaselineAlert: nil,
			Model: ModelStatusInfo{
				Status: "unavailable",
			},
			Advisory: true,
		}
		raw := map[string]any{"seeded": true, "reviewed_healthy": true}
		if err := repo.UpsertWindow(ctx, healthyWin, raw); err != nil {
			t.Fatalf("seed healthy window %d failed: %v", i, err)
		}
	}

	// Now aggregate window 8 (from = baseTime + 8*WindowDuration)
	scoreFrom := baseTime.Add(8 * WindowDuration)
	scoreTo := scoreFrom.Add(WindowDuration)

	// Insert payments in window 8 with 30 payments, 10 failures to trigger high failure rate deviation
	bankID := uuid.New()
	_, _ = pool.Exec(ctx, "INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Bank Calib', 'ACTIVE')", bankID, testBankCode)

	senderID := uuid.New()
	senderPhone := "+9199" + uuid.NewString()[:8]
	_, _ = pool.Exec(ctx, `
		INSERT INTO users (id, phone, name, upi_id, password_hash, role)
		VALUES ($1, $2, 'Calib Sender', $3, 'hash', 'CUSTOMER') ON CONFLICT DO NOTHING;
	`, senderID, senderPhone, senderPhone+"@upi")

	accID := uuid.New()
	_, _ = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status)
		VALUES ($1, $2, $3, $4, 5000000, 'ACTIVE') ON CONFLICT DO NOTHING;
	`, accID, senderID, bankID, "ACC_"+uuid.NewString()[:8])

	for p := 0; p < 30; p++ {
		payID := uuid.New()
		payState := "COMPLETED"
		var failReason *string
		if p < 10 {
			payState = "FAILED"
			reason := "NETWORK_ERROR"
			failReason = &reason
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO payments (
				id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise,
				currency, state, source_bank_id, destination_bank_id, failure_reason,
				created_at, updated_at
			) VALUES ($1, $2, $3, $3, 10000, 'INR', $4, $5, $5, $6, $7, $7);
		`, payID, senderID, accID, payState, bankID, failReason, scoreFrom.Add(time.Duration(p)*5*time.Second))
		if err != nil {
			t.Fatalf("insert payment %d: %v", p, err)
		}

		_, err = pool.Exec(ctx, `
			INSERT INTO payment_state_transitions (id, payment_id, from_state, to_state, transitioned_at)
			VALUES ($1, $2, 'CREATED', $3, $4);
		`, uuid.New(), payID, payState, scoreFrom.Add(time.Duration(p)*5*time.Second+time.Second))
		if err != nil {
			t.Fatalf("insert transition %d: %v", p, err)
		}
	}

	win, _, err := repo.AggregateWindow(ctx, "bank", testBankCode, scoreFrom, scoreTo)
	if err != nil {
		t.Fatalf("AggregateWindow failed: %v", err)
	}

	if win.SampleCount != 30 {
		t.Fatalf("expected sampleCount 30, got %d", win.SampleCount)
	}

	if win.BaselineAlert == nil {
		t.Fatalf("expected BaselineAlert from calibrated baseline, got nil")
	}

	if win.BaselineAlert.Code != "BASELINE_DEVIATION" {
		t.Errorf("expected BaselineAlert code 'BASELINE_DEVIATION', got %s", win.BaselineAlert.Code)
	}
	if !strings.Contains(win.BaselineAlert.Message, "failureRate_deviation") {
		t.Errorf("expected failureRate_deviation in alert message, got: %s", win.BaselineAlert.Message)
	}
}

func TestAnalytics_NullMeasurements_AndPersistedProvenance(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	repo := NewPostgresRepository(pool)
	testBankCode := "BANK_NULLMEAS_" + uuid.NewString()[:8]
	baseTime := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	scoreFrom := baseTime
	scoreTo := baseTime.Add(WindowDuration)

	bankID := uuid.New()
	_, _ = pool.Exec(ctx, "INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'NullMeas Bank', 'ACTIVE')", bankID, testBankCode)

	senderID := uuid.New()
	senderPhone := "+9199" + uuid.NewString()[:8]
	_, _ = pool.Exec(ctx, `
		INSERT INTO users (id, phone, name, upi_id, password_hash, role)
		VALUES ($1, $2, 'NullMeas User', $3, 'hash', 'CUSTOMER') ON CONFLICT DO NOTHING;
	`, senderID, senderPhone, senderPhone+"@upi")

	accID := uuid.New()
	_, _ = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status)
		VALUES ($1, $2, $3, $4, 5000000, 'ACTIVE') ON CONFLICT DO NOTHING;
	`, accID, senderID, bankID, "ACC_"+uuid.NewString()[:8])

	// Insert 22 completed payments with no recovery attempts and no health probes
	for p := 0; p < 22; p++ {
		payID := uuid.New()
		pTime := scoreFrom.Add(time.Duration(p*10) * time.Second)
		_, err := pool.Exec(ctx, `
			INSERT INTO payments (
				id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise,
				currency, state, source_bank_id, destination_bank_id,
				created_at, updated_at
			) VALUES ($1, $2, $3, $3, 10000, 'INR', 'COMPLETED', $4, $4, $5, $5);
		`, payID, senderID, accID, bankID, pTime)
		if err != nil {
			t.Fatalf("insert payment %d: %v", p, err)
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO payment_state_transitions (id, payment_id, from_state, to_state, transitioned_at)
			VALUES ($1, $2, 'CREATED', 'COMPLETED', $3);
		`, uuid.New(), payID, pTime)
		if err != nil {
			t.Fatalf("insert transition %d: %v", p, err)
		}
	}

	win, raw, err := repo.AggregateWindow(ctx, "bank", testBankCode, scoreFrom, scoreTo)
	if err != nil {
		t.Fatalf("AggregateWindow failed with null measurements: %v", err)
	}

	if win.SampleCount != 22 {
		t.Fatalf("expected sampleCount == 22, got %d", win.SampleCount)
	}

	// Verify null measurements (e.g. latency, health availability) are preserved as nil without failure
	if win.Features.P95LatencyMs != nil {
		t.Errorf("expected nil P95LatencyMs when no attempt latency exists, got %v", *win.Features.P95LatencyMs)
	}
	if win.Features.HealthAvailabilityRate != nil {
		t.Errorf("expected nil HealthAvailabilityRate when no health samples exist, got %v", *win.Features.HealthAvailabilityRate)
	}

	// Provenance and scoring outcome checks
	if raw["scoring_outcome"] != "warming_up" {
		t.Errorf("expected scoring_outcome 'warming_up', got %v", raw["scoring_outcome"])
	}
	if raw["reviewed_healthy"] != false {
		t.Errorf("expected live window to have reviewed_healthy = false, got %v", raw["reviewed_healthy"])
	}
	if raw["provenance"] != "unreviewed_live" {
		t.Errorf("expected provenance 'unreviewed_live', got %v", raw["provenance"])
	}

	// Persist window and inspect PostgreSQL row
	if err := repo.UpsertWindow(ctx, win, raw); err != nil {
		t.Fatalf("UpsertWindow failed: %v", err)
	}

	var dbScoringOutcome, dbModelStatus, dbProvenance string
	var dbReviewedHealthy bool
	err = pool.QueryRow(ctx, `
		SELECT
			raw_aggregates->>'scoring_outcome',
			model_status,
			raw_aggregates->>'provenance',
			COALESCE((raw_aggregates->>'reviewed_healthy')::boolean, false)
		FROM operational_aggregate_windows
		WHERE id = $1::uuid;
	`, win.ID).Scan(&dbScoringOutcome, &dbModelStatus, &dbProvenance, &dbReviewedHealthy)
	if err != nil {
		t.Fatalf("query persisted window row: %v", err)
	}

	if dbScoringOutcome != "warming_up" {
		t.Errorf("DB scoring outcome mismatch: %s", dbScoringOutcome)
	}
	if dbModelStatus != "unavailable" {
		t.Errorf("DB model status mismatch: %s", dbModelStatus)
	}
	if dbProvenance != "unreviewed_live" {
		t.Errorf("DB provenance mismatch: %s", dbProvenance)
	}
	if dbReviewedHealthy {
		t.Errorf("DB reviewed_healthy should be false for unreviewed live window")
	}
}

func TestAnalytics_PrecedenceVetoRules_NegativeReviewOverridesSeededAndHealthy(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	repo := NewPostgresRepository(pool)
	testBankCode := "BANK_VETO_" + uuid.NewString()[:8]
	baseTime := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	scoreFrom := baseTime.Add(10 * WindowDuration)
	scoreTo := scoreFrom.Add(WindowDuration)

	lowFail := 0.01
	lowTimeout := 0.005
	lat := 100.0

	// Seed windows with contradictory positive and negative flags:
	// In all cases, the negative flag / veto MUST take precedence.
	vetoConfigs := []struct {
		offset       int
		rawOverrides map[string]any
	}{
		{0, map[string]any{"reviewed_healthy": false, "seeded": true}},
		{1, map[string]any{"reviewed_healthy": false, "healthy": true}},
		{2, map[string]any{"anomalous": true, "seeded": true, "reviewed_healthy": true}},
		{3, map[string]any{"ineligible": true, "healthy": true, "reviewed_healthy": true}},
		{4, map[string]any{"unreviewed": true, "seeded": true, "reviewed_healthy": true}},
		{5, map[string]any{"provenance": "unreviewed_live", "seeded": true}},
		{6, map[string]any{"provenance": "anomalous", "healthy": true}},
		{7, map[string]any{"provenance": "ineligible", "reviewed_healthy": true}},
	}

	for i, cfg := range vetoConfigs {
		wFrom := baseTime.Add(time.Duration(cfg.offset) * WindowDuration)
		wTo := wFrom.Add(WindowDuration)
		win := &AnalyticsWindow{
			ID:                   uuid.NewString(),
			EntityType:           "bank",
			EntityID:             testBankCode,
			From:                 wFrom,
			To:                   wTo,
			SampleCount:          40,
			FeatureSchemaVersion: FeatureSchemaVersionV1,
			Features: FeatureVector{
				FailureRate:  &lowFail,
				TimeoutRate:  &lowTimeout,
				P95LatencyMs: &lat,
			},
			Model:    ModelStatusInfo{Status: "unavailable"},
			Advisory: true,
		}
		if err := repo.UpsertWindow(ctx, win, cfg.rawOverrides); err != nil {
			t.Fatalf("seed veto window %d failed: %v", i, err)
		}
	}

	// Setup bank, user, accounts, and payments for scoring window
	bankID := uuid.New()
	_, _ = pool.Exec(ctx, "INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Veto Bank', 'ACTIVE')", bankID, testBankCode)
	userID := uuid.New()
	userPhone := "+9199" + uuid.NewString()[:8]
	_, _ = pool.Exec(ctx, "INSERT INTO users (id, phone, name, upi_id, password_hash, role) VALUES ($1, $2, 'User', $3, 'h', 'CUSTOMER')", userID, userPhone, userPhone+"@upi")
	accID := uuid.New()
	_, _ = pool.Exec(ctx, "INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status) VALUES ($1, $2, $3, $4, 5000000, 'ACTIVE')", accID, userID, bankID, "ACC_"+uuid.NewString()[:8])

	for p := 0; p < 25; p++ {
		payID := uuid.New()
		pTime := scoreFrom.Add(time.Duration(p*5) * time.Second)
		_, _ = pool.Exec(ctx, `
			INSERT INTO payments (
				id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise,
				currency, state, source_bank_id, destination_bank_id, created_at, updated_at
			) VALUES ($1, $2, $3, $3, 1000, 'INR', 'COMPLETED', $4, $4, $5, $5);
		`, payID, userID, accID, bankID, pTime)
		_, _ = pool.Exec(ctx, `
			INSERT INTO payment_state_transitions (id, payment_id, from_state, to_state, transitioned_at)
			VALUES ($1, $2, 'CREATED', 'COMPLETED', $3);
		`, uuid.New(), payID, pTime)
	}

	win, raw, err := repo.AggregateWindow(ctx, "bank", testBankCode, scoreFrom, scoreTo)
	if err != nil {
		t.Fatalf("AggregateWindow failed: %v", err)
	}

	// Precedence rule verification: all 8 windows must be vetoed
	calibSamples := raw["calibration_samples"].(int)
	if calibSamples != 0 {
		t.Errorf("expected 0 calibration samples (all contradictory windows vetoed), got %d", calibSamples)
	}
	if raw["baseline_status"] != "warming_up" {
		t.Errorf("expected baseline_status 'warming_up', got %v", raw["baseline_status"])
	}
	if win.BaselineAlert != nil {
		t.Errorf("expected nil baseline alert, got %v", win.BaselineAlert)
	}

	// Persist and check PostgreSQL
	if err := repo.UpsertWindow(ctx, win, raw); err != nil {
		t.Fatalf("UpsertWindow failed: %v", err)
	}

	var dbCalibSamples int
	var dbStatus string
	err = pool.QueryRow(ctx, `
		SELECT (raw_aggregates->>'calibration_samples')::int, raw_aggregates->>'baseline_status'
		FROM operational_aggregate_windows WHERE id = $1::uuid;
	`, win.ID).Scan(&dbCalibSamples, &dbStatus)
	if err != nil {
		t.Fatalf("query DB for veto window: %v", err)
	}
	if dbCalibSamples != 0 {
		t.Errorf("expected DB calibration_samples = 0, got %d", dbCalibSamples)
	}
	if dbStatus != "warming_up" {
		t.Errorf("expected DB baseline_status = 'warming_up', got %s", dbStatus)
	}
}

func TestAnalytics_MalformedProvenance_FailsClosedToError(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	repo := NewPostgresRepository(pool)
	testBankCode := "BANK_MALFORMED_" + uuid.NewString()[:8]
	baseTime := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	scoreFrom := baseTime.Add(10 * WindowDuration)
	scoreTo := scoreFrom.Add(WindowDuration)

	lowFail := 0.01
	lowTimeout := 0.005
	lat := 100.0

	// Seed a window with malformed boolean string in raw_aggregates
	wFrom := baseTime
	wTo := wFrom.Add(WindowDuration)
	winBad := &AnalyticsWindow{
		ID:                   uuid.NewString(),
		EntityType:           "bank",
		EntityID:             testBankCode,
		From:                 wFrom,
		To:                   wTo,
		SampleCount:          40,
		FeatureSchemaVersion: FeatureSchemaVersionV1,
		Features: FeatureVector{
			FailureRate:  &lowFail,
			TimeoutRate:  &lowTimeout,
			P95LatencyMs: &lat,
		},
		Model:    ModelStatusInfo{Status: "unavailable"},
		Advisory: true,
	}

	// Insert row with invalid non-boolean value in reviewed_healthy
	_, err := pool.Exec(ctx, `
		INSERT INTO operational_aggregate_windows (
			id, entity_type, entity_id, window_from, window_to, sample_count,
			feature_schema_version, features, raw_aggregates, model_status
		) VALUES (
			$1, 'bank', $2, $3, $4, 40,
			$5, '{"failureRate": 0.01}'::jsonb, '{"reviewed_healthy": "invalid_not_a_boolean"}'::jsonb, 'unavailable'
		)
	`, winBad.ID, testBankCode, wFrom, wTo, FeatureSchemaVersionV1)
	if err != nil {
		t.Fatalf("insert malformed window failed: %v", err)
	}

	// Setup bank, user, accounts, and payments for scoring window
	bankID := uuid.New()
	_, _ = pool.Exec(ctx, "INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Malformed Bank', 'ACTIVE')", bankID, testBankCode)
	userID := uuid.New()
	userPhone := "+9199" + uuid.NewString()[:8]
	_, _ = pool.Exec(ctx, "INSERT INTO users (id, phone, name, upi_id, password_hash, role) VALUES ($1, $2, 'User', $3, 'h', 'CUSTOMER')", userID, userPhone, userPhone+"@upi")
	accID := uuid.New()
	_, _ = pool.Exec(ctx, "INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status) VALUES ($1, $2, $3, $4, 5000000, 'ACTIVE')", accID, userID, bankID, "ACC_"+uuid.NewString()[:8])

	for p := 0; p < 25; p++ {
		payID := uuid.New()
		pTime := scoreFrom.Add(time.Duration(p*5) * time.Second)
		_, _ = pool.Exec(ctx, `
			INSERT INTO payments (
				id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise,
				currency, state, source_bank_id, destination_bank_id, created_at, updated_at
			) VALUES ($1, $2, $3, $3, 1000, 'INR', 'COMPLETED', $4, $4, $5, $5);
		`, payID, userID, accID, bankID, pTime)
		_, _ = pool.Exec(ctx, `
			INSERT INTO payment_state_transitions (id, payment_id, from_state, to_state, transitioned_at)
			VALUES ($1, $2, 'CREATED', 'COMPLETED', $3);
		`, uuid.New(), payID, pTime)
	}

	win, raw, err := repo.AggregateWindow(ctx, "bank", testBankCode, scoreFrom, scoreTo)
	if err != nil {
		t.Fatalf("AggregateWindow failed: %v", err)
	}

	// Must fail closed: baseline_status and scoring_outcome must be "error", model "unavailable"
	if raw["baseline_status"] != "error" {
		t.Errorf("expected baseline_status 'error' on malformed provenance, got %v", raw["baseline_status"])
	}
	if raw["scoring_outcome"] != "error" {
		t.Errorf("expected scoring_outcome 'error' on malformed provenance, got %v", raw["scoring_outcome"])
	}
	if win.Model.Status != "unavailable" {
		t.Errorf("expected model status 'unavailable', got %v", win.Model.Status)
	}
	calibErr, hasCalibErr := raw["calibration_error"].(string)
	if !hasCalibErr || calibErr == "" {
		t.Errorf("expected non-empty calibration_error in raw_aggregates, got %v", raw["calibration_error"])
	}
	if win.BaselineAlert != nil {
		t.Errorf("expected nil baseline alert on evaluation error (no false alerts), got %v", win.BaselineAlert)
	}

	// Persist window and inspect PostgreSQL
	if err := repo.UpsertWindow(ctx, win, raw); err != nil {
		t.Fatalf("UpsertWindow failed: %v", err)
	}

	var dbOutcome, dbStatus, dbModelStatus string
	var dbCalibErr string
	err = pool.QueryRow(ctx, `
		SELECT
			raw_aggregates->>'scoring_outcome',
			raw_aggregates->>'baseline_status',
			model_status,
			COALESCE(raw_aggregates->>'calibration_error', '')
		FROM operational_aggregate_windows WHERE id = $1::uuid;
	`, win.ID).Scan(&dbOutcome, &dbStatus, &dbModelStatus, &dbCalibErr)
	if err != nil {
		t.Fatalf("query DB for malformed window: %v", err)
	}
	if dbOutcome != "error" {
		t.Errorf("expected DB scoring_outcome 'error', got %s", dbOutcome)
	}
	if dbStatus != "error" {
		t.Errorf("expected DB baseline_status 'error', got %s", dbStatus)
	}
	if dbModelStatus != "unavailable" {
		t.Errorf("expected DB model_status 'unavailable', got %s", dbModelStatus)
	}
	if dbCalibErr == "" {
		t.Errorf("expected non-empty calibration_error in DB raw_aggregates")
	}
}
