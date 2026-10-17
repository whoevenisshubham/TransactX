package projections_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/database"
	"github.com/transactx/backend/internal/outbox"
	"github.com/transactx/backend/internal/projections"
)

type testEntities struct {
	pool        *pgxpool.Pool
	lockConn    *pgxpool.Conn
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
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		t.Fatalf("acquire lock connection: %v", err)
	}
	if _, err := lockConn.Exec(ctx, `SELECT pg_advisory_lock(88880001)`); err != nil {
		lockConn.Release()
		pool.Close()
		t.Fatalf("acquire test lock: %v", err)
	}

	userID := uuid.New()
	bankID := uuid.New()
	sourceAccID := uuid.New()
	destAccID := uuid.New()

	phone := "9" + uuid.NewString()[:9]
	upiID := "user-" + uuid.NewString()[:8] + "@tx"
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, name, phone, upi_id, password_hash, role)
		VALUES ($1, 'Projection Test User', $2, $3, 'hash', 'CUSTOMER')`,
		userID, phone, upiID)
	if err != nil {
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock(88880001)`)
		lockConn.Release()
		pool.Close()
		t.Fatalf("setup user: %v", err)
	}

	bankCode := "PR-" + uuid.NewString()[:8]
	_, err = pool.Exec(ctx, `
		INSERT INTO banks (id, code, name, status)
		VALUES ($1, $2, 'Projection Bank', 'ACTIVE')`,
		bankID, bankCode)
	if err != nil {
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock(88880001)`)
		lockConn.Release()
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
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock(88880001)`)
		lockConn.Release()
		pool.Close()
		t.Fatalf("setup accounts: %v", err)
	}

	return &testEntities{
		pool:        pool,
		lockConn:    lockConn,
		userID:      userID,
		bankID:      bankID,
		sourceAccID: sourceAccID,
		destAccID:   destAccID,
	}
}

