package reconciliation_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/payments"
	"github.com/transactx/backend/internal/reconciliation"
)

// Helper: build a baseline valid financial state where every check passes cleanly.
func newValidBaselineStore(t *testing.T) (*reconciliation.MemoryFinancialDataStore, reconciliation.Scope, string) {
	t.Helper()
	store := reconciliation.NewMemoryFinancialDataStore()

	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{
		From: base,
		To:   base.Add(4 * time.Hour),
	}
	participantID := "BANK-A"

	user1 := uuid.New()
	user2 := uuid.New()
	bankID := uuid.New()
	acc1ID := uuid.New()
	acc2ID := uuid.New()
	payID := uuid.New()
	ltID := uuid.New()
	opID := uuid.New()

	store.Accounts = []reconciliation.FinancialAccount{
		{
			ID:                  acc1ID,
			UserID:              user1,
			BankID:              bankID,
			AccountNumber:       "ACC-1001",
			BalancePaise:        50000,
			OpeningBalancePaise: 50000,
			Status:              "ACTIVE",
		},
		{
			ID:                  acc2ID,
			UserID:              user2,
			BankID:              bankID,
			AccountNumber:       "ACC-1002",
			BalancePaise:        25000,
			OpeningBalancePaise: 25000,
			Status:              "ACTIVE",
		},
	}

	compTime := base.Add(30 * time.Minute)
	store.Payments = []reconciliation.FinancialPayment{
		{
			ID:                payID,
			InitiatedByUserID: user1,
			SenderAccountID:   acc1ID,
			ReceiverAccountID: acc2ID,
			AmountPaise:       1000,
			Currency:          "INR",
			State:             payments.StateCompleted,
			CreatedAt:         base.Add(15 * time.Minute),
			CompletedAt:       &compTime,
		},
	}

	store.LedgerTransactions = []reconciliation.FinancialLedgerTransaction{
		{
			ID:        ltID,
			PaymentID: payID,
			CreatedAt: base.Add(20 * time.Minute),
			Entries: []reconciliation.FinancialLedgerEntry{
				{
					ID:          uuid.New(),
					AccountID:   acc1ID,
					EntryType:   "DEBIT",
					AmountPaise: 1000,
					CreatedAt:   base.Add(20 * time.Minute),
				},
				{
					ID:          uuid.New(),
					AccountID:   acc2ID,
					EntryType:   "CREDIT",
					AmountPaise: 1000,
					CreatedAt:   base.Add(20 * time.Minute),
				},
			},
		},
	}

	store.IdempotencyRecords = []reconciliation.FinancialIdempotencyRecord{
		{
			ID:          uuid.New(),
			UserID:      user1,
			Key:         "idemp-key-101",
			RequestHash: "hash-101",
			PaymentID:   &payID,
			CreatedAt:   base.Add(10 * time.Minute),
		},
	}

	store.BankOperations = []reconciliation.FinancialBankOperation{
		{
			ID:            uuid.New(),
			PaymentID:     payID,
			BankID:        bankID,
			OperationID:   opID,
			OperationType: "HOLD",
			Status:        "CONFIRMED",
			AmountPaise:   1000,
			Currency:      "INR",
			CreatedAt:     base.Add(16 * time.Minute),
		},
	}

	// Create a valid Merkle record and calculate its expected root using authoritative Merkle ledger
	canonRec := reconciliation.CanonicalRecord{
		OperationID: opID,
		PaymentID:   payID,
		AccountID:   acc1ID,
		EntryType:   "DEBIT",
		AmountPaise: 1000,
		Currency:    "INR",
		OccurredAt:  base.Add(16 * time.Minute),
	}
	records := []reconciliation.CanonicalRecord{canonRec}

	ledger, err := reconciliation.NewIncrementalMerkleLedger(participantID, 1*time.Hour, scope)
	if err != nil {
		t.Fatalf("NewIncrementalMerkleLedger: %v", err)
	}
	rebuild, err := ledger.Bootstrap(context.Background(), records)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	key := reconciliationScopeKey(participantID, scope)
	store.MerkleRoots[key] = rebuild.ResultingRoot
	store.MerkleRecords[key] = records

	return store, scope, participantID
}

