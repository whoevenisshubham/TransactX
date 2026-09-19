package payments

// acceptance_test.go — M1 Acceptance Hardening: K1-K15 regression suite.
//
// Tests requiring a live Postgres database are gated by DATABASE_URL via
// newRepositoryTestData (same pattern as existing tests).
// Pure-logic tests run unconditionally.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/bankservice"
	"github.com/transactx/backend/internal/recipients"
)

// ---------------------------------------------------------------------------
// K1: Concurrent routed idempotency — exactly 1 payment, 1 idempotency record,
// 4 bank operations, and all goroutines return the same payment ID + COMPLETED.
// ---------------------------------------------------------------------------

func TestK1_ConcurrentRoutedIdempotencyCreatesSinglePayment(t *testing.T) {
	data := newRepositoryTestData(t)
	setBalances(t, data, 5000, 0)
	insertParticipantAccount(t, data.pool, "bank_a", data.sourceID, "k1-src-"+data.sourceID.String(), 5000)
	insertParticipantAccount(t, data.pool, "bank_a", data.receiverID, "k1-dst-"+data.receiverID.String(), 0)
	bankASvc := bankservice.NewService(data.pool)
	bankAServer := httptest.NewServer(bankservice.Handler(bankASvc))
	defer bankAServer.Close()
	adapterA, err := bank.NewHTTPClient(bankAServer.URL, bankAServer.Client())
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM bank_a.ledger_entries WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM bank_a.operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM bank_a.accounts WHERE id IN ($1, $2)`, data.sourceID, data.receiverID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM payment_bank_operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		data.close(t)
	}()

	const goroutines = 10
	type result struct {
		payment   Payment
		duplicate bool
		err       error
	}
	ch := make(chan result, goroutines)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			p, dup, callErr := data.repository.CreateRoutedIdempotent(
				context.Background(),
				settlementPayment(data, 100),
				"k1-concurrent-key", "k1-hash",
				data.bankID, data.bankID,
				adapterA, adapterA,
			)
			ch <- result{p, dup, callErr}
		}()
	}
	close(start)
	wg.Wait()
	close(ch)

	var firstPaymentID uuid.UUID
	for r := range ch {
		if r.err != nil {
			t.Fatalf("K1: concurrent call failed: %v", r.err)
		}
		if r.payment.State != StateCompleted {
			t.Fatalf("K1: payment state = %q, want COMPLETED", r.payment.State)
		}
		if firstPaymentID == uuid.Nil {
			firstPaymentID = r.payment.ID
		} else if r.payment.ID != firstPaymentID {
			t.Fatalf("K1: concurrent call returned payment %s, want %s", r.payment.ID, firstPaymentID)
		}
	}

	var paymentCount, idempotencyCount, opCount int
	if err := data.pool.QueryRow(context.Background(), `SELECT count(*) FROM payments WHERE initiated_by_user_id = $1`, data.userID).Scan(&paymentCount); err != nil {
		t.Fatal(err)
	}
	if err := data.pool.QueryRow(context.Background(), `SELECT count(*) FROM idempotency_records WHERE user_id = $1 AND key = 'k1-concurrent-key'`, data.userID).Scan(&idempotencyCount); err != nil {
		t.Fatal(err)
	}
	if err := data.pool.QueryRow(context.Background(), `SELECT count(*) FROM payment_bank_operations WHERE payment_id = $1`, firstPaymentID).Scan(&opCount); err != nil {
		t.Fatal(err)
	}
	if paymentCount != 1 {
		t.Fatalf("K1: payments = %d, want 1", paymentCount)
	}
	if idempotencyCount != 1 {
		t.Fatalf("K1: idempotency records = %d, want 1", idempotencyCount)
	}
	if opCount != 4 {
		t.Fatalf("K1: bank operations = %d, want 4 (hold, provisional-credit, confirm-hold, finalize-credit)", opCount)
	}
}

// ---------------------------------------------------------------------------
// K2: Same key + changed payload → ErrIdempotencyConflict.
// ---------------------------------------------------------------------------

func TestK2_IdempotencyKeyConflictOnChangedPayload(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)

	paymentID := uuid.New()
	if _, err := data.pool.Exec(context.Background(),
		`INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state)
		 VALUES ($1, $2, $3, $4, 100, 'INR', 'COMPLETED')`,
		paymentID, data.userID, data.sourceID, data.receiverID); err != nil {
		t.Fatal(err)
	}
	if _, err := data.pool.Exec(context.Background(),
		`INSERT INTO idempotency_records (id, user_id, key, request_hash, payment_id, response_snapshot)
		 VALUES ($1, $2, 'k2-key', 'k2-hash-original', $3, '{}')`,
		uuid.New(), data.userID, paymentID); err != nil {
		t.Fatal(err)
	}

	_, _, conflictErr := data.repository.GetIdempotent(context.Background(), data.userID, "k2-key", "k2-hash-CHANGED")
	if !errors.Is(conflictErr, ErrIdempotencyConflict) {
		t.Fatalf("K2: error = %v, want ErrIdempotencyConflict", conflictErr)
	}
}

// ---------------------------------------------------------------------------
// K3: Retry with a pending payment reuses the existing operation ID and does
// NOT duplicate monetary calls.
// ---------------------------------------------------------------------------

func TestK3_RetryReusesOperationIDWithoutDuplicatingMonetaryCalls(t *testing.T) {
	data := newRepositoryTestData(t)
	setBalances(t, data, 1000, 0)
	defer func() {
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM payment_bank_operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		data.close(t)
	}()

	src := &transientBankAdapter{}
	// First call → PENDING_RECONCILIATION (hold returns transient error).
	p, _, err := data.repository.CreateRoutedIdempotent(
		context.Background(),
		settlementPayment(data, 100),
		"k3-key", "k3-hash",
		data.bankID, data.bankID,
		src, successfulBankAdapter{},
	)
	if err != nil || p.State != StatePendingReconciliation {
		t.Fatalf("K3: first call state = %q, err=%v; want PENDING_RECONCILIATION, nil", p.State, err)
	}
	if src.calls != 1 {
		t.Fatalf("K3: hold calls after first attempt = %d, want 1", src.calls)
	}

	// Retry with the same key/hash: the operation is already persisted as PENDING.
	// GetOperationStatus on transientBankAdapter (inherits successfulBankAdapter) returns SUCCEEDED.
	recovered, duplicate, err2 := data.repository.CreateRoutedIdempotent(
		context.Background(),
		settlementPayment(data, 100),
		"k3-key", "k3-hash",
		data.bankID, data.bankID,
		src, successfulBankAdapter{},
	)
	if err2 != nil || !duplicate || recovered.State != StateCompleted {
		t.Fatalf("K3: retry state = %q, duplicate=%v, err=%v; want COMPLETED, true, nil", recovered.State, duplicate, err2)
	}
	// Must NOT have called HoldFunds again.
	if src.calls != 1 {
		t.Fatalf("K3: hold calls after retry = %d, want 1 (no repeated monetary call)", src.calls)
	}

	var holdOpID uuid.UUID
	var holdCount int
	if err := data.pool.QueryRow(context.Background(),
		`SELECT operation_id, count(*) OVER () FROM payment_bank_operations WHERE payment_id = $1 AND operation_type = 'HOLD'`,
		recovered.ID).Scan(&holdOpID, &holdCount); err != nil {
		t.Fatalf("K3: load hold operation: %v", err)
	}
	if holdCount != 1 {
		t.Fatalf("K3: HOLD operations = %d, want 1", holdCount)
	}
	expectedHoldID := operationID(recovered.ID, "hold")
	if holdOpID != expectedHoldID {
		t.Fatalf("K3: persisted hold operation_id = %s, want stable %s", holdOpID, expectedHoldID)
	}
}

// ---------------------------------------------------------------------------
// K4: Unknown / unexpected bank status string never becomes success.
// ---------------------------------------------------------------------------

type unknownStatusAdapter struct{ successfulBankAdapter }

func (a *unknownStatusAdapter) HoldFunds(_ context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
	return bank.HoldResult{OperationResult: bank.OperationResult{
		PaymentID:   req.PaymentID,
		OperationID: req.OperationID,
		Status:      "WHATEVER",
	}}, nil
}

func TestK4_UnknownBankStatusNeverBecomesSuccess(t *testing.T) {
	data := newRepositoryTestData(t)
	setBalances(t, data, 1000, 0)
	defer data.close(t)

	p, _, err := data.repository.CreateRoutedIdempotent(
		context.Background(),
		settlementPayment(data, 100),
		"k4-key", "k4-hash",
		data.bankID, data.bankID,
		&unknownStatusAdapter{}, successfulBankAdapter{},
	)
	if err != nil {
		t.Fatalf("K4: unexpected error: %v", err)
	}
	if p.State != StatePendingReconciliation {
		t.Fatalf("K4: payment state = %q, want PENDING_RECONCILIATION", p.State)
	}

	var opStatus string
	var opCount int
	if err := data.pool.QueryRow(context.Background(),
		`SELECT status, count(*) OVER () FROM payment_bank_operations WHERE payment_id = $1 AND operation_type = 'HOLD'`,
		p.ID).Scan(&opStatus, &opCount); err != nil {
		t.Fatalf("K4: load hold operation: %v", err)
	}
	if opCount != 1 {
		t.Fatalf("K4: HOLD operations = %d, want 1", opCount)
	}
	if opStatus != bankOperationPending {
		t.Fatalf("K4: hold status = %q, want PENDING (unresolved)", opStatus)
	}
	var creditCount int
	if err := data.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM payment_bank_operations WHERE payment_id = $1 AND operation_type <> 'HOLD'`,
		p.ID).Scan(&creditCount); err != nil {
		t.Fatal(err)
	}
	if creditCount != 0 {
		t.Fatalf("K4: downstream monetary operations = %d, want 0", creditCount)
	}
}

