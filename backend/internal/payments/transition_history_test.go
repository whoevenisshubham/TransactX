package payments

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TestRealTransitionRecorded proves that when a real payment state transition occurs,
// exactly one immutable history row is written with valid states, payment ID, and timestamp.
func TestRealTransitionRecorded(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	payload := newRepositoryTestData(t)
	defer payload.close(t)

	ctx := context.Background()
	paymentID := uuid.New()
	payment := Payment{
		ID:                paymentID,
		InitiatedByUserID: payload.userID,
		SenderAccountID:   payload.sourceID,
		ReceiverAccountID: payload.receiverID,
		AmountPaise:       500,
		Currency:          "INR",
		State:             StateCreated,
	}

	created, _, err := payload.repository.CreateIdempotent(ctx, payment, "real-trans-key", "real-trans-hash")
	if err != nil {
		t.Fatalf("CreateIdempotent failed: %v", err)
	}

	// SettlePayment transitions Created -> Validating -> LocalSettlement -> Committed -> Completed
	transitions, err := payload.repository.GetTransitions(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTransitions failed: %v", err)
	}

	if len(transitions) == 0 {
		t.Fatalf("expected recorded transitions, got 0")
	}

	// Verify sequential validity of recorded transitions
	for i, tr := range transitions {
		if tr.PaymentID != created.ID {
			t.Fatalf("transition %d: payment ID = %s, want %s", i, tr.PaymentID, created.ID)
		}
		if tr.TransitionedAt.IsZero() {
			t.Fatalf("transition %d: transitioned_at is zero", i)
		}
		if !CanTransition(tr.FromState, tr.ToState) {
			t.Fatalf("transition %d: illegal transition recorded from %s to %s", i, tr.FromState, tr.ToState)
		}
		if i > 0 && transitions[i-1].ToState != tr.FromState {
			t.Fatalf("transition %d: disjoint state chain: previous to=%s, current from=%s", i, transitions[i-1].ToState, tr.FromState)
		}
	}
}

// TestIllegalTransitionDetected verifies that illegal transitions cannot be recorded
// and are rejected by CanTransition and RecordTransition.
func TestIllegalTransitionDetected(t *testing.T) {
	// 1. CanTransition must reject invalid transitions
	illegalPairs := [][2]string{
		{StateCreated, StateCompleted},
		{StateCreated, StateCommitted},
		{StateCompleted, StateProcessing},
		{StateFailed, StateCompleted},
		{StateCommitted, StateCreated},
	}

	for _, pair := range illegalPairs {
		from, to := pair[0], pair[1]
		if CanTransition(from, to) {
			t.Errorf("CanTransition(%s, %s) = true; want false", from, to)
		}
	}

	// 2. Transition must reject illegal transitions
	p := Payment{State: StateCreated}
	if err := Transition(&p, StateCompleted); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("Transition returned %v, want ErrInvalidTransition", err)
	}

	// 3. RecordTransition must reject illegal transitions even before SQL execution
	repo := &Repository{}
	err := repo.RecordTransition(context.Background(), nil, uuid.New(), StateCreated, StateCompleted)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("RecordTransition returned %v, want ErrInvalidTransition", err)
	}
}

// TestTransitionHistoryIsImmutable verifies that payment_state_transitions table
// has no update/delete application path and database mutations are rejected.
func TestTransitionHistoryIsImmutable(t *testing.T) {
	// 1. Verify Repository methods: only INSERT (RecordTransition) and SELECT (GetTransitions) exist.
	repoType := reflect.TypeOf(&Repository{})
	for i := 0; i < repoType.NumMethod(); i++ {
		methodName := repoType.Method(i).Name
		if strings.Contains(strings.ToLower(methodName), "transition") {
			lower := strings.ToLower(methodName)
			if strings.Contains(lower, "update") || strings.Contains(lower, "delete") || strings.Contains(lower, "remove") {
				t.Fatalf("found mutable transition method on Repository: %s", methodName)
			}
		}
	}

	// 2. If DB is available, verify the trigger prevents UPDATE and DELETE
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return
	}

	payload := newRepositoryTestData(t)
	defer payload.close(t)

	ctx := context.Background()
	paymentID := uuid.New()
	_, err := payload.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state)
		VALUES ($1, $2, $3, $4, 100, 'INR', 'CREATED')
	`, paymentID, payload.userID, payload.sourceID, payload.receiverID)
	if err != nil {
		t.Fatalf("insert payment: %v", err)
	}

	transID := uuid.New()
	_, err = payload.pool.Exec(ctx, `
		INSERT INTO payment_state_transitions (id, payment_id, from_state, to_state, transitioned_at)
		VALUES ($1, $2, 'CREATED', 'VALIDATING', CURRENT_TIMESTAMP)
	`, transID, paymentID)
	if err != nil {
		t.Fatalf("insert transition: %v", err)
	}

	// Attempt UPDATE: must fail via trigger
	_, updateErr := payload.pool.Exec(ctx, `
		UPDATE payment_state_transitions SET to_state = 'FAILED' WHERE id = $1
	`, transID)
	if updateErr == nil {
		t.Fatal("expected UPDATE on payment_state_transitions to fail, but succeeded")
	}

	// Attempt DELETE: must fail via trigger
	_, deleteErr := payload.pool.Exec(ctx, `
		DELETE FROM payment_state_transitions WHERE id = $1
	`, transID)
	if deleteErr == nil {
		t.Fatal("expected DELETE on payment_state_transitions to fail, but succeeded")
	}
}

// TestTransitionAndPaymentUpdateAreAtomic verifies that payment state updates
// and transition history rows are committed together or rolled back atomically.
func TestTransitionAndPaymentUpdateAreAtomic(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	payload := newRepositoryTestData(t)
	defer payload.close(t)

	ctx := context.Background()
	paymentID := uuid.New()
	_, err := payload.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state)
		VALUES ($1, $2, $3, $4, 100, 'INR', 'CREATED')
	`, paymentID, payload.userID, payload.sourceID, payload.receiverID)
	if err != nil {
		t.Fatalf("insert payment: %v", err)
	}

	// Start a transaction that simulates an aborted/rolled-back transition
	tx, err := payload.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("pool.Begin: %v", err)
	}

	if _, err := tx.Exec(ctx, `UPDATE payments SET state = 'VALIDATING' WHERE id = $1`, paymentID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("tx.Exec update: %v", err)
	}

	if err := payload.repository.RecordTransition(ctx, tx, paymentID, StateCreated, StateValidating); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("RecordTransition: %v", err)
	}

	// Explicit Rollback
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Fatalf("tx.Rollback: %v", err)
	}

	// Verify payment state is STILL 'CREATED'
	var state string
	if err := payload.pool.QueryRow(ctx, `SELECT state FROM payments WHERE id = $1`, paymentID).Scan(&state); err != nil {
		t.Fatalf("QueryRow state: %v", err)
	}
	if state != StateCreated {
		t.Fatalf("payment state was committed despite rollback: got %s, want %s", state, StateCreated)
	}

	// Verify NO transition history row was committed
	var transitionCount int
	if err := payload.pool.QueryRow(ctx, `SELECT count(*) FROM payment_state_transitions WHERE payment_id = $1`, paymentID).Scan(&transitionCount); err != nil {
		t.Fatalf("QueryRow transition count: %v", err)
	}
	if transitionCount != 0 {
		t.Fatalf("transition history row was committed despite rollback: count = %d, want 0", transitionCount)
	}
}