func reconciliationScopeKey(participantID string, scope reconciliation.Scope) string {
	return fmt.Sprintf("%s:%d:%d", participantID, scope.From.UnixNano(), scope.To.UnixNano())
}

// -----------------------------------------------------------------------------
// Baseline Health Check
// -----------------------------------------------------------------------------

func TestBaselineIntegrityPassesAllChecks(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)
	runStore := reconciliation.NewMemoryIntegrityRunStore()
	engine := reconciliation.NewRuntimeIntegrityEngine(store, runStore)

	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
	})
	if err != nil {
		t.Fatalf("engine.Run failed: %v", err)
	}

	if res.Status != reconciliation.IntegrityRunStatusCompleted {
		t.Fatalf("expected status COMPLETED, got %s", res.Status)
	}
	if res.Summary.Passed != len(reconciliation.AuthoritativeCheckRegistry) {
		t.Fatalf("expected %d passed checks, got %d (summary: %+v)", len(reconciliation.AuthoritativeCheckRegistry), res.Summary.Passed, res.Summary)
	}
	if res.Summary.Failed != 0 || res.Summary.Errors != 0 {
		t.Fatalf("expected 0 fails and 0 errors, got fails=%d, errors=%d", res.Summary.Failed, res.Summary.Errors)
	}
}

// -----------------------------------------------------------------------------
// Required Invariant Violation Tests (Section 8)
// -----------------------------------------------------------------------------

// TestDebitCreditConservationViolation tests that unbalanced debits/credits cause FAIL.
func TestDebitCreditConservationViolation(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	// Tamper with credit entry so debits != credits (debit: 1000, credit: 800)
	store.LedgerTransactions[0].Entries[1].AmountPaise = 800

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	var found bool
	for _, check := range res.Checks {
		if check.Code == reconciliation.CheckDebitCreditConservation {
			found = true
			if check.Status != reconciliation.CheckStatusFail {
				t.Fatalf("expected DEBIT_CREDIT_CONSERVATION to FAIL, got %s", check.Status)
			}
			if len(check.Violations) == 0 {
				t.Fatalf("expected violations to be recorded, got 0")
			}
		}
	}
	if !found {
		t.Fatalf("check %s not found in results", reconciliation.CheckDebitCreditConservation)
	}
}

// TestNegativeBalanceViolation tests that negative account balances cause FAIL.
func TestNegativeBalanceViolation(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	// Inject negative balance into account 0
	store.Accounts[0].BalancePaise = -2500

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	var found bool
	for _, check := range res.Checks {
		if check.Code == reconciliation.CheckNonNegativeBalances {
			found = true
			if check.Status != reconciliation.CheckStatusFail {
				t.Fatalf("expected NON_NEGATIVE_BALANCES to FAIL, got %s", check.Status)
			}
			if len(check.Violations) == 0 {
				t.Fatalf("expected violations to be recorded, got 0")
			}
		}
	}
	if !found {
		t.Fatalf("check %s not found in results", reconciliation.CheckNonNegativeBalances)
	}
}

// TestDuplicateTransactionViolation tests that duplicate payment or bank operation identities cause FAIL.
func TestDuplicateTransactionViolation(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	// Duplicate the existing payment identity
	dupPayment := store.Payments[0]
	store.Payments = append(store.Payments, dupPayment)

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	var found bool
	for _, check := range res.Checks {
		if check.Code == reconciliation.CheckTransactionUniqueness {
			found = true
			if check.Status != reconciliation.CheckStatusFail {
				t.Fatalf("expected TRANSACTION_UNIQUENESS to FAIL, got %s", check.Status)
			}
			if len(check.Violations) == 0 {
				t.Fatalf("expected violations to be recorded, got 0")
			}
		}
	}
	if !found {
		t.Fatalf("check %s not found in results", reconciliation.CheckTransactionUniqueness)
	}
}

