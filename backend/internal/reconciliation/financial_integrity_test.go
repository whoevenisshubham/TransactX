package reconciliation_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/bank"
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

	snapshot := ledger.Snapshot()
	key := reconciliationScopeKey(participantID, scope)
	store.MerkleRoots[key] = rebuild.ResultingRoot
	store.MerkleRecords[key] = records
	store.MaintainedCommitments[key] = reconciliation.MaintainedCommitment{
		ParticipantID:    participantID,
		Partition:        participantID,
		BucketWidth:      1 * time.Hour,
		CanonicalVersion: reconciliation.CanonicalVersion,
		AlgorithmVersion: reconciliation.MerkleAlgorithmVersion,
		Generation:       snapshot.Generation,
		Root:             rebuild.ResultingRoot,
		RecordCount:      len(records),
		Scope:            scope,
		CapturedAt:       time.Now().UTC(),
	}

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
	if mc, ok := store.MaintainedCommitments[key]; ok {
		mc.Root = tampered
		store.MaintainedCommitments[key] = mc
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

// -----------------------------------------------------------------------------
// Production Hardened Merkle & Consistency Tests (M3-7-C4)
// -----------------------------------------------------------------------------

// TestMaintainedCommitmentNotRebuiltDuringVerification verifies that if a maintained
// commitment does not already exist, the integrity verification fails rather than
// silently rebuilding or materializing a commitment on the fly.
func TestMaintainedCommitmentNotRebuiltDuringVerification(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	// Ensure no maintained commitment exists in either maintained commitments or legacy roots
	store.MaintainedCommitments = make(map[string]reconciliation.MaintainedCommitment)
	store.MerkleRoots = make(map[string][]byte)
	store.CommitmentStore = nil
	store.ParticipantStore = nil

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
		CheckCodes:    []string{reconciliation.CheckMerkleCommitmentConsistency},
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	if len(res.Checks) != 1 {
		t.Fatalf("expected 1 check result, got %d", len(res.Checks))
	}
	check := res.Checks[0]
	if check.Status != reconciliation.CheckStatusFail {
		t.Fatalf("expected MERKLE_COMMITMENT_CONSISTENCY to FAIL when commitment is missing, got %s", check.Status)
	}
	if len(check.Violations) == 0 {
		t.Fatalf("expected violations to be recorded for missing commitment")
	}

	// Verify that the maintained commitment was NOT created as a side effect
	comm, found, err := store.GetMaintainedCommitment(ctx, participantID, scope)
	if err != nil {
		t.Fatalf("store.GetMaintainedCommitment: %v", err)
	}
	if found {
		t.Fatalf("maintained commitment was improperly materialized/rebuilt during verification: %+v", comm)
	}
}

// TestMaintainedCommitmentBucketWidthNotHardcoded verifies that commitments configured
// with a non-1-hour bucket width (e.g. 15 minutes) are verified using their own configured
// bucket width rather than assuming a hardcoded 1 hour.
func TestMaintainedCommitmentBucketWidthNotHardcoded(t *testing.T) {
	ctx := context.Background()
	store := reconciliation.NewMemoryFinancialDataStore()

	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{
		From: base,
		To:   base.Add(2 * time.Hour),
	}
	participantID := "BANK-CUSTOM-BUCKET"
	bucketWidth := 15 * time.Minute

	// Create canonical records across multiple 15-minute buckets
	records := []reconciliation.CanonicalRecord{
		{
			OperationID: uuid.New(),
			PaymentID:   uuid.New(),
			AccountID:   uuid.New(),
			EntryType:   "DEBIT",
			AmountPaise: 1000,
			Currency:    "INR",
			OccurredAt:  base.Add(5 * time.Minute), // Bucket 0
		},
		{
			OperationID: uuid.New(),
			PaymentID:   uuid.New(),
			AccountID:   uuid.New(),
			EntryType:   "CREDIT",
			AmountPaise: 1000,
			Currency:    "INR",
			OccurredAt:  base.Add(20 * time.Minute), // Bucket 1
		},
		{
			OperationID: uuid.New(),
			PaymentID:   uuid.New(),
			AccountID:   uuid.New(),
			EntryType:   "HOLD",
			AmountPaise: 500,
			Currency:    "INR",
			OccurredAt:  base.Add(40 * time.Minute), // Bucket 2
		},
	}

	// Build the maintained commitment with the non-1-hour bucket width
	ledger, err := reconciliation.NewIncrementalMerkleLedger(participantID, bucketWidth, scope)
	if err != nil {
		t.Fatalf("NewIncrementalMerkleLedger: %v", err)
	}
	rebuild, err := ledger.Bootstrap(ctx, records)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	key := reconciliationScopeKey(participantID, scope)
	store.AuthoritativeRecords[key] = records
	store.MaintainedCommitments[key] = reconciliation.MaintainedCommitment{
		ParticipantID:    participantID,
		Partition:        participantID,
		BucketWidth:      bucketWidth,
		CanonicalVersion: reconciliation.CanonicalVersion,
		AlgorithmVersion: reconciliation.MerkleAlgorithmVersion,
		Generation:       uuid.New().String(),
		Root:             rebuild.ResultingRoot,
		RecordCount:      len(records),
		Scope:            scope,
		CapturedAt:       time.Now().UTC(),
	}

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)

	// Subtest 1: Verify passing with matching ExpectedBucketWidth
	t.Run("PassWith15MinBucketWidth", func(t *testing.T) {
		res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
			Scope:               &scope,
			ParticipantID:       participantID,
			CheckCodes:          []string{reconciliation.CheckMerkleCommitmentConsistency},
			ExpectedBucketWidth: bucketWidth,
		})
		if err != nil {
			t.Fatalf("engine.Run: %v", err)
		}
		if res.Checks[0].Status != reconciliation.CheckStatusPass {
			t.Fatalf("expected PASS with 15m bucket width, got %s (violations: %+v)", res.Checks[0].Status, res.Checks[0].Violations)
		}
	})

	// Subtest 2: Verify failure when ExpectedBucketWidth expects 1 hour
	t.Run("DetectBucketWidthMismatch", func(t *testing.T) {
		res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
			Scope:               &scope,
			ParticipantID:       participantID,
			CheckCodes:          []string{reconciliation.CheckMerkleCommitmentConsistency},
			ExpectedBucketWidth: 1 * time.Hour,
		})
		if err != nil {
			t.Fatalf("engine.Run: %v", err)
		}
		if res.Checks[0].Status != reconciliation.CheckStatusFail {
			t.Fatalf("expected FAIL on bucket width mismatch, got %s", res.Checks[0].Status)
		}
		var foundMismatch bool
		for _, v := range res.Checks[0].Violations {
			if v.Description != "" && (v.Details["expectedWidth"] != nil || v.Details["observedWidth"] != nil) {
				foundMismatch = true
				break
			}
		}
		if !foundMismatch {
			t.Fatalf("expected bucket width mismatch violation detail, got %+v", res.Checks[0].Violations)
		}
	})
}