// ---------------------------------------------------------------------------
// K5: Correlation safety — wrong PaymentID or OperationID from bank must not succeed.
// ---------------------------------------------------------------------------

type wrongPaymentIDAdapter struct{ successfulBankAdapter }

func (a *wrongPaymentIDAdapter) HoldFunds(_ context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
	return bank.HoldResult{OperationResult: bank.OperationResult{
		PaymentID:   uuid.New(),
		OperationID: req.OperationID,
		Status:      bank.OperationSucceeded,
	}, HoldID: req.OperationID}, nil
}

type wrongOperationIDAdapter struct{ successfulBankAdapter }

func (a *wrongOperationIDAdapter) HoldFunds(_ context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
	return bank.HoldResult{OperationResult: bank.OperationResult{
		PaymentID:   req.PaymentID,
		OperationID: uuid.New(),
		Status:      bank.OperationSucceeded,
	}, HoldID: req.OperationID}, nil
}

func TestK5_CorrelationMismatchOnPaymentIDNeverSucceeds(t *testing.T) {
	data := newRepositoryTestData(t)
	setBalances(t, data, 1000, 0)
	defer data.close(t)

	p, _, err := data.repository.CreateRoutedIdempotent(
		context.Background(),
		settlementPayment(data, 100),
		"k5a-key", "k5a-hash",
		data.bankID, data.bankID,
		&wrongPaymentIDAdapter{}, successfulBankAdapter{},
	)
	if err != nil {
		t.Fatalf("K5a: unexpected error: %v", err)
	}
	if p.State != StatePendingReconciliation {
		t.Fatalf("K5a: payment state = %q, want PENDING_RECONCILIATION", p.State)
	}
	assertNoDownstreamBankOps(t, data, p.ID, "K5a")
}