// TestIdempotencyViolation tests that invalid idempotency mapping causes FAIL.
func TestIdempotencyViolation(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	// Create an idempotency record for user1 that points to a payment initiated by user2
	otherUser := store.Accounts[1].UserID
	foreignPayID := uuid.New()
	store.Payments = append(store.Payments, reconciliation.FinancialPayment{
		ID:                foreignPayID,
		InitiatedByUserID: otherUser, // user2
		SenderAccountID:   store.Accounts[1].ID,
		ReceiverAccountID: store.Accounts[0].ID,
		AmountPaise:       500,
		Currency:          "INR",
		State:             payments.StateCompleted,
		CreatedAt:         scope.From.Add(10 * time.Minute),
	})

	store.IdempotencyRecords = append(store.IdempotencyRecords, reconciliation.FinancialIdempotencyRecord{
		ID:          uuid.New(),
		UserID:      store.Accounts[0].UserID, // user1 claims foreign payment!
		Key:         "malicious-stolen-key",
		RequestHash: "hash-stolen",
		PaymentID:   &foreignPayID,
		CreatedAt:   scope.From.Add(5 * time.Minute),
	})

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	var found bool
	for _, check := range res.Checks {
		if check.Code == reconciliation.CheckIdempotencyMapping {
			found = true
			if check.Status != reconciliation.CheckStatusFail {
				t.Fatalf("expected IDEMPOTENCY_MAPPING to FAIL, got %s", check.Status)
			}
			if len(check.Violations) == 0 {
				t.Fatalf("expected violations to be recorded, got 0")
			}
		}
	}
	if !found {
		t.Fatalf("check %s not found in results", reconciliation.CheckIdempotencyMapping)
	}
}

// TestInvalidStateTransition tests that illegal transitions (e.g. PENDING_RECONCILIATION -> COMPLETED) cause FAIL.
func TestInvalidStateTransition(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	// Inject an illegal historical state transition: PENDING_RECONCILIATION -> COMPLETED
	store.StateTransitions = []reconciliation.FinancialStateTransition{
		{
			PaymentID:      store.Payments[0].ID,
			FromState:      payments.StatePendingReconciliation,
			ToState:        payments.StateCompleted, // Illegal transition! Must go through COMMITTED first
			TransitionedAt: scope.From.Add(25 * time.Minute),
		},
	}

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	var found bool
	for _, check := range res.Checks {
		if check.Code == reconciliation.CheckPaymentStateValidity {
			found = true
			if check.Status != reconciliation.CheckStatusFail {
				t.Fatalf("expected PAYMENT_STATE_VALIDITY to FAIL, got %s", check.Status)
			}
			if len(check.Violations) == 0 {
				t.Fatalf("expected violations to be recorded, got 0")
			}
		}
	}
	if !found {
		t.Fatalf("check %s not found in results", reconciliation.CheckPaymentStateValidity)
	}
}

// TestCompletedPaymentLedgerMissing tests that a completed payment without ledger rows causes FAIL.
func TestCompletedPaymentLedgerMissing(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	// Clear ledger transactions so completed payment has no representation
	store.LedgerTransactions = nil

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	var found bool
	for _, check := range res.Checks {
		if check.Code == reconciliation.CheckCompletedPaymentLedgerCompleteness {
			found = true
			if check.Status != reconciliation.CheckStatusFail {
				t.Fatalf("expected COMPLETED_PAYMENT_LEDGER_COMPLETENESS to FAIL, got %s", check.Status)
			}
			if len(check.Violations) == 0 {
				t.Fatalf("expected violations to be recorded, got 0")
			}
		}
	}
	if !found {
		t.Fatalf("check %s not found in results", reconciliation.CheckCompletedPaymentLedgerCompleteness)
	}
}

// TestMerkleCommitmentMismatch tests that a tampered Merkle root causes FAIL.
func TestMerkleCommitmentMismatch(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	// Tamper with the maintained Merkle root
	key := reconciliationScopeKey(participantID, scope)
	tampered := append([]byte(nil), store.MerkleRoots[key]...)
	tampered[0] ^= 0xFF
	store.MerkleRoots[key] = tampered

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	var found bool
	for _, check := range res.Checks {
		if check.Code == reconciliation.CheckMerkleCommitmentConsistency {
			found = true
			if check.Status != reconciliation.CheckStatusFail {
				t.Fatalf("expected MERKLE_COMMITMENT_CONSISTENCY to FAIL, got %s", check.Status)
			}
			if len(check.Violations) == 0 {
				t.Fatalf("expected violations to be recorded, got 0")
			}
		}
	}
	if !found {
		t.Fatalf("check %s not found in results", reconciliation.CheckMerkleCommitmentConsistency)
	}
}