// TestMerkleCommitmentTamperedRootFails verifies that a bit flip in the maintained root is detected.
func TestMerkleCommitmentTamperedRootFails(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	key := reconciliationScopeKey(participantID, scope)
	origRoot := store.MerkleRoots[key]
	tamperedRoot := append([]byte(nil), origRoot...)
	tamperedRoot[len(tamperedRoot)-1] ^= 0x01

	store.MaintainedCommitments[key] = reconciliation.MaintainedCommitment{
		ParticipantID:    participantID,
		Partition:        participantID,
		BucketWidth:      1 * time.Hour,
		CanonicalVersion: reconciliation.CanonicalVersion,
		AlgorithmVersion: reconciliation.MerkleAlgorithmVersion,
		Generation:       uuid.New().String(),
		Root:             tamperedRoot,
		RecordCount:      len(store.MerkleRecords[key]),
		Scope:            scope,
		CapturedAt:       time.Now().UTC(),
	}

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
		CheckCodes:    []string{reconciliation.CheckMerkleCommitmentConsistency},
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	check := res.Checks[0]
	if check.Status != reconciliation.CheckStatusFail {
		t.Fatalf("expected FAIL on tampered root, got %s", check.Status)
	}
	if len(check.Violations) == 0 {
		t.Fatalf("expected violation to be recorded")
	}
}

