package outbox_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/database"
	"github.com/transactx/backend/internal/outbox"
)

type testEntities struct {
	pool        *pgxpool.Pool
	userID      uuid.UUID
	bankID      uuid.UUID
	sourceAccID uuid.UUID
	destAccID   uuid.UUID
}

func setupTestEntities(t *testing.T) *testEntities {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := database.NewPool(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}

	ctx := context.Background()
	userID := uuid.New()
	bankID := uuid.New()
	sourceAccID := uuid.New()
	destAccID := uuid.New()

	phone := "9" + uuid.NewString()[:9]
	upiID := "user-" + uuid.NewString()[:8] + "@tx"
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, name, phone, upi_id, password_hash, role)
		VALUES ($1, 'Outbox Test User', $2, $3, 'hash', 'CUSTOMER')`,
		userID, phone, upiID)
	if err != nil {
		pool.Close()
		t.Fatalf("setup user: %v", err)
	}

	bankCode := "OB-" + uuid.NewString()[:8]
	_, err = pool.Exec(ctx, `
		INSERT INTO banks (id, code, name, status)
		VALUES ($1, $2, 'Outbox Bank', 'ACTIVE')`,
		bankID, bankCode)
	if err != nil {
		pool.Close()
		t.Fatalf("setup bank: %v", err)
	}

	accNum1 := "ACC-" + uuid.NewString()[:10]
	accNum2 := "ACC-" + uuid.NewString()[:10]
	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, bank_id, account_number, status)
		VALUES ($1, $3, $4, $5, 'ACTIVE'), ($2, $3, $4, $6, 'ACTIVE')`,
		sourceAccID, destAccID, userID, bankID, accNum1, accNum2)
	if err != nil {
		pool.Close()
		t.Fatalf("setup accounts: %v", err)
	}

	return &testEntities{
		pool:        pool,
		userID:      userID,
		bankID:      bankID,
		sourceAccID: sourceAccID,
		destAccID:   destAccID,
	}
}

func (te *testEntities) close() {
	ctx := context.Background()
	_, _ = te.pool.Exec(ctx, `DELETE FROM payments WHERE initiated_by_user_id = $1`, te.userID)
	_, _ = te.pool.Exec(ctx, `DELETE FROM accounts WHERE id IN ($1, $2)`, te.sourceAccID, te.destAccID)
	_, _ = te.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, te.userID)
	_, _ = te.pool.Exec(ctx, `DELETE FROM banks WHERE id = $1`, te.bankID)
	te.pool.Close()
}

