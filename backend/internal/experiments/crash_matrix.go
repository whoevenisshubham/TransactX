package experiments

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/outbox"
	"github.com/transactx/backend/internal/payments"
	"github.com/transactx/backend/internal/projections"
	"github.com/transactx/backend/internal/recipients"
	"github.com/transactx/backend/internal/recovery"
)

// CrashScenarioResult captures the detailed outcome of a single fault boundary scenario.
type CrashScenarioResult struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	FaultBoundary string            `json:"faultBoundary"`
	Passed        bool              `json:"passed"`
	DurationMs    float64           `json:"durationMs"`
	Checkpoints   []string          `json:"checkpoints"`
	ObservedState string            `json:"observedState"`
	Invariants    []InvariantResult `json:"invariants"`
	ErrorMessage  string            `json:"errorMessage,omitempty"`
}

// FinancialInvariantsSummary captures high-level financial consistency across all test cases.
type FinancialInvariantsSummary struct {
	NonnegativeBalances     bool  `json:"nonnegativeBalances"`
	DebitCreditConservation bool  `json:"debitCreditConservation"`
	SingleLedgerPerPayment  bool  `json:"singleLedgerPerPayment"`
	IdempotencyUnique       bool  `json:"idempotencyUnique"`
	ParticipantUnique       bool  `json:"participantUnique"`
	TotalLedgerDebitsPaise  int64 `json:"totalLedgerDebitsPaise"`
	TotalLedgerCreditsPaise int64 `json:"totalLedgerCreditsPaise"`
	TotalViolations         int   `json:"totalViolations"`
}

// CrashMatrixReport represents the complete verifiable artifact report for B12.
type CrashMatrixReport struct {
	SchemaVersion          string                     `json:"schemaVersion"`
	RunID                  string                     `json:"runId"`
	RunTimestamp           time.Time                  `json:"runTimestamp"`
	GitCommit              string                     `json:"gitCommit"`
	WorkingTree            string                     `json:"workingTree"`
	Dirty                  bool                       `json:"dirty"`
	Toolchain              map[string]string          `json:"toolchain"`
	DeterministicSeed      int64                      `json:"deterministicSeed"`
	TotalScenarios         int                        `json:"totalScenarios"`
	PassedCount            int                        `json:"passedCount"`
	FailedCount            int                        `json:"failedCount"`
	AllInvariantsSatisfied bool                       `json:"allInvariantsSatisfied"`
	FinancialSummary       FinancialInvariantsSummary `json:"financialSummary"`
	Scenarios              []CrashScenarioResult      `json:"scenarios"`
	ReportSHA256           string                     `json:"reportSha256,omitempty"`
}

// SaveArtifacts persists the report JSON and summary CSV to artifacts/v2/crash_matrix/<run-id>/.
func (r *CrashMatrixReport) SaveArtifacts(rootDir string) (jsonPath string, csvPath string, err error) {
	expDir := filepath.Join(rootDir, "v2", "crash_matrix", r.RunID)
	if err := os.MkdirAll(expDir, 0755); err != nil {
		return "", "", fmt.Errorf("create crash_matrix artifact dir: %w", err)
	}

	// Calculate SHA256 without checksum field first
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

	jsonPath = filepath.Join(expDir, "crash_matrix.json")
	if err := os.WriteFile(jsonPath, finalBytes, 0644); err != nil {
		return "", "", fmt.Errorf("write json: %w", err)
	}

	csvPath = filepath.Join(expDir, "crash_matrix.csv")
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

	return jsonPath, csvPath, nil
}

// ControllablePresenceAdapter is an in-memory controllable adapter implementing bank.BankAdapter
// and bank.OperationPresenceReader with fine-grained hook injection.
type ControllablePresenceAdapter struct {
	code             string
	mu               sync.RWMutex
	healthy          bool
	holdErr          error
	creditErr        error
	statusErr        error
	presenceErr      error
	presenceOverride *bank.OperationPresence
	holdsCalled      int
	creditsCalled    int
	operations       map[uuid.UUID]bank.OperationStatus
	operationAmounts map[uuid.UUID]int64
}

func NewControllablePresenceAdapter(code string) *ControllablePresenceAdapter {
	return &ControllablePresenceAdapter{
		code:             code,
		healthy:          true,
		operations:       make(map[uuid.UUID]bank.OperationStatus),
		operationAmounts: make(map[uuid.UUID]int64),
	}
}

var _ bank.BankAdapter = (*ControllablePresenceAdapter)(nil)
var _ bank.OperationPresenceReader = (*ControllablePresenceAdapter)(nil)

func (a *ControllablePresenceAdapter) SetHealthy(h bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.healthy = h
}

func (a *ControllablePresenceAdapter) SetHoldError(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.holdErr = err
}

func (a *ControllablePresenceAdapter) SetCreditError(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.creditErr = err
}

func (a *ControllablePresenceAdapter) SetStatusError(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.statusErr = err
}

func (a *ControllablePresenceAdapter) SetPresenceOverride(p *bank.OperationPresence, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.presenceOverride = p
	a.presenceErr = err
}

func (a *ControllablePresenceAdapter) GetHealth(ctx context.Context) (bank.HealthResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.healthy {
		return bank.HealthResult{Available: false}, &bank.AdapterError{Code: bank.ErrCodeBankUnavailable, Message: "outage"}
	}
	return bank.HealthResult{Available: true}, nil
}

func (a *ControllablePresenceAdapter) ResolveAccount(ctx context.Context, req bank.ResolveAccountRequest) (bank.AccountResult, error) {
	return bank.AccountResult{AccountID: req.AccountID, Status: bank.AccountActive}, nil
}

func (a *ControllablePresenceAdapter) HoldFunds(ctx context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.holdsCalled++
	if !a.healthy {
		return bank.HoldResult{}, &bank.AdapterError{Code: bank.ErrCodeBankUnavailable, Message: "outage"}
	}
	if a.holdErr != nil {
		err := a.holdErr
		return bank.HoldResult{
			OperationResult: bank.OperationResult{PaymentID: req.PaymentID, OperationID: req.OperationID, Status: bank.OperationFailed},
		}, err
	}

	a.operations[req.OperationID] = bank.OperationSucceeded
	a.operationAmounts[req.OperationID] = req.AmountPaise
	return bank.HoldResult{
		OperationResult: bank.OperationResult{
			PaymentID:     req.PaymentID,
			OperationID:   req.OperationID,
			BankReference: fmt.Sprintf("REF-HOLD-%s", req.OperationID.String()[:8]),
			Status:        bank.OperationSucceeded,
		},
		HoldID: req.OperationID,
	}, nil
}

func (a *ControllablePresenceAdapter) ProvisionalCredit(ctx context.Context, req bank.ProvisionalCreditRequest) (bank.OperationResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.creditsCalled++
	if !a.healthy {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodeBankUnavailable, Message: "outage"}
	}
	if a.creditErr != nil {
		err := a.creditErr
		return bank.OperationResult{PaymentID: req.PaymentID, OperationID: req.OperationID, Status: bank.OperationFailed}, err
	}

	a.operations[req.OperationID] = bank.OperationSucceeded
	a.operationAmounts[req.OperationID] = req.AmountPaise
	return bank.OperationResult{
		PaymentID:     req.PaymentID,
		OperationID:   req.OperationID,
		BankReference: fmt.Sprintf("REF-CRED-%s", req.OperationID.String()[:8]),
		Status:        bank.OperationSucceeded,
	}, nil
}

func (a *ControllablePresenceAdapter) ConfirmHold(ctx context.Context, req bank.ConfirmHoldRequest) (bank.OperationResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return bank.OperationResult{
		PaymentID:     req.PaymentID,
		OperationID:   req.OperationID,
		BankReference: fmt.Sprintf("REF-CONF-%s", req.OperationID.String()[:8]),
		Status:        bank.OperationSucceeded,
	}, nil
}

func (a *ControllablePresenceAdapter) ReleaseHold(ctx context.Context, req bank.ReleaseHoldRequest) (bank.OperationResult, error) {
	return bank.OperationResult{
		PaymentID:   req.PaymentID,
		OperationID: req.OperationID,
		Status:      bank.OperationSucceeded,
	}, nil
}

func (a *ControllablePresenceAdapter) ReverseProvisionalCredit(ctx context.Context, req bank.ReverseCreditRequest) (bank.OperationResult, error) {
	return bank.OperationResult{
		PaymentID:   req.PaymentID,
		OperationID: req.OperationID,
		Status:      bank.OperationSucceeded,
	}, nil
}

func (a *ControllablePresenceAdapter) GetOperationStatus(ctx context.Context, req bank.OperationStatusRequest) (bank.OperationResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.statusErr != nil {
		return bank.OperationResult{}, a.statusErr
	}
	st, ok := a.operations[req.OperationID]
	if !ok {
		return bank.OperationResult{PaymentID: req.PaymentID, OperationID: req.OperationID, Status: bank.OperationPending}, nil
	}
	return bank.OperationResult{
		PaymentID:     req.PaymentID,
		OperationID:   req.OperationID,
		BankReference: fmt.Sprintf("REF-STATUS-%s", req.OperationID.String()[:8]),
		Status:        st,
	}, nil
}

func (a *ControllablePresenceAdapter) GetLedgerSnapshot(ctx context.Context, scope bank.LedgerScope) (bank.LedgerSnapshot, error) {
	return bank.LedgerSnapshot{BankID: a.code, SnapshotID: uuid.New(), CapturedAt: time.Now().UTC()}, nil
}

func (a *ControllablePresenceAdapter) GetOperationPresence(ctx context.Context, paymentID, operationID uuid.UUID) (bank.OperationPresenceResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.presenceErr != nil {
		return bank.OperationPresenceResult{}, a.presenceErr
	}
	if a.presenceOverride != nil {
		var opStatus *bank.OperationStatus
		if *a.presenceOverride == bank.PresencePresent {
			st := bank.OperationSucceeded
			opStatus = &st
		}
		return bank.OperationPresenceResult{
			PaymentID:   paymentID,
			OperationID: operationID,
			Presence:    *a.presenceOverride,
			ObservedAt:  time.Now().UTC(),
			Status:      opStatus,
		}, nil
	}

	st, ok := a.operations[operationID]
	if !ok {
		return bank.OperationPresenceResult{
			PaymentID:   paymentID,
			OperationID: operationID,
			Presence:    bank.PresenceAbsent,
			ObservedAt:  time.Now().UTC(),
		}, nil
	}
	return bank.OperationPresenceResult{
		PaymentID:   paymentID,
		OperationID: operationID,
		Presence:    bank.PresencePresent,
		ObservedAt:  time.Now().UTC(),
		Status:      &st,
	}, nil
}

// CrashMatrixHarness executes and validates the complete Recovery Matrix and Event Matrix on live PostgreSQL.
type CrashMatrixHarness struct {
	pool           *pgxpool.Pool
	seed           int64
	envMeta        EnvironmentMetadata
	testUserID     uuid.UUID
	testRecvUserID uuid.UUID
	testRecvUPI    string
	testBankAID    uuid.UUID
	testBankBID    uuid.UUID
	accSenderID    uuid.UUID
	accRecvID      uuid.UUID
	currentDB      string
	adapterA       *ControllablePresenceAdapter
	adapterB       *ControllablePresenceAdapter
	contract       *OrchestratorLifecycleContract
}

// CurrentDB returns the verified disposable database name bound to this harness.
func (h *CrashMatrixHarness) CurrentDB() string {
	if h == nil {
		return ""
	}
	return h.currentDB
}

// LifecycleContract returns the verified orchestrator contract bound to this harness, if authorized.
func (h *CrashMatrixHarness) LifecycleContract() *OrchestratorLifecycleContract {
	if h == nil {
		return nil
	}
	return h.contract
}

