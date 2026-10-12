package analytics

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres@localhost:5432/transactx?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("cannot connect to postgresql (%s): %v", dbURL, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("cannot ping postgresql (%s): %v", dbURL, err)
	}
	return pool
}

func TestSchemaCompliance_AgainstV2Schema(t *testing.T) {
	schemaPaths := []string{
		"C:/DL_EXam/tut/m/Project_update/TransactX_M2/docs/next-gen/execution-pack/docs/next-gen/contracts/v2.schema.json",
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
		t.Skip("v2.schema.json not accessible from test path, skipping schema contract test")
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

func TestAnalytics_BaselineAlert_OnHighFailureRate(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	repo := NewPostgresRepository(pool)
	now := time.Now().UTC().Truncate(time.Minute)
	from := now.Add(-60 * time.Minute).Truncate(WindowDuration)
	to := from.Add(WindowDuration)

	bankCode := "BANK_ALERT_" + uuid.NewString()[:8]
	bankID := uuid.New()
	_, err := pool.Exec(ctx, "INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Alert Bank', 'ACTIVE')", bankID, bankCode)
	if err != nil {
		t.Fatalf("insert bank: %v", err)
	}

	userID := uuid.New()
	userPhone := "+9199" + uuid.NewString()[:8]
	_, err = pool.Exec(ctx, "INSERT INTO users (id, phone, name, upi_id, password_hash, role) VALUES ($1, $2, 'Alert User', $3, 'hash', 'CUSTOMER')", userID, userPhone, userPhone+"@upi")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	accID := uuid.New()
	_, err = pool.Exec(ctx, "INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status) VALUES ($1, $2, $3, $4, 5000000, 'ACTIVE')", accID, userID, bankID, "ACC_"+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	// Insert 25 payments: 15 COMPLETED, 10 FAILED (failureRate = 10/25 = 0.40 > 0.15)
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

	win, _, err := repo.AggregateWindow(ctx, "bank", bankCode, from, to)
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
	if win.BaselineAlert == nil {
		t.Fatalf("expected non-nil baselineAlert for 40%% failure rate with 25 samples")
	}
	if win.BaselineAlert.Code != "HIGH_FAILURE_RATE" {
		t.Errorf("expected alert code HIGH_FAILURE_RATE, got %s", win.BaselineAlert.Code)
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