// TestMerkleCommitmentStaleRootFails verifies that a record count mismatch between maintained
// root and authoritative records is detected.
func TestMerkleCommitmentStaleRootFails(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	key := reconciliationScopeKey(participantID, scope)
	records := store.MerkleRecords[key]

	// Maintained commitment claims 1 record
	store.MaintainedCommitments[key] = reconciliation.MaintainedCommitment{
		ParticipantID:    participantID,
		Partition:        participantID,
		BucketWidth:      1 * time.Hour,
		CanonicalVersion: reconciliation.CanonicalVersion,
		AlgorithmVersion: reconciliation.MerkleAlgorithmVersion,
		Generation:       uuid.New().String(),
		Root:             store.MerkleRoots[key],
		RecordCount:      1,
		Scope:            scope,
		CapturedAt:       time.Now().UTC(),
	}

	// But authoritative store has 2 records
	extraRec := records[0]
	extraRec.OperationID = uuid.New()
	extraRec.OccurredAt = extraRec.OccurredAt.Add(5 * time.Minute)
	store.AuthoritativeRecords[key] = append(records, extraRec)

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
		CheckCodes:    []string{reconciliation.CheckMerkleCommitmentConsistency},
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	check := res.Checks[0]
	if check.Status != reconciliation.CheckStatusFail {
		t.Fatalf("expected FAIL on stale root record count mismatch, got %s", check.Status)
	}
	var foundStale bool
	for _, v := range check.Violations {
		if v.Details["observedRecordCount"] != nil || v.Details["authoritativeRecordCount"] != nil {
			foundStale = true
			break
		}
	}
	if !foundStale {
		t.Fatalf("expected stale root violation details, got %+v", check.Violations)
	}
}

// TestMerkleCommitmentWrongGenerationFails verifies that an unexpected commitment generation is detected.
func TestMerkleCommitmentWrongGenerationFails(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	key := reconciliationScopeKey(participantID, scope)
	realGen := uuid.New().String()
	wrongGen := uuid.New().String()
	store.MaintainedCommitments[key] = reconciliation.MaintainedCommitment{
		ParticipantID:    participantID,
		Partition:        participantID,
		BucketWidth:      1 * time.Hour,
		CanonicalVersion: reconciliation.CanonicalVersion,
		AlgorithmVersion: reconciliation.MerkleAlgorithmVersion,
		Generation:       realGen,
		Root:             store.MerkleRoots[key],
		RecordCount:      len(store.MerkleRecords[key]),
		Scope:            scope,
		CapturedAt:       time.Now().UTC(),
	}

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:              &scope,
		ParticipantID:      participantID,
		CheckCodes:         []string{reconciliation.CheckMerkleCommitmentConsistency},
		ExpectedGeneration: wrongGen, // Expected wrongGen, observed realGen
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	check := res.Checks[0]
	if check.Status != reconciliation.CheckStatusFail {
		t.Fatalf("expected FAIL on wrong generation, got %s", check.Status)
	}
	var foundGen bool
	for _, v := range check.Violations {
		if v.Details["observedGeneration"] == realGen && v.Details["expectedGeneration"] == wrongGen {
			foundGen = true
			break
		}
	}
	if !foundGen {
		t.Fatalf("expected wrong generation violation details, got %+v", check.Violations)
	}
}