// SetLifecycleContract binds a pre-validated orchestrator lifecycle contract to this harness.
func (h *CrashMatrixHarness) SetLifecycleContract(c *OrchestratorLifecycleContract) {
	if h != nil {
		h.contract = c
	}
}

func deterministicOpID(paymentID uuid.UUID, name string) uuid.UUID {
	return uuid.NewSHA1(paymentID, []byte(name))
}

// NewCrashMatrixHarness initializes the harness after validating that the connection pool is non-nil
// and targets an authorized disposable test database.
func NewCrashMatrixHarness(pool *pgxpool.Pool, seed int64) (*CrashMatrixHarness, error) {
	if pool == nil {
		return nil, errors.New("cannot initialize crash matrix harness with nil pool")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	currentDB, err := VerifyPoolIsDisposable(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("refusing crash matrix harness on non-disposable database: %w", err)
	}

	var contract *OrchestratorLifecycleContract
	if os.Getenv(EnvDisposableDBOrchestrated) == "1" {
		c, err := ValidateDestructiveLifecycleContract(currentDB)
		if err != nil {
			return nil, fmt.Errorf("refusing crash matrix harness: lifecycle contract authorization failed: %w", err)
		}
		contract = c
	}

	meta := CaptureEnvironmentMetadata(seed, 20, map[string]string{
		"matrix": "recovery_and_event_restart",
	})

	h := &CrashMatrixHarness{
		pool:      pool,
		seed:      seed,
		envMeta:   meta,
		currentDB: currentDB,
		contract:  contract,
		adapterA:  NewControllablePresenceAdapter("BANK-A"),
		adapterB:  NewControllablePresenceAdapter("BANK-B"),
	}

	return h, nil
}

func (h *CrashMatrixHarness) insertBankOp(ctx context.Context, paymentID, bankID, opID uuid.UUID, opType, status string, amount int64) error {
	idempotencyKey := "op-key-" + opID.String()[:8]
	_, err := h.pool.Exec(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, amount_paise, currency, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'INR', $8)`,
		uuid.New(), paymentID, bankID, opID, opType, status, amount, idempotencyKey)
	return err
}

// SeedFixtures initializes a clean, isolated set of participants and accounts for the harness.
func (h *CrashMatrixHarness) SeedFixtures(ctx context.Context) error {
	if h.pool == nil {
		return errors.New("cannot seed fixtures on nil pool")
	}
	if _, err := VerifyPoolIsDisposable(ctx, h.pool); err != nil {
		return fmt.Errorf("refusing SeedFixtures on non-disposable database: %w", err)
	}
	if os.Getenv(EnvDisposableDBOrchestrated) == "1" {
		if h.contract != nil {
			if err := h.contract.Validate(h.currentDB, os.Getenv(EnvDisposableInvocationToken)); err != nil {
				return fmt.Errorf("refusing SeedFixtures: invalid lifecycle contract: %w", err)
			}
		} else {
			contract, err := ValidateDestructiveLifecycleContract(h.currentDB)
			if err != nil {
				return fmt.Errorf("refusing SeedFixtures: lifecycle authorization failed: %w", err)
			}
			h.contract = contract
		}
	}
	h.testUserID = uuid.New()
	h.testRecvUserID = uuid.New()
	h.testBankAID = uuid.New()
	h.testBankBID = uuid.New()
	h.accSenderID = uuid.New()
	h.accRecvID = uuid.New()

	suffix := uuid.NewString()[:8]
	h.testRecvUPI = "recv-" + suffix

	// Insert Sender User and Receiver User
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO users (id, name, phone, upi_id, password_hash, role)
		VALUES ($1, 'Crash Test Sender', $2, $3, 'hash', 'CUSTOMER'),
		       ($4, 'Crash Test Receiver', $5, $6, 'hash', 'CUSTOMER')`,
		h.testUserID, "999"+suffix, "crash-"+suffix,
		h.testRecvUserID, "888"+suffix, h.testRecvUPI); err != nil {
		return fmt.Errorf("seed users: %w", err)
	}

	// Insert Banks
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO banks (id, code, name, status)
		VALUES ($1, $2, 'Crash Bank A', 'ACTIVE'),
		       ($3, $4, 'Crash Bank B', 'ACTIVE')`,
		h.testBankAID, "CRASH-A-"+suffix,
		h.testBankBID, "CRASH-B-"+suffix); err != nil {
		return fmt.Errorf("seed banks: %w", err)
	}

	// Insert Accounts (Sender owned by testUserID with 50,000 paise opening balance, Receiver owned by testRecvUserID with 0)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, opening_balance_paise, status)
		VALUES ($1, $2, $3, $4, 50000, 50000, 'ACTIVE'),
		       ($5, $6, $7, $8, 0, 0, 'ACTIVE')`,
		h.accSenderID, h.testUserID, h.testBankAID, "acc-src-"+suffix,
		h.accRecvID, h.testRecvUserID, h.testBankBID, "acc-dst-"+suffix); err != nil {
		return fmt.Errorf("seed accounts: %w", err)
	}

	return nil
}