// -----------------------------------------------------------------------------
// Required Behavioral Tests (Section 8)
// -----------------------------------------------------------------------------

// TestAllChecksRunAfterOneFailure proves that one failed check does not abort execution of later checks.
func TestAllChecksRunAfterOneFailure(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	// Cause check 1 (DEBIT_CREDIT_CONSERVATION) to FAIL
	store.LedgerTransactions[0].Entries[1].AmountPaise = 1

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	// Verify all 7 registered checks were evaluated
	if len(res.Checks) != len(reconciliation.AuthoritativeCheckRegistry) {
		t.Fatalf("expected %d checks to run, got %d", len(reconciliation.AuthoritativeCheckRegistry), len(res.Checks))
	}
	if res.Summary.TotalChecks != len(reconciliation.AuthoritativeCheckRegistry) {
		t.Fatalf("expected TotalChecks=%d, got %d", len(reconciliation.AuthoritativeCheckRegistry), res.Summary.TotalChecks)
	}

	// Check 1 failed, but others passed
	if res.Summary.Failed < 1 {
		t.Fatalf("expected at least 1 failed check, got %d", res.Summary.Failed)
	}
	if res.Summary.Passed < 5 {
		t.Fatalf("expected remaining checks to pass, got %d passed", res.Summary.Passed)
	}
}

// TestCheckExecutionErrorIsNotPass verifies that a database/query failure produces ERROR, never PASS.
func TestCheckExecutionErrorIsNotPass(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	// Inject a fatal query error into the data store
	expectedErr := errors.New("simulated network partition to database")
	store.SimulatedError = expectedErr

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	if res.Summary.Errors == 0 {
		t.Fatalf("expected errors to be counted in summary, got 0")
	}
	if res.Summary.Passed != 0 {
		t.Fatalf("expected 0 checks to pass on store failure, got %d", res.Summary.Passed)
	}

	for _, check := range res.Checks {
		if check.Status == reconciliation.CheckStatusPass {
			t.Fatalf("check %s returned PASS during store outage! Must be ERROR", check.Code)
		}
		if check.Status != reconciliation.CheckStatusError {
			t.Fatalf("check %s status=%s, want ERROR", check.Code, check.Status)
		}
		if check.Error == "" {
			t.Fatalf("check %s missing error description", check.Code)
		}
	}
}

// TestIntegrityRunPersisted verifies that run results are durably stored and retrievable.
func TestIntegrityRunPersisted(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)
	runStore := reconciliation.NewMemoryIntegrityRunStore()
	engine := reconciliation.NewRuntimeIntegrityEngine(store, runStore)

	runRes, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	// Retrieve by run ID
	retrieved, err := runStore.GetRun(ctx, runRes.RunID)
	if err != nil {
		t.Fatalf("runStore.GetRun: %v", err)
	}
	if retrieved.RunID != runRes.RunID {
		t.Fatalf("retrieved RunID %s != original %s", retrieved.RunID, runRes.RunID)
	}
	if len(retrieved.Checks) != len(runRes.Checks) {
		t.Fatalf("retrieved %d checks, want %d", len(retrieved.Checks), len(runRes.Checks))
	}

	// Retrieve by list
	list, total, err := runStore.ListRuns(ctx, 10, 0)
	if err != nil {
		t.Fatalf("runStore.ListRuns: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("expected 1 run in list, got total=%d, len=%d", total, len(list))
	}
	if list[0].RunID != runRes.RunID {
		t.Fatalf("listed RunID %s != original %s", list[0].RunID, runRes.RunID)
	}
}