func (te *testEntities) close() {
	ctx := context.Background()
	tx, err := te.pool.Begin(ctx)
	if err == nil {
		defer tx.Rollback(ctx)
		_, _ = tx.Exec(ctx, `SET LOCAL session_replication_role = 'replica'`)
		_, _ = tx.Exec(ctx, `DELETE FROM payment_state_transitions WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, te.userID)
		_, _ = tx.Exec(ctx, `DELETE FROM consumer_inbox WHERE event_id IN (SELECT id FROM outbox_events WHERE aggregate_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1))`)
		_, _ = tx.Exec(ctx, `DELETE FROM outbox_deliveries WHERE event_id IN (SELECT id FROM outbox_events WHERE aggregate_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1))`)
		_, _ = tx.Exec(ctx, `DELETE FROM outbox_events WHERE aggregate_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`)
		_, _ = tx.Exec(ctx, `DELETE FROM ops_payment_worklist WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`)
		_, _ = tx.Exec(ctx, `DELETE FROM payments WHERE initiated_by_user_id = $1`, te.userID)
		_, _ = tx.Exec(ctx, `DELETE FROM accounts WHERE id IN ($1, $2)`, te.sourceAccID, te.destAccID)
		_, _ = tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, te.userID)
		_, _ = tx.Exec(ctx, `DELETE FROM banks WHERE id = $1`, te.bankID)
		_ = tx.Commit(ctx)
	}
	if te.lockConn != nil {
		_, _ = te.lockConn.Exec(ctx, `SELECT pg_advisory_unlock(88880001)`)
		te.lockConn.Release()
	}
	te.pool.Close()
}

func TestProjector_StandardEventDeliveryAndProjection(t *testing.T) {
	te := setupTestEntities(t)
	defer te.close()
	ctx := context.Background()

	outboxStore := outbox.NewPostgresStore(te.pool)
	projector := projections.NewProjector(te.pool, outbox.ConsumerOpsWorklist)
	repo := projections.NewRepository(te.pool)

	paymentID := uuid.New()

	// 1. Insert payment in CREATED state
	insertQuery := `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 75000, 'INR', 'CREATED', NOW(), NOW())`
	if _, err := te.pool.Exec(ctx, insertQuery, paymentID, te.userID, te.sourceAccID, te.destAccID); err != nil {
		t.Fatalf("insert payment: %v", err)
	}

	events, err := outboxStore.ListEventsByAggregate(ctx, outbox.AggregateTypePayment, paymentID)
	if err != nil || len(events) != 1 {
		t.Fatalf("expected 1 created event, got %d (err: %v)", len(events), err)
	}

	// Project the created event
	dup, err := projector.ProjectEvent(ctx, events[0])
	if err != nil || dup {
		t.Fatalf("project event: dup=%v, err=%v", dup, err)
	}

	// Verify worklist item projected
	item, err := repo.GetWorklistItem(ctx, paymentID)
	if err != nil {
		t.Fatalf("get worklist item: %v", err)
	}
	if item.PaymentID != paymentID || item.CurrentState != "CREATED" || item.EventVersion != 1 || item.AmountPaise != 75000 {
		t.Fatalf("unexpected worklist item: %+v", item)
	}

	// 2. Advance payment state to PROCESSING
	if _, err := te.pool.Exec(ctx, `UPDATE payments SET state = 'PROCESSING', updated_at = NOW() WHERE id = $1`, paymentID); err != nil {
		t.Fatalf("update state: %v", err)
	}

	events, err = outboxStore.ListEventsByAggregate(ctx, outbox.AggregateTypePayment, paymentID)
	if err != nil || len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	dup, err = projector.ProjectEvent(ctx, events[1])
	if err != nil || dup {
		t.Fatalf("project event 2: dup=%v, err=%v", dup, err)
	}

	item, err = repo.GetWorklistItem(ctx, paymentID)
	if err != nil {
		t.Fatalf("get worklist item: %v", err)
	}
	if item.CurrentState != "PROCESSING" || item.EventVersion != 2 {
		t.Fatalf("expected state PROCESSING and version 2, got %+v", item)
	}
}

func TestProjector_DuplicateDeliveryAbsorbedByIdempotentInbox(t *testing.T) {
	te := setupTestEntities(t)
	defer te.close()
	ctx := context.Background()

	outboxStore := outbox.NewPostgresStore(te.pool)
	projector := projections.NewProjector(te.pool, outbox.ConsumerOpsWorklist)

	paymentID := uuid.New()
	insertQuery := `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 12000, 'INR', 'CREATED', NOW(), NOW())`
	if _, err := te.pool.Exec(ctx, insertQuery, paymentID, te.userID, te.sourceAccID, te.destAccID); err != nil {
		t.Fatalf("insert payment: %v", err)
	}

	events, err := outboxStore.ListEventsByAggregate(ctx, outbox.AggregateTypePayment, paymentID)
	if err != nil || len(events) == 0 {
		t.Fatalf("events not found: %v", err)
	}

	// First projection
	dup1, err1 := projector.ProjectEvent(ctx, events[0])
	if err1 != nil || dup1 {
		t.Fatalf("first project: dup=%v, err=%v", dup1, err1)
	}

	// Second projection with exact same event -> MUST be absorbed as duplicate
	dup2, err2 := projector.ProjectEvent(ctx, events[0])
	if err2 != nil {
		t.Fatalf("second project failed: %v", err2)
	}
	if !dup2 {
		t.Fatalf("expected duplicate=true on second projection")
	}
}

func TestProjector_OutOfOrderEventDoesNotRegressState(t *testing.T) {
	te := setupTestEntities(t)
	defer te.close()
	ctx := context.Background()

	outboxStore := outbox.NewPostgresStore(te.pool)
	projector := projections.NewProjector(te.pool, outbox.ConsumerOpsWorklist)
	repo := projections.NewRepository(te.pool)

	paymentID := uuid.New()

	// 1. Create payment (ver 1)
	if _, err := te.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 88000, 'INR', 'CREATED', NOW(), NOW())`,
		paymentID, te.userID, te.sourceAccID, te.destAccID); err != nil {
		t.Fatalf("insert payment: %v", err)
	}

	// 2. Advance to COMPLETED (ver 2)
	if _, err := te.pool.Exec(ctx, `UPDATE payments SET state = 'COMPLETED', updated_at = NOW() WHERE id = $1`, paymentID); err != nil {
		t.Fatalf("advance to COMPLETED: %v", err)
	}

	events, err := outboxStore.ListEventsByAggregate(ctx, outbox.AggregateTypePayment, paymentID)
	if err != nil || len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	// Project latest event first (ver 2 COMPLETED)
	_, err = projector.ProjectEvent(ctx, events[1])
	if err != nil {
		t.Fatalf("project event 2: %v", err)
	}

	item, err := repo.GetWorklistItem(ctx, paymentID)
	if err != nil || item.CurrentState != "COMPLETED" || item.EventVersion != 2 {
		t.Fatalf("expected COMPLETED ver 2, got %+v", item)
	}

	// Now deliver older event (ver 1 CREATED) late
	// Simulate unique event delivery by using a distinct consumer or clearing inbox entry for ver 1
	_, _ = te.pool.Exec(ctx, `DELETE FROM consumer_inbox WHERE event_id = $1`, events[0].ID)
	dup, err := projector.ProjectEvent(ctx, events[0])
	if err != nil {
		t.Fatalf("project late event 1 failed: %v", err)
	}
	if dup {
		t.Fatalf("expected not duplicate")
	}

	// Worklist state MUST NOT regress to CREATED!
	itemAfter, err := repo.GetWorklistItem(ctx, paymentID)
	if err != nil {
		t.Fatalf("get item after: %v", err)
	}
	if itemAfter.CurrentState != "COMPLETED" || itemAfter.EventVersion != 2 {
		t.Fatalf("worklist state regressed! got state=%s, version=%d (expected COMPLETED, 2)", itemAfter.CurrentState, itemAfter.EventVersion)
	}
}