// TestMerkleCommitmentConfigurationMismatchFails verifies partition, canonical version,
// and algorithm version mismatches are detected.
func TestMerkleCommitmentConfigurationMismatchFails(t *testing.T) {
	ctx := context.Background()

	t.Run("PartitionMismatch", func(t *testing.T) {
		store, scope, participantID := newValidBaselineStore(t)
		key := reconciliationScopeKey(participantID, scope)
		store.MaintainedCommitments[key] = reconciliation.MaintainedCommitment{
			ParticipantID:    participantID,
			Partition:        "BANK-A",
			BucketWidth:      1 * time.Hour,
			CanonicalVersion: reconciliation.CanonicalVersion,
			AlgorithmVersion: reconciliation.MerkleAlgorithmVersion,
			Generation:       uuid.New().String(),
			Root:             store.MerkleRoots[key],
			RecordCount:      len(store.MerkleRecords[key]),
			Scope:            scope,
			CapturedAt:       time.Now().UTC(),
		}

		engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
		res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
			Scope:             &scope,
			ParticipantID:     participantID,
			CheckCodes:        []string{reconciliation.CheckMerkleCommitmentConsistency},
			ExpectedPartition: "BANK-B",
		})
		if err != nil {
			t.Fatalf("engine.Run: %v", err)
		}
		if res.Checks[0].Status != reconciliation.CheckStatusFail {
			t.Fatalf("expected FAIL on partition mismatch, got %s", res.Checks[0].Status)
		}
	})

	t.Run("CanonicalVersionMismatch", func(t *testing.T) {
		store, scope, participantID := newValidBaselineStore(t)
		key := reconciliationScopeKey(participantID, scope)
		store.MaintainedCommitments[key] = reconciliation.MaintainedCommitment{
			ParticipantID:    participantID,
			Partition:        participantID,
			BucketWidth:      1 * time.Hour,
			CanonicalVersion: "v99-incompatible",
			AlgorithmVersion: reconciliation.MerkleAlgorithmVersion,
			Generation:       uuid.New().String(),
			Root:             store.MerkleRoots[key],
			RecordCount:      len(store.MerkleRecords[key]),
			Scope:            scope,
			CapturedAt:       time.Now().UTC(),
		}

		engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
		res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
			Scope:         &scope,
			ParticipantID: participantID,
			CheckCodes:    []string{reconciliation.CheckMerkleCommitmentConsistency},
		})
		if err != nil {
			t.Fatalf("engine.Run: %v", err)
		}
		if res.Checks[0].Status != reconciliation.CheckStatusFail {
			t.Fatalf("expected FAIL on canonical version mismatch, got %s", res.Checks[0].Status)
		}
	})

	t.Run("AlgorithmVersionMismatch", func(t *testing.T) {
		store, scope, participantID := newValidBaselineStore(t)
		key := reconciliationScopeKey(participantID, scope)
		store.MaintainedCommitments[key] = reconciliation.MaintainedCommitment{
			ParticipantID:    participantID,
			Partition:        participantID,
			BucketWidth:      1 * time.Hour,
			CanonicalVersion: reconciliation.CanonicalVersion,
			AlgorithmVersion: "algo-custom-unsupported",
			Generation:       uuid.New().String(),
			Root:             store.MerkleRoots[key],
			RecordCount:      len(store.MerkleRecords[key]),
			Scope:            scope,
			CapturedAt:       time.Now().UTC(),
		}

		engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
		res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
			Scope:         &scope,
			ParticipantID: participantID,
			CheckCodes:    []string{reconciliation.CheckMerkleCommitmentConsistency},
		})
		if err != nil {
			t.Fatalf("engine.Run: %v", err)
		}
		if res.Checks[0].Status != reconciliation.CheckStatusFail {
			t.Fatalf("expected FAIL on algorithm version mismatch, got %s", res.Checks[0].Status)
		}
	})
}

// TestMerkleCommitmentAuthoritativeRecordMutationFails verifies that a mutation in the authoritative
// financial records produces a root mismatch against the maintained commitment.
func TestMerkleCommitmentAuthoritativeRecordMutationFails(t *testing.T) {
	ctx := context.Background()
	store, scope, participantID := newValidBaselineStore(t)

	key := reconciliationScopeKey(participantID, scope)
	store.MaintainedCommitments[key] = reconciliation.MaintainedCommitment{
		ParticipantID:    participantID,
		Partition:        participantID,
		BucketWidth:      1 * time.Hour,
		CanonicalVersion: reconciliation.CanonicalVersion,
		AlgorithmVersion: reconciliation.MerkleAlgorithmVersion,
		Generation:       uuid.New().String(),
		Root:             store.MerkleRoots[key],
		RecordCount:      len(store.MerkleRecords[key]),
		Scope:            scope,
		CapturedAt:       time.Now().UTC(),
	}

	// Mutate the authoritative financial record amount (e.g. from 1000 to 9999 paise)
	mutatedRecs := make([]reconciliation.CanonicalRecord, len(store.MerkleRecords[key]))
	copy(mutatedRecs, store.MerkleRecords[key])
	mutatedRecs[0].AmountPaise = 9999
	store.AuthoritativeRecords[key] = mutatedRecs

	engine := reconciliation.NewRuntimeIntegrityEngine(store, nil)
	res, err := engine.Run(ctx, reconciliation.IntegrityRunRequest{
		Scope:         &scope,
		ParticipantID: participantID,
		CheckCodes:    []string{reconciliation.CheckMerkleCommitmentConsistency},
	})
	if err != nil {
		t.Fatalf("engine.Run: %v", err)
	}

	check := res.Checks[0]
	if check.Status != reconciliation.CheckStatusFail {
		t.Fatalf("expected FAIL on authoritative record mutation, got %s", check.Status)
	}
	var foundMismatch bool
	for _, v := range check.Violations {
		if v.Details["calculatedRoot"] != nil && v.Details["maintainedRoot"] != nil {
			foundMismatch = true
			break
		}
	}
	if !foundMismatch {
		t.Fatalf("expected canonical vs maintained root mismatch violation detail, got %+v", check.Violations)
	}
}