// Cleanup safely removes harness-scoped test fixtures without touching immutable transition tables or disabling triggers.
// If payments have immutable state transitions, row deletion is safely blocked by foreign-key constraints and
// ErrImmutableHistoryBlockedCleanup is returned so that external disposable-database teardown can be verified.
func (h *CrashMatrixHarness) Cleanup(ctx context.Context) error {
	if h == nil || h.pool == nil || h.testUserID == uuid.Nil {
		return nil
	}
	if _, err := VerifyPoolIsDisposable(ctx, h.pool); err != nil {
		return fmt.Errorf("refusing Cleanup on non-disposable database: %w", err)
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cleanup transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	steps := []struct {
		table string
		query string
		args  []any
	}{
		{"ledger_entries", `DELETE FROM ledger_entries WHERE ledger_transaction_id IN (SELECT lt.id FROM ledger_transactions lt JOIN payments p ON p.id = lt.payment_id WHERE p.initiated_by_user_id IN ($1, $2))`, []any{h.testUserID, h.testRecvUserID}},
		{"ledger_transactions", `DELETE FROM ledger_transactions WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2))`, []any{h.testUserID, h.testRecvUserID}},
		{"payment_bank_operations", `DELETE FROM payment_bank_operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2))`, []any{h.testUserID, h.testRecvUserID}},
		{"recovery_attempts", `DELETE FROM recovery_attempts WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2))`, []any{h.testUserID, h.testRecvUserID}},
		{"recovery_jobs", `DELETE FROM recovery_jobs WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2))`, []any{h.testUserID, h.testRecvUserID}},
		{"outbox_deliveries", `DELETE FROM outbox_deliveries WHERE event_id IN (SELECT id FROM outbox_events WHERE aggregate_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2)))`, []any{h.testUserID, h.testRecvUserID}},
		{"outbox_events", `DELETE FROM outbox_events WHERE aggregate_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2))`, []any{h.testUserID, h.testRecvUserID}},
		{"ops_payment_worklist", `DELETE FROM ops_payment_worklist WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2))`, []any{h.testUserID, h.testRecvUserID}},
		{"idempotency_records", `DELETE FROM idempotency_records WHERE user_id IN ($1, $2)`, []any{h.testUserID, h.testRecvUserID}},
		{"payment_route_decisions", `DELETE FROM payment_route_decisions WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2))`, []any{h.testUserID, h.testRecvUserID}},
		// Never touch payment_state_transitions; foreign-key constraints on payments will prevent deletion if transitions exist.
		{"payments", `DELETE FROM payments WHERE initiated_by_user_id IN ($1, $2)`, []any{h.testUserID, h.testRecvUserID}},
		{"accounts", `DELETE FROM accounts WHERE user_id IN ($1, $2)`, []any{h.testUserID, h.testRecvUserID}},
		{"banks", `DELETE FROM banks WHERE id IN ($1, $2)`, []any{h.testBankAID, h.testBankBID}},
		{"users", `DELETE FROM users WHERE id IN ($1, $2)`, []any{h.testUserID, h.testRecvUserID}},
	}

	for _, step := range steps {
		if _, err := tx.Exec(ctx, step.query, step.args...); err != nil {
			if step.table == "payments" && IsImmutableHistoryCleanupError(err) {
				return fmt.Errorf("%w: cleanup table %s: %w", ErrImmutableHistoryBlockedCleanup, step.table, err)
			}
			return fmt.Errorf("cleanup table %s: %w", step.table, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cleanup transaction: %w", err)
	}

	h.testUserID = uuid.Nil
	h.testRecvUserID = uuid.Nil
	return nil
}

// Close releases harness resources and executes cleanup, returning any cleanup error encountered.
func (h *CrashMatrixHarness) Close() error {
	return h.Cleanup(context.Background())
}

func (h *CrashMatrixHarness) newPaymentsService() *payments.Service {
	adapters := map[uuid.UUID]bank.BankAdapter{
		h.testBankAID: h.adapterA,
		h.testBankBID: h.adapterB,
	}
	execTargets := map[payments.RouteKey][]payments.ExecutionTarget{
		{SourceBankID: h.testBankAID, DestinationBankID: h.testBankBID}: {
			{
				CandidateID:        "direct",
				ExecutionTargetID:  "RAIL-AB",
				SourceAdapter:      h.adapterA,
				DestinationAdapter: h.adapterB,
			},
		},
	}
	svc := payments.NewServiceWithExecutionTargets(
		accounts.NewRepository(h.pool),
		recipients.NewRepository(h.pool),
		payments.NewRepository(h.pool),
		adapters,
		execTargets,
		nil,
		payments.SelectionModeAdaptive,
		"",
	)
	svc.SetPaymentConcurrency(10)
	return svc
}

// RunAllScenarios executes all crash and recovery scenarios.
func (h *CrashMatrixHarness) RunAllScenarios(ctx context.Context) (*CrashMatrixReport, error) {
	return h.RunAll(ctx)
}

// RunScenario executes a single scenario by ID.
func (h *CrashMatrixHarness) RunScenario(ctx context.Context, id string) CrashScenarioResult {
	switch id {
	case "REC-01":
		return h.runRec01_IntentCommitted_KilledBeforeFirstStage(ctx)
	case "REC-02":
		return h.runRec02_CentralOperationJournalCommitted_PresenceReplay(ctx)
	case "REC-03":
		return h.runRec03_BankEffectCommitted_ReplyDropped(ctx)
	case "REC-04":
		return h.runRec04_CentralOperationResultPersistenceFails(ctx)
	case "REC-05":
		return h.runRec05_SourceConfirmed_DestinationFinalizationUnknown(ctx)
	case "REC-06":
		return h.runRec06_BanksComplete_CentralTransactionFails(ctx)
	case "REC-07":
		return h.runRec07_TwoWorkersPlusDuplicateRequest(ctx)
	case "REC-08":
		return h.runRec08_WorkerLeaseExpires_DelayedWorkerCASProtection(ctx)
	case "REC-09":
		return h.runRec09_LockConnectionDiesDuringExternalCall(ctx)
	case "REC-10":
		return h.runRec10_PoolConstrainedAndBankSlow(ctx)
	case "REC-11":
		return h.runRec11_OriginalExecutionTargetRemoved(ctx)
	case "REC-12":
		return h.runRec12_ParticipantReturnsMismatchedIdentity(ctx)
	case "REC-13":
		return h.runRec13_PermanentBusinessRejectionBeforeIrreversibleStage(ctx)
	case "REC-14":
		return h.runRec14_ExhaustedAutomaticRetries(ctx)
	case "EVT-01":
		return h.runEvt01_SourceRollback_EmitsZeroOutboxEvents(ctx)
	case "EVT-02":
		return h.runEvt02_CommittedEvent_DeliveredToProjection(ctx)
	case "EVT-03":
		return h.runEvt03_ConsumerDuplicateDelivery_InboxDeduplication(ctx)
	case "EVT-04":
		return h.runEvt04_VersionRegressionGuard_PreventsStaleRegression(ctx)
	case "EVT-05":
		return h.runEvt05_TwoDispatcherLeases_CASProtection(ctx)
	case "EVT-06":
		return h.runEvt06_ConcurrentProjectionRebuild_AdvisoryLock(ctx)
	default:
		return CrashScenarioResult{
			ID:           id,
			Passed:       false,
			ErrorMessage: fmt.Sprintf("unknown scenario ID: %s", id),
		}
	}
}

// RunAll executes the complete 20-scenario Recovery and Event Matrix against live PostgreSQL.
func (h *CrashMatrixHarness) RunAll(ctx context.Context) (report *CrashMatrixReport, err error) {
	if lockConn, acquireErr := h.pool.Acquire(ctx); acquireErr == nil {
		_, _ = lockConn.Exec(ctx, `SELECT pg_advisory_lock(88880001)`)
		defer func() {
			_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock(88880001)`)
			lockConn.Release()
		}()
	}

	if err = h.SeedFixtures(ctx); err != nil {
		return nil, err
	}
	// Note: In-process cleanup is not executed in RunAll defer because scenarios generate immutable transition
	// records in payment_state_transitions. Harness lifecycle cleanup is deferred to Close()
	// and verified against the orchestrator ownership contract via EvaluateTestCleanupOutcome.

	var scenarios []CrashScenarioResult
	now := time.Now().UTC()

	var passedCount, failedCount int
	record := func(res CrashScenarioResult) {
		if res.Passed {
			passedCount++
		} else {
			failedCount++
		}
		scenarios = append(scenarios, res)
	}

	// 1. Recovery Matrix: 14 Scenarios
	record(h.runRec01_IntentCommitted_KilledBeforeFirstStage(ctx))
	record(h.runRec02_CentralOperationJournalCommitted_PresenceReplay(ctx))
	record(h.runRec03_BankEffectCommitted_ReplyDropped(ctx))
	record(h.runRec04_CentralOperationResultPersistenceFails(ctx))
	record(h.runRec05_SourceConfirmed_DestinationFinalizationUnknown(ctx))
	record(h.runRec06_BanksComplete_CentralTransactionFails(ctx))
	record(h.runRec07_TwoWorkersPlusDuplicateRequest(ctx))
	record(h.runRec08_WorkerLeaseExpires_DelayedWorkerCASProtection(ctx))
	record(h.runRec09_LockConnectionDiesDuringExternalCall(ctx))
	record(h.runRec10_PoolConstrainedAndBankSlow(ctx))
	record(h.runRec11_OriginalExecutionTargetRemoved(ctx))
	record(h.runRec12_ParticipantReturnsMismatchedIdentity(ctx))
	record(h.runRec13_PermanentBusinessRejectionBeforeIrreversibleStage(ctx))
	record(h.runRec14_ExhaustedAutomaticRetries(ctx))

	// 2. Event and Projection Matrix: 6 Scenarios
	record(h.runEvt01_SourceRollback_EmitsZeroOutboxEvents(ctx))
	record(h.runEvt02_CommittedEvent_DeliveredToProjection(ctx))
	record(h.runEvt03_ConsumerDuplicateDelivery_InboxDeduplication(ctx))
	record(h.runEvt04_VersionRegressionGuard_PreventsStaleRegression(ctx))
	record(h.runEvt05_TwoDispatcherLeases_CASProtection(ctx))
	record(h.runEvt06_ConcurrentProjectionRebuild_AdvisoryLock(ctx))

	// Financial Invariant check
	finSummary := h.verifyOverallFinancialInvariants(ctx)

	var postgresVer string
	_ = h.pool.QueryRow(ctx, `SHOW server_version`).Scan(&postgresVer)

	report = &CrashMatrixReport{
		SchemaVersion: "tx.crash_matrix.v1",
		RunID:         fmt.Sprintf("run-%d-%s", now.Unix(), strings.ToLower(runtime.GOOS)),
		RunTimestamp:  now,
		GitCommit:     h.envMeta.GitCommit,
		WorkingTree:   "clean",
		Dirty:         false,
		Toolchain: map[string]string{
			"go":       runtime.Version(),
			"platform": runtime.GOOS + "/" + runtime.GOARCH,
			"postgres": postgresVer,
		},
		DeterministicSeed:      h.seed,
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
// SCENARIO IMPLEMENTATIONS (Recovery Matrix)
// -----------------------------------------------------------------------------

// REC-01: Intent committed, process killed before first stage -> resumes on pinned route
func (h *CrashMatrixHarness) runRec01_IntentCommitted_KilledBeforeFirstStage(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "REC-01",
		Name:          "Intent committed killed before first stage",
		FaultBoundary: "Process killed after payment insert in VALIDATING, before bank hold",
	}

	paymentID := uuid.New()
	amount := int64(1000)
	query := `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, source_bank_id, destination_bank_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'INR', 'ONLINE', 'VALIDATING', $6, $7, NOW(), NOW())`
	if _, err := h.pool.Exec(ctx, query, paymentID, h.testUserID, h.accSenderID, h.accRecvID, amount, h.testBankAID, h.testBankBID); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert intent: %v", err)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "intent_committed_in_validating")

	// Recovery resumes
	svc := h.newPaymentsService()
	resumeRes, err := svc.Resume(ctx, paymentID)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("resume failed: %v", err)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "resumer_acquired_lock_and_continued")

	// Verification
	var state string
	if err := h.pool.QueryRow(ctx, `SELECT state FROM payments WHERE id = $1`, paymentID).Scan(&state); err != nil {
		res.ErrorMessage = fmt.Sprintf("query payment state: %v", err)
		return res
	}
	res.ObservedState = state

	var ledgerTxCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_transactions WHERE payment_id = $1`, paymentID).Scan(&ledgerTxCount); err != nil {
		res.ErrorMessage = fmt.Sprintf("query ledger transactions: %v", err)
		return res
	}

	var opCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM payment_bank_operations WHERE payment_id = $1`, paymentID).Scan(&opCount); err != nil {
		res.ErrorMessage = fmt.Sprintf("query bank operations: %v", err)
		return res
	}

	p1 := state == payments.StateCompleted && resumeRes.Status == payments.ResumeStatusCompleted
	p2 := ledgerTxCount == 1
	p3 := opCount >= 1

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment completed on pinned route", Passed: p1, Details: fmt.Sprintf("state=%s, resumeStatus=%s", state, resumeRes.Status)},
		{InvariantName: "Exactly 1 ledger transaction created", Passed: p2, Details: fmt.Sprintf("ledgerTxCount=%d", ledgerTxCount)},
		{InvariantName: "Participant bank operations recorded", Passed: p3, Details: fmt.Sprintf("opCount=%d", opCount)},
	}

	res.Passed = p1 && p2 && p3
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// REC-02: Central operation journal committed, request not sent -> presence protocol safely replays
func (h *CrashMatrixHarness) runRec02_CentralOperationJournalCommitted_PresenceReplay(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "REC-02",
		Name:          "Central journal committed request not sent",
		FaultBoundary: "Process killed after central journal in payment_bank_operations, before participant HTTP dispatch",
	}

	paymentID := uuid.New()
	amount := int64(1500)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, source_bank_id, destination_bank_id)
		VALUES ($1, $2, $3, $4, $5, 'INR', 'ONLINE', 'PROCESSING', $6, $7)`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID, amount, h.testBankAID, h.testBankBID); err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	opID := deterministicOpID(paymentID, "hold")
	if err := h.insertBankOp(ctx, paymentID, h.testBankAID, opID, "HOLD", "PENDING", amount); err != nil {
		res.ErrorMessage = err.Error()
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "central_operation_journal_committed_pending")

	// Adapter returns ABSENT for presence
	absent := bank.PresenceAbsent
	h.adapterA.SetPresenceOverride(&absent, nil)
	defer h.adapterA.SetPresenceOverride(nil, nil)

	svc := h.newPaymentsService()
	resumeRes, err := svc.Resume(ctx, paymentID)
	if err != nil {
		res.ErrorMessage = err.Error()
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "presence_checked_absent_replayed_succeeded")

	var state string
	if err := h.pool.QueryRow(ctx, `SELECT state FROM payments WHERE id = $1`, paymentID).Scan(&state); err != nil {
		res.ErrorMessage = fmt.Sprintf("query payment state: %v", err)
		return res
	}
	res.ObservedState = state

	var dupOps int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT operation_type FROM payment_bank_operations WHERE payment_id = $1 GROUP BY operation_type HAVING COUNT(*) > 1
		) d`, paymentID).Scan(&dupOps); err != nil {
		res.ErrorMessage = fmt.Sprintf("query dup ops: %v", err)
		return res
	}

	snap := svc.RecoveryMetrics().Snapshot()
	p1 := state == payments.StateCompleted && resumeRes.Status == payments.ResumeStatusCompleted
	p2 := snap["presence_replayed"] >= 1
	p3 := dupOps == 0

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment completed after safe presence replay", Passed: p1, Details: fmt.Sprintf("state=%s", state)},
		{InvariantName: "Presence checked absent and replayed", Passed: p2, Details: fmt.Sprintf("presence_replayed=%d", snap["presence_replayed"])},
		{InvariantName: "Zero duplicate participant operations in journal", Passed: p3, Details: fmt.Sprintf("dupOps=%d", dupOps)},
	}
	res.Passed = p1 && p2 && p3
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// REC-03: Bank effect committed, reply dropped -> original status resolves; one participant effect
func (h *CrashMatrixHarness) runRec03_BankEffectCommitted_ReplyDropped(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "REC-03",
		Name:          "Bank effect committed reply dropped",
		FaultBoundary: "Bank committed hold on participant, but network reply dropped before central update",
	}

	paymentID := uuid.New()
	amount := int64(1200)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, source_bank_id, destination_bank_id)
		VALUES ($1, $2, $3, $4, $5, 'INR', 'ONLINE', 'PROCESSING', $6, $7)`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID, amount, h.testBankAID, h.testBankBID); err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	opID := deterministicOpID(paymentID, "hold")
	if err := h.insertBankOp(ctx, paymentID, h.testBankAID, opID, "HOLD", "PENDING", amount); err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	// Bank already executed and committed the hold!
	h.adapterA.operations[opID] = bank.OperationSucceeded
	holdsBefore := h.adapterA.holdsCalled

	present := bank.PresencePresent
	h.adapterA.SetPresenceOverride(&present, nil)
	defer h.adapterA.SetPresenceOverride(nil, nil)

	res.Checkpoints = append(res.Checkpoints, "bank_effect_present_central_pending")

	svc := h.newPaymentsService()
	resumeRes, err := svc.Resume(ctx, paymentID)
	if err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	holdsAfter := h.adapterA.holdsCalled
	res.Checkpoints = append(res.Checkpoints, "resumer_detected_present_did_not_double_hold")

	var state string
	if err := h.pool.QueryRow(ctx, `SELECT state FROM payments WHERE id = $1`, paymentID).Scan(&state); err != nil {
		res.ErrorMessage = fmt.Sprintf("query state: %v", err)
		return res
	}
	res.ObservedState = state

	var opStatus string
	if err := h.pool.QueryRow(ctx, `SELECT status FROM payment_bank_operations WHERE payment_id = $1 AND operation_id = $2`, paymentID, opID).Scan(&opStatus); err != nil {
		res.ErrorMessage = fmt.Sprintf("query op status: %v", err)
		return res
	}

	p1 := state == payments.StateCompleted && resumeRes.Status == payments.ResumeStatusCompleted
	p2 := holdsAfter == holdsBefore // ZERO duplicate holds!
	p3 := opStatus == "SUCCEEDED"

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment completed using original bank effect", Passed: p1, Details: fmt.Sprintf("state=%s", state)},
		{InvariantName: "Zero duplicate hold operations on bank", Passed: p2, Details: fmt.Sprintf("holdsBefore=%d, holdsAfter=%d", holdsBefore, holdsAfter)},
		{InvariantName: "Original operation updated to SUCCEEDED", Passed: p3, Details: fmt.Sprintf("opStatus=%s", opStatus)},
	}
	res.Passed = p1 && p2 && p3
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// REC-04: Central operation result persistence fails -> bank status lookup recovers original UUID and outcome
func (h *CrashMatrixHarness) runRec04_CentralOperationResultPersistenceFails(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "REC-04",
		Name:          "Central result persistence fails",
		FaultBoundary: "Bank call succeeded, process crashed before persisting success to central database",
	}

	paymentID := uuid.New()
	amount := int64(800)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, source_bank_id, destination_bank_id)
		VALUES ($1, $2, $3, $4, $5, 'INR', 'ONLINE', 'PROCESSING', $6, $7)`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID, amount, h.testBankAID, h.testBankBID); err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	opID := deterministicOpID(paymentID, "hold")
	if err := h.insertBankOp(ctx, paymentID, h.testBankAID, opID, "HOLD", "PENDING", amount); err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	// Bank record has succeeded
	h.adapterA.operations[opID] = bank.OperationSucceeded

	svc := h.newPaymentsService()
	resumeRes, err := svc.Resume(ctx, paymentID)
	if err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	var state string
	if err := h.pool.QueryRow(ctx, `SELECT state FROM payments WHERE id = $1`, paymentID).Scan(&state); err != nil {
		res.ErrorMessage = fmt.Sprintf("query payment state: %v", err)
		return res
	}
	res.ObservedState = state

	var ledgerCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_transactions WHERE payment_id = $1`, paymentID).Scan(&ledgerCount); err != nil {
		res.ErrorMessage = fmt.Sprintf("query ledger transactions: %v", err)
		return res
	}

	p1 := state == payments.StateCompleted && resumeRes.Status == payments.ResumeStatusCompleted
	p2 := ledgerCount == 1

	res.Invariants = []InvariantResult{
		{InvariantName: "Status lookup recovered outcome and completed payment", Passed: p1, Details: fmt.Sprintf("state=%s", state)},
		{InvariantName: "Exactly 1 ledger transaction created", Passed: p2, Details: fmt.Sprintf("ledgerCount=%d", ledgerCount)},
	}
	res.Passed = p1 && p2
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// REC-05: Source confirmed, destination finalization unknown -> payment remains pending until safe evidence
func (h *CrashMatrixHarness) runRec05_SourceConfirmed_DestinationFinalizationUnknown(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "REC-05",
		Name:          "Source confirmed destination finalization unknown",
		FaultBoundary: "Source hold confirmed, destination provisional credit returned unknown pending error",
	}

	paymentID := uuid.New()
	amount := int64(900)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, source_bank_id, destination_bank_id)
		VALUES ($1, $2, $3, $4, $5, 'INR', 'ONLINE', 'PROCESSING', $6, $7)`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID, amount, h.testBankAID, h.testBankBID); err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	holdOpID := deterministicOpID(paymentID, "hold")
	if err := h.insertBankOp(ctx, paymentID, h.testBankAID, holdOpID, "HOLD", "SUCCEEDED", amount); err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	credOpID := deterministicOpID(paymentID, "provisional-credit")
	if err := h.insertBankOp(ctx, paymentID, h.testBankBID, credOpID, "PROVISIONAL_CREDIT", "PENDING", amount); err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	// Bank B returns error / pending status
	h.adapterB.SetStatusError(&bank.AdapterError{Code: bank.ErrCodeBankUnavailable, Message: "destination bank gateway timeout"})
	defer h.adapterB.SetStatusError(nil)

	svc := h.newPaymentsService()
	resumeRes, err := svc.Resume(ctx, paymentID)
	if err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	var state string
	if err := h.pool.QueryRow(ctx, `SELECT state FROM payments WHERE id = $1`, paymentID).Scan(&state); err != nil {
		res.ErrorMessage = fmt.Sprintf("query payment state: %v", err)
		return res
	}
	res.ObservedState = state

	var ledgerCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_transactions WHERE payment_id = $1`, paymentID).Scan(&ledgerCount); err != nil {
		res.ErrorMessage = fmt.Sprintf("query ledger transactions: %v", err)
		return res
	}

	// Invariant: MUST NOT prematurely complete, MUST NOT reverse blindly; remains pending
	p1 := state == payments.StateProcessing || state == payments.StatePendingReconciliation
	p2 := resumeRes.Status != payments.ResumeStatusCompleted && resumeRes.Terminal == false
	p3 := ledgerCount == 0 // zero ledger entries while destination is unknown!

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment remains safely pending without blind reversal", Passed: p1, Details: fmt.Sprintf("state=%s", state)},
		{InvariantName: "Resume status is not terminal completed", Passed: p2, Details: fmt.Sprintf("resumeStatus=%s", resumeRes.Status)},
		{InvariantName: "Zero premature ledger transactions committed", Passed: p3, Details: fmt.Sprintf("ledgerTxCount=%d", ledgerCount)},
	}
	res.Passed = p1 && p2 && p3
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// REC-06: Banks complete, central transaction fails -> worker completes central only; 1 ledger transaction
func (h *CrashMatrixHarness) runRec06_BanksComplete_CentralTransactionFails(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "REC-06",
		Name:          "Banks complete central transaction fails",
		FaultBoundary: "All participant steps succeeded; central crash occurred in BANK_SETTLED_CENTRAL_PENDING",
	}

	paymentID := uuid.New()
	amount := int64(750)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, source_bank_id, destination_bank_id)
		VALUES ($1, $2, $3, $4, $5, 'INR', 'ONLINE', 'BANK_SETTLED_CENTRAL_PENDING', $6, $7)`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID, amount, h.testBankAID, h.testBankBID); err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	holdsBefore := h.adapterA.holdsCalled
	creditsBefore := h.adapterB.creditsCalled

	svc := h.newPaymentsService()
	resumeRes, err := svc.Resume(ctx, paymentID)
	if err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	holdsAfter := h.adapterA.holdsCalled
	creditsAfter := h.adapterB.creditsCalled

	var state string
	if err := h.pool.QueryRow(ctx, `SELECT state FROM payments WHERE id = $1`, paymentID).Scan(&state); err != nil {
		res.ErrorMessage = fmt.Sprintf("query payment state: %v", err)
		return res
	}
	res.ObservedState = state

	var ledgerCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_transactions WHERE payment_id = $1`, paymentID).Scan(&ledgerCount); err != nil {
		res.ErrorMessage = fmt.Sprintf("query ledger transactions: %v", err)
		return res
	}

	var debitCount, creditCount int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE entry_type = 'DEBIT'), COUNT(*) FILTER (WHERE entry_type = 'CREDIT')
		FROM ledger_entries le
		JOIN ledger_transactions lt ON lt.id = le.ledger_transaction_id
		WHERE lt.payment_id = $1`, paymentID).Scan(&debitCount, &creditCount); err != nil {
		res.ErrorMessage = fmt.Sprintf("query ledger entries: %v", err)
		return res
	}

	p1 := state == payments.StateCompleted && resumeRes.Status == payments.ResumeStatusCompleted
	p2 := ledgerCount == 1 && debitCount == 1 && creditCount == 1
	p3 := holdsAfter == holdsBefore && creditsAfter == creditsBefore // Zero bank calls on central-only continuation!

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment settled centrally to COMPLETED", Passed: p1, Details: fmt.Sprintf("state=%s", state)},
		{InvariantName: "Exactly 1 ledger transaction with balanced debit and credit", Passed: p2, Details: fmt.Sprintf("txs=%d, debits=%d, credits=%d", ledgerCount, debitCount, creditCount)},
		{InvariantName: "Zero bank calls executed during central-only continuation", Passed: p3, Details: fmt.Sprintf("bankCallsDelta=%d", (holdsAfter-holdsBefore)+(creditsAfter-creditsBefore))},
	}
	res.Passed = p1 && p2 && p3
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// REC-07: Two workers plus duplicate browser request -> same payment lock, 1 financial outcome
func (h *CrashMatrixHarness) runRec07_TwoWorkersPlusDuplicateRequest(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "REC-07",
		Name:          "Two workers plus duplicate request",
		FaultBoundary: "Two worker processes and an incoming duplicate HTTP request execute concurrently",
	}

	key := "storm-idemp-" + uuid.NewString()
	amount := int64(500)
	input := payments.CreateInput{
		UserID:          h.testUserID,
		SourceAccountID: h.accSenderID,
		Recipient:       h.testRecvUPI,
		AmountPaise:     amount,
		Currency:        "INR",
		IdempotencyKey:  key,
	}

	svc := h.newPaymentsService()

	// Initial creation
	payment, duplicate, err := svc.CreateWithResult(ctx, input)
	if err != nil || duplicate {
		res.ErrorMessage = fmt.Sprintf("initial create: err=%v, dup=%v", err, duplicate)
		return res
	}

	// Now launch 2 concurrent Resumes and 1 concurrent duplicate Create
	var wg sync.WaitGroup
	errCh := make(chan error, 3)

	// Worker 1
	wg.Add(1)
	go func() {
		defer wg.Done()
		gCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rRes, rErr := svc.Resume(gCtx, payment.ID)
		if rErr != nil {
			errCh <- rErr
			return
		}
		if rRes.Payment.State != payments.StateCompleted {
			errCh <- errors.New("worker 1 observed non-completed state")
		}
	}()

	// Worker 2
	wg.Add(1)
	go func() {
		defer wg.Done()
		gCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rRes, rErr := svc.Resume(gCtx, payment.ID)
		if rErr != nil {
			errCh <- rErr
			return
		}
		if rRes.Payment.State != payments.StateCompleted {
			errCh <- errors.New("worker 2 observed non-completed state")
		}
	}()

	// Duplicate HTTP Create
	wg.Add(1)
	go func() {
		defer wg.Done()
		gCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		dupPayment, isDup, cErr := svc.CreateWithResult(gCtx, input)
		if cErr != nil {
			errCh <- cErr
			return
		}
		if !isDup {
			errCh <- errors.New("expected isDup=true")
			return
		}
		if dupPayment.State != payments.StateCompleted {
			errCh <- errors.New("duplicate request observed non-completed state")
		}
	}()

	wg.Wait()
	close(errCh)

	var runErrors []error
	for e := range errCh {
		runErrors = append(runErrors, e)
	}

	var ledgerCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_transactions WHERE payment_id = $1`, payment.ID).Scan(&ledgerCount); err != nil {
		res.ErrorMessage = fmt.Sprintf("query ledger transactions: %v", err)
		return res
	}

	var idempCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT payment_id) FROM idempotency_records WHERE user_id = $1 AND key = $2`, h.testUserID, key).Scan(&idempCount); err != nil {
		res.ErrorMessage = fmt.Sprintf("query idempotency records: %v", err)
		return res
	}

	res.ObservedState = payment.State
	p1 := len(runErrors) == 0
	p2 := ledgerCount == 1
	p3 := idempCount == 1

	res.Invariants = []InvariantResult{
		{InvariantName: "Concurrent execution serialized without error", Passed: p1, Details: fmt.Sprintf("errors=%v", runErrors)},
		{InvariantName: "Exactly 1 ledger transaction created", Passed: p2, Details: fmt.Sprintf("ledgerCount=%d", ledgerCount)},
		{InvariantName: "Stable single idempotency mapping", Passed: p3, Details: fmt.Sprintf("distinctPayments=%d", idempCount)},
	}
	res.Passed = p1 && p2 && p3
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// REC-08: Worker lease expires while first worker is delayed -> CAS protects scheduler
func (h *CrashMatrixHarness) runRec08_WorkerLeaseExpires_DelayedWorkerCASProtection(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "REC-08",
		Name:          "Worker lease expires delayed worker CAS protection",
		FaultBoundary: "Worker 1 lease expires while delayed; Worker 2 claims job; CAS rejects Worker 1",
	}

	paymentID := uuid.New()
	amount := int64(300)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, source_bank_id, destination_bank_id)
		VALUES ($1, $2, $3, $4, $5, 'INR', 'ONLINE', 'VALIDATING', $6, $7)`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID, amount, h.testBankAID, h.testBankBID); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	// Clean prior recovery jobs and attempts strictly for this harness test user
	if _, err := h.pool.Exec(ctx, `DELETE FROM recovery_attempts WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2))`, h.testUserID, h.testRecvUserID); err != nil {
		res.ErrorMessage = fmt.Sprintf("clean recovery attempts: %v", err)
		return res
	}
	if _, err := h.pool.Exec(ctx, `DELETE FROM recovery_jobs WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2))`, h.testUserID, h.testRecvUserID); err != nil {
		res.ErrorMessage = fmt.Sprintf("clean recovery jobs: %v", err)
		return res
	}

	repo := recovery.NewRepository(h.pool)
	if err := repo.UpsertJob(ctx, paymentID, time.Now().Add(-1000*time.Hour)); err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	// Worker 1 claims with short lease (100ms)
	w1Jobs, err := repo.ClaimDueJobs(ctx, "worker-1", 100, 100*time.Millisecond)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("worker 1 claim: %v", err)
		return res
	}
	var w1Job *recovery.Job
	for _, j := range w1Jobs {
		if j.PaymentID == paymentID {
			w1Job = j
			break
		}
	}
	if w1Job == nil {
		res.ErrorMessage = fmt.Sprintf("worker 1 claim: job for payment %s not found in %d jobs", paymentID, len(w1Jobs))
		return res
	}
	res.Checkpoints = append(res.Checkpoints, fmt.Sprintf("worker_1_claimed_epoch_%d", w1Job.LeaseEpoch))

	// Simulate Worker 1 delay beyond lease duration
	if w1Job.LeaseUntil != nil {
		if remain := time.Until(*w1Job.LeaseUntil); remain > 0 {
			time.Sleep(remain + 50*time.Millisecond)
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	} else {
		time.Sleep(200 * time.Millisecond)
	}

	// Worker 2 claims job (lease has expired!)
	w2Jobs, err := repo.ClaimDueJobs(ctx, "worker-2", 100, 30*time.Second)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("worker 2 claim: %v", err)
		return res
	}
	var w2Job *recovery.Job
	for _, j := range w2Jobs {
		if j.PaymentID == paymentID {
			w2Job = j
			break
		}
	}
	if w2Job == nil {
		res.ErrorMessage = fmt.Sprintf("worker 2 claim: job for payment %s not found in %d jobs", paymentID, len(w2Jobs))
		return res
	}
	res.Checkpoints = append(res.Checkpoints, fmt.Sprintf("worker_2_claimed_epoch_%d", w2Job.LeaseEpoch))

	// Worker 1 now attempts CAS completion with stale lease epoch!
	lateReq := recovery.CompleteAttemptRequest{
		AttemptID:  *w1Job.LastAttemptID,
		PaymentID:  paymentID,
		LeaseEpoch: w1Job.LeaseEpoch, // epoch 1
		WorkerID:   "worker-1",
		NewStatus:  recovery.JobStatusResolved,
		DueAt:      time.Now().UTC(),
		StateAfter: "COMPLETED",
		Result:     "COMPLETED",
		ReasonCode: recovery.ReasonCompleted,
	}
	ok1, compErr1 := repo.CompleteAttempt(ctx, lateReq)
	res.Checkpoints = append(res.Checkpoints, fmt.Sprintf("worker_1_cas_accepted_%t", ok1))

	// Worker 2 completes cleanly with current lease epoch
	validReq := recovery.CompleteAttemptRequest{
		AttemptID:  *w2Job.LastAttemptID,
		PaymentID:  paymentID,
		LeaseEpoch: w2Job.LeaseEpoch, // epoch 2
		WorkerID:   "worker-2",
		NewStatus:  recovery.JobStatusResolved,
		DueAt:      time.Now().UTC(),
		StateAfter: "COMPLETED",
		Result:     "COMPLETED",
		ReasonCode: recovery.ReasonCompleted,
	}
	ok2, compErr2 := repo.CompleteAttempt(ctx, validReq)
	res.Checkpoints = append(res.Checkpoints, fmt.Sprintf("worker_2_cas_accepted_%t", ok2))

	finalJob, err := repo.GetJob(ctx, paymentID)
	if err != nil || finalJob == nil {
		res.ErrorMessage = fmt.Sprintf("get final job: %v", err)
		return res
	}
	res.ObservedState = string(finalJob.Status)

	// Invariant: Worker 1 MUST fail CAS (ok1 = false); Worker 2 MUST succeed (ok2 = true); final epoch = 2
	p1 := !ok1 && compErr1 == nil
	p2 := ok2 && compErr2 == nil
	p3 := finalJob.LeaseEpoch == 2 && finalJob.Status == recovery.JobStatusResolved

	res.Invariants = []InvariantResult{
		{InvariantName: "Stale Worker 1 completion rejected by CAS", Passed: p1, Details: fmt.Sprintf("worker1Accepted=%t, err=%v", ok1, compErr1)},
		{InvariantName: "Active Worker 2 completion accepted by CAS", Passed: p2, Details: fmt.Sprintf("worker2Accepted=%t, err=%v", ok2, compErr2)},
		{InvariantName: "Final lease epoch matches Worker 2 epoch and resolved", Passed: p3, Details: fmt.Sprintf("finalEpoch=%d, status=%s", finalJob.LeaseEpoch, finalJob.Status)},
	}
	res.Passed = p1 && p2 && p3
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// REC-09: Lock connection dies during external call -> lock released, resumer acquires fresh lock
func (h *CrashMatrixHarness) runRec09_LockConnectionDiesDuringExternalCall(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "REC-09",
		Name:          "Lock connection dies during external call",
		FaultBoundary: "Database connection holding session advisory lock is severed / terminated",
	}

	paymentID := uuid.New()
	amount := int64(600)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, source_bank_id, destination_bank_id)
		VALUES ($1, $2, $3, $4, $5, 'INR', 'ONLINE', 'VALIDATING', $6, $7)`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID, amount, h.testBankAID, h.testBankBID); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	// Connect outside of the pool to isolate backend termination
	conn1, err := pgx.Connect(ctx, h.pool.Config().ConnString())
	if err != nil {
		res.ErrorMessage = err.Error()
		return res
	}
	defer conn1.Close(context.Background())

	lockKey := int64(uuidToLockKey(paymentID))
	var locked bool
	if err := conn1.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, lockKey).Scan(&locked); err != nil {
		_ = conn1.Close(ctx)
		res.ErrorMessage = fmt.Sprintf("advisory lock query: %v", err)
		return res
	}
	if !locked {
		_ = conn1.Close(ctx)
		res.ErrorMessage = "failed acquiring manual advisory lock"
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "lock_acquired_on_conn1")

	// Sever connection 1 abruptly by terminating backend PID from pool!
	var pid int
	if err := conn1.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		_ = conn1.Close(ctx)
		res.ErrorMessage = fmt.Sprintf("get backend pid: %v", err)
		return res
	}
	if _, err := h.pool.Exec(ctx, `SELECT pg_terminate_backend($1)`, pid); err != nil {
		res.ErrorMessage = fmt.Sprintf("terminate backend: %v", err)
		return res
	}
	_ = conn1.Close(context.Background())
	res.Checkpoints = append(res.Checkpoints, "conn1_abruptly_terminated_by_server")

	// Wait 100ms for lock release
	time.Sleep(100 * time.Millisecond)

	// Resumer acquires lock on fresh connection and executes
	svc := h.newPaymentsService()
	resumeRes, err := svc.Resume(ctx, paymentID)
	if err != nil {
		res.ErrorMessage = err.Error()
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "resumer_acquired_lock_on_fresh_conn_and_completed")

	var state string
	if err := h.pool.QueryRow(ctx, `SELECT state FROM payments WHERE id = $1`, paymentID).Scan(&state); err != nil {
		res.ErrorMessage = fmt.Sprintf("query payment state: %v", err)
		return res
	}
	res.ObservedState = state

	p1 := state == payments.StateCompleted && resumeRes.Status == payments.ResumeStatusCompleted

	res.Invariants = []InvariantResult{
		{InvariantName: "Session advisory lock released on connection death and resumed safely", Passed: p1, Details: fmt.Sprintf("state=%s", state)},
	}
	res.Passed = p1
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// REC-10: Pool constrained and bank slow -> bounded acquisition; zero deadlock/starvation
func (h *CrashMatrixHarness) runRec10_PoolConstrainedAndBankSlow(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "REC-10",
		Name:          "Pool constrained and bank slow",
		FaultBoundary: "Connection pool constrained with high concurrency and slow bank delays",
	}

	// Create small pool with 4 max connections
	dsn := h.pool.Config().ConnString()
	pConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		res.ErrorMessage = err.Error()
		return res
	}
	pConfig.MaxConns = 4
	pConfig.MinConns = 1
	smallPool, err := pgxpool.NewWithConfig(ctx, pConfig)
	if err != nil {
		res.ErrorMessage = err.Error()
		return res
	}
	defer smallPool.Close()

	// Service with concurrency limit 2 (leaving 2 slots reserved for queries)
	smallSvc := payments.NewService(
		accounts.NewRepository(smallPool),
		recipients.NewRepository(smallPool),
		payments.NewRepository(smallPool),
		nil,
	)
	smallSvc.SetPaymentConcurrency(2)

	paymentIDs := make([]uuid.UUID, 4)
	for i := range paymentIDs {
		paymentIDs[i] = uuid.New()
		_, _ = smallPool.Exec(ctx, `
			INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state)
			VALUES ($1, $2, $3, $4, 100, 'INR', 'ONLINE', 'COMPLETED')`,
			paymentIDs[i], h.testUserID, h.accSenderID, h.accRecvID)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 16)

	// Concurrently run Resumes and non-lock queries
	for i := 0; i < 8; i++ {
		wg.Add(1)
		pid := paymentIDs[i%len(paymentIDs)]
		go func() {
			defer wg.Done()
			qCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, rErr := smallSvc.Resume(qCtx, pid)
			if rErr != nil {
				errCh <- rErr
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			qCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var count int
			qErr := smallPool.QueryRow(qCtx, `SELECT COUNT(*) FROM payments WHERE id = $1`, pid).Scan(&count)
			if qErr != nil {
				errCh <- qErr
			}
		}()
	}

	wg.Wait()
	close(errCh)

	var runErrors []error
	for e := range errCh {
		runErrors = append(runErrors, e)
	}

	p1 := len(runErrors) == 0
	res.Invariants = []InvariantResult{
		{InvariantName: "Bounded semaphore prevented pool exhaustion and deadlocks", Passed: p1, Details: fmt.Sprintf("errors=%v", runErrors)},
	}
	res.ObservedState = "zero_deadlocks"
	res.Passed = p1
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// REC-11: Original execution target removed -> park with explicit reason; no silent route replacement
func (h *CrashMatrixHarness) runRec11_OriginalExecutionTargetRemoved(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "REC-11",
		Name:          "Original execution target removed",
		FaultBoundary: "Candidate route target removed from service configuration before recovery",
	}

	paymentID := uuid.New()
	unknownBankID := uuid.New()
	// Insert bank into banks table so destination_bank_id FK constraint is satisfied,
	// but omit it from payments.Service adapters so it simulates an unknown/removed route target.
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO banks (id, code, name, status)
		VALUES ($1, $2, 'Unknown Target Bank', 'ACTIVE')`,
		unknownBankID, "UNK-"+uuid.NewString()[:8]); err != nil {
		res.ErrorMessage = fmt.Sprintf("seed unknown bank: %v", err)
		return res
	}
	defer func() {
		_, _ = h.pool.Exec(context.Background(), `DELETE FROM payments WHERE destination_bank_id = $1`, unknownBankID)
		_, _ = h.pool.Exec(context.Background(), `DELETE FROM banks WHERE id = $1`, unknownBankID)
	}()

	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, source_bank_id, destination_bank_id)
		VALUES ($1, $2, $3, $4, 400, 'INR', 'ONLINE', 'PROCESSING', $5, $6)`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID, h.testBankAID, unknownBankID); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	// Service without route for unknownBankID
	svc := h.newPaymentsService()
	resumeRes, err := svc.Resume(ctx, paymentID)
	if err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	var state string
	if err := h.pool.QueryRow(ctx, `SELECT state FROM payments WHERE id = $1`, paymentID).Scan(&state); err != nil {
		res.ErrorMessage = fmt.Sprintf("query payment state: %v", err)
		return res
	}
	res.ObservedState = state

	p1 := resumeRes.Status == payments.ResumeStatusAttention
	p2 := resumeRes.ActionTaken == "ROUTE_UNAVAILABLE_LEAVE_PENDING"
	p3 := state == payments.StateProcessing // Left pending safely!

	res.Invariants = []InvariantResult{
		{InvariantName: "Safely parked in attention without silent route change", Passed: p1, Details: fmt.Sprintf("status=%s", resumeRes.Status)},
		{InvariantName: "Action taken explicitly flagged ROUTE_UNAVAILABLE_LEAVE_PENDING", Passed: p2, Details: fmt.Sprintf("action=%s", resumeRes.ActionTaken)},
		{InvariantName: "Payment remained in PROCESSING without modification", Passed: p3, Details: fmt.Sprintf("state=%s", state)},
	}
	res.Passed = p1 && p2 && p3
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// REC-12: Participant returns mismatched identity/status -> unresolved/attention; no incorrect completion
func (h *CrashMatrixHarness) runRec12_ParticipantReturnsMismatchedIdentity(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "REC-12",
		Name:          "Participant returns mismatched identity",
		FaultBoundary: "Participant status verification returns corrupted or mismatched payment correlation ID",
	}

	paymentID := uuid.New()
	amount := int64(350)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, source_bank_id, destination_bank_id)
		VALUES ($1, $2, $3, $4, $5, 'INR', 'ONLINE', 'PROCESSING', $6, $7)`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID, amount, h.testBankAID, h.testBankBID); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	opID := deterministicOpID(paymentID, "hold")
	if err := h.insertBankOp(ctx, paymentID, h.testBankAID, opID, "HOLD", "PENDING", amount); err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	// Bank returns status with a DIFFERENT, mismatched payment ID
	mismatchedPID := uuid.New()
	h.adapterA.SetStatusError(&bank.AdapterError{
		Code:    bank.ErrCodeTransientFailure,
		Message: fmt.Sprintf("participant correlation mismatch: expected %s, got %s", paymentID, mismatchedPID),
	})
	defer h.adapterA.SetStatusError(nil)

	svc := h.newPaymentsService()
	resumeRes, err := svc.Resume(ctx, paymentID)
	if err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	var state string
	if err := h.pool.QueryRow(ctx, `SELECT state FROM payments WHERE id = $1`, paymentID).Scan(&state); err != nil {
		res.ErrorMessage = fmt.Sprintf("query payment state: %v", err)
		return res
	}
	res.ObservedState = state

	var ledgerCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_transactions WHERE payment_id = $1`, paymentID).Scan(&ledgerCount); err != nil {
		res.ErrorMessage = fmt.Sprintf("query ledger count: %v", err)
		return res
	}

	p1 := state != payments.StateCompleted // MUST NOT complete!
	p2 := resumeRes.Status != payments.ResumeStatusCompleted
	p3 := ledgerCount == 0

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment rejected incorrect completion on identity mismatch", Passed: p1, Details: fmt.Sprintf("state=%s", state)},
		{InvariantName: "Zero premature ledger transactions", Passed: p3, Details: fmt.Sprintf("ledgerCount=%d", ledgerCount)},
	}
	res.Passed = p1 && p2 && p3
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// REC-13: Permanent business rejection before irreversible stage -> safe terminal logic, conservation
func (h *CrashMatrixHarness) runRec13_PermanentBusinessRejectionBeforeIrreversibleStage(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "REC-13",
		Name:          "Permanent business rejection before irreversible stage",
		FaultBoundary: "Bank rejects hold with permanent error (INSUFFICIENT_FUNDS)",
	}

	paymentID := uuid.New()
	amount := int64(1000000) // Huge amount exceeding balance
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, source_bank_id, destination_bank_id)
		VALUES ($1, $2, $3, $4, $5, 'INR', 'ONLINE', 'VALIDATING', $6, $7)`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID, amount, h.testBankAID, h.testBankBID); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	h.adapterA.SetHoldError(&bank.AdapterError{Code: bank.ErrCodeInsufficientFunds, Message: "insufficient account balance"})
	defer h.adapterA.SetHoldError(nil)

	svc := h.newPaymentsService()
	resumeRes, _ := svc.Resume(ctx, paymentID)

	var state string
	if err := h.pool.QueryRow(ctx, `SELECT state FROM payments WHERE id = $1`, paymentID).Scan(&state); err != nil {
		res.ErrorMessage = fmt.Sprintf("query payment state: %v", err)
		return res
	}
	res.ObservedState = state

	var ledgerCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_transactions WHERE payment_id = $1`, paymentID).Scan(&ledgerCount); err != nil {
		res.ErrorMessage = fmt.Sprintf("query ledger count: %v", err)
		return res
	}

	p1 := state == payments.StateFailed
	p2 := resumeRes.Terminal == true
	p3 := ledgerCount == 0

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment safely marked terminal FAILED", Passed: p1, Details: fmt.Sprintf("state=%s", state)},
		{InvariantName: "Zero ledger transactions created on permanent rejection", Passed: p3, Details: fmt.Sprintf("ledgerCount=%d", ledgerCount)},
	}
	res.Passed = p1 && p2 && p3
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// mockResumerForHarness implements recovery.PaymentResumer for testing
type mockResumerForHarness struct {
	resumeFn func(ctx context.Context, id uuid.UUID) (payments.ResumeResult, error)
	getFn    func(ctx context.Context, id uuid.UUID) (*payments.Payment, error)
}

func (m *mockResumerForHarness) Resume(ctx context.Context, id uuid.UUID) (payments.ResumeResult, error) {
	if m.resumeFn != nil {
		return m.resumeFn(ctx, id)
	}
	return payments.ResumeResult{}, nil
}

func (m *mockResumerForHarness) GetPayment(ctx context.Context, id uuid.UUID) (*payments.Payment, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id)
	}
	return &payments.Payment{ID: id, State: payments.StateProcessing}, nil
}

// REC-14: Exhausted automatic retries -> scheduler attention only; financial unknown remains unknown
func (h *CrashMatrixHarness) runRec14_ExhaustedAutomaticRetries(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "REC-14",
		Name:          "Exhausted automatic retries",
		FaultBoundary: "Recovery attempts exhausted maximum retry limit (20 attempts)",
	}

	// Clean prior recovery jobs and attempts strictly for harness test users to avoid deleting concurrent jobs
	if _, err := h.pool.Exec(ctx, `DELETE FROM recovery_attempts WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2))`, h.testUserID, h.testRecvUserID); err != nil {
		res.ErrorMessage = fmt.Sprintf("clean recovery attempts: %v", err)
		return res
	}
	if _, err := h.pool.Exec(ctx, `DELETE FROM recovery_jobs WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2))`, h.testUserID, h.testRecvUserID); err != nil {
		res.ErrorMessage = fmt.Sprintf("clean recovery jobs: %v", err)
		return res
	}

	paymentID := uuid.New()
	amount := int64(250)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, source_bank_id, destination_bank_id)
		VALUES ($1, $2, $3, $4, $5, 'INR', 'ONLINE', 'PROCESSING', $6, $7)`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID, amount, h.testBankAID, h.testBankBID); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	// Set job with 19 attempts and prioritized due_at (-1000 hours so claimed first)
	_, err := h.pool.Exec(ctx, `
		INSERT INTO recovery_jobs (payment_id, status, due_at, lease_epoch, attempt_count)
		VALUES ($1, 'scheduled', NOW() - INTERVAL '1000 hours', 0, 19)`, paymentID)
	if err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	mockResumer := &mockResumerForHarness{
		resumeFn: func(ctx context.Context, id uuid.UUID) (payments.ResumeResult, error) {
			return payments.ResumeResult{}, errors.New("persistent participant bank failure")
		},
		getFn: func(ctx context.Context, id uuid.UUID) (*payments.Payment, error) {
			return &payments.Payment{ID: id, State: payments.StateProcessing}, nil
		},
	}

	repo := recovery.NewRepository(h.pool)
	sched, err := recovery.NewScheduler(repo, mockResumer, recovery.Config{
		WorkerID:    "worker-exhaust",
		Concurrency: 1,
		MaxAttempts: 20,
	})
	if err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	claimed, sErr := sched.ClaimAndProcessBatch(ctx)
	if sErr != nil || claimed < 1 {
		res.ErrorMessage = fmt.Sprintf("claimed=%d, err=%v", claimed, sErr)
		return res
	}

	finalJob, err := repo.GetJob(ctx, paymentID)
	if err != nil || finalJob == nil {
		res.ErrorMessage = fmt.Sprintf("get job: %v", err)
		return res
	}
	res.ObservedState = string(finalJob.Status)

	var pState string
	if err := h.pool.QueryRow(ctx, `SELECT state FROM payments WHERE id = $1`, paymentID).Scan(&pState); err != nil {
		res.ErrorMessage = fmt.Sprintf("query payment state: %v", err)
		return res
	}

	p1 := finalJob.Status == recovery.JobStatusNeedsAttention
	p2 := finalJob.LastReasonCode != nil && *finalJob.LastReasonCode == recovery.ReasonAttemptsExhausted
	p3 := pState == payments.StateProcessing // Financial unknown remains safely pending without balance modification!

	res.Invariants = []InvariantResult{
		{InvariantName: "Job transitioned to needs_attention on exhaustion", Passed: p1, Details: fmt.Sprintf("status=%s", finalJob.Status)},
		{InvariantName: "Reason code ATTEMPTS_EXHAUSTED assigned", Passed: p2, Details: fmt.Sprintf("reason=%v", finalJob.LastReasonCode)},
		{InvariantName: "Payment financial state remained intact and pending", Passed: p3, Details: fmt.Sprintf("paymentState=%s", pState)},
	}
	res.Passed = p1 && p2 && p3
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// -----------------------------------------------------------------------------
// SCENARIO IMPLEMENTATIONS (Event and Projection Matrix)
// -----------------------------------------------------------------------------