func TestOutbox_PaymentCreatedAndStateTransitions(t *testing.T) {
	te := setupTestEntities(t)
	defer te.close()
	ctx := context.Background()
	store := outbox.NewPostgresStore(te.pool)

	paymentID := uuid.New()

	// 1. Insert payment in CREATED state
	insertQuery := `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 50000, 'INR', 'CREATED', NOW(), NOW())`

	if _, err := te.pool.Exec(ctx, insertQuery, paymentID, te.userID, te.sourceAccID, te.destAccID); err != nil {
		t.Fatalf("insert payment: %v", err)
	}

	// Verify payment.created.v1 event
	events, err := store.ListEventsByAggregate(ctx, outbox.AggregateTypePayment, paymentID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event on insert, got %d", len(events))
	}

	e1 := events[0]
	if e1.EventType != outbox.EventTypePaymentCreated || e1.AggregateVersion != 1 || e1.SchemaVersion != 1 {
		t.Fatalf("unexpected event metadata: %+v", e1)
	}

	payload1, err := e1.ParsePaymentPayload()
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if payload1.PaymentID != paymentID || payload1.PreviousState != nil || payload1.NewState != "CREATED" || payload1.EventVersion != 1 {
		t.Fatalf("unexpected payload: %+v", payload1)
	}

	// Verify delivery row created
	d1, err := store.GetDelivery(ctx, outbox.ConsumerOpsWorklist, e1.ID)
	if err != nil || d1 == nil {
		t.Fatalf("delivery row not found for event %s: %v", e1.ID, err)
	}
	if d1.Status != outbox.DeliveryStatusPending {
		t.Fatalf("expected status pending, got %s", d1.Status)
	}

	// 2. Transition CREATED -> VALIDATING -> ROUTING -> PROCESSING -> COMPLETED
	transitions := []struct {
		targetState string
		expectedVer int64
		prevState   string
	}{
		{"VALIDATING", 2, "CREATED"},
		{"ROUTING", 3, "VALIDATING"},
		{"PROCESSING", 4, "ROUTING"},
		{"COMPLETED", 5, "PROCESSING"},
	}

	for _, tr := range transitions {
		updateQuery := `UPDATE payments SET state = $1, updated_at = NOW() WHERE id = $2`
		if _, err := te.pool.Exec(ctx, updateQuery, tr.targetState, paymentID); err != nil {
			t.Fatalf("update state to %s: %v", tr.targetState, err)
		}

		events, err := store.ListEventsByAggregate(ctx, outbox.AggregateTypePayment, paymentID)
		if err != nil {
			t.Fatalf("list events: %v", err)
		}
		if int64(len(events)) != tr.expectedVer {
			t.Fatalf("expected %d events after state %s, got %d", tr.expectedVer, tr.targetState, len(events))
		}

		latest := events[len(events)-1]
		if latest.EventType != outbox.EventTypePaymentStateChanged || latest.AggregateVersion != tr.expectedVer {
			t.Fatalf("expected version %d for state %s, got %+v", tr.expectedVer, tr.targetState, latest)
		}

		pl, err := latest.ParsePaymentPayload()
		if err != nil {
			t.Fatalf("parse payload: %v", err)
		}
		if pl.PreviousState == nil || *pl.PreviousState != tr.prevState || pl.NewState != tr.targetState || pl.EventVersion != tr.expectedVer {
			t.Fatalf("mismatched payload state: %+v (want prev=%s, new=%s)", pl, tr.prevState, tr.targetState)
		}

		// Verify delivery
		d, err := store.GetDelivery(ctx, outbox.ConsumerOpsWorklist, latest.ID)
		if err != nil || d == nil {
			t.Fatalf("delivery row missing for event %s: %v", latest.ID, err)
		}
	}

	// Verify payment row event_version matches latest
	var finalVer int64
	if err := te.pool.QueryRow(ctx, "SELECT event_version FROM payments WHERE id = $1", paymentID).Scan(&finalVer); err != nil {
		t.Fatalf("read final event_version: %v", err)
	}
	if finalVer != 5 {
		t.Fatalf("expected payment event_version 5, got %d", finalVer)
	}
}

func TestOutbox_SameStateUpdateEmitsNothing(t *testing.T) {
	te := setupTestEntities(t)
	defer te.close()
	ctx := context.Background()
	store := outbox.NewPostgresStore(te.pool)

	paymentID := uuid.New()
	insertQuery := `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 10000, 'INR', 'CREATED', NOW(), NOW())`

	if _, err := te.pool.Exec(ctx, insertQuery, paymentID, te.userID, te.sourceAccID, te.destAccID); err != nil {
		t.Fatalf("insert payment: %v", err)
	}

	// Update with identical state
	updateQuery := `UPDATE payments SET state = 'CREATED', updated_at = NOW() WHERE id = $1`
	if _, err := te.pool.Exec(ctx, updateQuery, paymentID); err != nil {
		t.Fatalf("update same state: %v", err)
	}

	events, err := store.ListEventsByAggregate(ctx, outbox.AggregateTypePayment, paymentID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event (no duplicates on identical state), got %d", len(events))
	}
}

func TestOutbox_NonStateUpdateEmitsNothing(t *testing.T) {
	te := setupTestEntities(t)
	defer te.close()
	ctx := context.Background()
	store := outbox.NewPostgresStore(te.pool)

	paymentID := uuid.New()
	insertQuery := `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 15000, 'INR', 'CREATED', NOW(), NOW())`

	if _, err := te.pool.Exec(ctx, insertQuery, paymentID, te.userID, te.sourceAccID, te.destAccID); err != nil {
		t.Fatalf("insert payment: %v", err)
	}

	// Update non-state column (failure_reason)
	updateQuery := `UPDATE payments SET failure_reason = 'transient network hiccup', updated_at = NOW() WHERE id = $1`
	if _, err := te.pool.Exec(ctx, updateQuery, paymentID); err != nil {
		t.Fatalf("update non-state: %v", err)
	}

	events, err := store.ListEventsByAggregate(ctx, outbox.AggregateTypePayment, paymentID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event (non-state changes emit nothing), got %d", len(events))
	}
}

