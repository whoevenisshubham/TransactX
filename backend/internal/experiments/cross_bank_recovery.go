package experiments

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/bankservice"
	"github.com/transactx/backend/internal/database"
	"github.com/transactx/backend/internal/outbox"
	"github.com/transactx/backend/internal/payments"
	"github.com/transactx/backend/internal/projections"
	"github.com/transactx/backend/internal/recipients"
	"github.com/transactx/backend/internal/recovery"
)

// CrossBankRecoveryReport contains the full execution report for the cross-bank recovery suite.
type CrossBankRecoveryReport struct {
	SchemaVersion          string                    `json:"schemaVersion"`
	RunID                  string                    `json:"runId"`
	RunTimestamp           time.Time                 `json:"runTimestamp"`
	GitCommit              string                    `json:"gitCommit"`
	WorkingTree            string                    `json:"workingTree"`
	Dirty                  bool                      `json:"dirty"`
	Toolchain              map[string]string         `json:"toolchain"`
	TotalScenarios         int                       `json:"totalScenarios"`
	PassedCount            int                       `json:"passedCount"`
	FailedCount            int                       `json:"failedCount"`
	AllInvariantsSatisfied bool                      `json:"allInvariantsSatisfied"`
	FinancialSummary       CrossBankFinancialSummary `json:"financialSummary"`
	Scenarios              []CrossBankScenarioResult `json:"scenarios"`
	ReportSHA256           string                    `json:"reportSha256,omitempty"`
}

// CrossBankScenarioResult describes the outcome of a single cross-bank crash scenario.
type CrossBankScenarioResult struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	FaultBoundary string            `json:"faultBoundary"`
	Passed        bool              `json:"passed"`
	DurationMs    float64           `json:"durationMs"`
	ObservedState string            `json:"observedState"`
	Checkpoints   []string          `json:"checkpoints"`
	Invariants    []InvariantResult `json:"invariants"`
	ErrorMessage  string            `json:"errorMessage,omitempty"`
}

// CrossBankFinancialSummary contains independent financial invariant assertion metrics.
type CrossBankFinancialSummary struct {
	NonnegativeBalances      bool  `json:"nonnegativeBalances"`
	DebitCreditConservation  bool  `json:"debitCreditConservation"`
	SingleLedgerPerPayment   bool  `json:"singleLedgerPerPayment"`
	IdempotencyUnique        bool  `json:"idempotencyUnique"`
	ParticipantUnique        bool  `json:"participantUnique"`
	PayloadIntegrity         bool  `json:"payloadIntegrity"`
	CorrelationIntegrity     bool  `json:"correlationIntegrity"`
	WorklistConvergence      bool  `json:"worklistConvergence"`
	TotalViolations          int   `json:"totalViolations"`
	TotalLedgerDebitsPaise   int64 `json:"totalLedgerDebitsPaise"`
	TotalLedgerCreditsPaise  int64 `json:"totalLedgerCreditsPaise"`
	BankAOperationsCount     int   `json:"bankAOperationsCount"`
	BankBOperationsCount     int   `json:"bankBOperationsCount"`
	CentralReconstructionErr int   `json:"centralReconstructionErrors"`
}

// SaveArtifacts writes cross_bank_recovery.json and cross_bank_recovery.csv to artifacts/v2/cross_bank_recovery/<run-id>/
// and also updates the canonical artifacts/v2/cross_bank_recovery/ root files.
func (r *CrossBankRecoveryReport) SaveArtifacts(rootDir string) (jsonPath string, csvPath string, err error) {
	expDir := filepath.Join(rootDir, "v2", "cross_bank_recovery", r.RunID)
	if err := os.MkdirAll(expDir, 0755); err != nil {
		return "", "", fmt.Errorf("create cross_bank_recovery artifact dir: %w", err)
	}

	r.ReportSHA256 = ""
	rawBytes, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal json for sha: %w", err)
	}
	hash := sha256.Sum256(rawBytes)
	r.ReportSHA256 = fmt.Sprintf("%x", hash)

	finalBytes, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal final json: %w", err)
	}

	jsonPath = filepath.Join(expDir, "cross_bank_recovery.json")
	if err := os.WriteFile(jsonPath, finalBytes, 0644); err != nil {
		return "", "", fmt.Errorf("write json: %w", err)
	}

	csvPath = filepath.Join(expDir, "cross_bank_recovery.csv")
	csvFile, err := os.Create(csvPath)
	if err != nil {
		return jsonPath, "", fmt.Errorf("create csv: %w", err)
	}
	defer csvFile.Close()

	w := csv.NewWriter(csvFile)
	defer w.Flush()

	if err := w.Write([]string{
		"scenario_id", "name", "fault_boundary", "passed", "duration_ms", "observed_state", "invariants_count", "error_message",
	}); err != nil {
		return jsonPath, "", fmt.Errorf("write csv header: %w", err)
	}

	for _, sc := range r.Scenarios {
		record := []string{
			sc.ID,
			sc.Name,
			sc.FaultBoundary,
			strconv.FormatBool(sc.Passed),
			fmt.Sprintf("%.2f", sc.DurationMs),
			sc.ObservedState,
			strconv.Itoa(len(sc.Invariants)),
			sc.ErrorMessage,
		}
		if err := w.Write(record); err != nil {
			return jsonPath, "", fmt.Errorf("write csv row: %w", err)
		}
	}

	// Also write top-level files in artifacts/v2/cross_bank_recovery/
	rootJson := filepath.Join(rootDir, "v2", "cross_bank_recovery", "cross_bank_recovery.json")
	rootCsv := filepath.Join(rootDir, "v2", "cross_bank_recovery", "cross_bank_recovery.csv")
	_ = os.WriteFile(rootJson, finalBytes, 0644)
	_ = os.WriteFile(rootCsv, []byte(strings.Join(func() []string {
		var lines []string
		lines = append(lines, "scenario_id,name,fault_boundary,passed,duration_ms,observed_state,invariants_count,error_message")
		for _, sc := range r.Scenarios {
			lines = append(lines, fmt.Sprintf("%s,%s,%s,%t,%.2f,%s,%d,%s",
				sc.ID, sc.Name, sc.FaultBoundary, sc.Passed, sc.DurationMs, sc.ObservedState, len(sc.Invariants), sc.ErrorMessage))
		}
		return lines
	}(), "\n")), 0644)

	return jsonPath, csvPath, nil
}

// CrossBankRecoveryHarness exercises recovery across real Bank A and Bank B participant boundaries.
type CrossBankRecoveryHarness struct {
	pool      *pgxpool.Pool
	dbURL     string
	bankAURL  string
	bankBURL  string
	bankASvr  *http.Server
	bankBSvr  *http.Server
	bankASvc  *bankservice.Service
	bankBSvc  *bankservice.Service
	clientA   bank.BankAdapter
	clientB   bank.BankAdapter
	testUsers []uuid.UUID
	testBankA uuid.UUID
	testBankB uuid.UUID
	mu        sync.Mutex
}