func TestProjector_UnsupportedVersionQuarantined(t *testing.T) {
	te := setupTestEntities(t)
	defer te.close()
	ctx := context.Background()

	projector := projections.NewProjector(te.pool, outbox.ConsumerOpsWorklist)

	invalidEvent := &outbox.Event{
		ID:            uuid.New(),
		EventType:     "unsupported.event.v99",
		SchemaVersion: 99,
		AggregateType: outbox.AggregateTypePayment,
		AggregateID:   uuid.New(),
	}

	_, err := projector.ProjectEvent(ctx, invalidEvent)
	if err == nil {
		t.Fatalf("expected error for unsupported version 99, got nil")
	}
}

func TestService_ProcessBatch_ClaimsAndAcknowledges(t *testing.T) {
	te := setupTestEntities(t)
	defer te.close()
	ctx := context.Background()

	outboxStore := outbox.NewPostgresStore(te.pool)
	dispatcher := outbox.NewDispatcher(te.pool)
	projector := projections.NewProjector(te.pool, outbox.ConsumerOpsWorklist)
	svc := projections.NewService(te.pool, outboxStore, dispatcher, projector)
	repo := projections.NewRepository(te.pool)

	paymentID := uuid.New()
	insertQuery := `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 60000, 'INR', 'CREATED', NOW(), NOW())`
	if _, err := te.pool.Exec(ctx, insertQuery, paymentID, te.userID, te.sourceAccID, te.destAccID); err != nil {
		t.Fatalf("insert payment: %v", err)
	}

	events, err := outboxStore.ListEventsByAggregate(ctx, outbox.AggregateTypePayment, paymentID)
	if err != nil || len(events) == 0 {
		t.Fatalf("events not found: %v", err)
	}

	// Process batches until our payment's delivery is processed
	var delivery *outbox.Delivery
	for i := 0; i < 20; i++ {
		_, err := svc.ProcessBatch(ctx, "worker-test-1", 100)
		if err != nil {
			t.Fatalf("ProcessBatch failed: %v", err)
		}
		delivery, err = outboxStore.GetDelivery(ctx, outbox.ConsumerOpsWorklist, events[0].ID)
		if err == nil && delivery != nil && delivery.Status == outbox.DeliveryStatusDelivered {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	// Verify delivery acknowledged
	if delivery == nil {
		t.Fatalf("expected delivery for event %s, got nil (events count: %d)", events[0].ID, len(events))
	}
	if delivery.Status != outbox.DeliveryStatusDelivered || delivery.DeliveredAt == nil {
		t.Fatalf("expected delivery delivered, got status=%s, deliveredAt=%v, lastError=%v", delivery.Status, delivery.DeliveredAt, delivery.LastErrorCode)
	}

	// Verify item exists in worklist
	item, err := repo.GetWorklistItem(ctx, paymentID)
	if err != nil {
		t.Fatalf("worklist item not found: %v", err)
	}
	if item.PaymentID != paymentID || item.CurrentState != "CREATED" {
		t.Fatalf("unexpected worklist item: %+v", item)
	}
}