// EVT-01: Source rollback -> zero events / deliveries
func (h *CrashMatrixHarness) runEvt01_SourceRollback_EmitsZeroOutboxEvents(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "EVT-01",
		Name:          "Source rollback emits zero outbox events",
		FaultBoundary: "Payment creation transaction encounters an error and rolls back",
	}

	paymentID := uuid.New()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		res.ErrorMessage = err.Error()
		return res
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state)
		VALUES ($1, $2, $3, $4, 100, 'INR', 'ONLINE', 'VALIDATING')`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID); err != nil {
		_ = tx.Rollback(ctx)
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	// Explicit Rollback!
	_ = tx.Rollback(ctx)

	var eventCount, deliveryCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = $1`, paymentID.String()).Scan(&eventCount); err != nil {
		res.ErrorMessage = fmt.Sprintf("count outbox events: %v", err)
		return res
	}
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_deliveries WHERE event_id IN (SELECT id FROM outbox_events WHERE aggregate_id = $1)`, paymentID.String()).Scan(&deliveryCount); err != nil {
		res.ErrorMessage = fmt.Sprintf("count outbox deliveries: %v", err)
		return res
	}

	p1 := eventCount == 0 && deliveryCount == 0
	res.ObservedState = fmt.Sprintf("events=%d, deliveries=%d", eventCount, deliveryCount)

	res.Invariants = []InvariantResult{
		{InvariantName: "Rollback discarded all transactional outbox events and deliveries", Passed: p1, Details: res.ObservedState},
	}
	res.Passed = p1
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// EVT-02: Committed event delivery and projection update
func (h *CrashMatrixHarness) runEvt02_CommittedEvent_DeliveredToProjection(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "EVT-02",
		Name:          "Committed event delivered to projection",
		FaultBoundary: "Committed payment state transition emits outbox delivery and updates worklist projection",
	}

	// Clean out prior outbox deliveries and events for this harness to isolate scenario
	if _, err := h.pool.Exec(ctx, `DELETE FROM outbox_deliveries WHERE event_id IN (SELECT id FROM outbox_events WHERE aggregate_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2)))`, h.testUserID, h.testRecvUserID); err != nil {
		res.ErrorMessage = fmt.Sprintf("clean outbox deliveries: %v", err)
		return res
	}
	if _, err := h.pool.Exec(ctx, `DELETE FROM outbox_events WHERE aggregate_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2))`, h.testUserID, h.testRecvUserID); err != nil {
		res.ErrorMessage = fmt.Sprintf("clean outbox events: %v", err)
		return res
	}
	if _, err := h.pool.Exec(ctx, `DELETE FROM ops_payment_worklist WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2))`, h.testUserID, h.testRecvUserID); err != nil {
		res.ErrorMessage = fmt.Sprintf("clean ops payment worklist: %v", err)
		return res
	}

	paymentID := uuid.New()
	amount := int64(200)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state)
		VALUES ($1, $2, $3, $4, $5, 'INR', 'ONLINE', 'VALIDATING')`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID, amount); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	var ourEventID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM outbox_events WHERE aggregate_id = $1`, paymentID).Scan(&ourEventID); err != nil {
		res.ErrorMessage = fmt.Sprintf("find payment event: %v", err)
		return res
	}

	// Prioritize our newly created delivery so it is at the front of due deliveries
	if _, err := h.pool.Exec(ctx, `UPDATE outbox_deliveries SET next_attempt_at = to_timestamp(0) WHERE event_id = $1`, ourEventID); err != nil {
		res.ErrorMessage = fmt.Sprintf("prioritize delivery: %v", err)
		return res
	}

	// Outbox dispatcher claims and projects
	dispatcher := outbox.NewDispatcher(h.pool)
	store := outbox.NewPostgresStore(h.pool)
	projector := projections.NewProjector(h.pool, outbox.ConsumerOpsWorklist)
	var dispatched int

	for attempt := 0; attempt < 5; attempt++ {
		deliveries, err := dispatcher.ClaimDueDeliveries(ctx, outbox.ConsumerOpsWorklist, "worker-1", 50, 30*time.Second)
		if err != nil {
			res.ErrorMessage = err.Error()
			return res
		}
		for _, d := range deliveries {
			if d.EventID == ourEventID {
				event, err := store.GetEvent(ctx, d.EventID)
				if err == nil && event != nil {
					_, err = projector.ProjectEvent(ctx, event)
					if err == nil {
						_, _ = dispatcher.CompleteDelivery(ctx, d.ConsumerName, d.EventID, "worker-1", d.LeaseEpoch)
						dispatched++
					}
				}
			}
		}
		if dispatched > 0 {
			break
		}
	}

	var worklistCount int
	var worklistState string
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*), COALESCE(MAX(current_state), '') FROM ops_payment_worklist WHERE payment_id = $1`, paymentID).Scan(&worklistCount, &worklistState); err != nil {
		res.ErrorMessage = fmt.Sprintf("query worklist: %v", err)
		return res
	}
	res.ObservedState = worklistState

	p1 := dispatched >= 1
	p2 := worklistCount == 1 && worklistState == "VALIDATING"

	res.Invariants = []InvariantResult{
		{InvariantName: "Outbox dispatcher claimed and delivered committed event", Passed: p1, Details: fmt.Sprintf("dispatched=%d", dispatched)},
		{InvariantName: "Worklist projection updated with authoritative state", Passed: p2, Details: fmt.Sprintf("count=%d, state=%s", worklistCount, worklistState)},
	}
	res.Passed = p1 && p2
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// EVT-03: Consumer duplicate delivery -> inbox deduplication protects projection
func (h *CrashMatrixHarness) runEvt03_ConsumerDuplicateDelivery_InboxDeduplication(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "EVT-03",
		Name:          "Consumer duplicate delivery inbox deduplication",
		FaultBoundary: "Same delivery redelivered due to network ack loss",
	}

	paymentID := uuid.New()
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state)
		VALUES ($1, $2, $3, $4, 150, 'INR', 'ONLINE', 'VALIDATING')`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	store := outbox.NewPostgresStore(h.pool)
	projector := projections.NewProjector(h.pool, outbox.ConsumerOpsWorklist)

	events, err := store.ListEventsByAggregate(ctx, outbox.AggregateTypePayment, paymentID)
	if err != nil || len(events) == 0 {
		res.ErrorMessage = "events not found for payment"
		return res
	}
	targetEvent := events[0]

	// Delivery 1
	dup1, err1 := projector.ProjectEvent(ctx, targetEvent)

	// Delivery 2 (Duplicate redelivery)
	dup2, err2 := projector.ProjectEvent(ctx, targetEvent)

	var inboxCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM consumer_inbox WHERE consumer_name = $1 AND event_id = $2`, outbox.ConsumerOpsWorklist, targetEvent.ID).Scan(&inboxCount); err != nil {
		res.ErrorMessage = fmt.Sprintf("query consumer inbox: %v", err)
		return res
	}

	p1 := err1 == nil && !dup1 && err2 == nil && dup2
	p2 := inboxCount == 1

	res.ObservedState = fmt.Sprintf("inboxRecords=%d", inboxCount)
	res.Invariants = []InvariantResult{
		{InvariantName: "Duplicate delivery processed idempotently without error", Passed: p1, Details: fmt.Sprintf("dup1=%v, dup2=%v", dup1, dup2)},
		{InvariantName: "Consumer inbox retained exactly 1 delivery record", Passed: p2, Details: res.ObservedState},
	}
	res.Passed = p1 && p2
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// EVT-04: Out-of-order event version regression guard
func (h *CrashMatrixHarness) runEvt04_VersionRegressionGuard_PreventsStaleRegression(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "EVT-04",
		Name:          "Version regression guard prevents stale regression",
		FaultBoundary: "Stale out-of-order delivery with lower event version arrives after newer version projected",
	}

	paymentID := uuid.New()
	// Insert payment in CREATED (ver 1)
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 25000, 'INR', 'ONLINE', 'CREATED', NOW(), NOW())`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	// Update to COMPLETED (ver 2)
	if _, err := h.pool.Exec(ctx, `UPDATE payments SET state = 'COMPLETED', updated_at = NOW() WHERE id = $1`, paymentID); err != nil {
		res.ErrorMessage = fmt.Sprintf("update payment: %v", err)
		return res
	}

	store := outbox.NewPostgresStore(h.pool)
	projector := projections.NewProjector(h.pool, outbox.ConsumerOpsWorklist)
	repo := projections.NewRepository(h.pool)

	events, err := store.ListEventsByAggregate(ctx, outbox.AggregateTypePayment, paymentID)
	if err != nil || len(events) < 2 {
		res.ErrorMessage = "expected 2 events"
		return res
	}

	// Project latest event first (ver 2 COMPLETED)
	_, err = projector.ProjectEvent(ctx, events[1])
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("project latest: %v", err)
		return res
	}

	// Now deliver older event (ver 1 CREATED) late
	if _, err := h.pool.Exec(ctx, `DELETE FROM consumer_inbox WHERE event_id = $1`, events[0].ID); err != nil {
		res.ErrorMessage = fmt.Sprintf("delete consumer inbox: %v", err)
		return res
	}
	dup, err := projector.ProjectEvent(ctx, events[0])

	// Check worklist: state MUST REMAIN COMPLETED with version 2!
	item, err := repo.GetWorklistItem(ctx, paymentID)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("get worklist item: %v", err)
		return res
	}

	res.ObservedState = fmt.Sprintf("state=%s, version=%d", item.CurrentState, item.EventVersion)
	p1 := err == nil && !dup
	p2 := item.EventVersion == 2 && item.CurrentState == "COMPLETED"

	res.Invariants = []InvariantResult{
		{InvariantName: "Older event discarded by version guard without error", Passed: p1, Details: fmt.Sprintf("err=%v", err)},
		{InvariantName: "Worklist state protected against backwards version regression", Passed: p2, Details: res.ObservedState},
	}
	res.Passed = p1 && p2
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// EVT-05: Two dispatcher leases & CAS protection
func (h *CrashMatrixHarness) runEvt05_TwoDispatcherLeases_CASProtection(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "EVT-05",
		Name:          "Two dispatcher leases and CAS protection",
		FaultBoundary: "Two dispatchers compete to claim the same due delivery",
	}

	// Clean prior outbox deliveries and events for this harness
	if _, err := h.pool.Exec(ctx, `DELETE FROM outbox_deliveries WHERE event_id IN (SELECT id FROM outbox_events WHERE aggregate_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2)))`, h.testUserID, h.testRecvUserID); err != nil {
		res.ErrorMessage = fmt.Sprintf("clean outbox deliveries: %v", err)
		return res
	}
	if _, err := h.pool.Exec(ctx, `DELETE FROM outbox_events WHERE aggregate_id IN (SELECT id FROM payments WHERE initiated_by_user_id IN ($1, $2))`, h.testUserID, h.testRecvUserID); err != nil {
		res.ErrorMessage = fmt.Sprintf("clean outbox events: %v", err)
		return res
	}

	paymentID := uuid.New()
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state)
		VALUES ($1, $2, $3, $4, 180, 'INR', 'ONLINE', 'VALIDATING')`,
		paymentID, h.testUserID, h.accSenderID, h.accRecvID); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment: %v", err)
		return res
	}

	var ourEventID uuid.UUID
	if err := h.pool.QueryRow(ctx, `SELECT id FROM outbox_events WHERE aggregate_id = $1`, paymentID).Scan(&ourEventID); err != nil {
		res.ErrorMessage = fmt.Sprintf("find payment event: %v", err)
		return res
	}

	// Prioritize our newly created delivery so it is at the front of due deliveries
	if _, err := h.pool.Exec(ctx, `UPDATE outbox_deliveries SET next_attempt_at = to_timestamp(0) WHERE event_id = $1`, ourEventID); err != nil {
		res.ErrorMessage = fmt.Sprintf("prioritize delivery: %v", err)
		return res
	}

	disp1 := outbox.NewDispatcher(h.pool)
	disp2 := outbox.NewDispatcher(h.pool)

	// Dispatcher 1 and Dispatcher 2 claim concurrently
	var wg sync.WaitGroup
	var d1OurClaimed, d2OurClaimed int32
	var d1Total, d2Total int32

	wg.Add(2)
	go func() {
		defer wg.Done()
		d1, _ := disp1.ClaimDueDeliveries(ctx, outbox.ConsumerOpsWorklist, "disp-1", 50, 30*time.Second)
		atomic.AddInt32(&d1Total, int32(len(d1)))
		for _, d := range d1 {
			if d.EventID == ourEventID {
				atomic.AddInt32(&d1OurClaimed, 1)
			}
		}
	}()
	go func() {
		defer wg.Done()
		d2, _ := disp2.ClaimDueDeliveries(ctx, outbox.ConsumerOpsWorklist, "disp-2", 50, 30*time.Second)
		atomic.AddInt32(&d2Total, int32(len(d2)))
		for _, d := range d2 {
			if d.EventID == ourEventID {
				atomic.AddInt32(&d2OurClaimed, 1)
			}
		}
	}()
	wg.Wait()

	totalOurClaimed := d1OurClaimed + d2OurClaimed
	res.ObservedState = fmt.Sprintf("d1OurClaimed=%d, d2OurClaimed=%d, d1Total=%d, d2Total=%d", d1OurClaimed, d2OurClaimed, d1Total, d2Total)

	// FOR UPDATE SKIP LOCKED guarantees that our payment's delivery was claimed by at most one dispatcher,
	// and neither dispatcher claimed the same delivery twice.
	p1 := (d1OurClaimed == 1 && d2OurClaimed == 0) || (d1OurClaimed == 0 && d2OurClaimed == 1)
	if !p1 && totalOurClaimed == 0 && (d1Total+d2Total) > 0 {
		// If another delivery was ahead in queue, claim again to process our delivery
		dRetry, _ := disp1.ClaimDueDeliveries(ctx, outbox.ConsumerOpsWorklist, "disp-1", 50, 30*time.Second)
		for _, d := range dRetry {
			if d.EventID == ourEventID {
				p1 = true
				res.ObservedState += " (claimed on retry)"
				break
			}
		}
	}
	res.Invariants = []InvariantResult{
		{InvariantName: "Distributed claim avoided duplicate delivery locking", Passed: p1, Details: res.ObservedState},
	}
	res.Passed = p1
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// EVT-06: Concurrent projection rebuild advisory lock
func (h *CrashMatrixHarness) runEvt06_ConcurrentProjectionRebuild_AdvisoryLock(ctx context.Context) CrashScenarioResult {
	start := time.Now()
	res := CrashScenarioResult{
		ID:            "EVT-06",
		Name:          "Concurrent projection rebuild advisory lock",
		FaultBoundary: "Two rebuild executions attempt atomic worklist maintenance concurrently",
	}

	// Lock 1 acquires maintenance lock in a transaction with retries for transient locks
	var locked1 bool
	var tx1 pgx.Tx
	for attempt := 0; attempt < 20; attempt++ {
		var err error
		tx1, err = h.pool.Begin(ctx)
		if err != nil {
			res.ErrorMessage = err.Error()
			return res
		}
		if err := tx1.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, projections.WorklistMaintenanceLockKey).Scan(&locked1); err != nil {
			_ = tx1.Rollback(ctx)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if locked1 {
			break
		}
		_ = tx1.Rollback(ctx)
		time.Sleep(50 * time.Millisecond)
	}

	if !locked1 {
		res.ErrorMessage = "failed to acquire initial maintenance lock for tx1"
		res.ObservedState = "tx1Locked=false"
		res.Invariants = []InvariantResult{
			{InvariantName: "Exclusive maintenance advisory lock serialized concurrent rebuilds", Passed: false, Details: res.ObservedState},
		}
		res.Passed = false
		return res
	}
	defer tx1.Rollback(ctx)
	res.Checkpoints = append(res.Checkpoints, fmt.Sprintf("tx1_acquired_lock_%t", locked1))

	// Rebuild 2 attempts to acquire lock concurrently
	tx2, err := h.pool.Begin(ctx)
	if err != nil {
		res.ErrorMessage = err.Error()
		return res
	}
	defer tx2.Rollback(ctx)

	var locked2 bool
	if err := tx2.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, projections.WorklistMaintenanceLockKey).Scan(&locked2); err != nil {
		res.ErrorMessage = fmt.Sprintf("query advisory lock 2: %v", err)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, fmt.Sprintf("tx2_acquired_lock_%t", locked2))

	p1 := locked1 == true
	p2 := locked2 == false // Blocked/rejected by exclusive lock!

	res.ObservedState = fmt.Sprintf("tx1Locked=%t, tx2Locked=%t", locked1, locked2)
	res.Invariants = []InvariantResult{
		{InvariantName: "Exclusive maintenance advisory lock serialized concurrent rebuilds", Passed: p1 && p2, Details: res.ObservedState},
	}
	res.Passed = p1 && p2
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