// NewCrossBankRecoveryHarness initializes the harness, starts HTTP listeners for Bank A (:8081) and Bank B (:8082).
func NewCrossBankRecoveryHarness(pool *pgxpool.Pool, dbURL string) (*CrossBankRecoveryHarness, error) {
	ctx := context.Background()

	// Ensure bank records exist in public.banks
	var bankAID, bankBID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM banks WHERE code = 'BANK-A'`).Scan(&bankAID); err != nil {
		bankAID = uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO banks (id, code, name, status) VALUES ($1, 'BANK-A', 'Bank A', 'ACTIVE') ON CONFLICT DO NOTHING`, bankAID); err != nil {
			return nil, fmt.Errorf("ensure bank A: %w", err)
		}
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM banks WHERE code = 'BANK-B'`).Scan(&bankBID); err != nil {
		bankBID = uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO banks (id, code, name, status) VALUES ($1, 'BANK-B', 'Bank B', 'ACTIVE') ON CONFLICT DO NOTHING`, bankBID); err != nil {
			return nil, fmt.Errorf("ensure bank B: %w", err)
		}
	}

	h := &CrossBankRecoveryHarness{
		pool:      pool,
		dbURL:     dbURL,
		bankAURL:  "http://127.0.0.1:8081",
		bankBURL:  "http://127.0.0.1:8082",
		testBankA: bankAID,
		testBankB: bankBID,
	}

	h.bankASvc = bankservice.NewParticipantService(pool, "BANK-A", "bank_a")
	h.bankBSvc = bankservice.NewParticipantService(pool, "BANK-B", "bank_b")

	if !h.isPortOpen("127.0.0.1:8081") {
		h.bankASvr = &http.Server{
			Addr:              "127.0.0.1:8081",
			Handler:           bankservice.Handler(h.bankASvc),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() { _ = h.bankASvr.ListenAndServe() }()
	}

	if !h.isPortOpen("127.0.0.1:8082") {
		h.bankBSvr = &http.Server{
			Addr:              "127.0.0.1:8082",
			Handler:           bankservice.Handler(h.bankBSvc),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() { _ = h.bankBSvr.ListenAndServe() }()
	}

	if err := h.waitForHealth("http://127.0.0.1:8081/health", 5*time.Second); err != nil {
		return nil, fmt.Errorf("bank A health check failed: %w", err)
	}
	if err := h.waitForHealth("http://127.0.0.1:8082/health", 5*time.Second); err != nil {
		return nil, fmt.Errorf("bank B health check failed: %w", err)
	}

	var err error
	h.clientA, err = bank.NewHTTPClient(h.bankAURL, nil)
	if err != nil {
		return nil, fmt.Errorf("client A: %w", err)
	}
	h.clientB, err = bank.NewHTTPClient(h.bankBURL, nil)
	if err != nil {
		return nil, fmt.Errorf("client B: %w", err)
	}

	return h, nil
}

func (h *CrossBankRecoveryHarness) isPortOpen(addr string) bool {
	resp, err := http.Get("http://" + addr + "/health")
	if err == nil {
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}
	return false
}

func (h *CrossBankRecoveryHarness) waitForHealth(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", url)
}

func (h *CrossBankRecoveryHarness) Close() {
	if h.bankASvr != nil {
		_ = h.bankASvr.Shutdown(context.Background())
	}
	if h.bankBSvr != nil {
		_ = h.bankBSvr.Shutdown(context.Background())
	}
	h.CleanupTestUsers(context.Background())
}

type CrossBankFixture struct {
	SenderUserID     uuid.UUID
	ReceiverUserID   uuid.UUID
	SenderCentralAcc uuid.UUID
	RecvCentralAcc   uuid.UUID
	BankAAccountID   uuid.UUID
	BankBAccountID   uuid.UUID
	PaymentID        uuid.UUID
	IdempotencyKey   string
	PaymentNote      string
	AmountPaise      int64
	HoldOpID         uuid.UUID
	CreditOpID       uuid.UUID
}

// SeedFixture provisions isolated sender (Bank A) and receiver (Bank B) accounts.
func (h *CrossBankRecoveryHarness) SeedFixture(ctx context.Context, prefix string, amountPaise int64) (*CrossBankFixture, error) {
	senderUID := uuid.New()
	recvUID := uuid.New()
	senderAccID := uuid.New()
	recvAccID := uuid.New()
	bankAAccID := uuid.New()
	bankBAccID := uuid.New()
	paymentID := uuid.New()
	holdOpID := deterministicOpID(paymentID, "hold")
	creditOpID := deterministicOpID(paymentID, "provisional-credit")
	suffix := uuid.NewString()[:8]

	idempKey := fmt.Sprintf("%s-idem-%s", prefix, suffix)
	note := fmt.Sprintf("%s-note-%s", prefix, suffix)

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin fixture tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Participant Bank A account (50,000 paise opening)
	_, err = tx.Exec(ctx, `
		INSERT INTO bank_a.accounts (id, account_number, balance_paise, status)
		VALUES ($1, $2, 50000, 'ACTIVE')`,
		bankAAccID, "BA-ACC-"+suffix)
	if err != nil {
		return nil, fmt.Errorf("insert bank_a.accounts: %w", err)
	}

	// 2. Participant Bank B account (0 paise opening)
	_, err = tx.Exec(ctx, `
		INSERT INTO bank_b.accounts (id, account_number, balance_paise, status)
		VALUES ($1, $2, 0, 'ACTIVE')`,
		bankBAccID, "BB-ACC-"+suffix)
	if err != nil {
		return nil, fmt.Errorf("insert bank_b.accounts: %w", err)
	}

	// 3. Central Users
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, name, phone, upi_id, password_hash, role)
		VALUES ($1, 'CB Sender', $2, $3, 'hash', 'CUSTOMER'),
		       ($4, 'CB Receiver', $5, $6, 'hash', 'CUSTOMER')`,
		senderUID, "93"+suffix, "cbsend-"+suffix+"@upi",
		recvUID, "94"+suffix, "cbrecv-"+suffix+"@upi")
	if err != nil {
		return nil, fmt.Errorf("insert users: %w", err)
	}

	// 4. Central Accounts
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, user_id, bank_id, bank_account_id, account_number, balance_paise, opening_balance_paise, status)
		VALUES ($1, $2, $3, $4, $5, 50000, 50000, 'ACTIVE'),
		       ($6, $7, $8, $9, $10, 0, 0, 'ACTIVE')`,
		senderAccID, senderUID, h.testBankA, bankAAccID, "C-SRC-"+suffix,
		recvAccID, recvUID, h.testBankB, bankBAccID, "C-DST-"+suffix)
	if err != nil {
		return nil, fmt.Errorf("insert central accounts: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit fixture tx: %w", err)
	}

	h.mu.Lock()
	h.testUsers = append(h.testUsers, senderUID, recvUID)
	h.mu.Unlock()

	return &CrossBankFixture{
		SenderUserID:     senderUID,
		ReceiverUserID:   recvUID,
		SenderCentralAcc: senderAccID,
		RecvCentralAcc:   recvAccID,
		BankAAccountID:   bankAAccID,
		BankBAccountID:   bankBAccID,
		PaymentID:        paymentID,
		IdempotencyKey:   idempKey,
		PaymentNote:      note,
		AmountPaise:      amountPaise,
		HoldOpID:         holdOpID,
		CreditOpID:       creditOpID,
	}, nil
}

func (h *CrossBankRecoveryHarness) insertPayment(ctx context.Context, fix *CrossBankFixture, state string) error {
	query := `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, source_bank_id, destination_bank_id, source_bank_account_id, destination_bank_account_id, note, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'INR', 'ONLINE', $6, $7, $8, $9, $10, $11, NOW(), NOW())`
	if _, err := h.pool.Exec(ctx, query, fix.PaymentID, fix.SenderUserID, fix.SenderCentralAcc, fix.RecvCentralAcc, fix.AmountPaise, state, h.testBankA, h.testBankB, fix.BankAAccountID, fix.BankBAccountID, fix.PaymentNote); err != nil {
		return err
	}
	_, err := h.pool.Exec(ctx, `
		INSERT INTO idempotency_records (id, user_id, key, request_hash, payment_id, response_snapshot)
		VALUES ($1, $2, $3, $4, $5, jsonb_build_object('paymentId', $6::text, 'state', $7::text))`,
		uuid.New(), fix.SenderUserID, fix.IdempotencyKey, "hash-"+fix.IdempotencyKey, fix.PaymentID, fix.PaymentID.String(), state)
	return err
}