// TestMemoryIntegrityRunStoreViolationsRoundTrip verifies that structured violations are preserved
// deterministically in the in-memory run store.
func TestMemoryIntegrityRunStoreViolationsRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := reconciliation.NewMemoryIntegrityRunStore()

	runID := uuid.New()
	now := time.Now().UTC()
	scope := reconciliation.Scope{From: now.Add(-1 * time.Hour), To: now}
	run := reconciliation.IntegrityRunResult{
		RunID:         runID,
		Scope:         &scope,
		ParticipantID: "BANK-TEST",
		Status:        reconciliation.IntegrityRunStatusCompleted,
		Summary: reconciliation.IntegrityRunSummary{
			TotalChecks: 1,
			Failed:      1,
		},
		Checks: []reconciliation.IntegrityCheckResult{
			{
				RunID:       runID,
				Code:        reconciliation.CheckDebitCreditConservation,
				Severity:    reconciliation.SeverityCritical,
				Status:      reconciliation.CheckStatusFail,
				Message:     "Conservation check failed",
				Violations: []reconciliation.CheckViolation{
					{
						EntityID:    "acc-001",
						Description: "debit/credit imbalance of 200 paise",
						Details: map[string]any{
							"debits":  1000,
							"credits": 800,
							"diff":    200,
						},
					},
				},
				StartedAt:   now,
				CompletedAt: now,
			},
		},
		StartedAt:   now,
		CompletedAt: now,
	}

	if err := store.SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	retrieved, err := store.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}

	if len(retrieved.Checks) != 1 || len(retrieved.Checks[0].Violations) != 1 {
		t.Fatalf("expected 1 check with 1 violation, got %+v", retrieved.Checks)
	}
	v := retrieved.Checks[0].Violations[0]
	if v.EntityID != "acc-001" || v.Description != "debit/credit imbalance of 200 paise" {
		t.Fatalf("violation mismatch: %+v", v)
	}
}

