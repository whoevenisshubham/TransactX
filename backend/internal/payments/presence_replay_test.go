package payments

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/chaos"
	"github.com/transactx/backend/internal/recipients"
)

type presenceSpyAdapter struct {
	successfulBankAdapter
	presenceFn    func(ctx context.Context, paymentID, operationID uuid.UUID) (bank.OperationPresenceResult, error)
	statusFn      func(ctx context.Context, request bank.OperationStatusRequest) (bank.OperationResult, error)
	holdFn        func(ctx context.Context, request bank.HoldFundsRequest) (bank.HoldResult, error)
	holdsCalled   int
	creditsCalled int
}

func (s *presenceSpyAdapter) GetOperationStatus(ctx context.Context, req bank.OperationStatusRequest) (bank.OperationResult, error) {
	if s.statusFn != nil {
		return s.statusFn(ctx, req)
	}
	return bank.OperationResult{
		PaymentID:   req.PaymentID,
		OperationID: req.OperationID,
		Status:      bank.OperationPending,
	}, nil
}

func (s *presenceSpyAdapter) GetOperationPresence(ctx context.Context, paymentID, operationID uuid.UUID) (bank.OperationPresenceResult, error) {
	if s.presenceFn != nil {
		return s.presenceFn(ctx, paymentID, operationID)
	}
	return bank.OperationPresenceResult{
		PaymentID:   paymentID,
		OperationID: operationID,
		Presence:    bank.PresenceAbsent,
		ObservedAt:  time.Now().UTC(),
		Status:      nil,
	}, nil
}

func (s *presenceSpyAdapter) HoldFunds(ctx context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
	s.holdsCalled++
	if s.holdFn != nil {
		return s.holdFn(ctx, req)
	}
	return s.successfulBankAdapter.HoldFunds(ctx, req)
}

func (s *presenceSpyAdapter) ProvisionalCredit(ctx context.Context, req bank.ProvisionalCreditRequest) (bank.OperationResult, error) {
	s.creditsCalled++
	return s.successfulBankAdapter.ProvisionalCredit(ctx, req)
}

var _ bank.BankAdapter = (*presenceSpyAdapter)(nil)
var _ bank.OperationPresenceReader = (*presenceSpyAdapter)(nil)

type noPresenceAdapter struct {
	successfulBankAdapter
	statusFn func(ctx context.Context, request bank.OperationStatusRequest) (bank.OperationResult, error)
}

func (n *noPresenceAdapter) GetOperationStatus(ctx context.Context, req bank.OperationStatusRequest) (bank.OperationResult, error) {
	if n.statusFn != nil {
		return n.statusFn(ctx, req)
	}
	return bank.OperationResult{
		PaymentID:   req.PaymentID,
		OperationID: req.OperationID,
		Status:      bank.OperationPending,
	}, nil
}

var _ bank.BankAdapter = (*noPresenceAdapter)(nil)