func TestK5_CorrelationMismatchOnOperationIDNeverSucceeds(t *testing.T) {
	data := newRepositoryTestData(t)
	setBalances(t, data, 1000, 0)
	defer data.close(t)

	p, _, err := data.repository.CreateRoutedIdempotent(
		context.Background(),
		settlementPayment(data, 100),
		"k5b-key", "k5b-hash",
		data.bankID, data.bankID,
		&wrongOperationIDAdapter{}, successfulBankAdapter{},
	)
	if err != nil {
		t.Fatalf("K5b: unexpected error: %v", err)
	}
	if p.State != StatePendingReconciliation {
		t.Fatalf("K5b: payment state = %q, want PENDING_RECONCILIATION", p.State)
	}
	assertNoDownstreamBankOps(t, data, p.ID, "K5b")
}

func assertNoDownstreamBankOps(t *testing.T, data repositoryTestData, paymentID uuid.UUID, label string) {
	t.Helper()
	var holdStatus string
	if err := data.pool.QueryRow(context.Background(),
		`SELECT status FROM payment_bank_operations WHERE payment_id = $1 AND operation_type = 'HOLD'`,
		paymentID).Scan(&holdStatus); err != nil {
		t.Fatalf("%s: load hold status: %v", label, err)
	}
	if holdStatus != bankOperationPending {
		t.Fatalf("%s: hold status = %q, want PENDING", label, holdStatus)
	}
	var other int
	if err := data.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM payment_bank_operations WHERE payment_id = $1 AND operation_type <> 'HOLD'`,
		paymentID).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if other != 0 {
		t.Fatalf("%s: downstream monetary operations = %d, want 0", label, other)
	}
}

// ---------------------------------------------------------------------------
// K6: State transition safety (pure logic, no database).
// ---------------------------------------------------------------------------

func TestK6_PendingReconciliationCannotDirectlyTransitionToCompleted(t *testing.T) {
	p := Payment{State: StatePendingReconciliation}
	if err := Transition(&p, StateCompleted); err == nil {
		t.Fatal("K6: PENDING_RECONCILIATION -> COMPLETED was accepted, want ErrInvalidTransition")
	}
	if p.State != StatePendingReconciliation {
		t.Fatalf("K6: state changed to %q after rejected transition", p.State)
	}
}

func TestK6_PendingReconciliationMustGoViaCommittedToCompleted(t *testing.T) {
	p := Payment{State: StatePendingReconciliation}
	if err := Transition(&p, StateCommitted); err != nil {
		t.Fatalf("K6: PENDING_RECONCILIATION -> COMMITTED: %v", err)
	}
	if err := Transition(&p, StateCompleted); err != nil {
		t.Fatalf("K6: COMMITTED -> COMPLETED: %v", err)
	}
	if p.State != StateCompleted {
		t.Fatalf("K6: final state = %q, want COMPLETED", p.State)
	}
}

func TestK6_RuntimeUpdateStateEnforcesPendingReconciliationPath(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)

	paymentID := uuid.New()
	if _, err := data.pool.Exec(context.Background(),
		`INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state)
		 VALUES ($1, $2, $3, $4, 100, 'INR', 'PENDING_RECONCILIATION')`,
		paymentID, data.userID, data.sourceID, data.receiverID); err != nil {
		t.Fatal(err)
	}

	if err := data.repository.updateState(context.Background(), paymentID, StateCompleted); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("K6 runtime: direct PENDING_RECONCILIATION -> COMPLETED error = %v, want ErrInvalidTransition", err)
	}
	var state string
	if err := data.pool.QueryRow(context.Background(), `SELECT state FROM payments WHERE id = $1`, paymentID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != StatePendingReconciliation {
		t.Fatalf("K6 runtime: state = %q after rejected transition, want PENDING_RECONCILIATION", state)
	}
	if err := data.repository.updateState(context.Background(), paymentID, StateCommitted); err != nil {
		t.Fatalf("K6 runtime: PENDING_RECONCILIATION -> COMMITTED: %v", err)
	}
	if err := data.repository.updateState(context.Background(), paymentID, StateCompleted); err != nil {
		t.Fatalf("K6 runtime: COMMITTED -> COMPLETED: %v", err)
	}
}

// ---------------------------------------------------------------------------
// K7: CustomerPayment DTO contains no internal UUIDs (structural test).
// ---------------------------------------------------------------------------

func TestK7_CustomerPaymentDTODoesNotExposeInternalIdentifiers(t *testing.T) {
	note := "Coffee"
	srcName, srcCode := "Source Bank", "BANK-SRC"
	dstName, dstCode := "Dest Bank", "BANK-DST"
	var duration int64 = 42
	cp := CustomerPayment{
		ID:                  uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		AmountPaise:         12550,
		Currency:            "INR",
		Note:                &note,
		Origin:              "ONLINE",
		State:               StateCompleted,
		Direction:           "SENT",
		SenderName:          "Alice",
		ReceiverName:        "Bob",
		SenderPaymentID:     "alice@transactx",
		ReceiverPaymentID:   "bob@transactx",
		SourceBankName:      &srcName,
		SourceBankCode:      &srcCode,
		DestinationBankName: &dstName,
		DestinationBankCode: &dstCode,
		DurationMs:          &duration,
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(raw, &encoded); err != nil {
		t.Fatal(err)
	}
	banned := []string{
		"senderAccountId", "receiverAccountId", "initiatedByUserId",
		"sourceBankId", "destinationBankId", "sourceBankAccountId", "destinationBankAccountId",
		"routeBankId", "operationId", "holdId", "bankOperationId",
	}
	for _, key := range banned {
		if _, found := encoded[key]; found {
			t.Errorf("K7: customer DTO JSON exposes internal field %q", key)
		}
	}
	required := []string{"id", "amountPaise", "currency", "state", "direction", "senderName", "receiverName", "senderPaymentIdentifier", "receiverPaymentIdentifier", "origin"}
	for _, key := range required {
		if _, found := encoded[key]; !found {
			t.Errorf("K7: customer DTO JSON missing required field %q", key)
		}
	}
}

// ---------------------------------------------------------------------------
// K8: Customer payment POST DTO includes required fields (integration).
// ---------------------------------------------------------------------------

func TestK8_CustomerPaymentDTOHasRequiredFields(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)
	setBalances(t, data, 500, 0)

	paymentID := uuid.New()
	if _, err := data.pool.Exec(context.Background(),
		`INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state)
		 VALUES ($1, $2, $3, $4, 50, 'INR', 'COMPLETED')`,
		paymentID, data.userID, data.sourceID, data.receiverID); err != nil {
		t.Fatal(err)
	}

	cp, err := data.repository.GetForUser(context.Background(), data.userID, paymentID)
	if err != nil {
		t.Fatalf("K8: GetForUser error: %v", err)
	}
	if cp.Direction != "SENT" && cp.Direction != "RECEIVED" {
		t.Fatalf("K8: direction = %q, want SENT or RECEIVED", cp.Direction)
	}
	if cp.SenderName == "" {
		t.Fatal("K8: senderName is empty")
	}
	if cp.ReceiverName == "" {
		t.Fatal("K8: receiverName is empty")
	}
	if cp.AmountPaise != 50 {
		t.Fatalf("K8: amountPaise = %d, want 50", cp.AmountPaise)
	}
}

// ---------------------------------------------------------------------------
// K9: Customer history includes SENT and RECEIVED; third-party payments hidden.
// ---------------------------------------------------------------------------

func TestK9_HistoryShowsSentAndReceivedDirections(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)
	setBalances(t, data, 1000, 1000)

	paymentID := uuid.New()
	if _, err := data.pool.Exec(context.Background(),
		`INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state)
		 VALUES ($1, $2, $3, $4, 100, 'INR', 'COMPLETED')`,
		paymentID, data.userID, data.sourceID, data.receiverID); err != nil {
		t.Fatal(err)
	}

	senderView, err := data.repository.ListForUser(context.Background(), data.userID, 10)
	if err != nil {
		t.Fatal(err)
	}
	hasSent := false
	for _, cp := range senderView {
		if cp.ID == paymentID && cp.Direction == "SENT" {
			hasSent = true
		}
	}
	if !hasSent {
		t.Fatalf("K9: sender history has no SENT entry for payment %s", paymentID)
	}

	receiverView, err := data.repository.ListForUser(context.Background(), data.receiverUserID, 10)
	if err != nil {
		t.Fatal(err)
	}
	hasReceived := false
	for _, cp := range receiverView {
		if cp.ID == paymentID && cp.Direction == "RECEIVED" {
			hasReceived = true
		}
	}
	if !hasReceived {
		t.Fatalf("K9: receiver history has no RECEIVED entry for payment %s", paymentID)
	}

	// Third-party user must not see this payment.
	thirdPartyID := uuid.New()
	if _, err := data.pool.Exec(context.Background(),
		`INSERT INTO users (id, name, phone, upi_id, password_hash, role)
		 VALUES ($1, 'Third', $2, $3, 'hash', 'CUSTOMER')`,
		thirdPartyID, thirdPartyID.String()[:20], "third-"+thirdPartyID.String()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, thirdPartyID)
	}()

	thirdView, err := data.repository.ListForUser(context.Background(), thirdPartyID, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, cp := range thirdView {
		if cp.ID == paymentID {
			t.Fatalf("K9: third-party can see payment %s (must be hidden)", paymentID)
		}
	}
}

// ---------------------------------------------------------------------------
// K10: Authorization — sender and receiver may retrieve the detail; an
// unrelated third party receives ErrNotFound.
// ---------------------------------------------------------------------------

func TestK10_PaymentDetailAuthorizesOnlySenderAndReceiver(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)
	setBalances(t, data, 500, 0)

	paymentID := uuid.New()
	if _, err := data.pool.Exec(context.Background(),
		`INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state)
		 VALUES ($1, $2, $3, $4, 100, 'INR', 'COMPLETED')`,
		paymentID, data.userID, data.sourceID, data.receiverID); err != nil {
		t.Fatal(err)
	}

	if _, err := data.repository.GetForUser(context.Background(), data.userID, paymentID); err != nil {
		t.Fatalf("K10: sender cannot see own payment: %v", err)
	}
	if _, err := data.repository.GetForUser(context.Background(), data.receiverUserID, paymentID); err != nil {
		t.Fatalf("K10: receiver cannot see received payment: %v", err)
	}
	thirdParty := uuid.New()
	if _, err := data.repository.GetForUser(context.Background(), thirdParty, paymentID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("K10: third-party access error = %v, want ErrNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// K11: Notes end-to-end — persisted, same hash replays, changed hash conflicts.
// ---------------------------------------------------------------------------

func TestK11_NoteIsPersistedAndHashedForIdempotency(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)
	setBalances(t, data, 1000, 0)

	service := NewService(accounts.NewRepository(data.pool), recipients.NewRepository(data.pool), data.repository, nil)
	recipientID := "receiver-" + data.receiverUserID.String()
	note := "Test note for K11"
	first, duplicate, err := service.CreateWithResult(context.Background(), CreateInput{
		UserID:          data.userID,
		SourceAccountID: data.sourceID,
		Recipient:       recipientID,
		AmountPaise:     100,
		Currency:        "INR",
		Note:            note,
		IdempotencyKey:  "k11-service-key",
	})
	if err != nil || duplicate {
		t.Fatalf("K11: create error = %v, duplicate = %v", err, duplicate)
	}
	if first.Note == nil || *first.Note != note {
		t.Fatalf("K11: persisted note = %v, want %q", first.Note, note)
	}
	if first.State != StateCompleted {
		t.Fatalf("K11: state = %q, want COMPLETED", first.State)
	}

	replay, replayDup, replayErr := service.CreateWithResult(context.Background(), CreateInput{
		UserID:          data.userID,
		SourceAccountID: data.sourceID,
		Recipient:       recipientID,
		AmountPaise:     100,
		Currency:        "INR",
		Note:            note,
		IdempotencyKey:  "k11-service-key",
	})
	if replayErr != nil || !replayDup {
		t.Fatalf("K11: replay error = %v, duplicate = %v", replayErr, replayDup)
	}
	if replay.ID != first.ID {
		t.Fatalf("K11: replay payment = %s, want %s", replay.ID, first.ID)
	}

	_, _, conflictErr := service.CreateWithResult(context.Background(), CreateInput{
		UserID:          data.userID,
		SourceAccountID: data.sourceID,
		Recipient:       recipientID,
		AmountPaise:     100,
		Currency:        "INR",
		Note:            "changed note",
		IdempotencyKey:  "k11-service-key",
	})
	if !errors.Is(conflictErr, ErrIdempotencyConflict) {
		t.Fatalf("K11: changed note error = %v, want ErrIdempotencyConflict", conflictErr)
	}
}

// ---------------------------------------------------------------------------
// K12: Local settlement state machine path (pure logic, no DB).
// ---------------------------------------------------------------------------

func TestK12_LocalSettlementStateTransitionsAreValid(t *testing.T) {
	p := Payment{State: StateCreated}
	for _, next := range []string{StateValidating, StateLocalSettlement, StateCommitted, StateCompleted} {
		if err := Transition(&p, next); err != nil {
			t.Fatalf("K12: local settlement transition -> %q failed: %v", next, err)
		}
	}
	if p.State != StateCompleted {
		t.Fatalf("K12: final state = %q, want COMPLETED", p.State)
	}
}

func TestK12_FreshRegistrationUsesLocalSettlementWhenBankAdaptersDoNotMatch(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)
	setBalances(t, data, 5000, 0)

	// Adapters exist for other banks, but these accounts remain on data.bankID (no matching adapter).
	foreignBankID := uuid.New()
	adapters := map[uuid.UUID]bank.BankAdapter{
		foreignBankID: successfulBankAdapter{},
	}
	service := NewServiceWithAdapters(accounts.NewRepository(data.pool), recipients.NewRepository(data.pool), data.repository, adapters)
	payment, duplicate, err := service.CreateWithResult(context.Background(), CreateInput{
		UserID:          data.userID,
		SourceAccountID: data.sourceID,
		Recipient:       "receiver-" + data.receiverUserID.String(),
		AmountPaise:     250,
		Currency:        "INR",
		Note:            "local after register",
		IdempotencyKey:  "k12-local-key",
	})
	if err != nil || duplicate {
		t.Fatalf("K12 registration path: create error = %v, duplicate = %v", err, duplicate)
	}
	if payment.State != StateCompleted {
		t.Fatalf("K12 registration path: state = %q, want COMPLETED", payment.State)
	}
	if payment.SourceBankID != nil || payment.DestinationBankID != nil {
		t.Fatalf("K12 registration path: unexpected routed bank IDs source=%v dest=%v", payment.SourceBankID, payment.DestinationBankID)
	}
	var opCount int
	if err := data.pool.QueryRow(context.Background(), `SELECT count(*) FROM payment_bank_operations WHERE payment_id = $1`, payment.ID).Scan(&opCount); err != nil {
		t.Fatal(err)
	}
	if opCount != 0 {
		t.Fatalf("K12 registration path: bank operations = %d, want 0 (local settlement)", opCount)
	}
}

// ---------------------------------------------------------------------------
// K13: Error sanitisation — raw internal errors must not appear in the
// customer-safe failure reason.
// ---------------------------------------------------------------------------

func TestK13_ErrorSanitizationDoesNotLeakInternalErrors(t *testing.T) {
	internalErrors := []error{
		fmt.Errorf("pq: connection refused: dial tcp 127.0.0.1:5432"),
		fmt.Errorf("pgx: unexpected message type"),
		fmt.Errorf("sql: transaction has already been committed or rolled back"),
		fmt.Errorf("context deadline exceeded"),
		fmt.Errorf("ERROR: relation bank_a.accounts does not exist (SQLSTATE 42P01)"),
	}
	banned := []string{"pq:", "pgx:", "sql:", "127.0.0.1", "dial tcp", "connection refused", "SQLSTATE", "does not exist"}
	for _, raw := range internalErrors {
		safe := customerSafeFailureReason(raw)
		if safe == "" {
			t.Errorf("K13: failure reason is empty for error: %q", raw)
		}
		for _, b := range banned {
			if strings.Contains(safe, b) {
				t.Errorf("K13: failure reason leaks internal detail %q: safe=%q (raw=%q)", b, safe, raw)
			}
		}
	}

	bankCases := []struct {
		code bank.ErrorCode
		want string
	}{
		{bank.ErrCodeInsufficientFunds, "Insufficient funds."},
		{bank.ErrCodeInvalidAccount, "Account could not be found."},
		{bank.ErrCodeInactiveAccount, "Account is inactive."},
		{bank.ErrCodeBankUnavailable, "Bank service is temporarily unavailable."},
	}
	for _, c := range bankCases {
		got := customerSafeFailureReason(&bank.AdapterError{Code: c.code, Message: "raw internal detail should not appear"})
		if got != c.want {
			t.Errorf("K13: code %v: got %q, want %q", c.code, got, c.want)
		}
		if strings.Contains(got, "raw internal detail") {
			t.Errorf("K13: safe reason leaks raw adapter message: %q", got)
		}
	}
}

// ---------------------------------------------------------------------------
// K14: Bank B identifies as BANK-B; Bank A identifies as BANK-A.
// ---------------------------------------------------------------------------

func TestK14_BankBUnavailableMessageReferencesBankB(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)

	bankBSvc := bankservice.NewParticipantService(data.pool, "BANK-B", "bank_b")
	bankBSvc.SetAvailable(false)
	_, err := bankBSvc.ResolveAccount(context.Background(), bank.ResolveAccountRequest{AccountID: uuid.New()})
	if err == nil {
		t.Fatal("K14: expected unavailable error from BANK-B, got nil")
	}
	var adapterErr *bank.AdapterError
	if !errors.As(err, &adapterErr) {
		t.Fatalf("K14: error type = %T, want *bank.AdapterError", err)
	}
	if adapterErr.Code != bank.ErrCodeBankUnavailable {
		t.Fatalf("K14: BANK-B code = %v, want BANK_UNAVAILABLE", adapterErr.Code)
	}
	if strings.Contains(strings.ToLower(adapterErr.Message), "bank a") || strings.Contains(adapterErr.Message, "BANK-A") {
		t.Errorf("K14: BANK-B message references bank A: %q", adapterErr.Message)
	}
	if !strings.Contains(adapterErr.Message, "BANK-B") {
		t.Errorf("K14: BANK-B message does not reference BANK-B: %q", adapterErr.Message)
	}
}

func TestK14_BankAUnavailableMessageDoesNotReferenceBankB(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)

	bankASvc := bankservice.NewService(data.pool)
	bankASvc.SetAvailable(false)
	_, err := bankASvc.ResolveAccount(context.Background(), bank.ResolveAccountRequest{AccountID: uuid.New()})
	if err == nil {
		t.Fatal("K14: expected unavailable error from BANK-A, got nil")
	}
	var adapterErr *bank.AdapterError
	if !errors.As(err, &adapterErr) {
		t.Fatalf("K14: error type = %T, want *bank.AdapterError", err)
	}
	if adapterErr.Code != bank.ErrCodeBankUnavailable {
		t.Fatalf("K14: BANK-A code = %v, want BANK_UNAVAILABLE", adapterErr.Code)
	}
	if !strings.Contains(adapterErr.Message, "BANK-A") {
		t.Errorf("K14: BANK-A message does not reference BANK-A: %q", adapterErr.Message)
	}
	if strings.Contains(adapterErr.Message, "BANK-B") || strings.Contains(strings.ToLower(adapterErr.Message), "bank b") {
		t.Errorf("K14: BANK-A message references BANK-B: %q", adapterErr.Message)
	}
}

// ---------------------------------------------------------------------------
// K15: Typed bank error codes — all defined codes are distinct and properly
// round-trip through errors.As.
// ---------------------------------------------------------------------------

func TestK15_TypedBankErrorCodesAreDistinctAndCorrect(t *testing.T) {
	codes := []bank.ErrorCode{
		bank.ErrCodeInsufficientFunds,
		bank.ErrCodeInvalidAccount,
		bank.ErrCodeInactiveAccount,
		bank.ErrCodeBankUnavailable,
		bank.ErrCodeTransientFailure,
		bank.ErrCodePermanentFailure,
	}
	seen := make(map[bank.ErrorCode]bool)
	for _, code := range codes {
		if string(code) == "" {
			t.Errorf("K15: error code is empty string")
		}
		if seen[code] {
			t.Errorf("K15: duplicate error code: %v", code)
		}
		seen[code] = true

		err := &bank.AdapterError{Code: code, Message: "test"}
		var adapterErr *bank.AdapterError
		if !errors.As(err, &adapterErr) {
			t.Errorf("K15: errors.As failed for code %v", code)
		}
		if adapterErr.Code != code {
			t.Errorf("K15: code round-trip = %v, want %v", adapterErr.Code, code)
		}
	}
}

func TestK15_CustomerSafeReasonMapsDistinctCodesDistinctly(t *testing.T) {
	cases := []struct {
		code bank.ErrorCode
		want string
	}{
		{bank.ErrCodeInsufficientFunds, "Insufficient funds."},
		{bank.ErrCodeInvalidAccount, "Account could not be found."},
		{bank.ErrCodeInactiveAccount, "Account is inactive."},
		{bank.ErrCodeBankUnavailable, "Bank service is temporarily unavailable."},
		{bank.ErrCodeTransientFailure, "Payment is still being confirmed."},
		{bank.ErrCodePermanentFailure, "Payment could not be completed."},
	}
	seen := make(map[string]bank.ErrorCode)
	for _, c := range cases {
		got := customerSafeFailureReason(&bank.AdapterError{Code: c.code, Message: "internal"})
		if got != c.want {
			t.Errorf("K15: code %v: got %q, want %q", c.code, got, c.want)
		}
		if prev, conflict := seen[got]; conflict {
			t.Errorf("K15: codes %v and %v produce identical safe reason %q", prev, c.code, got)
		}
		seen[got] = c.code
	}
}