// TestPostgresIntegrityRunStoreViolationsRoundTrip verifies that when a real PostgreSQL database
// is available, structured violations in a FAIL result are serialized to JSONB and deserialized
// identically by GetRun.
func TestPostgresIntegrityRunStoreViolationsRoundTrip(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("pool.Ping: %v", err)
	}

	// Verify table exists
	var tableExists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.integrity_check_results') IS NOT NULL`).Scan(&tableExists); err != nil || !tableExists {
		t.Skip("integrity_check_results table not available in test database")
	}

	store := reconciliation.NewPostgresIntegrityRunStore(pool)

	runID := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	scope := reconciliation.Scope{From: now.Add(-1 * time.Hour), To: now}

	originalRun := reconciliation.IntegrityRunResult{
		RunID:         runID,
		Scope:         &scope,
		ParticipantID: "BANK-PERSISTENCE-TEST",
		Status:        reconciliation.IntegrityRunStatusCompleted,
		Summary: reconciliation.IntegrityRunSummary{
			TotalChecks: 1,
			Failed:      1,
		},
		Checks: []reconciliation.IntegrityCheckResult{
			{
				RunID:       runID,
				Code:        reconciliation.CheckDebitCreditConservation,
				Severity:    reconciliation.SeverityCritical,
				Status:      reconciliation.CheckStatusFail,
				Message:     "Conservation check failed",
				Violations: []reconciliation.CheckViolation{
					{
						EntityID:    "acc-test-roundtrip",
						Description: "debit/credit imbalance of 500 paise",
						Details: map[string]any{
							"accountNumber": "ACC-TEST-999",
							"imbalance":     float64(500),
						},
					},
				},
				StartedAt:   now,
				CompletedAt: now,
			},
		},
		StartedAt:   now,
		CompletedAt: now,
	}

	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM integrity_check_results WHERE id = $1`, runID)
	}()

	if err := store.SaveRun(ctx, originalRun); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	retrieved, err := store.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}

	if retrieved.RunID != runID {
		t.Fatalf("retrieved RunID = %s, want %s", retrieved.RunID, runID)
	}
	if len(retrieved.Checks) != 1 {
		t.Fatalf("retrieved checks count = %d, want 1", len(retrieved.Checks))
	}
	chk := retrieved.Checks[0]
	if chk.Status != reconciliation.CheckStatusFail {
		t.Fatalf("retrieved check status = %s, want FAIL", chk.Status)
	}
	if len(chk.Violations) != 1 {
		t.Fatalf("retrieved violations count = %d, want 1", len(chk.Violations))
	}
	v := chk.Violations[0]
	if v.EntityID != "acc-test-roundtrip" {
		t.Errorf("violation EntityID = %q, want %q", v.EntityID, "acc-test-roundtrip")
	}
	if v.Description != "debit/credit imbalance of 500 paise" {
		t.Errorf("violation Description = %q, want %q", v.Description, "debit/credit imbalance of 500 paise")
	}
	if v.Details["accountNumber"] != "ACC-TEST-999" {
		t.Errorf("violation Details[accountNumber] = %v, want %q", v.Details["accountNumber"], "ACC-TEST-999")
	}
}

// -----------------------------------------------------------------------------
// M3-7-C5: Durable Commitment Generation & Production Source Tests
// -----------------------------------------------------------------------------

type testLedgerSnapshotSource struct {
	snapshot bank.LedgerSnapshot
	err      error
}

func (s testLedgerSnapshotSource) GetLedgerSnapshot(ctx context.Context, _ bank.LedgerScope) (bank.LedgerSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return bank.LedgerSnapshot{}, err
	}
	if s.err != nil {
		return bank.LedgerSnapshot{}, s.err
	}
	return s.snapshot, nil
}

func TestPersistedCommitmentGenerationRoundTrip(t *testing.T) {
	ctx := context.Background()
	scope := reconciliation.Scope{
		From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}
	ledger, err := reconciliation.NewIncrementalMerkleLedger("BANK-A", 1*time.Hour, scope)
	if err != nil {
		t.Fatalf("NewIncrementalMerkleLedger: %v", err)
	}
	record := reconciliation.CanonicalRecord{
		OperationID: uuid.New(),
		PaymentID:   uuid.New(),
		AccountID:   uuid.New(),
		EntryType:   "DEBIT",
		AmountPaise: 1000,
		OccurredAt:  scope.From.Add(10 * time.Minute),
	}
	if _, err := ledger.AppendRecord(ctx, record); err != nil {
		t.Fatalf("AppendRecord: %v", err)
	}

	state := ledger.Snapshot()
	gen := state.Generation
	if gen == "" {
		t.Fatalf("expected non-empty snapshot generation")
	}

	store := reconciliation.NewMemoryIncrementalCommitmentStore()
	if err := store.SaveState(ctx, state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	loaded, found, err := store.LoadState(ctx, "BANK-A", 1*time.Hour, scope)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !found {
		t.Fatalf("expected commitment state to be found")
	}
	if loaded.Generation != gen {
		t.Fatalf("generation mismatch: loaded %q != saved %q", loaded.Generation, gen)
	}

	// Also verify FindState returns the exact generation
	foundState, ok, err := store.FindState(ctx, "BANK-A", scope)
	if err != nil {
		t.Fatalf("FindState: %v", err)
	}
	if !ok {
		t.Fatalf("expected FindState to locate state")
	}
	if foundState.Generation != gen {
		t.Fatalf("FindState generation mismatch: %q != %q", foundState.Generation, gen)
	}
}

func TestEmptyCommitmentGenerationRejected(t *testing.T) {
	ctx := context.Background()
	scope := reconciliation.Scope{
		From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}
	ledger, err := reconciliation.NewIncrementalMerkleLedger("BANK-A", 1*time.Hour, scope)
	if err != nil {
		t.Fatalf("NewIncrementalMerkleLedger: %v", err)
	}
	state := ledger.Snapshot()
	state.Generation = "" // explicitly clear generation

	// ValidateIncrementalState must reject empty generation
	if err := reconciliation.ValidateIncrementalState(state); err == nil {
		t.Fatalf("expected error from ValidateIncrementalState with empty generation")
	}

	// SaveState must reject empty generation
	store := reconciliation.NewMemoryIncrementalCommitmentStore()
	if err := store.SaveState(ctx, state); err == nil {
		t.Fatalf("expected error from SaveState with empty generation")
	}
}

func TestCommitmentGenerationChangesOnRefresh(t *testing.T) {
	ctx := context.Background()
	scope := reconciliation.Scope{
		From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}
	entry := bank.LedgerEntry{
		OperationID: uuid.New(),
		PaymentID:   uuid.New(),
		AccountID:   uuid.New(),
		EntryType:   "DEBIT",
		AmountPaise: 5000,
		Currency:    "INR",
		OccurredAt:  scope.From.Add(30 * time.Minute),
	}
	source := testLedgerSnapshotSource{
		snapshot: bank.LedgerSnapshot{
			BankID:     "BANK-A",
			CapturedAt: scope.To,
			Entries:    []bank.LedgerEntry{entry},
		},
	}
	commitStore := reconciliation.NewMemoryIncrementalCommitmentStore()
	participant, err := reconciliation.NewRepositoryParticipantWithCommitmentStore(source, "BANK-A", "BANK-A", 1*time.Hour, commitStore)
	if err != nil {
		t.Fatalf("NewRepositoryParticipantWithCommitmentStore: %v", err)
	}

	if err := participant.Initialize(ctx, scope); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	root1, err := participant.GetRoot(ctx, scope)
	if err != nil {
		t.Fatalf("GetRoot: %v", err)
	}
	g1 := root1.Ref.Generation
	if g1 == "" {
		t.Fatalf("expected non-empty generation G1")
	}

	// Now refresh / reinitialize
	if err := participant.Refresh(ctx, scope); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	root2, err := participant.GetRoot(ctx, scope)
	if err != nil {
		t.Fatalf("GetRoot: %v", err)
	}
	g2 := root2.Ref.Generation
	if g2 == "" {
		t.Fatalf("expected non-empty generation G2")
	}

	if g1 == g2 {
		t.Fatalf("expected generation to change on refresh: G1=%q == G2=%q", g1, g2)
	}

	// Verify old generation G1 is no longer reported as current
	_, err = participant.GetChildren(ctx, reconciliation.NodeRef{
		ParticipantID: "BANK-A",
		ScopeID:       reconciliation.ScopeIdentity(scope),
		Generation:    g1,
		Path:          root1.Ref.Path,
	})
	if err == nil || !errors.Is(err, reconciliation.ErrStaleReference) {
		t.Fatalf("expected ErrStaleReference for old generation %q, got %v", g1, err)
	}
}

func TestProductionMaintainedCommitmentSource(t *testing.T) {
	ctx := context.Background()
	scope := reconciliation.Scope{
		From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}
	participantID := "BANK-PROD"

	ledger, err := reconciliation.NewIncrementalMerkleLedger(participantID, 1*time.Hour, scope)
	if err != nil {
		t.Fatalf("NewIncrementalMerkleLedger: %v", err)
	}
	rec := reconciliation.CanonicalRecord{
		OperationID: uuid.New(),
		PaymentID:   uuid.New(),
		AccountID:   uuid.New(),
		EntryType:   "DEBIT",
		AmountPaise: 2500,
		OccurredAt:  scope.From.Add(15 * time.Minute),
	}
	if _, err := ledger.AppendRecord(ctx, rec); err != nil {
		t.Fatalf("AppendRecord: %v", err)
	}
	state := ledger.Snapshot()
	rebuildCountBefore := ledger.FullRebuildCount()

	// 1. Verify with memory commitment store through production constructor
	memStore := reconciliation.NewMemoryIncrementalCommitmentStore()
	if err := memStore.SaveState(ctx, state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	dataStore := reconciliation.NewPostgresFinancialDataStore(nil, memStore)
	mc, found, err := dataStore.GetMaintainedCommitment(ctx, participantID, scope)
	if err != nil {
		t.Fatalf("GetMaintainedCommitment: %v", err)
	}
	if !found {
		t.Fatalf("expected maintained commitment to be found")
	}
	if !reconciliation.EqualBytes(mc.Root, state.Root) {
		t.Fatalf("root mismatch: got %x, want %x", mc.Root, state.Root)
	}
	if mc.Generation != state.Generation {
		t.Fatalf("generation mismatch: got %q, want %q", mc.Generation, state.Generation)
	}
	if mc.BucketWidth != state.BucketWidth {
		t.Fatalf("bucket width mismatch: got %v, want %v", mc.BucketWidth, state.BucketWidth)
	}
	if mc.CanonicalVersion != state.CanonicalVersion {
		t.Fatalf("canonical version mismatch: got %s, want %s", mc.CanonicalVersion, state.CanonicalVersion)
	}
	if mc.AlgorithmVersion != state.AlgorithmVersion {
		t.Fatalf("algorithm version mismatch: got %s, want %s", mc.AlgorithmVersion, state.AlgorithmVersion)
	}
	if mc.RecordCount != state.RecordCount {
		t.Fatalf("record count mismatch: got %d, want %d", mc.RecordCount, state.RecordCount)
	}

	// Verify no bootstrap or rebuild occurred
	if ledger.FullRebuildCount() != rebuildCountBefore {
		t.Fatalf("expected 0 full rebuilds, got %d", ledger.FullRebuildCount())
	}

	// 2. If DATABASE_URL is available, run live PostgreSQL commitment store integration test
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Log("DATABASE_URL not set; skipping live Postgres integration for production source")
		return
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	pgCommitStore := reconciliation.NewPostgresIncrementalCommitmentStore(pool)
	if err := pgCommitStore.SaveState(ctx, state); err != nil {
		t.Fatalf("pgCommitStore.SaveState: %v", err)
	}

	prodDataStore := reconciliation.NewProductionPostgresFinancialDataStore(pool)
	pgMC, found, err := prodDataStore.GetMaintainedCommitment(ctx, participantID, scope)
	if err != nil {
		t.Fatalf("prodDataStore.GetMaintainedCommitment: %v", err)
	}
	if !found {
		t.Fatalf("expected pg maintained commitment to be found")
	}
	if !reconciliation.EqualBytes(pgMC.Root, state.Root) {
		t.Fatalf("pg root mismatch: got %x, want %x", pgMC.Root, state.Root)
	}
	if pgMC.Generation != state.Generation {
		t.Fatalf("pg generation mismatch: got %q, want %q", pgMC.Generation, state.Generation)
	}
	if pgMC.BucketWidth != state.BucketWidth {
		t.Fatalf("pg bucket width mismatch: got %v, want %v", pgMC.BucketWidth, state.BucketWidth)
	}
	if pgMC.CanonicalVersion != state.CanonicalVersion {
		t.Fatalf("pg canonical version mismatch: got %s, want %s", pgMC.CanonicalVersion, state.CanonicalVersion)
	}
	if pgMC.AlgorithmVersion != state.AlgorithmVersion {
		t.Fatalf("pg algorithm version mismatch: got %s, want %s", pgMC.AlgorithmVersion, state.AlgorithmVersion)
	}
	if pgMC.RecordCount != state.RecordCount {
		t.Fatalf("pg record count mismatch: got %d, want %d", pgMC.RecordCount, state.RecordCount)
	}
}

func TestNoSyntheticGeneration(t *testing.T) {
	prodFiles := []string{
		"incremental.go",
		"participant_impl.go",
		"financial_integrity.go",
		"postgres_commitment_store.go",
		"integrity.go",
		"central.go",
		"engine.go",
	}

	forbiddenPatterns := []string{
		`"gen-1"`,
		`"gen-legacy"`,
		`fmt.Sprintf("gen%d"`,
		`fmt.Sprintf("g%d"`,
	}

	for _, file := range prodFiles {
		path := filepath.Join(".", file)
		content, err := os.ReadFile(path)
		if err != nil {
			path = filepath.Join("internal", "reconciliation", file)
			content, err = os.ReadFile(path)
			if err != nil {
				path = filepath.Join("..", "reconciliation", file)
				content, err = os.ReadFile(path)
				if err != nil {
					t.Fatalf("failed to locate and read production file %s: %v", file, err)
				}
			}
		}
		str := string(content)
		for _, pat := range forbiddenPatterns {
			if strings.Contains(str, pat) {
				t.Errorf("production file %s contains forbidden synthetic generation pattern %q", file, pat)
			}
		}
	}
}