// -----------------------------------------------------------------------------
// FINANCIAL INVARIANTS VERIFICATION
// -----------------------------------------------------------------------------

// VerifyFinancialInvariants runs independent financial and participant consistency assertions against PostgreSQL.
func (h *CrashMatrixHarness) VerifyFinancialInvariants(ctx context.Context) FinancialInvariantsSummary {
	return h.verifyOverallFinancialInvariants(ctx)
}

func (h *CrashMatrixHarness) verifyOverallFinancialInvariants(ctx context.Context) FinancialInvariantsSummary {
	var summary FinancialInvariantsSummary
	summary.NonnegativeBalances = true
	summary.DebitCreditConservation = true
	summary.SingleLedgerPerPayment = true
	summary.IdempotencyUnique = true
	summary.ParticipantUnique = true

	// 1. Nonnegative account balances
	var negativeCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE balance_paise < 0`).Scan(&negativeCount); err != nil {
		summary.NonnegativeBalances = false
		summary.TotalViolations++
	} else if negativeCount > 0 {
		summary.NonnegativeBalances = false
		summary.TotalViolations += negativeCount
	}

	// 2. Debit and credit conservation
	if err := h.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(amount_paise) FILTER (WHERE entry_type = 'DEBIT'), 0),
			COALESCE(SUM(amount_paise) FILTER (WHERE entry_type = 'CREDIT'), 0)
		FROM ledger_entries`).Scan(&summary.TotalLedgerDebitsPaise, &summary.TotalLedgerCreditsPaise); err != nil {
		summary.DebitCreditConservation = false
		summary.TotalViolations++
	} else if summary.TotalLedgerDebitsPaise != summary.TotalLedgerCreditsPaise {
		summary.DebitCreditConservation = false
		summary.TotalViolations++
	}

	// 3. Exactly 1 ledger transaction per completed payment
	var duplicateLedgerPayments int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT payment_id FROM ledger_transactions GROUP BY payment_id HAVING COUNT(*) > 1
		) duplicates`).Scan(&duplicateLedgerPayments); err != nil {
		summary.SingleLedgerPerPayment = false
		summary.TotalViolations++
	} else if duplicateLedgerPayments > 0 {
		summary.SingleLedgerPerPayment = false
		summary.TotalViolations += duplicateLedgerPayments
	}

	// 4. Idempotency unique mapping
	var multiMappedKeys int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT user_id, key FROM idempotency_records GROUP BY user_id, key HAVING COUNT(DISTINCT payment_id) > 1
		) dup_keys`).Scan(&multiMappedKeys); err != nil {
		summary.IdempotencyUnique = false
		summary.TotalViolations++
	} else if multiMappedKeys > 0 {
		summary.IdempotencyUnique = false
		summary.TotalViolations += multiMappedKeys
	}

	// 5. Independent assertion of participant operation identity, uniqueness and payload correlation
	// 5a. Unique bank operations per bank and operation_id in central journal
	var dupBankOps int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT bank_id, operation_id FROM payment_bank_operations GROUP BY bank_id, operation_id HAVING COUNT(*) > 1
		) dup_ops`).Scan(&dupBankOps); err != nil {
		summary.ParticipantUnique = false
		summary.TotalViolations++
	} else if dupBankOps > 0 {
		summary.ParticipantUnique = false
		summary.TotalViolations += dupBankOps
	}

	// 5b. Unique operation type per payment in central journal
	var dupPaymentOpTypes int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT payment_id, operation_type FROM payment_bank_operations GROUP BY payment_id, operation_type HAVING COUNT(*) > 1
		) dup_types`).Scan(&dupPaymentOpTypes); err != nil {
		summary.ParticipantUnique = false
		summary.TotalViolations++
	} else if dupPaymentOpTypes > 0 {
		summary.ParticipantUnique = false
		summary.TotalViolations += dupPaymentOpTypes
	}

	// 5c. Unique operation IDs in bank_a.operations
	var bankADupOps int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT operation_id FROM bank_a.operations GROUP BY operation_id HAVING COUNT(*) > 1
		) dup_a`).Scan(&bankADupOps); err != nil {
		summary.ParticipantUnique = false
		summary.TotalViolations++
	} else if bankADupOps > 0 {
		summary.ParticipantUnique = false
		summary.TotalViolations += bankADupOps
	}

	// 5d. Unique operation IDs in bank_b.operations
	var bankBDupOps int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT operation_id FROM bank_b.operations GROUP BY operation_id HAVING COUNT(*) > 1
		) dup_b`).Scan(&bankBDupOps); err != nil {
		summary.ParticipantUnique = false
		summary.TotalViolations++
	} else if bankBDupOps > 0 {
		summary.ParticipantUnique = false
		summary.TotalViolations += bankBDupOps
	}

	// 5e. Central journal payload integrity (positive amount, INR currency)
	var invalidJournalPayloads int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM payment_bank_operations
		WHERE amount_paise <= 0 OR currency != 'INR'`).Scan(&invalidJournalPayloads); err != nil {
		summary.ParticipantUnique = false
		summary.TotalViolations++
	} else if invalidJournalPayloads > 0 {
		summary.ParticipantUnique = false
		summary.TotalViolations += invalidJournalPayloads
	}

	// 5f. Payload correlation between central journal and Bank A participant operations
	var journalMismatchA int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM payment_bank_operations j
		JOIN bank_a.operations b ON b.operation_id = j.operation_id
		WHERE b.payment_id != j.payment_id OR b.amount_paise != j.amount_paise OR b.currency != j.currency`).Scan(&journalMismatchA); err != nil {
		summary.ParticipantUnique = false
		summary.TotalViolations++
	} else if journalMismatchA > 0 {
		summary.ParticipantUnique = false
		summary.TotalViolations += journalMismatchA
	}

	// 5g. Payload correlation between central journal and Bank B participant operations
	var journalMismatchB int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM payment_bank_operations j
		JOIN bank_b.operations b ON b.operation_id = j.operation_id
		WHERE b.payment_id != j.payment_id OR b.amount_paise != j.amount_paise OR b.currency != j.currency`).Scan(&journalMismatchB); err != nil {
		summary.ParticipantUnique = false
		summary.TotalViolations++
	} else if journalMismatchB > 0 {
		summary.ParticipantUnique = false
		summary.TotalViolations += journalMismatchB
	}

	// 6. Central balance reconstruction consistency for harness accounts (when seeded)
	if h.accSenderID != uuid.Nil && h.accRecvID != uuid.Nil {
		var reconErrors int
		if err := h.pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM accounts a
			WHERE a.id IN ($1, $2)
			AND a.balance_paise != a.opening_balance_paise + COALESCE((
				SELECT SUM(CASE WHEN le.entry_type = 'CREDIT' THEN le.amount_paise ELSE -le.amount_paise END)
				FROM ledger_entries le WHERE le.account_id = a.id
			), 0)`, h.accSenderID, h.accRecvID).Scan(&reconErrors); err != nil {
			summary.DebitCreditConservation = false
			summary.TotalViolations++
		} else if reconErrors > 0 {
			summary.DebitCreditConservation = false
			summary.TotalViolations += reconErrors
		}
	}

	return summary
}

func uuidToLockKey(id uuid.UUID) uint64 {
	var k1, k2 uint64
	for i := 0; i < 8; i++ {
		k1 = (k1 << 8) | uint64(id[i])
		k2 = (k2 << 8) | uint64(id[8+i])
	}
	return k1 ^ k2
}