func TestOutbox_SourceTransactionRollbackLeavesNoEvents(t *testing.T) {
	te := setupTestEntities(t)
	defer te.close()
	ctx := context.Background()
	store := outbox.NewPostgresStore(te.pool)

	paymentID := uuid.New()

	tx, err := te.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	insertQuery := `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 25000, 'INR', 'CREATED', NOW(), NOW())`

	if _, err := tx.Exec(ctx, insertQuery, paymentID, te.userID, te.sourceAccID, te.destAccID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("insert payment: %v", err)
	}

	// Roll back source transaction!
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback tx: %v", err)
	}

	events, err := store.ListEventsByAggregate(ctx, outbox.AggregateTypePayment, paymentID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events after rollback, got %d", len(events))
	}

	var deliveryCount int64
	if err := te.pool.QueryRow(ctx, "SELECT COUNT(*) FROM outbox_deliveries od JOIN outbox_events oe ON od.event_id = oe.id WHERE oe.aggregate_id = $1", paymentID).Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveryCount != 0 {
		t.Fatalf("expected 0 deliveries after rollback, got %d", deliveryCount)
	}
}

func TestOutbox_ImmutableEventsDenyUpdate(t *testing.T) {
	te := setupTestEntities(t)
	defer te.close()
	ctx := context.Background()
	store := outbox.NewPostgresStore(te.pool)

	paymentID := uuid.New()
	insertQuery := `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 30000, 'INR', 'CREATED', NOW(), NOW())`

	if _, err := te.pool.Exec(ctx, insertQuery, paymentID, te.userID, te.sourceAccID, te.destAccID); err != nil {
		t.Fatalf("insert payment: %v", err)
	}

	events, err := store.ListEventsByAggregate(ctx, outbox.AggregateTypePayment, paymentID)
	if err != nil || len(events) == 0 {
		t.Fatalf("event not found: %v", err)
	}

	// Direct update to outbox_events MUST fail
	_, err = te.pool.Exec(ctx, `UPDATE outbox_events SET event_type = 'tampered' WHERE id = $1`, events[0].ID)
	if err == nil {
		t.Fatalf("expected error updating immutable outbox_events, got nil")
	}
}

func TestOutbox_PayloadSanitization(t *testing.T) {
	te := setupTestEntities(t)
	defer te.close()
	ctx := context.Background()
	store := outbox.NewPostgresStore(te.pool)

	paymentID := uuid.New()

	insertQuery := `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, note, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 40000, 'INR', 'CREATED', 'Sensitive internal note', NOW(), NOW())`

	if _, err := te.pool.Exec(ctx, insertQuery, paymentID, te.userID, te.sourceAccID, te.destAccID); err != nil {
		t.Fatalf("insert payment: %v", err)
	}

	events, err := store.ListEventsByAggregate(ctx, outbox.AggregateTypePayment, paymentID)
	if err != nil || len(events) == 0 {
		t.Fatalf("events not found: %v", err)
	}

	rawJSON := string(events[0].Payload)
	var rawMap map[string]any
	if err := json.Unmarshal(events[0].Payload, &rawMap); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}

	// Verify exclusions per EVENT_ARCHITECTURE.md
	if _, exists := rawMap["note"]; exists {
		t.Fatalf("payload leaked customer note: %s", rawJSON)
	}
	if _, exists := rawMap["initiatedByUserId"]; exists {
		t.Fatalf("payload leaked user ID: %s", rawJSON)
	}
	if _, exists := rawMap["senderAccountId"]; exists {
		t.Fatalf("payload leaked sender account: %s", rawJSON)
	}
	if _, exists := rawMap["receiverAccountId"]; exists {
		t.Fatalf("payload leaked receiver account: %s", rawJSON)
	}
}