// TestIntegrityReadOnly verifies that running integrity checks leaves financial state strictly untouched.
func TestIntegrityReadOnly(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	// Take full pre-run snapshot of financial state
	preAccounts := make([]reconciliation.FinancialAccount, len(store.Accounts))
	copy(preAccounts, store.Accounts)

	prePayments := make([]reconciliation.FinancialPayment, len(store.Payments))
	copy(prePayments, store.Payments)

	preTxs, _ := store.GetLedgerTransactions(ctx, nil)
	preIdemp, _ := store.GetIdempotencyRecords(ctx)
	preOps, _ := store.GetBankOperations(ctx, nil)

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	_, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	// Compare post-run financial state with pre-run snapshot
	if !reflect.DeepEqual(store.Accounts, preAccounts) {
		t.Fatalf("VIOLATION: accounts mutated during integrity execution!\nBefore: %+v\nAfter: %+v", preAccounts, store.Accounts)
	}
	if !reflect.DeepEqual(store.Payments, prePayments) {
		t.Fatalf("VIOLATION: payments mutated during integrity execution!\nBefore: %+v\nAfter: %+v", prePayments, store.Payments)
	}
	postTxs, _ := store.GetLedgerTransactions(ctx, nil)
	if !reflect.DeepEqual(postTxs, preTxs) {
		t.Fatalf("VIOLATION: ledger transactions mutated during integrity execution!")
	}
	postIdemp, _ := store.GetIdempotencyRecords(ctx)
	if !reflect.DeepEqual(postIdemp, preIdemp) {
		t.Fatalf("VIOLATION: idempotency records mutated during integrity execution!")
	}
	postOps, _ := store.GetBankOperations(ctx, nil)
	if !reflect.DeepEqual(postOps, preOps) {
		t.Fatalf("VIOLATION: bank operations mutated during integrity execution!")
	}
}

// TestStableCheckCodes verifies that authoritative check codes and severities remain stable.
func TestStableCheckCodes(t *testing.T) {
	expectedCodes := map[string]reconciliation.IntegritySeverity{
		"DEBIT_CREDIT_CONSERVATION":             reconciliation.SeverityCritical,
		"NON_NEGATIVE_BALANCES":                 reconciliation.SeverityCritical,
		"TRANSACTION_UNIQUENESS":               reconciliation.SeverityCritical,
		"IDEMPOTENCY_MAPPING":                   reconciliation.SeverityHigh,
		"PAYMENT_STATE_VALIDITY":                reconciliation.SeverityHigh,
		"COMPLETED_PAYMENT_LEDGER_COMPLETENESS": reconciliation.SeverityCritical,
		"MERKLE_COMMITMENT_CONSISTENCY":         reconciliation.SeverityCritical,
	}

	if len(reconciliation.AuthoritativeCheckRegistry) != len(expectedCodes) {
		t.Fatalf("registry count = %d, want %d", len(reconciliation.AuthoritativeCheckRegistry), len(expectedCodes))
	}

	for _, check := range reconciliation.AuthoritativeCheckRegistry {
		expectedSev, exists := expectedCodes[check.Code]
		if !exists {
			t.Fatalf("unexpected check code in registry: %s", check.Code)
		}
		if check.Severity != expectedSev {
			t.Fatalf("check %s severity = %s, want %s", check.Code, check.Severity, expectedSev)
		}
		if check.Description == "" {
			t.Fatalf("check %s has empty description", check.Code)
		}
	}
}

// TestUnknownStateHistoryDoesNotInventTransition verifies that when schema lacks transition history,
// the check validates current states without fabricating intermediate transitions.
func TestUnknownStateHistoryDoesNotInventTransition(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	// Ensure StateTransitions slice is explicitly empty (resembling the current database schema)
	store.StateTransitions = nil

	// The baseline payment is in COMPLETED state. In the absence of an audit log, the check validates
	// that COMPLETED is a recognized valid payment state and does NOT fabricate a fake transition log.
	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
		CheckCodes:    []string{reconciliation.CheckPaymentStateValidity},
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	if len(res.Checks) != 1 {
		t.Fatalf("expected 1 check result, got %d", len(res.Checks))
	}
	if res.Checks[0].Status != reconciliation.CheckStatusPass {
		t.Fatalf("expected PAYMENT_STATE_VALIDITY to PASS on valid current state, got %s (violations: %+v)", res.Checks[0].Status, res.Checks[0].Violations)
	}
}