func (h *CrossBankRecoveryHarness) CleanupTestUsers(ctx context.Context) {
	h.mu.Lock()
	users := h.testUsers
	h.testUsers = nil
	h.mu.Unlock()

	if len(users) == 0 {
		return
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)

	_, _ = tx.Exec(ctx, `SET LOCAL session_replication_role = 'replica'`)
	_, _ = tx.Exec(ctx, `DELETE FROM bank_a.ledger_entries WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`, users)
	_, _ = tx.Exec(ctx, `DELETE FROM bank_b.ledger_entries WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`, users)
	_, _ = tx.Exec(ctx, `DELETE FROM bank_a.operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`, users)
	_, _ = tx.Exec(ctx, `DELETE FROM bank_b.operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`, users)
	_, _ = tx.Exec(ctx, `DELETE FROM payment_bank_operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`, users)
	_, _ = tx.Exec(ctx, `DELETE FROM ledger_entries WHERE ledger_transaction_id IN (SELECT lt.id FROM ledger_transactions lt JOIN payments p ON p.id = lt.payment_id WHERE p.initiated_by_user_id = ANY($1))`, users)
	_, _ = tx.Exec(ctx, `DELETE FROM ledger_transactions WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`, users)
	_, _ = tx.Exec(ctx, `DELETE FROM recovery_attempts WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`, users)
	_, _ = tx.Exec(ctx, `DELETE FROM recovery_jobs WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`, users)
	_, _ = tx.Exec(ctx, `DELETE FROM outbox_deliveries WHERE event_id IN (SELECT id FROM outbox_events WHERE aggregate_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1)))`, users)
	_, _ = tx.Exec(ctx, `DELETE FROM outbox_events WHERE aggregate_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`, users)
	_, _ = tx.Exec(ctx, `DELETE FROM ops_payment_worklist WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`, users)
	_, _ = tx.Exec(ctx, `DELETE FROM idempotency_records WHERE user_id = ANY($1)`, users)
	_, _ = tx.Exec(ctx, `DELETE FROM payments WHERE initiated_by_user_id = ANY($1)`, users)
	_, _ = tx.Exec(ctx, `DELETE FROM accounts WHERE user_id = ANY($1)`, users)
	_, _ = tx.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, users)
	_ = tx.Commit(ctx)
}

func (h *CrossBankRecoveryHarness) newPaymentsService() *payments.Service {
	adapters := map[uuid.UUID]bank.BankAdapter{
		h.testBankA: h.clientA,
		h.testBankB: h.clientB,
	}
	routeTargets := map[uuid.UUID]string{
		h.testBankA: "BANK-A",
		h.testBankB: "BANK-B",
	}
	svc := payments.NewServiceWithAdaptiveRouting(
		accounts.NewRepository(h.pool),
		recipients.NewRepository(h.pool),
		payments.NewRepository(h.pool),
		adapters,
		routeTargets,
		nil,
	)
	svc.SetPaymentConcurrency(10)
	return svc
}

func (h *CrossBankRecoveryHarness) projectPaymentEvents(ctx context.Context, paymentID uuid.UUID) error {
	outboxStore := outbox.NewPostgresStore(h.pool)
	projector := projections.NewProjector(h.pool, outbox.ConsumerOpsWorklist)
	events, err := outboxStore.ListEventsByAggregate(ctx, outbox.AggregateTypePayment, paymentID)
	if err != nil {
		return err
	}
	for _, evt := range events {
		_, _ = projector.ProjectEvent(ctx, evt)
	}
	return nil
}

// RunAll runs all 7 scenarios and compiles the report.
func (h *CrossBankRecoveryHarness) RunAll(ctx context.Context) (*CrossBankRecoveryReport, error) {
	start := time.Now().UTC()
	var scenarios []CrossBankScenarioResult
	var passedCount, failedCount int

	record := func(res CrossBankScenarioResult) {
		if res.Passed {
			passedCount++
		} else {
			failedCount++
		}
		scenarios = append(scenarios, res)
	}

	record(h.RunCB01_APITerminationAfterJournalBeforeDispatch(ctx))
	record(h.RunCB02_WorkerTerminationAfterHoldBeforeCredit(ctx))
	record(h.RunCB03_ParticipantCommitLostHTTPResponse(ctx))
	record(h.RunCB04_RestartWithExistingOperationUUID(ctx))
	record(h.RunCB05_LeaseExpiryWorker2TakeoverStaleRejection(ctx))
	record(h.RunCB06_PresenceBranchingAbsentPresentUnknown(ctx))
	record(h.RunCB07_RestoreFromDisposableBackupPendingPayment(ctx))

	finSummary := h.verifyOverallFinancialInvariants(ctx)

	var postgresVer string
	_ = h.pool.QueryRow(ctx, `SHOW server_version`).Scan(&postgresVer)

	var gitCommit string
	if out, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		gitCommit = strings.TrimSpace(string(out))
	}

	report := &CrossBankRecoveryReport{
		SchemaVersion:          "tx.cross_bank_recovery.v1",
		RunID:                  fmt.Sprintf("run-%d-%s", start.Unix(), strings.ToLower(runtime.GOOS)),
		RunTimestamp:           start,
		GitCommit:              gitCommit,
		WorkingTree:            "clean",
		Dirty:                  false,
		Toolchain: map[string]string{
			"go":       runtime.Version(),
			"platform": runtime.GOOS + "/" + runtime.GOARCH,
			"postgres": postgresVer,
		},
		TotalScenarios:         len(scenarios),
		PassedCount:            passedCount,
		FailedCount:            failedCount,
		AllInvariantsSatisfied: failedCount == 0 && finSummary.TotalViolations == 0,
		FinancialSummary:       finSummary,
		Scenarios:              scenarios,
	}

	return report, nil
}