// TestPresence_KillAfterCentralJournalBeforeHTTP_ReplaysAndConverges tests that when a process
// fails/crashes after central journals the bank operation, recovery queries presence,
// authoritatively observes "absent", replays the original immutable operation,
// and successfully converges the payment to COMPLETED.
func TestPresence_KillAfterCentralJournalBeforeHTTP_ReplaysAndConverges(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)

	ctx := context.Background()
	setBalances(t, data, 10000, 0)

	secondBankID := uuid.New()
	if _, err := data.pool.Exec(ctx, `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Bank B', 'ACTIVE')`, secondBankID, "BANK-B-"+secondBankID.String()[:12]); err != nil {
		t.Fatal(err)
	}

	// 1. Initial attempt: HoldFunds simulates a dispatch crash/drop by returning transient failure
	initialHoldCalled := false
	sourceSpy := &presenceSpyAdapter{
		holdFn: func(ctx context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
			if !initialHoldCalled {
				initialHoldCalled = true
				return bank.HoldResult{}, &bank.AdapterError{
					Code:    bank.ErrCodeTransientFailure,
					Message: "process killed before HTTP request delivered",
				}
			}
			// On replay: succeeds
			return bank.HoldResult{
				OperationResult: bank.OperationResult{
					PaymentID:     req.PaymentID,
					OperationID:   req.OperationID,
					Status:        bank.OperationSucceeded,
					BankReference: "REF-HOLD-" + req.OperationID.String()[:8],
				},
				HoldID: req.OperationID,
			}, nil
		},
	}

	adapters := map[uuid.UUID]bank.BankAdapter{
		data.bankID:  sourceSpy,
		secondBankID: successfulBankAdapter{},
	}

	service := NewServiceWithAdapters(
		accounts.NewRepository(data.pool),
		recipients.NewRepository(data.pool),
		data.repository,
		adapters,
	)

	// Create routed payment: initial execution fails into PENDING
	payment, duplicate, err := data.repository.CreateRoutedIdempotent(
		ctx,
		settlementPayment(data, 2500),
		"pres-kill-key",
		"pres-kill-hash",
		data.bankID,
		secondBankID,
		sourceSpy,
		successfulBankAdapter{},
	)
	if err != nil {
		t.Fatalf("CreateRoutedIdempotent returned error: %v", err)
	}
	if duplicate {
		t.Fatal("expected duplicate=false on initial creation")
	}
	if payment.State != StatePendingReconciliation && payment.State != StateProcessing {
		t.Fatalf("expected state pending, got %s", payment.State)
	}

	// 2. Recovery: Service.Resume is called
	presenceChecked := false
	sourceSpy.presenceFn = func(ctx context.Context, pID, opID uuid.UUID) (bank.OperationPresenceResult, error) {
		presenceChecked = true
		return bank.OperationPresenceResult{
			PaymentID:   pID,
			OperationID: opID,
			Presence:    bank.PresenceAbsent,
			ObservedAt:  time.Now().UTC(),
			Status:      nil,
		}, nil
	}

	resumeRes, err := service.Resume(ctx, payment.ID)
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}
	if !presenceChecked {
		t.Fatal("expected presence check to have been executed during recovery")
	}
	if resumeRes.Status != ResumeStatusCompleted {
		t.Fatalf("expected status completed, got %v (reason: %s)", resumeRes.Status, resumeRes.Reason)
	}
	if resumeRes.Payment.State != StateCompleted {
		t.Fatalf("expected payment state COMPLETED, got %s", resumeRes.Payment.State)
	}

	// Verify holds called twice (1 initial failed attempt + 1 successful replay)
	if sourceSpy.holdsCalled != 2 {
		t.Fatalf("expected holds called 2 times, got %d", sourceSpy.holdsCalled)
	}

	// Verify metrics
	metrics := service.RecoveryMetrics().Snapshot()
	if metrics["presence_checked"] < 1 {
		t.Fatalf("expected presence_checked >= 1, got %d", metrics["presence_checked"])
	}
	if metrics["presence_absent"] < 1 {
		t.Fatalf("expected presence_absent >= 1, got %d", metrics["presence_absent"])
	}
	if metrics["presence_replayed"] < 1 {
		t.Fatalf("expected presence_replayed >= 1, got %d", metrics["presence_replayed"])
	}
}

// TestPresence_PresentOperationDoesNotReplay verifies that when an operation is present
// on the participant, recovery does NOT replay it and leaves it pending.
func TestPresence_PresentOperationDoesNotReplay(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)

	ctx := context.Background()
	setBalances(t, data, 10000, 0)

	secondBankID := uuid.New()
	if _, err := data.pool.Exec(ctx, `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Bank B', 'ACTIVE')`, secondBankID, "BANK-B-"+secondBankID.String()[:12]); err != nil {
		t.Fatal(err)
	}

	sourceSpy := &presenceSpyAdapter{
		holdFn: func(ctx context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
			return bank.HoldResult{}, &bank.AdapterError{
				Code:    bank.ErrCodeTransientFailure,
				Message: "initial hold transient error",
			}
		},
	}

	adapters := map[uuid.UUID]bank.BankAdapter{
		data.bankID:  sourceSpy,
		secondBankID: successfulBankAdapter{},
	}

	service := NewServiceWithAdapters(
		accounts.NewRepository(data.pool),
		recipients.NewRepository(data.pool),
		data.repository,
		adapters,
	)

	payment, _, err := data.repository.CreateRoutedIdempotent(
		ctx,
		settlementPayment(data, 2000),
		"pres-present-key",
		"pres-present-hash",
		data.bankID,
		secondBankID,
		sourceSpy,
		successfulBankAdapter{},
	)
	if err != nil {
		t.Fatalf("CreateRoutedIdempotent failed: %v", err)
	}

	// Reset spy calls before recovery
	sourceSpy.holdsCalled = 0
	// Participant presence returns PRESENT
	sourceSpy.presenceFn = func(ctx context.Context, pID, opID uuid.UUID) (bank.OperationPresenceResult, error) {
		opStatus := bank.OperationPending
		return bank.OperationPresenceResult{
			PaymentID:   pID,
			OperationID: opID,
			Presence:    bank.PresencePresent,
			ObservedAt:  time.Now().UTC(),
			Status:      &opStatus,
		}, nil
	}

	resumeRes, err := service.Resume(ctx, payment.ID)
	if err != nil {
		t.Fatalf("Resume returned unexpected error: %v", err)
	}
	// Must NOT replay!
	if sourceSpy.holdsCalled != 0 {
		t.Fatalf("expected 0 replay hold calls when present, got %d", sourceSpy.holdsCalled)
	}
	if resumeRes.Status == ResumeStatusCompleted {
		t.Fatal("expected payment to remain pending, but was completed")
	}

	metrics := service.RecoveryMetrics().Snapshot()
	if metrics["presence_present"] < 1 {
		t.Fatalf("expected presence_present >= 1, got %d", metrics["presence_present"])
	}
	if metrics["presence_replayed"] != 0 {
		t.Fatalf("expected presence_replayed == 0, got %d", metrics["presence_replayed"])
	}
}