func TestOutbox_RecoveryAttemptAndJobTriggers(t *testing.T) {
	te := setupTestEntities(t)
	defer te.close()
	ctx := context.Background()
	store := outbox.NewPostgresStore(te.pool)

	paymentID := uuid.New()
	insertPaymentQuery := `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 99000, 'INR', 'PROCESSING', NOW(), NOW())`

	if _, err := te.pool.Exec(ctx, insertPaymentQuery, paymentID, te.userID, te.sourceAccID, te.destAccID); err != nil {
		t.Fatalf("insert payment: %v", err)
	}

	// 1. Insert recovery job
	insertJobQuery := `
		INSERT INTO recovery_jobs (payment_id, status, due_at, lease_epoch, attempt_count, created_at, updated_at)
		VALUES ($1, 'scheduled', NOW(), 1, 1, NOW(), NOW())`
	if _, err := te.pool.Exec(ctx, insertJobQuery, paymentID); err != nil {
		t.Fatalf("insert recovery job: %v", err)
	}

	// 2. Insert recovery attempt -> triggers recovery.attempt_started.v1
	attemptID := uuid.New()
	insertAttemptQuery := `
		INSERT INTO recovery_attempts (id, payment_id, lease_epoch, worker_identity, started_at, state_before)
		VALUES ($1, $2, 1, 'worker-1', NOW(), 'PROCESSING')`
	if _, err := te.pool.Exec(ctx, insertAttemptQuery, attemptID, paymentID); err != nil {
		t.Fatalf("insert recovery attempt: %v", err)
	}

	events, err := store.ListEventsByAggregate(ctx, outbox.AggregateTypeRecovery, paymentID)
	if err != nil {
		t.Fatalf("list recovery events: %v", err)
	}
	if len(events) != 1 || events[0].EventType != outbox.EventTypeRecoveryAttemptStarted {
		t.Fatalf("expected recovery.attempt_started.v1, got %+v", events)
	}

	// 3. Update recovery attempt finished -> triggers recovery.attempt_finished.v1
	updateAttemptQuery := `
		UPDATE recovery_attempts
		SET finished_at = NOW(), state_after = 'PROCESSING', result = 'TRANSIENT_FAILURE', reason_code = 'TIMEOUT'
		WHERE id = $1`
	if _, err := te.pool.Exec(ctx, updateAttemptQuery, attemptID); err != nil {
		t.Fatalf("update recovery attempt: %v", err)
	}

	events, err = store.ListEventsByAggregate(ctx, outbox.AggregateTypeRecovery, paymentID)
	if err != nil {
		t.Fatalf("list recovery events: %v", err)
	}
	if len(events) != 2 || events[1].EventType != outbox.EventTypeRecoveryAttemptFinished {
		t.Fatalf("expected recovery.attempt_finished.v1, got %+v", events)
	}

	// 4. Update recovery job to needs_attention -> triggers recovery.attention_required.v1
	updateJobQuery := `
		UPDATE recovery_jobs
		SET status = 'needs_attention', last_reason_code = 'ATTEMPTS_EXHAUSTED', updated_at = NOW()
		WHERE payment_id = $1`
	if _, err := te.pool.Exec(ctx, updateJobQuery, paymentID); err != nil {
		t.Fatalf("update recovery job: %v", err)
	}

	events, err = store.ListEventsByAggregate(ctx, outbox.AggregateTypeRecovery, paymentID)
	if err != nil {
		t.Fatalf("list recovery events: %v", err)
	}
	if len(events) != 3 || events[2].EventType != outbox.EventTypeRecoveryAttentionRequired {
		t.Fatalf("expected recovery.attention_required.v1, got %+v", events)
	}

	// Verify deliveries exist for all recovery events
	for _, e := range events {
		d, err := store.GetDelivery(ctx, outbox.ConsumerOpsWorklist, e.ID)
		if err != nil || d == nil {
			t.Fatalf("missing delivery for recovery event %s: %v", e.ID, err)
		}
	}
}

func TestOutbox_DeliveriesQueryingAndCounts(t *testing.T) {
	te := setupTestEntities(t)
	defer te.close()
	ctx := context.Background()
	store := outbox.NewPostgresStore(te.pool)

	count, err := store.CountPendingDeliveries(ctx, outbox.ConsumerOpsWorklist)
	if err != nil {
		t.Fatalf("count pending deliveries: %v", err)
	}
	if count < 0 {
		t.Fatalf("invalid negative count: %d", count)
	}

	deliveries, err := store.ListDeliveries(ctx, outbox.ConsumerOpsWorklist, outbox.DeliveryStatusPending, 10)
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if len(deliveries) > 10 {
		t.Fatalf("expected at most 10 deliveries, got %d", len(deliveries))
	}
}