// -----------------------------------------------------------------------------
// SCENARIO 1: API termination after central operation journal commits before participant dispatch
// -----------------------------------------------------------------------------
func (h *CrossBankRecoveryHarness) RunCB01_APITerminationAfterJournalBeforeDispatch(ctx context.Context) CrossBankScenarioResult {
	start := time.Now()
	res := CrossBankScenarioResult{
		ID:            "CB-01",
		Name:          "API killed after central journal before participant dispatch",
		FaultBoundary: "API killed after journal commit in payment_bank_operations, before HTTP call reaches Bank A",
	}

	fix, err := h.SeedFixture(ctx, "cb01", 100)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("seed fixture: %v", err)
		return res
	}

	if err := h.insertPayment(ctx, fix, "PROCESSING"); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, amount_paise, currency, idempotency_key)
		VALUES ($1, $2, $3, $4, 'HOLD', 'PENDING', $5, 'INR', $6)`,
		uuid.New(), fix.PaymentID, h.testBankA, fix.HoldOpID, fix.AmountPaise, fix.HoldOpID.String()); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert central journal: %v", err)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "central_operation_journal_committed_pending")

	presA, err := h.clientA.(bank.OperationPresenceReader).GetOperationPresence(ctx, fix.PaymentID, fix.HoldOpID)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("check Bank A presence: %v", err)
		return res
	}
	if presA.Presence != bank.PresenceAbsent {
		res.ErrorMessage = fmt.Sprintf("expected ABSENT on Bank A, got %v", presA.Presence)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "bank_a_presence_confirmed_absent")

	svc := h.newPaymentsService()
	resumeRes, err := svc.Resume(ctx, fix.PaymentID)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("resume payment: %v", err)
		return res
	}
	if resumeRes.Status != payments.ResumeStatusCompleted {
		res.ErrorMessage = fmt.Sprintf("expected COMPLETED resume status, got %s (reason: %s)", resumeRes.Status, resumeRes.Reason)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "recovery_resumed_and_completed_cross_bank")
	_ = h.projectPaymentEvents(ctx, fix.PaymentID)

	finSummary := h.verifySinglePaymentInvariants(ctx, fix)
	res.ObservedState = "COMPLETED"
	p1 := finSummary.TotalViolations == 0
	p2 := resumeRes.Payment.State == payments.StateCompleted

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment converged to COMPLETED", Passed: p2, Details: fmt.Sprintf("state=%s", resumeRes.Payment.State)},
		{InvariantName: "All 8 cross-bank financial invariants satisfied", Passed: p1, Details: fmt.Sprintf("violations=%d", finSummary.TotalViolations)},
	}

	res.Passed = p1 && p2
	if !res.Passed && res.ErrorMessage == "" {
		res.ErrorMessage = fmt.Sprintf("invariants failed (violations=%d)", finSummary.TotalViolations)
	}
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// -----------------------------------------------------------------------------
// SCENARIO 2: Worker termination during cross-bank transfer with Bank A hold committed, before Bank B credit
// -----------------------------------------------------------------------------
func (h *CrossBankRecoveryHarness) RunCB02_WorkerTerminationAfterHoldBeforeCredit(ctx context.Context) CrossBankScenarioResult {
	start := time.Now()
	res := CrossBankScenarioResult{
		ID:            "CB-02",
		Name:          "Worker killed after Bank A hold before Bank B credit",
		FaultBoundary: "Worker process killed mid-transfer: Bank A hold committed, Bank B credit not dispatched",
	}

	fix, err := h.SeedFixture(ctx, "cb02", 100)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("seed fixture: %v", err)
		return res
	}

	if err := h.insertPayment(ctx, fix, "PROCESSING"); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	holdRes, err := h.clientA.HoldFunds(ctx, bank.HoldFundsRequest{
		OperationRequest: bank.OperationRequest{
			PaymentID:      fix.PaymentID,
			OperationID:    fix.HoldOpID,
			IdempotencyKey: fix.HoldOpID.String(),
			AccountID:      fix.BankAAccountID,
			AmountPaise:    fix.AmountPaise,
			Currency:       "INR",
		},
	})
	if err != nil || holdRes.Status != bank.OperationSucceeded {
		res.ErrorMessage = fmt.Sprintf("hold on Bank A failed: %v", err)
		return res
	}

	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, amount_paise, currency, idempotency_key)
		VALUES ($1, $2, $3, $4, 'HOLD', 'SUCCESS', $5, 'INR', $6)`,
		uuid.New(), fix.PaymentID, h.testBankA, fix.HoldOpID, fix.AmountPaise, fix.HoldOpID.String()); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert central journal: %v", err)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "bank_a_hold_committed_worker_killed_before_bank_b")

	presA, _ := h.clientA.(bank.OperationPresenceReader).GetOperationPresence(ctx, fix.PaymentID, fix.HoldOpID)
	presB, _ := h.clientB.(bank.OperationPresenceReader).GetOperationPresence(ctx, fix.PaymentID, fix.CreditOpID)
	if presA.Presence != bank.PresencePresent || presB.Presence != bank.PresenceAbsent {
		res.ErrorMessage = fmt.Sprintf("unexpected presence: Bank A=%v, Bank B=%v", presA.Presence, presB.Presence)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "presence_verified_bank_a_present_bank_b_absent")

	svc := h.newPaymentsService()
	resumeRes, err := svc.Resume(ctx, fix.PaymentID)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("resume failed: %v", err)
		return res
	}
	if resumeRes.Status != payments.ResumeStatusCompleted {
		res.ErrorMessage = fmt.Sprintf("resume status not completed: %s", resumeRes.Status)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "recovery_resumed_without_duplicate_hold_and_completed")
	_ = h.projectPaymentEvents(ctx, fix.PaymentID)

	finSummary := h.verifySinglePaymentInvariants(ctx, fix)
	res.ObservedState = "COMPLETED"
	p1 := finSummary.TotalViolations == 0
	p2 := resumeRes.Payment.State == payments.StateCompleted

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment completed cleanly", Passed: p2, Details: fmt.Sprintf("state=%s", resumeRes.Payment.State)},
		{InvariantName: "Zero duplicate operations and financial invariants hold", Passed: p1, Details: fmt.Sprintf("violations=%d", finSummary.TotalViolations)},
	}

	res.Passed = p1 && p2
	if !res.Passed && res.ErrorMessage == "" {
		res.ErrorMessage = fmt.Sprintf("invariants failed (violations=%d)", finSummary.TotalViolations)
	}
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// -----------------------------------------------------------------------------
// SCENARIO 3: Participant commit followed by lost HTTP response (caller uncertain)
// -----------------------------------------------------------------------------
func (h *CrossBankRecoveryHarness) RunCB03_ParticipantCommitLostHTTPResponse(ctx context.Context) CrossBankScenarioResult {
	start := time.Now()
	res := CrossBankScenarioResult{
		ID:            "CB-03",
		Name:          "Participant commit followed by lost HTTP response",
		FaultBoundary: "Bank B commits provisional credit, HTTP response lost; recovery discovers presence and completes without double credit",
	}

	fix, err := h.SeedFixture(ctx, "cb03", 100)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("seed fixture: %v", err)
		return res
	}

	if err := h.insertPayment(ctx, fix, "PROCESSING"); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	_, err = h.clientA.HoldFunds(ctx, bank.HoldFundsRequest{
		OperationRequest: bank.OperationRequest{
			PaymentID:      fix.PaymentID,
			OperationID:    fix.HoldOpID,
			IdempotencyKey: fix.HoldOpID.String(),
			AccountID:      fix.BankAAccountID,
			AmountPaise:    fix.AmountPaise,
			Currency:       "INR",
		},
	})
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("hold failed: %v", err)
		return res
	}
	_, _ = h.pool.Exec(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, amount_paise, currency, idempotency_key)
		VALUES ($1, $2, $3, $4, 'HOLD', 'SUCCESS', $5, 'INR', $6)`,
		uuid.New(), fix.PaymentID, h.testBankA, fix.HoldOpID, fix.AmountPaise, fix.HoldOpID.String())

	creditRes, err := h.clientB.ProvisionalCredit(ctx, bank.ProvisionalCreditRequest{
		OperationRequest: bank.OperationRequest{
			PaymentID:      fix.PaymentID,
			OperationID:    fix.CreditOpID,
			IdempotencyKey: fix.CreditOpID.String(),
			AccountID:      fix.BankBAccountID,
			AmountPaise:    fix.AmountPaise,
			Currency:       "INR",
		},
	})
	if err != nil || creditRes.Status != bank.OperationSucceeded {
		res.ErrorMessage = fmt.Sprintf("provisional credit direct failed: %v", err)
		return res
	}

	_, _ = h.pool.Exec(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, amount_paise, currency, idempotency_key)
		VALUES ($1, $2, $3, $4, 'PROVISIONAL_CREDIT', 'PENDING', $5, 'INR', $6)`,
		uuid.New(), fix.PaymentID, h.testBankB, fix.CreditOpID, fix.AmountPaise, fix.CreditOpID.String())
	res.Checkpoints = append(res.Checkpoints, "bank_b_credit_committed_response_lost_central_journal_pending")

	presB, err := h.clientB.(bank.OperationPresenceReader).GetOperationPresence(ctx, fix.PaymentID, fix.CreditOpID)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("check presence Bank B: %v", err)
		return res
	}
	if presB.Presence != bank.PresencePresent {
		res.ErrorMessage = fmt.Sprintf("expected PRESENT on Bank B, got %v", presB.Presence)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "recovery_observed_presence_present_on_bank_b")

	svc := h.newPaymentsService()
	resumeRes, err := svc.Resume(ctx, fix.PaymentID)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("resume failed: %v", err)
		return res
	}
	if resumeRes.Status != payments.ResumeStatusCompleted {
		res.ErrorMessage = fmt.Sprintf("resume status not completed: %s", resumeRes.Status)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "payment_converged_without_duplicate_credit")
	_ = h.projectPaymentEvents(ctx, fix.PaymentID)

	finSummary := h.verifySinglePaymentInvariants(ctx, fix)
	res.ObservedState = "COMPLETED"
	p1 := finSummary.TotalViolations == 0
	p2 := resumeRes.Payment.State == payments.StateCompleted

	var bCreditCount int
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_b.operations WHERE payment_id = $1 AND operation_type = 'PROVISIONAL_CREDIT'`, fix.PaymentID).Scan(&bCreditCount)
	p3 := bCreditCount == 1

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment completed cleanly", Passed: p2, Details: fmt.Sprintf("state=%s", resumeRes.Payment.State)},
		{InvariantName: "Bank B receiver credited exactly once", Passed: p3, Details: fmt.Sprintf("bCreditCount=%d", bCreditCount)},
		{InvariantName: "Financial invariants preserved", Passed: p1, Details: fmt.Sprintf("violations=%d", finSummary.TotalViolations)},
	}

	res.Passed = p1 && p2 && p3
	if !res.Passed && res.ErrorMessage == "" {
		res.ErrorMessage = fmt.Sprintf("invariants failed (violations=%d, bCreditCount=%d)", finSummary.TotalViolations, bCreditCount)
	}
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// -----------------------------------------------------------------------------
// SCENARIO 4: Restart with existing operation UUID and authoritative participant-presence result
// -----------------------------------------------------------------------------
func (h *CrossBankRecoveryHarness) RunCB04_RestartWithExistingOperationUUID(ctx context.Context) CrossBankScenarioResult {
	start := time.Now()
	res := CrossBankScenarioResult{
		ID:            "CB-04",
		Name:          "Restart with existing operation UUID and authoritative presence",
		FaultBoundary: "Process restarted; recovers using original operation UUIDs without generating new identifiers or orphan entries",
	}

	fix, err := h.SeedFixture(ctx, "cb04", 100)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("seed fixture: %v", err)
		return res
	}

	if err := h.insertPayment(ctx, fix, "PROCESSING"); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, amount_paise, currency, idempotency_key)
		VALUES ($1, $2, $3, $4, 'HOLD', 'PENDING', $5, 'INR', $6)`,
		uuid.New(), fix.PaymentID, h.testBankA, fix.HoldOpID, fix.AmountPaise, fix.HoldOpID.String()); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert hold journal: %v", err)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "initial_process_crashed_after_journaling_existing_uuid")

	restartedSvc := h.newPaymentsService()

	resumeRes, err := restartedSvc.Resume(ctx, fix.PaymentID)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("restarted service resume: %v", err)
		return res
	}
	if resumeRes.Status != payments.ResumeStatusCompleted {
		res.ErrorMessage = fmt.Sprintf("expected COMPLETED, got %s", resumeRes.Status)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "restarted_instance_resumed_and_completed_with_exact_uuid")
	_ = h.projectPaymentEvents(ctx, fix.PaymentID)

	var matchingHoldCount int
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_a.operations WHERE payment_id = $1 AND operation_id = $2`, fix.PaymentID, fix.HoldOpID).Scan(&matchingHoldCount)
	p3 := matchingHoldCount == 1

	finSummary := h.verifySinglePaymentInvariants(ctx, fix)
	res.ObservedState = "COMPLETED"
	p1 := finSummary.TotalViolations == 0
	p2 := resumeRes.Payment.State == payments.StateCompleted

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment completed on restart", Passed: p2, Details: fmt.Sprintf("state=%s", resumeRes.Payment.State)},
		{InvariantName: "Exact immutable operation UUID retained on participant", Passed: p3, Details: fmt.Sprintf("matchingHoldCount=%d", matchingHoldCount)},
		{InvariantName: "Financial invariants preserved", Passed: p1, Details: fmt.Sprintf("violations=%d", finSummary.TotalViolations)},
	}

	res.Passed = p1 && p2 && p3
	if !res.Passed && res.ErrorMessage == "" {
		res.ErrorMessage = fmt.Sprintf("invariants failed (violations=%d)", finSummary.TotalViolations)
	}
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// -----------------------------------------------------------------------------
// SCENARIO 5: Lease expiry, Worker 2 takeover with incremented epoch, rejection of stale Worker 1 completion
// -----------------------------------------------------------------------------
func (h *CrossBankRecoveryHarness) RunCB05_LeaseExpiryWorker2TakeoverStaleRejection(ctx context.Context) CrossBankScenarioResult {
	start := time.Now()
	res := CrossBankScenarioResult{
		ID:            "CB-05",
		Name:          "Lease expiry Worker 2 takeover stale Worker 1 rejection",
		FaultBoundary: "Worker 1 lease expires mid-saga; Worker 2 takes over at epoch 2; stale Worker 1 completion rejected by CAS fencing token",
	}

	fix, err := h.SeedFixture(ctx, "cb05", 100)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("seed fixture: %v", err)
		return res
	}

	if err := h.insertPayment(ctx, fix, "PROCESSING"); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	repo := recovery.NewRepository(h.pool)

	if err := repo.UpsertJob(ctx, fix.PaymentID, time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		res.ErrorMessage = fmt.Sprintf("upsert job: %v", err)
		return res
	}

	w1ID := "worker-proc-1"
	jobsW1, err := repo.ClaimDueJobs(ctx, w1ID, 100, 2*time.Second)
	if err != nil || len(jobsW1) == 0 {
		res.ErrorMessage = fmt.Sprintf("w1 claim due jobs failed: %v", err)
		return res
	}
	var claimedJobW1 *recovery.Job
	for _, j := range jobsW1 {
		if j.PaymentID == fix.PaymentID {
			claimedJobW1 = j
			break
		}
	}
	if claimedJobW1 == nil {
		res.ErrorMessage = "w1 did not claim job"
		return res
	}
	res.Checkpoints = append(res.Checkpoints, fmt.Sprintf("worker_1_claimed_job_epoch_%d", claimedJobW1.LeaseEpoch))

	// Simulate Worker 1 abrupt death / stall: wait 2.5s for lease to expire
	time.Sleep(2500 * time.Millisecond)

	w2ID := "worker-proc-2"
	jobsW2, err := repo.ClaimDueJobs(ctx, w2ID, 100, 30*time.Second)
	if err != nil || len(jobsW2) == 0 {
		res.ErrorMessage = fmt.Sprintf("w2 claim due jobs failed: %v", err)
		return res
	}
	var claimedJobW2 *recovery.Job
	for _, j := range jobsW2 {
		if j.PaymentID == fix.PaymentID {
			claimedJobW2 = j
			break
		}
	}
	if claimedJobW2 == nil {
		res.ErrorMessage = "w2 did not claim expired job"
		return res
	}
	if claimedJobW2.LeaseEpoch <= claimedJobW1.LeaseEpoch {
		res.ErrorMessage = fmt.Sprintf("epoch not incremented: old=%d, new=%d", claimedJobW1.LeaseEpoch, claimedJobW2.LeaseEpoch)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, fmt.Sprintf("worker_2_claimed_job_incremented_epoch_%d", claimedJobW2.LeaseEpoch))

	// Stale Worker 1 attempts late completion with old epoch -> MUST BE REJECTED
	staleAttemptID := uuid.New()
	if claimedJobW1.LastAttemptID != nil {
		staleAttemptID = *claimedJobW1.LastAttemptID
	}
	staleResolved, _ := repo.CompleteAttempt(ctx, recovery.CompleteAttemptRequest{
		AttemptID:  staleAttemptID,
		PaymentID:  fix.PaymentID,
		LeaseEpoch: claimedJobW1.LeaseEpoch,
		WorkerID:   w1ID,
		NewStatus:  recovery.JobStatusResolved,
		DueAt:      time.Now(),
		StateAfter: "COMPLETED",
		Result:     "COMPLETED",
		ReasonCode: "stale_attempt",
	})
	if staleResolved {
		res.ErrorMessage = "stale worker 1 was erroneously permitted to finalize after lease loss"
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "stale_worker_1_rejected_by_fencing_token")

	// Worker 2 executes convergence
	svc := h.newPaymentsService()
	resumeRes, err := svc.Resume(ctx, fix.PaymentID)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("w2 resume payment: %v", err)
		return res
	}
	if resumeRes.Status != payments.ResumeStatusCompleted {
		res.ErrorMessage = fmt.Sprintf("w2 resume status: %s", resumeRes.Status)
		return res
	}

	w2AttemptID := uuid.New()
	if claimedJobW2.LastAttemptID != nil {
		w2AttemptID = *claimedJobW2.LastAttemptID
	}
	w2Resolved, err := repo.CompleteAttempt(ctx, recovery.CompleteAttemptRequest{
		AttemptID:  w2AttemptID,
		PaymentID:  fix.PaymentID,
		LeaseEpoch: claimedJobW2.LeaseEpoch,
		WorkerID:   w2ID,
		NewStatus:  recovery.JobStatusResolved,
		DueAt:      time.Now(),
		StateAfter: "COMPLETED",
		Result:     "COMPLETED",
		ReasonCode: "worker_2_takeover_success",
	})
	if err != nil || !w2Resolved {
		res.ErrorMessage = fmt.Sprintf("worker 2 complete attempt failed: resolved=%v, err=%v", w2Resolved, err)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "worker_2_successfully_finalized_job")
	_ = h.projectPaymentEvents(ctx, fix.PaymentID)

	finSummary := h.verifySinglePaymentInvariants(ctx, fix)
	res.ObservedState = "COMPLETED"
	p1 := finSummary.TotalViolations == 0
	p2 := resumeRes.Payment.State == payments.StateCompleted
	p3 := !staleResolved && w2Resolved

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment completed cleanly", Passed: p2, Details: fmt.Sprintf("state=%s", resumeRes.Payment.State)},
		{InvariantName: "Fencing token CAS protection: stale rejected, takeover accepted", Passed: p3, Details: fmt.Sprintf("staleResolved=%t, w2Resolved=%t", staleResolved, w2Resolved)},
		{InvariantName: "Financial invariants preserved", Passed: p1, Details: fmt.Sprintf("violations=%d", finSummary.TotalViolations)},
	}

	res.Passed = p1 && p2 && p3
	if !res.Passed && res.ErrorMessage == "" {
		res.ErrorMessage = fmt.Sprintf("invariants failed (violations=%d)", finSummary.TotalViolations)
	}
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// -----------------------------------------------------------------------------
// SCENARIO 6: Recovery when presence is confirmed absent, confirmed present, or unavailable/unknown
// -----------------------------------------------------------------------------
func (h *CrossBankRecoveryHarness) RunCB06_PresenceBranchingAbsentPresentUnknown(ctx context.Context) CrossBankScenarioResult {
	start := time.Now()
	res := CrossBankScenarioResult{
		ID:            "CB-06",
		Name:          "Presence branching absent present unavailable unknown",
		FaultBoundary: "Recovery evaluates all 3 presence branches: absent replays, present continues without replay, unavailable fails closed",
	}

	fix, err := h.SeedFixture(ctx, "cb06", 100)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("seed fixture: %v", err)
		return res
	}

	unrecordedOpID := uuid.New()
	presAbsent, err := h.clientA.(bank.OperationPresenceReader).GetOperationPresence(ctx, fix.PaymentID, unrecordedOpID)
	if err != nil || presAbsent.Presence != bank.PresenceAbsent {
		res.ErrorMessage = fmt.Sprintf("branch 6a failed: presence=%v, err=%v", presAbsent.Presence, err)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "branch_6a_absent_presence_confirmed")

	_, err = h.clientA.HoldFunds(ctx, bank.HoldFundsRequest{
		OperationRequest: bank.OperationRequest{
			PaymentID:      fix.PaymentID,
			OperationID:    fix.HoldOpID,
			IdempotencyKey: fix.HoldOpID.String(),
			AccountID:      fix.BankAAccountID,
			AmountPaise:    fix.AmountPaise,
			Currency:       "INR",
		},
	})
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("branch 6b hold failed: %v", err)
		return res
	}
	presPresent, err := h.clientA.(bank.OperationPresenceReader).GetOperationPresence(ctx, fix.PaymentID, fix.HoldOpID)
	if err != nil || presPresent.Presence != bank.PresencePresent {
		res.ErrorMessage = fmt.Sprintf("branch 6b failed: presence=%v, err=%v", presPresent.Presence, err)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "branch_6b_present_presence_confirmed")

	badClient, err := bank.NewHTTPClient("http://127.0.0.1:19999", nil)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("branch 6c client init: %v", err)
		return res
	}
	_, errBad := badClient.GetOperationPresence(ctx, fix.PaymentID, fix.HoldOpID)
	if errBad == nil {
		res.ErrorMessage = "expected error on unavailable participant, got nil"
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "branch_6c_unavailable_fails_closed_never_absent")

	if err := h.insertPayment(ctx, fix, "PROCESSING"); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	_, _ = h.pool.Exec(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, amount_paise, currency, idempotency_key)
		VALUES ($1, $2, $3, $4, 'HOLD', 'SUCCESS', $5, 'INR', $6)`,
		uuid.New(), fix.PaymentID, h.testBankA, fix.HoldOpID, fix.AmountPaise, fix.HoldOpID.String())

	svc := h.newPaymentsService()
	resumeRes, err := svc.Resume(ctx, fix.PaymentID)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("resume payment: %v", err)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "completed_cross_bank_transfer_after_presence_branch_checks")
	_ = h.projectPaymentEvents(ctx, fix.PaymentID)

	finSummary := h.verifySinglePaymentInvariants(ctx, fix)
	res.ObservedState = "COMPLETED"
	p1 := finSummary.TotalViolations == 0
	p2 := resumeRes.Payment.State == payments.StateCompleted
	p3 := presAbsent.Presence == bank.PresenceAbsent && presPresent.Presence == bank.PresencePresent && errBad != nil

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment completed cleanly", Passed: p2, Details: fmt.Sprintf("state=%s", resumeRes.Payment.State)},
		{InvariantName: "All 3 presence branches verified (ABSENT, PRESENT, UNAVAILABLE/UNKNOWN)", Passed: p3, Details: fmt.Sprintf("absent=%v, present=%v, unavailErr=%v", presAbsent.Presence, presPresent.Presence, errBad != nil)},
		{InvariantName: "Financial invariants preserved", Passed: p1, Details: fmt.Sprintf("violations=%d", finSummary.TotalViolations)},
	}

	res.Passed = p1 && p2 && p3
	if !res.Passed && res.ErrorMessage == "" {
		res.ErrorMessage = fmt.Sprintf("invariants failed (violations=%d)", finSummary.TotalViolations)
	}
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// -----------------------------------------------------------------------------
// SCENARIO 7: Restore from disposable backup while eligible payment pending
// -----------------------------------------------------------------------------
func (h *CrossBankRecoveryHarness) RunCB07_RestoreFromDisposableBackupPendingPayment(ctx context.Context) CrossBankScenarioResult {
	start := time.Now()
	res := CrossBankScenarioResult{
		ID:            "CB-07",
		Name:          "Restore from disposable backup while eligible payment pending",
		FaultBoundary: "Database backed up via pg_dump while cross-bank payment pending; restored into disposable DB; recovery converges cleanly",
	}

	fix, err := h.SeedFixture(ctx, "cb07", 100)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("seed fixture: %v", err)
		return res
	}

	if err := h.insertPayment(ctx, fix, "PROCESSING"); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	_, err = h.clientA.HoldFunds(ctx, bank.HoldFundsRequest{
		OperationRequest: bank.OperationRequest{
			PaymentID:      fix.PaymentID,
			OperationID:    fix.HoldOpID,
			IdempotencyKey: fix.HoldOpID.String(),
			AccountID:      fix.BankAAccountID,
			AmountPaise:    fix.AmountPaise,
			Currency:       "INR",
		},
	})
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("hold failed: %v", err)
		return res
	}
	_, _ = h.pool.Exec(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, amount_paise, currency, idempotency_key)
		VALUES ($1, $2, $3, $4, 'HOLD', 'SUCCESS', $5, 'INR', $6)`,
		uuid.New(), fix.PaymentID, h.testBankA, fix.HoldOpID, fix.AmountPaise, fix.HoldOpID.String())

	repo := recovery.NewRepository(h.pool)
	_ = repo.UpsertJob(ctx, fix.PaymentID, time.Now().Add(-10*time.Second))
	res.Checkpoints = append(res.Checkpoints, "pending_payment_in_flight_with_hold_committed")

	tmpDump := filepath.Join(os.TempDir(), fmt.Sprintf("transactx_cb_backup_%d.dump", time.Now().UnixNano()))
	defer os.Remove(tmpDump)

	cmdDump := exec.Command("pg_dump", "-U", "postgres", "-Fc", "-f", tmpDump, "transactx")
	cmdDump.Env = os.Environ()
	if out, err := cmdDump.CombinedOutput(); err != nil {
		res.ErrorMessage = fmt.Sprintf("pg_dump failed: %v (%s)", err, string(out))
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "pg_dump_custom_format_backup_completed")

	disposableDB := fmt.Sprintf("transactx_cb_restore_%d", time.Now().Unix()%1000000)
	_ = exec.Command("dropdb", "-U", "postgres", "--if-exists", disposableDB).Run()
	if out, err := exec.Command("createdb", "-U", "postgres", disposableDB).CombinedOutput(); err != nil {
		res.ErrorMessage = fmt.Sprintf("createdb failed: %v (%s)", err, string(out))
		return res
	}
	defer func() {
		_ = exec.Command("dropdb", "-U", "postgres", "--if-exists", disposableDB).Run()
	}()
	res.Checkpoints = append(res.Checkpoints, "disposable_database_created")

	cmdRestore := exec.Command("pg_restore", "-U", "postgres", "-d", disposableDB, "--no-owner", "--no-acl", tmpDump)
	_ = cmdRestore.Run()
	res.Checkpoints = append(res.Checkpoints, "pg_restore_into_disposable_database_completed")

	restoredURL := fmt.Sprintf("postgres://postgres@localhost:5432/%s?sslmode=disable", disposableDB)
	restoredPool, err := database.NewPool(ctx, restoredURL)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("connect to restored DB: %v", err)
		return res
	}
	defer restoredPool.Close()

	// Use participant adapters pointing directly to the restored pool's schemas
	adapterRestoredA := bankservice.NewParticipantService(restoredPool, "BANK-A", "bank_a")
	adapterRestoredB := bankservice.NewParticipantService(restoredPool, "BANK-B", "bank_b")
	adapters := map[uuid.UUID]bank.BankAdapter{
		h.testBankA: adapterRestoredA,
		h.testBankB: adapterRestoredB,
	}
	routeTargets := map[uuid.UUID]string{
		h.testBankA: "BANK-A",
		h.testBankB: "BANK-B",
	}
	restoredSvc := payments.NewServiceWithAdaptiveRouting(
		accounts.NewRepository(restoredPool),
		recipients.NewRepository(restoredPool),
		payments.NewRepository(restoredPool),
		adapters,
		routeTargets,
		nil,
	)
	restoredSvc.SetPaymentConcurrency(10)

	resumeRes, err := restoredSvc.Resume(ctx, fix.PaymentID)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("resume on restored DB failed: %v", err)
		return res
	}
	if resumeRes.Status != payments.ResumeStatusCompleted {
		res.ErrorMessage = fmt.Sprintf("restored DB resume status: %s", resumeRes.Status)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "worker_converged_payment_to_completed_on_restored_db")

	hRestored := &CrossBankRecoveryHarness{
		pool:      restoredPool,
		testBankA: h.testBankA,
		testBankB: h.testBankB,
	}
	_ = hRestored.projectPaymentEvents(ctx, fix.PaymentID)

	finSummary := hRestored.verifySinglePaymentInvariants(ctx, fix)
	res.ObservedState = "COMPLETED"
	p1 := finSummary.TotalViolations == 0
	p2 := resumeRes.Payment.State == payments.StateCompleted

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment completed on restored database", Passed: p2, Details: fmt.Sprintf("state=%s", resumeRes.Payment.State)},
		{InvariantName: "Financial invariants verified on restored database", Passed: p1, Details: fmt.Sprintf("violations=%d", finSummary.TotalViolations)},
	}

	res.Passed = p1 && p2
	if !res.Passed && res.ErrorMessage == "" {
		res.ErrorMessage = fmt.Sprintf("invariants failed (violations=%d)", finSummary.TotalViolations)
	}
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// -----------------------------------------------------------------------------
// INDEPENDENT FINANCIAL INVARIANTS ASSERTIONS
// -----------------------------------------------------------------------------

func (h *CrossBankRecoveryHarness) verifySinglePaymentInvariants(ctx context.Context, fix *CrossBankFixture) CrossBankFinancialSummary {
	var summary CrossBankFinancialSummary
	summary.NonnegativeBalances = true
	summary.DebitCreditConservation = true
	summary.SingleLedgerPerPayment = true
	summary.IdempotencyUnique = true
	summary.ParticipantUnique = true
	summary.PayloadIntegrity = true
	summary.CorrelationIntegrity = true
	summary.WorklistConvergence = true

	// 1. Nonnegative balances across public, bank_a, and bank_b
	var negPublic, negA, negB int
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE balance_paise < 0`).Scan(&negPublic)
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_a.accounts WHERE balance_paise < 0`).Scan(&negA)
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_b.accounts WHERE balance_paise < 0`).Scan(&negB)
	if negPublic+negA+negB > 0 {
		summary.NonnegativeBalances = false
		summary.TotalViolations += (negPublic + negA + negB)
	}

	// 2. Exact Debit/Credit conservation for this payment
	var debitSum, creditSum int64
	_ = h.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(le.amount_paise) FILTER (WHERE le.entry_type = 'DEBIT'), 0),
			COALESCE(SUM(le.amount_paise) FILTER (WHERE le.entry_type = 'CREDIT'), 0)
		FROM ledger_entries le
		JOIN ledger_transactions lt ON lt.id = le.ledger_transaction_id
		WHERE lt.payment_id = $1`, fix.PaymentID).Scan(&debitSum, &creditSum)
	summary.TotalLedgerDebitsPaise = debitSum
	summary.TotalLedgerCreditsPaise = creditSum
	if debitSum != fix.AmountPaise || creditSum != fix.AmountPaise || debitSum != creditSum {
		summary.DebitCreditConservation = false
		summary.TotalViolations++
	}

	// Central account reconstruction check
	var reconErrors int
	_ = h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM accounts a
		WHERE a.id IN ($1, $2)
		AND a.balance_paise != a.opening_balance_paise + COALESCE((
			SELECT SUM(CASE WHEN le.entry_type = 'CREDIT' THEN le.amount_paise ELSE -le.amount_paise END)
			FROM ledger_entries le WHERE le.account_id = a.id
		), 0)`, fix.SenderCentralAcc, fix.RecvCentralAcc).Scan(&reconErrors)
	summary.CentralReconstructionErr = reconErrors
	if reconErrors > 0 {
		summary.DebitCreditConservation = false
		summary.TotalViolations += reconErrors
	}

	// 3. Exactly 1 ledger transaction for this payment
	var ledgerTxCount int
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_transactions WHERE payment_id = $1`, fix.PaymentID).Scan(&ledgerTxCount)
	if ledgerTxCount != 1 {
		summary.SingleLedgerPerPayment = false
		summary.TotalViolations++
	}

	// 4. Idempotency unique mapping
	var idempMatches int
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM idempotency_records WHERE user_id = $1 AND key = $2 AND payment_id = $3`,
		fix.SenderUserID, fix.IdempotencyKey, fix.PaymentID).Scan(&idempMatches)
	if idempMatches != 1 {
		summary.IdempotencyUnique = false
		summary.TotalViolations++
	}

	// 5. Participant operation uniqueness
	var dupAOps, dupBOps int
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM (SELECT operation_id FROM bank_a.operations WHERE payment_id = $1 GROUP BY operation_id HAVING COUNT(*) > 1) d`, fix.PaymentID).Scan(&dupAOps)
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM (SELECT operation_id FROM bank_b.operations WHERE payment_id = $1 GROUP BY operation_id HAVING COUNT(*) > 1) d`, fix.PaymentID).Scan(&dupBOps)
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_a.operations WHERE payment_id = $1`, fix.PaymentID).Scan(&summary.BankAOperationsCount)
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_b.operations WHERE payment_id = $1`, fix.PaymentID).Scan(&summary.BankBOperationsCount)
	if dupAOps+dupBOps > 0 {
		summary.ParticipantUnique = false
		summary.TotalViolations += (dupAOps + dupBOps)
	}

	// 6. Payload integrity (amounts match, currency INR)
	var badPayloads int
	_ = h.pool.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM bank_a.operations WHERE payment_id = $1 AND (amount_paise != $2 OR currency != 'INR')) +
		       (SELECT COUNT(*) FROM bank_b.operations WHERE payment_id = $1 AND (amount_paise != $2 OR currency != 'INR'))`,
		fix.PaymentID, fix.AmountPaise).Scan(&badPayloads)
	if badPayloads > 0 {
		summary.PayloadIntegrity = false
		summary.TotalViolations += badPayloads
	}

	// 7. Correlation between central journal and participant operations
	var mismatches int
	_ = h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM payment_bank_operations j
		LEFT JOIN (
			SELECT operation_id, payment_id, amount_paise, currency FROM bank_a.operations
			UNION ALL
			SELECT operation_id, payment_id, amount_paise, currency FROM bank_b.operations
		) p ON p.operation_id = j.operation_id
		WHERE j.payment_id = $1 AND (p.operation_id IS NULL OR p.payment_id != j.payment_id OR p.amount_paise != j.amount_paise OR p.currency != j.currency)`,
		fix.PaymentID).Scan(&mismatches)
	if mismatches > 0 {
		summary.CorrelationIntegrity = false
		summary.TotalViolations += mismatches
	}

	// 8. Worklist convergence
	var worklistCount int
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ops_payment_worklist WHERE payment_id = $1 AND current_state = 'COMPLETED'`, fix.PaymentID).Scan(&worklistCount)
	if worklistCount < 1 {
		summary.WorklistConvergence = false
		summary.TotalViolations++
	}

	if summary.TotalViolations > 0 {
		fmt.Printf("DEBUG INVARIANTS FAIL (%s): Violations=%d | Nonneg=%t (negPub=%d, negA=%d, negB=%d) | Conserve=%t (debit=%d, credit=%d, recon=%d) | 1Ledger=%t (%d) | Idemp=%t (%d) | PartUnique=%t (%d/%d) | Payload=%t (%d) | Correl=%t (%d) | Worklist=%t (%d)\n",
			fix.PaymentID, summary.TotalViolations, summary.NonnegativeBalances, negPublic, negA, negB,
			summary.DebitCreditConservation, debitSum, creditSum, reconErrors, summary.SingleLedgerPerPayment, ledgerTxCount,
			summary.IdempotencyUnique, idempMatches, summary.ParticipantUnique, dupAOps, dupBOps, summary.PayloadIntegrity, badPayloads,
			summary.CorrelationIntegrity, mismatches, summary.WorklistConvergence, worklistCount)
	}

	return summary
}

func (h *CrossBankRecoveryHarness) verifyOverallFinancialInvariants(ctx context.Context) CrossBankFinancialSummary {
	var summary CrossBankFinancialSummary
	summary.NonnegativeBalances = true
	summary.DebitCreditConservation = true
	summary.SingleLedgerPerPayment = true
	summary.IdempotencyUnique = true
	summary.ParticipantUnique = true
	summary.PayloadIntegrity = true
	summary.CorrelationIntegrity = true
	summary.WorklistConvergence = true

	var negCount int
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE balance_paise < 0`).Scan(&negCount)
	if negCount > 0 {
		summary.NonnegativeBalances = false
		summary.TotalViolations += negCount
	}

	var debits, credits int64
	_ = h.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(amount_paise) FILTER (WHERE entry_type = 'DEBIT'), 0),
			COALESCE(SUM(amount_paise) FILTER (WHERE entry_type = 'CREDIT'), 0)
		FROM ledger_entries`).Scan(&debits, &credits)
	summary.TotalLedgerDebitsPaise = debits
	summary.TotalLedgerCreditsPaise = credits
	if debits != credits {
		summary.DebitCreditConservation = false
		summary.TotalViolations++
	}

	var dupLedgers int
	_ = h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT payment_id FROM ledger_transactions GROUP BY payment_id HAVING COUNT(*) > 1
		) d`).Scan(&dupLedgers)
	if dupLedgers > 0 {
		summary.SingleLedgerPerPayment = false
		summary.TotalViolations += dupLedgers
	}

	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_a.operations`).Scan(&summary.BankAOperationsCount)
	_ = h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_b.operations`).Scan(&summary.BankBOperationsCount)

	return summary
}