// TestPresence_UnavailableNeverMapsToAbsence verifies that errors/timeouts on presence
// check are NEVER mapped to absence, and recovery safely parks the operation in pending.
func TestPresence_UnavailableNeverMapsToAbsence(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)

	ctx := context.Background()
	setBalances(t, data, 10000, 0)

	secondBankID := uuid.New()
	if _, err := data.pool.Exec(ctx, `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Bank B', 'ACTIVE')`, secondBankID, "BANK-B-"+secondBankID.String()[:12]); err != nil {
		t.Fatal(err)
	}

	sourceSpy := &presenceSpyAdapter{
		holdFn: func(ctx context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
			return bank.HoldResult{}, &bank.AdapterError{
				Code:    bank.ErrCodeTransientFailure,
				Message: "initial hold transient error",
			}
		},
	}

	adapters := map[uuid.UUID]bank.BankAdapter{
		data.bankID:  sourceSpy,
		secondBankID: successfulBankAdapter{},
	}

	service := NewServiceWithAdapters(
		accounts.NewRepository(data.pool),
		recipients.NewRepository(data.pool),
		data.repository,
		adapters,
	)

	payment, _, err := data.repository.CreateRoutedIdempotent(
		ctx,
		settlementPayment(data, 1500),
		"pres-unavail-key",
		"pres-unavail-hash",
		data.bankID,
		secondBankID,
		sourceSpy,
		successfulBankAdapter{},
	)
	if err != nil {
		t.Fatalf("CreateRoutedIdempotent failed: %v", err)
	}

	sourceSpy.holdsCalled = 0
	// Presence check fails with error / timeout
	sourceSpy.presenceFn = func(ctx context.Context, pID, opID uuid.UUID) (bank.OperationPresenceResult, error) {
		return bank.OperationPresenceResult{}, errors.New("participant connection timeout")
	}

	resumeRes, err := service.Resume(ctx, payment.ID)
	if err != nil {
		t.Fatalf("Resume returned unexpected error: %v", err)
	}
	// Must NOT replay!
	if sourceSpy.holdsCalled != 0 {
		t.Fatalf("expected 0 replay hold calls on presence failure, got %d", sourceSpy.holdsCalled)
	}
	if resumeRes.Status == ResumeStatusCompleted {
		t.Fatal("expected payment to remain pending on presence error, but completed")
	}

	metrics := service.RecoveryMetrics().Snapshot()
	if metrics["presence_error"] < 1 {
		t.Fatalf("expected presence_error >= 1, got %d", metrics["presence_error"])
	}
	if metrics["presence_replayed"] != 0 {
		t.Fatalf("expected presence_replayed == 0 on presence error, got %d", metrics["presence_replayed"])
	}
}

// TestPresence_AdapterWithoutCapabilityRemainsPending verifies that adapters without
// OperationPresenceReader capability remain safely parked in pending status without replay.
func TestPresence_AdapterWithoutCapabilityRemainsPending(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)

	ctx := context.Background()
	setBalances(t, data, 10000, 0)

	secondBankID := uuid.New()
	if _, err := data.pool.Exec(ctx, `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Bank B', 'ACTIVE')`, secondBankID, "BANK-B-"+secondBankID.String()[:12]); err != nil {
		t.Fatal(err)
	}

	plainA := &noPresenceAdapter{}

	adapters := map[uuid.UUID]bank.BankAdapter{
		data.bankID:  plainA,
		secondBankID: successfulBankAdapter{},
	}

	service := NewServiceWithAdapters(
		accounts.NewRepository(data.pool),
		recipients.NewRepository(data.pool),
		data.repository,
		adapters,
	)

	payment, _, err := data.repository.CreateRoutedIdempotent(
		ctx,
		settlementPayment(data, 1500),
		"pres-nocap-key",
		"pres-nocap-hash",
		data.bankID,
		secondBankID,
		plainA,
		successfulBankAdapter{},
	)
	if err != nil {
		t.Fatalf("CreateRoutedIdempotent failed: %v", err)
	}

	resumeRes, err := service.Resume(ctx, payment.ID)
	if err != nil {
		t.Fatalf("Resume returned unexpected error: %v", err)
	}
	if resumeRes.Status == ResumeStatusCompleted {
		t.Fatal("expected payment to remain pending when adapter lacks presence capability, but completed")
	}

	metrics := service.RecoveryMetrics().Snapshot()
	if metrics["presence_replayed"] != 0 {
		t.Fatalf("expected presence_replayed == 0 when adapter lacks capability, got %d", metrics["presence_replayed"])
	}
}

// TestPresence_ChaosAdapterPreservesCapabilityAndReplays verifies that ChaosAdapter
// implementing bank.OperationPresenceReader forwards presence checks to the underlying adapter
// and enables authoritative replay recovery.
func TestPresence_ChaosAdapterPreservesCapabilityAndReplays(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)

	ctx := context.Background()
	setBalances(t, data, 10000, 0)

	secondBankID := uuid.New()
	if _, err := data.pool.Exec(ctx, `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Bank B', 'ACTIVE')`, secondBankID, "BANK-B-"+secondBankID.String()[:12]); err != nil {
		t.Fatal(err)
	}

	sourceSpy := &presenceSpyAdapter{
		presenceFn: func(ctx context.Context, pID, opID uuid.UUID) (bank.OperationPresenceResult, error) {
			return bank.OperationPresenceResult{
				PaymentID:   pID,
				OperationID: opID,
				Presence:    bank.PresenceAbsent,
				ObservedAt:  time.Now().UTC(),
			}, nil
		},
		holdFn: func(ctx context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
			return bank.HoldResult{}, &bank.AdapterError{
				Code:    bank.ErrCodeTransientFailure,
				Message: "initial hold simulated crash",
			}
		},
	}

	ctrl := chaos.NewController(nil)
	chaosSource := chaos.NewChaosAdapter("BANK-A", sourceSpy, ctrl)

	// Verify type assertion succeeds
	if _, ok := any(chaosSource).(bank.OperationPresenceReader); !ok {
		t.Fatal("expected ChaosAdapter to implement bank.OperationPresenceReader")
	}

	adapters := map[uuid.UUID]bank.BankAdapter{
		data.bankID:  chaosSource,
		secondBankID: successfulBankAdapter{},
	}

	service := NewServiceWithAdapters(
		accounts.NewRepository(data.pool),
		recipients.NewRepository(data.pool),
		data.repository,
		adapters,
	)

	payment, _, err := data.repository.CreateRoutedIdempotent(
		ctx,
		settlementPayment(data, 1500),
		"pres-chaos-key",
		"pres-chaos-hash",
		data.bankID,
		secondBankID,
		chaosSource,
		successfulBankAdapter{},
	)
	if err != nil {
		t.Fatalf("CreateRoutedIdempotent failed: %v", err)
	}

	if payment.State != StatePendingReconciliation {
		t.Fatalf("expected StatePendingReconciliation, got %s", payment.State)
	}

	// Now fix holdFn to succeed on replay
	sourceSpy.holdFn = nil
	sourceSpy.holdsCalled = 0

	resumeRes, err := service.Resume(ctx, payment.ID)
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	if resumeRes.Status != ResumeStatusCompleted {
		t.Fatalf("expected ResumeStatusCompleted via chaos presence replay, got %s (reason: %s)", resumeRes.Status, resumeRes.Reason)
	}
	if sourceSpy.holdsCalled != 1 {
		t.Fatalf("expected exactly 1 replay hold call, got %d", sourceSpy.holdsCalled)
	}

	metrics := service.RecoveryMetrics().Snapshot()
	if metrics["presence_replayed"] < 1 {
		t.Fatalf("expected presence_replayed >= 1, got %d", metrics["presence_replayed"])
	}
}

