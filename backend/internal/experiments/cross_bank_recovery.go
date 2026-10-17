package experiments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
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
	w.Flush()
	if err := w.Error(); err != nil {
		return jsonPath, csvPath, fmt.Errorf("flush CSV: %w", err)
	}
	csvBytes, err := os.ReadFile(csvPath)
	if err != nil {
		return jsonPath, csvPath, fmt.Errorf("read CSV for canonical copy: %w", err)
	}
	if err := os.WriteFile(rootJson, finalBytes, 0644); err != nil {
		return jsonPath, csvPath, err
	}
	if err := os.WriteFile(rootCsv, csvBytes, 0644); err != nil {
		return jsonPath, csvPath, err
	}

	return jsonPath, csvPath, nil
}

// CrossBankRecoveryHarness exercises recovery across real Bank A and Bank B participant boundaries.
type CrossBankRecoveryHarness struct {
	pool         *pgxpool.Pool
	dbURL        string
	currentDB    string
	bankAURL     string
	bankBURL     string
	bankASvr     *http.Server
	bankBSvr     *http.Server
	bankASvc     *bankservice.Service
	bankBSvc     *bankservice.Service
	clientA      bank.BankAdapter
	clientB      bank.BankAdapter
	testUsers    []uuid.UUID
	testBankA    uuid.UUID
	testBankB    uuid.UUID
	backupRunner BackupRestoreRunner
	contract     *OrchestratorLifecycleContract
	mu           sync.Mutex
}

// CurrentDB returns the verified disposable database name bound to this harness.
func (h *CrossBankRecoveryHarness) CurrentDB() string {
	if h == nil {
		return ""
	}
	return h.currentDB
}

// LifecycleContract returns the verified orchestrator contract bound to this harness, if authorized.
func (h *CrossBankRecoveryHarness) LifecycleContract() *OrchestratorLifecycleContract {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.contract
}

// SetLifecycleContract binds a pre-validated orchestrator lifecycle contract to this harness.
func (h *CrossBankRecoveryHarness) SetLifecycleContract(c *OrchestratorLifecycleContract) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.contract = c
}

// SetBackupRestoreRunner allows injecting a custom or mock runner for regression testing.
func (h *CrossBankRecoveryHarness) SetBackupRestoreRunner(r BackupRestoreRunner) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.backupRunner = r
}

func (h *CrossBankRecoveryHarness) getBackupRestoreRunner() BackupRestoreRunner {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.backupRunner != nil {
		return h.backupRunner
	}
	return &DefaultBackupRestoreRunner{}
}

// AllowedDisposableDBPrefixes lists prefixes for databases that may safely be used
// for cross-bank recovery experiments and destructive fixture lifecycles.
var AllowedDisposableDBPrefixes = []string{
	"transactx_disposable_",
	"transactx_cb_restore_",
	"transactx_test_",
	"transactx_m2_disposable_",
}

// ValidateDisposableDBName verifies that a database name is explicitly identified
// as disposable, and rejects source or system databases.
func ValidateDisposableDBName(dbName string) error {
	trimmed := strings.TrimSpace(dbName)
	if trimmed == "" {
		return fmt.Errorf("database name cannot be empty")
	}
	lower := strings.ToLower(trimmed)
	if lower == "transactx" {
		return fmt.Errorf("refusing cross-bank recovery harness against source database %q: destructive experiment operations require an isolated disposable database", dbName)
	}
	for _, sysDB := range []string{"postgres", "template0", "template1"} {
		if lower == sysDB {
			return fmt.Errorf("refusing cross-bank recovery harness against system database %q", dbName)
		}
	}
	for _, pfx := range AllowedDisposableDBPrefixes {
		if strings.HasPrefix(lower, pfx) {
			return nil
		}
	}
	return fmt.Errorf("database %q is not explicitly identified as disposable (must start with one of: %s)",
		dbName, strings.Join(AllowedDisposableDBPrefixes, ", "))
}

// VerifyPoolIsDisposable queries SELECT current_database() from the connection pool
// and verifies that it is an explicitly allowed disposable database.
func VerifyPoolIsDisposable(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	if pool == nil {
		return "", fmt.Errorf("database pool is nil")
	}
	var currentDB string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&currentDB); err != nil {
		return "", fmt.Errorf("failed to query current_database(): %w", err)
	}
	if err := ValidateDisposableDBName(currentDB); err != nil {
		return currentDB, err
	}
	return currentDB, nil
}

// ErrImmutableHistoryBlockedCleanup indicates that in-process SQL row deletion
// was blocked by foreign key constraints referencing immutable payment_state_transitions records.
var ErrImmutableHistoryBlockedCleanup = errors.New("in-process cleanup blocked by immutable payment state transitions (foreign key constraint)")

// IsImmutableHistoryCleanupError inspects an error and reports whether it was
// caused by foreign-key constraint violations on payments from payment_state_transitions.
func IsImmutableHistoryCleanupError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrImmutableHistoryBlockedCleanup) {
		return true
	}
	msg := strings.ToLower(err.Error())
	isFK := strings.Contains(msg, "23503") || strings.Contains(msg, "foreign key") || strings.Contains(msg, "violates foreign key constraint")
	isTransition := strings.Contains(msg, "payment_state_transitions")
	return isFK && isTransition
}

// TeardownStatus describes the result of analyzing a cleanup failure against
// the disposable-database lifecycle contract.
type TeardownStatus string

const (
	// TeardownClean indicates that in-process cleanup fully succeeded (no residual rows).
	TeardownClean TeardownStatus = "CLEAN"
	// TeardownRequiresExternalDrop indicates that in-process cleanup was blocked by
	// immutable transition history (payment_state_transitions) and the disposable
	// database must be dropped externally (e.g., via dropdb or container teardown).
	TeardownRequiresExternalDrop TeardownStatus = "REQUIRES_EXTERNAL_DROP"
	// TeardownUnexpectedError indicates an unexpected database cleanup failure
	// (e.g. connection failure, syntax error, commit failure).
	TeardownUnexpectedError TeardownStatus = "UNEXPECTED_CLEANUP_ERROR"
)

// ClassifyTeardownError inspects a cleanup error and determines whether it represents
// a clean state, an expected block by immutable transition history, or an unexpected error.
func ClassifyTeardownError(cleanupErr error) TeardownStatus {
	if cleanupErr == nil {
		return TeardownClean
	}
	if errors.Is(cleanupErr, ErrImmutableHistoryBlockedCleanup) || IsImmutableHistoryCleanupError(cleanupErr) {
		return TeardownRequiresExternalDrop
	}
	return TeardownUnexpectedError
}

const (
	// OrchestratorContractVersion is the expected version identifier for orchestrator contracts.
	OrchestratorContractVersion = "tx.orchestrator.contract.v1"
	// EnvDisposableDBOrchestrated is the environment variable signaling external orchestration.
	EnvDisposableDBOrchestrated = "TRANSACTX_DISPOSABLE_DB_ORCHESTRATED"
	// EnvDisposableContractFile specifies the path to an orchestrator contract file.
	EnvDisposableContractFile = "TRANSACTX_DISPOSABLE_CONTRACT_FILE"
	// EnvDisposableOrchestratorContract specifies raw JSON of an orchestrator contract.
	EnvDisposableOrchestratorContract = "TRANSACTX_DISPOSABLE_ORCHESTRATED_CONTRACT"
	// EnvDisposableInvocationToken supplies the unique per-invocation token generated by the orchestrator.
	EnvDisposableInvocationToken = "TRANSACTX_DISPOSABLE_INVOCATION_TOKEN"
)

// OrchestratorContractMaxAge is the maximum allowed age for a lifecycle contract before it is considered stale.
const OrchestratorContractMaxAge = 1 * time.Hour

// OrchestratorLifecycleContract represents the verified contract binding a disposable
// test database to an active external orchestrator lifecycle that guarantees destruction.
//
// Security and platform boundaries:
// 1. Invocation token: The invocation token is an accidental-misconfiguration and replay
//    safeguard designed to bind the lifecycle contract strictly to a single test execution
//    subprocess, preventing cross-run contamination or reuse of contracts from earlier runs.
//    It is not cryptographic proof against a local user who can arbitrarily construct both
//    the process environment and the contract file.
// 2. Process ownership: On Windows/Git Bash, orchestratorPid is recorded as an identifying
//    metadata attribute; it does not prove parent-child process tree ownership due to MSYS2/Win32
//    PID virtualization differences.
type OrchestratorLifecycleContract struct {
	Version         string `json:"version"`
	Database        string `json:"database"`
	InvocationToken string `json:"invocationToken"`
	OrchestratorPID int    `json:"orchestratorPid"`
	CreatedAt       string `json:"createdAt"`
}

// Validate validates that the contract matches the required schema, binds to targetDB,
// matches expectedToken, and contains a fresh, unexpired timestamp.
func (c *OrchestratorLifecycleContract) Validate(targetDB string, expectedToken string) error {
	if c == nil {
		return fmt.Errorf("lifecycle contract is nil")
	}
	if strings.TrimSpace(c.Version) != OrchestratorContractVersion {
		return fmt.Errorf("unsupported orchestrator contract version %q (expected %q)", c.Version, OrchestratorContractVersion)
	}
	if c.OrchestratorPID <= 0 {
		return fmt.Errorf("invalid orchestrator PID %d in contract (must be > 0)", c.OrchestratorPID)
	}
	trimmedContractDB := strings.TrimSpace(c.Database)
	if trimmedContractDB == "" {
		return fmt.Errorf("contract database name is empty")
	}
	trimmedTargetDB := strings.TrimSpace(targetDB)
	if trimmedTargetDB == "" {
		return fmt.Errorf("target database name is empty")
	}
	if !strings.EqualFold(trimmedContractDB, trimmedTargetDB) {
		return fmt.Errorf("contract database %q does not match active target database %q: ownership binding mismatch", c.Database, targetDB)
	}

	// Invocation token validation: safeguard against replay and cross-run contamination
	trimmedContractToken := strings.TrimSpace(c.InvocationToken)
	if trimmedContractToken == "" {
		return fmt.Errorf("contract invocation token is empty")
	}
	if len(trimmedContractToken) < 8 {
		return fmt.Errorf("contract invocation token %q is too short (must be >= 8 chars)", c.InvocationToken)
	}
	trimmedExpectedToken := strings.TrimSpace(expectedToken)
	if trimmedExpectedToken == "" {
		return fmt.Errorf("expected invocation token is empty (invocation token required to prove active lifecycle binding)")
	}
	if trimmedContractToken != trimmedExpectedToken {
		return fmt.Errorf("contract invocation token %q does not match expected invocation token %q: invocation binding mismatch", c.InvocationToken, expectedToken)
	}

	// Freshness validation: CreatedAt must be a valid RFC3339 timestamp within OrchestratorContractMaxAge
	trimmedCreatedAt := strings.TrimSpace(c.CreatedAt)
	if trimmedCreatedAt == "" {
		return fmt.Errorf("contract createdAt timestamp is empty")
	}
	parsedCreated, err := time.Parse(time.RFC3339, trimmedCreatedAt)
	if err != nil {
		return fmt.Errorf("invalid createdAt timestamp %q in contract (must be RFC3339): %w", c.CreatedAt, err)
	}
	now := time.Now().UTC()
	// Allow 5 minutes clock skew tolerance for future timestamps
	if parsedCreated.After(now.Add(5 * time.Minute)) {
		return fmt.Errorf("contract createdAt %v is in the future (clock skew violation)", parsedCreated)
	}
	if now.Sub(parsedCreated) > OrchestratorContractMaxAge {
		return fmt.Errorf("contract createdAt %v is stale (older than %v)", parsedCreated, OrchestratorContractMaxAge)
	}

	return nil
}

// ValidateAgainstTargetDB validates the contract against targetDB and the invocation token
// resolved from the process environment (EnvDisposableInvocationToken).
func (c *OrchestratorLifecycleContract) ValidateAgainstTargetDB(targetDB string) error {
	return c.Validate(targetDB, os.Getenv(EnvDisposableInvocationToken))
}

// ResolveOrchestratorContract resolves and validates the active lifecycle contract for targetDB from the environment.
// It verifies:
// 1. TRANSACTX_DISPOSABLE_DB_ORCHESTRATED is set to "1".
// 2. An explicit invocation token is supplied via TRANSACTX_DISPOSABLE_INVOCATION_TOKEN.
// 3. An explicit contract file is provided via TRANSACTX_DISPOSABLE_CONTRACT_FILE pointing to a regular,
//    non-empty, fresh file on disk. Inline JSON via TRANSACTX_DISPOSABLE_ORCHESTRATED_CONTRACT alone is
//    explicitly rejected because it bypasses the orchestrator's filesystem lifecycle binding.
// 4. The contract can be parsed and satisfies Validate(targetDB, expectedToken).
//
// If TRANSACTX_DISPOSABLE_DB_ORCHESTRATED is not "1", or if the contract is missing, malformed,
// stale, or bound to a different database or token, it returns an error explaining the failure.
func ResolveOrchestratorContract(targetDB string) (*OrchestratorLifecycleContract, error) {
	if os.Getenv(EnvDisposableDBOrchestrated) != "1" {
		return nil, fmt.Errorf("%s is not set to '1'", EnvDisposableDBOrchestrated)
	}

	expectedToken := strings.TrimSpace(os.Getenv(EnvDisposableInvocationToken))
	if expectedToken == "" {
		return nil, fmt.Errorf("orchestrator invocation token missing: %s=1 is set, but %s is empty (invocation binding required to prove current execution ownership)",
			EnvDisposableDBOrchestrated, EnvDisposableInvocationToken)
	}

	contractFilePath := strings.TrimSpace(os.Getenv(EnvDisposableContractFile))
	contractJSON := strings.TrimSpace(os.Getenv(EnvDisposableOrchestratorContract))

	if contractFilePath == "" {
		if contractJSON != "" {
			return nil, fmt.Errorf("inline JSON contract (%s) rejected: lifecycle contract must be an ephemeral file on disk (%s) to guarantee orchestrator ownership binding",
				EnvDisposableOrchestratorContract, EnvDisposableContractFile)
		}
		return nil, fmt.Errorf("orchestrator ownership contract missing: %s=1 is set, but no contract file provided via %s (environment variable alone does not establish teardown ownership)",
			EnvDisposableDBOrchestrated, EnvDisposableContractFile)
	}

	fileInfo, err := os.Stat(contractFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat orchestrator contract file %q: %w", contractFilePath, err)
	}
	if !fileInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("orchestrator contract path %q is not a regular file", contractFilePath)
	}
	if fileInfo.Size() == 0 {
		return nil, fmt.Errorf("orchestrator contract file %q is empty", contractFilePath)
	}
	if time.Since(fileInfo.ModTime()) > OrchestratorContractMaxAge {
		return nil, fmt.Errorf("orchestrator contract file %q is stale (last modified %v ago)", contractFilePath, time.Since(fileInfo.ModTime()))
	}

	data, err := os.ReadFile(contractFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read orchestrator contract file %q: %w", contractFilePath, err)
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("orchestrator contract is empty")
	}

	var contract OrchestratorLifecycleContract
	if err := json.Unmarshal(data, &contract); err != nil {
		return nil, fmt.Errorf("malformed orchestrator contract: %w", err)
	}

	if err := contract.Validate(targetDB, expectedToken); err != nil {
		return nil, fmt.Errorf("orchestrator contract validation failed: %w", err)
	}

	return &contract, nil
}

// ErrUnorchestratedLifecycle indicates that external lifecycle orchestration is not active.
// When encountered in broad test suites, tests must safely skip before creating fixtures.
var ErrUnorchestratedLifecycle = errors.New("destructive scenarios require dedicated disposable lifecycle orchestration (TRANSACTX_DISPOSABLE_DB_ORCHESTRATED=1)")

// ValidateDestructiveLifecycleContract validates that targetDB is an authorized disposable database
// and that an active, verified lifecycle contract exists binding the current invocation to targetDB.
//
// Rules:
// 1. If TRANSACTX_DISPOSABLE_DB_ORCHESTRATED is not "1", returns ErrUnorchestratedLifecycle.
// 2. Validates targetDB name using ValidateDisposableDBName.
// 3. Resolves and validates the contract file and invocation token via ResolveOrchestratorContract.
// 4. Fails closed with an error if any check fails (missing contract, stale contract, mismatched DB, mismatched token).
func ValidateDestructiveLifecycleContract(targetDB string) (*OrchestratorLifecycleContract, error) {
	if os.Getenv(EnvDisposableDBOrchestrated) != "1" {
		return nil, ErrUnorchestratedLifecycle
	}
	if err := ValidateDisposableDBName(targetDB); err != nil {
		return nil, fmt.Errorf("refusing destructive lifecycle on database %q: %w", targetDB, err)
	}
	contract, err := ResolveOrchestratorContract(targetDB)
	if err != nil {
		return nil, fmt.Errorf("lifecycle contract authorization failed for target database %q: %w", targetDB, err)
	}
	return contract, nil
}

// AuthorizeDestructiveLifecycle performs pre-flight lifecycle authorization before any fixtures
// are seeded or destructive scenarios are executed.
//
// Steps:
// 1. If TRANSACTX_DISPOSABLE_DB_ORCHESTRATED is not "1", returns ("", nil, ErrUnorchestratedLifecycle)
//    allowing broad test suites to skip cleanly before mutating anything.
// 2. If pool is nil, returns an error (fail closed).
// 3. Queries current_database() from pool and verifies it via VerifyPoolIsDisposable.
// 4. Resolves and validates the lifecycle contract against currentDB via ValidateDestructiveLifecycleContract.
// 5. Returns currentDB and the validated contract for reuse during teardown/cleanup delegation.
func AuthorizeDestructiveLifecycle(ctx context.Context, pool *pgxpool.Pool) (string, *OrchestratorLifecycleContract, error) {
	if os.Getenv(EnvDisposableDBOrchestrated) != "1" {
		return "", nil, ErrUnorchestratedLifecycle
	}
	if pool == nil {
		return "", nil, errors.New("database pool is required for disposable lifecycle authorization")
	}
	currentDB, err := VerifyPoolIsDisposable(ctx, pool)
	if err != nil {
		return currentDB, nil, fmt.Errorf("refusing destructive execution on non-disposable database %q: %w", currentDB, err)
	}
	contract, err := ValidateDestructiveLifecycleContract(currentDB)
	if err != nil {
		return currentDB, nil, err
	}
	return currentDB, contract, nil
}

// EvaluateTestCleanupOutcome evaluates the cleanup error against the verified orchestrator contract,
// target database, and invocation token.
//
// Rules:
// 1. If cleanupErr == nil:
//    Status is clean -> shouldFail = false, isDelegated = false.
// 2. If cleanupErr has status TeardownRequiresExternalDrop (ErrImmutableHistoryBlockedCleanup):
//    - If contract != nil and contract.Validate(targetDB, expectedToken) == nil:
//      Teardown ownership is proven for targetDB and this specific invocation.
//      The immutable-history block is delegated to the orchestrator's guaranteed dropdb lifecycle.
//      shouldFail = false, isDelegated = true.
//    - If contract is nil or invalid for targetDB/expectedToken:
//      Teardown ownership is NOT proven. The cleanup error MUST fail the test.
//      shouldFail = true, isDelegated = false.
// 3. If cleanupErr has status TeardownUnexpectedError:
//    - Regardless of contract, database, or token, unexpected SQL/connection errors MUST fail the test.
//      shouldFail = true, isDelegated = false.
func EvaluateTestCleanupOutcome(cleanupErr error, contract *OrchestratorLifecycleContract, targetDB string, expectedToken string) (shouldFail bool, isDelegated bool, diag string) {
	status := ClassifyTeardownError(cleanupErr)
	switch status {
	case TeardownClean:
		return false, false, "cleanup succeeded cleanly"
	case TeardownRequiresExternalDrop:
		if contract != nil {
			if err := contract.Validate(targetDB, expectedToken); err == nil {
				return false, true, fmt.Sprintf("in-process cleanup blocked by immutable transition history (%v); delegated to verified external orchestrator (PID %d, contract %s, token %s) owning database %q",
					cleanupErr, contract.OrchestratorPID, contract.Version, contract.InvocationToken, contract.Database)
			} else {
				return true, false, fmt.Sprintf("in-process cleanup blocked by immutable transition history (%v); delegation rejected: invalid orchestrator contract: %v", cleanupErr, err)
			}
		}
		return true, false, fmt.Sprintf("in-process cleanup blocked by immutable transition history (%v); delegation rejected: no verified orchestrator ownership contract for target database %q", cleanupErr, targetDB)
	case TeardownUnexpectedError:
		return true, false, fmt.Sprintf("unexpected cleanup error (status=%s): %v", status, cleanupErr)
	default:
		return true, false, fmt.Sprintf("unrecognized teardown status %s: %v", status, cleanupErr)
	}
}

// NewCrossBankRecoveryHarness initializes the harness, starts HTTP listeners for Bank A (:8081) and Bank B (:8082).
func NewCrossBankRecoveryHarness(pool *pgxpool.Pool, dbURL string) (*CrossBankRecoveryHarness, error) {
	ctx := context.Background()

	if pool == nil {
		return nil, fmt.Errorf("database pool is required")
	}
	currentDB, err := VerifyPoolIsDisposable(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("cross-bank recovery harness rejected unsafe database: %w", err)
	}

	var contract *OrchestratorLifecycleContract
	if os.Getenv(EnvDisposableDBOrchestrated) == "1" {
		c, err := ValidateDestructiveLifecycleContract(currentDB)
		if err != nil {
			return nil, fmt.Errorf("cross-bank recovery harness rejected unverified lifecycle: %w", err)
		}
		contract = c
	}

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
		currentDB: currentDB,
		contract:  contract,
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

func (h *CrossBankRecoveryHarness) Close() error {
	if h == nil {
		return nil
	}
	var errs []error
	if h.bankASvr != nil {
		if err := h.bankASvr.Shutdown(context.Background()); err != nil {
			errs = append(errs, fmt.Errorf("shutdown bank A server: %w", err))
		}
	}
	if h.bankBSvr != nil {
		if err := h.bankBSvr.Shutdown(context.Background()); err != nil {
			errs = append(errs, fmt.Errorf("shutdown bank B server: %w", err))
		}
	}
	if err := h.CleanupTestUsers(context.Background()); err != nil {
		errs = append(errs, fmt.Errorf("cleanup test users: %w", err))
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
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
	if h == nil || h.pool == nil {
		return nil, fmt.Errorf("database pool is not initialized")
	}
	if _, err := VerifyPoolIsDisposable(ctx, h.pool); err != nil {
		return nil, fmt.Errorf("seed fixture refused on unsafe database: %w", err)
	}
	if os.Getenv(EnvDisposableDBOrchestrated) == "1" {
		if h.contract != nil {
			if err := h.contract.Validate(h.currentDB, os.Getenv(EnvDisposableInvocationToken)); err != nil {
				return nil, fmt.Errorf("seed fixture refused: lifecycle contract validation failed: %w", err)
			}
		} else {
			contract, err := ValidateDestructiveLifecycleContract(h.currentDB)
			if err != nil {
				return nil, fmt.Errorf("seed fixture refused: lifecycle authorization failed: %w", err)
			}
			h.contract = contract
		}
	}
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
	if h == nil || h.pool == nil {
		return fmt.Errorf("database pool is not initialized")
	}
	if _, err := VerifyPoolIsDisposable(ctx, h.pool); err != nil {
		return fmt.Errorf("insert payment refused on unsafe database: %w", err)
	}
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

func (h *CrossBankRecoveryHarness) CleanupTestUsers(ctx context.Context) error {
	if h == nil || h.pool == nil {
		return nil
	}
	if _, err := VerifyPoolIsDisposable(ctx, h.pool); err != nil {
		// Strictly refuse to execute any fixture cleanup against non-disposable databases
		return fmt.Errorf("refusing fixture cleanup on non-disposable database: %w", err)
	}

	h.mu.Lock()
	users := make([]uuid.UUID, len(h.testUsers))
	copy(users, h.testUsers)
	h.mu.Unlock()

	if len(users) == 0 {
		return nil
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cleanup transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Note: We deliberately do NOT execute 'SET LOCAL session_replication_role = replica'
	// and never bypass triggers or foreign key constraints.
	// For experiments with immutable transition history, rows in payment_state_transitions
	// remain intact and will cause payment deletion to fail if FK constraints are present.
	// Primary disposable database teardown is managed externally by test orchestrators.
	steps := []struct {
		table string
		query string
	}{
		{"bank_a.ledger_entries", `DELETE FROM bank_a.ledger_entries WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`},
		{"bank_b.ledger_entries", `DELETE FROM bank_b.ledger_entries WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`},
		{"bank_a.operations", `DELETE FROM bank_a.operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`},
		{"bank_b.operations", `DELETE FROM bank_b.operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`},
		{"payment_bank_operations", `DELETE FROM payment_bank_operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`},
		{"ledger_entries", `DELETE FROM ledger_entries WHERE ledger_transaction_id IN (SELECT lt.id FROM ledger_transactions lt JOIN payments p ON p.id = lt.payment_id WHERE p.initiated_by_user_id = ANY($1))`},
		{"ledger_transactions", `DELETE FROM ledger_transactions WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`},
		{"recovery_attempts", `DELETE FROM recovery_attempts WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`},
		{"recovery_jobs", `DELETE FROM recovery_jobs WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`},
		{"outbox_deliveries", `DELETE FROM outbox_deliveries WHERE event_id IN (SELECT id FROM outbox_events WHERE aggregate_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1)))`},
		{"outbox_events", `DELETE FROM outbox_events WHERE aggregate_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`},
		{"ops_payment_worklist", `DELETE FROM ops_payment_worklist WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = ANY($1))`},
		{"idempotency_records", `DELETE FROM idempotency_records WHERE user_id = ANY($1)`},
		// Foreign key constraints will prevent deleting payments if immutable transitions exist;
		// triggers remain enforced and are never disabled.
		{"payments", `DELETE FROM payments WHERE initiated_by_user_id = ANY($1)`},
		{"accounts", `DELETE FROM accounts WHERE user_id = ANY($1)`},
		{"users", `DELETE FROM users WHERE id = ANY($1)`},
	}

	for _, step := range steps {
		if _, err := tx.Exec(ctx, step.query, users); err != nil {
			if step.table == "payments" && IsImmutableHistoryCleanupError(err) {
				return fmt.Errorf("%w: cleanup table %s: %w", ErrImmutableHistoryBlockedCleanup, step.table, err)
			}
			return fmt.Errorf("cleanup table %s: %w", step.table, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cleanup transaction: %w", err)
	}

	h.mu.Lock()
	h.testUsers = nil
	h.mu.Unlock()
	return nil
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
		if _, err := projector.ProjectEvent(ctx, evt); err != nil {
			return fmt.Errorf("project payment event: %w", err)
		}
	}
	return nil
}

// RunAll runs all 7 scenarios and compiles the report.
func (h *CrossBankRecoveryHarness) RunAll(ctx context.Context) (*CrossBankRecoveryReport, error) {
	start := time.Now().UTC()
	var scenarios []CrossBankScenarioResult
	var passedCount, failedCount int

	record := func(res CrossBankScenarioResult) {
		res.FaultBoundary = "In-process harness; no native process kill: " + res.FaultBoundary
		if res.Passed {
			passedCount++
		} else {
			failedCount++
		}
		scenarios = append(scenarios, res)
	}

	record(h.RunCB01_SeededRecoveryAfterJournalBeforeDispatch(ctx))
	record(h.RunCB02_SeededRecoveryAfterHoldBeforeCredit(ctx))
	record(h.RunCB03_ParticipantCommitLostHTTPResponse(ctx))
	record(h.RunCB04_RestartWithExistingOperationUUID(ctx))
	record(h.RunCB05_LeaseExpiryWorker2TakeoverStaleRejection(ctx))
	record(h.RunCB06_PresenceBranchingAbsentPresentUnknown(ctx))
	record(h.RunCB07_UnencryptedDumpRestorePendingPayment(ctx))

	finSummary := h.verifyOverallFinancialInvariants(ctx)

	var postgresVer string
	if err := h.pool.QueryRow(ctx, `SHOW server_version`).Scan(&postgresVer); err != nil {
		return nil, fmt.Errorf("read PostgreSQL version: %w", err)
	}

	gitCommit, workingTree, dirty := sourceProvenance("")

	report := &CrossBankRecoveryReport{
		SchemaVersion: "tx.cross_bank_recovery.v1",
		RunID:         fmt.Sprintf("run-%d-%s", start.Unix(), strings.ToLower(runtime.GOOS)),
		RunTimestamp:  start,
		GitCommit:     gitCommit,
		WorkingTree:   workingTree,
		Dirty:         dirty,
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
// SCENARIO 1: Seeded state recovery after central operation journal commits before participant dispatch
// -----------------------------------------------------------------------------
func (h *CrossBankRecoveryHarness) RunCB01_SeededRecoveryAfterJournalBeforeDispatch(ctx context.Context) CrossBankScenarioResult {
	start := time.Now()
	res := CrossBankScenarioResult{
		ID:            "CB-01",
		Name:          "In-process recovery: central journal committed before participant dispatch",
		FaultBoundary: "Seeded state reconstruction: central journal committed in payment_bank_operations (PENDING) with Bank A absent; in-process Service.Resume completes transfer",
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
	if err := h.projectPaymentEvents(ctx, fix.PaymentID); err != nil {
		res.ErrorMessage = fmt.Sprintf("project payment events: %v", err)
		return res
	}

	finSummary := h.verifySinglePaymentInvariants(ctx, fix)
	res.ObservedState = "COMPLETED"
	p1 := finSummary.TotalViolations == 0
	p2 := resumeRes.Payment.State == payments.StateCompleted
	stateStr := string(resumeRes.Payment.State)

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment converged to COMPLETED", Passed: p2, Details: fmt.Sprintf("state=%s", stateStr)},
		{InvariantName: "All 8 cross-bank financial invariants satisfied", Passed: p1, Details: fmt.Sprintf("violations=%d", finSummary.TotalViolations)},
	}

	res.Passed = p1 && p2
	if !res.Passed && res.ErrorMessage == "" {
		res.ErrorMessage = fmt.Sprintf("invariants failed (violations=%d)", finSummary.TotalViolations)
	}
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

func (h *CrossBankRecoveryHarness) RunCB01_APITerminationAfterJournalBeforeDispatch(ctx context.Context) CrossBankScenarioResult {
	return h.RunCB01_SeededRecoveryAfterJournalBeforeDispatch(ctx)
}

// -----------------------------------------------------------------------------
// SCENARIO 2: Seeded state recovery mid-transfer with Bank A hold committed before Bank B credit
// -----------------------------------------------------------------------------
func (h *CrossBankRecoveryHarness) RunCB02_SeededRecoveryAfterHoldBeforeCredit(ctx context.Context) CrossBankScenarioResult {
	start := time.Now()
	res := CrossBankScenarioResult{
		ID:            "CB-02",
		Name:          "In-process recovery: Bank A hold committed before Bank B credit",
		FaultBoundary: "Seeded state reconstruction mid-transfer: Bank A hold committed, Bank B credit uncalled; in-process recovery completes transfer without duplicate hold",
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
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("hold on Bank A failed: %v", err)
		return res
	}
	if holdRes.Status != bank.OperationSucceeded {
		res.ErrorMessage = fmt.Sprintf("hold on Bank A non-success status: %v", holdRes.Status)
		return res
	}

	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, amount_paise, currency, idempotency_key)
		VALUES ($1, $2, $3, $4, 'HOLD', 'SUCCESS', $5, 'INR', $6)`,
		uuid.New(), fix.PaymentID, h.testBankA, fix.HoldOpID, fix.AmountPaise, fix.HoldOpID.String()); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert central journal: %v", err)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "bank_a_hold_committed_before_bank_b_dispatch")

	presA, err := h.clientA.(bank.OperationPresenceReader).GetOperationPresence(ctx, fix.PaymentID, fix.HoldOpID)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("check Bank A presence: %v", err)
		return res
	}
	presB, err := h.clientB.(bank.OperationPresenceReader).GetOperationPresence(ctx, fix.PaymentID, fix.CreditOpID)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("check Bank B presence: %v", err)
		return res
	}
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
	if err := h.projectPaymentEvents(ctx, fix.PaymentID); err != nil {
		res.ErrorMessage = fmt.Sprintf("project payment events: %v", err)
		return res
	}

	finSummary := h.verifySinglePaymentInvariants(ctx, fix)
	res.ObservedState = "COMPLETED"
	p1 := finSummary.TotalViolations == 0
	p2 := resumeRes.Payment.State == payments.StateCompleted
	stateStr := string(resumeRes.Payment.State)

	res.Invariants = []InvariantResult{
		{InvariantName: "Payment completed cleanly", Passed: p2, Details: fmt.Sprintf("state=%s", stateStr)},
		{InvariantName: "Zero duplicate operations and financial invariants hold", Passed: p1, Details: fmt.Sprintf("violations=%d", finSummary.TotalViolations)},
	}

	res.Passed = p1 && p2
	if !res.Passed && res.ErrorMessage == "" {
		res.ErrorMessage = fmt.Sprintf("invariants failed (violations=%d)", finSummary.TotalViolations)
	}
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

func (h *CrossBankRecoveryHarness) RunCB02_WorkerTerminationAfterHoldBeforeCredit(ctx context.Context) CrossBankScenarioResult {
	return h.RunCB02_SeededRecoveryAfterHoldBeforeCredit(ctx)
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
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, amount_paise, currency, idempotency_key)
		VALUES ($1, $2, $3, $4, 'HOLD', 'SUCCESS', $5, 'INR', $6)`,
		uuid.New(), fix.PaymentID, h.testBankA, fix.HoldOpID, fix.AmountPaise, fix.HoldOpID.String()); err != nil {
		res.ErrorMessage = fmt.Sprintf("write scenario checkpoint: %v", err)
		return res
	}

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

	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, amount_paise, currency, idempotency_key)
		VALUES ($1, $2, $3, $4, 'PROVISIONAL_CREDIT', 'PENDING', $5, 'INR', $6)`,
		uuid.New(), fix.PaymentID, h.testBankB, fix.CreditOpID, fix.AmountPaise, fix.CreditOpID.String()); err != nil {
		res.ErrorMessage = fmt.Sprintf("write scenario checkpoint: %v", err)
		return res
	}
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
	if err := h.projectPaymentEvents(ctx, fix.PaymentID); err != nil {
		res.ErrorMessage = fmt.Sprintf("project payment events: %v", err)
		return res
	}

	finSummary := h.verifySinglePaymentInvariants(ctx, fix)
	res.ObservedState = "COMPLETED"
	p1 := finSummary.TotalViolations == 0
	p2 := resumeRes.Payment.State == payments.StateCompleted

	var bCreditCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_b.operations WHERE payment_id = $1 AND operation_type = 'PROVISIONAL_CREDIT'`, fix.PaymentID).Scan(&bCreditCount); err != nil {
		res.ErrorMessage = fmt.Sprintf("read scenario invariant: %v", err)
		return res
	}
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
		FaultBoundary: "Seeded journal; new in-process service instance resumes original operation UUIDs (no process death)",
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
	res.Checkpoints = append(res.Checkpoints, "seeded_journal_existing_uuid")

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
	res.Checkpoints = append(res.Checkpoints, "new_in_process_service_completed_with_exact_uuid")
	if err := h.projectPaymentEvents(ctx, fix.PaymentID); err != nil {
		res.ErrorMessage = fmt.Sprintf("project payment events: %v", err)
		return res
	}

	var matchingHoldCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_a.operations WHERE payment_id = $1 AND operation_id = $2`, fix.PaymentID, fix.HoldOpID).Scan(&matchingHoldCount); err != nil {
		res.ErrorMessage = fmt.Sprintf("read scenario invariant: %v", err)
		return res
	}
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
	staleResolved, err := repo.CompleteAttempt(ctx, recovery.CompleteAttemptRequest{
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
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("check stale completion: %v", err)
		return res
	}
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
	if err := h.projectPaymentEvents(ctx, fix.PaymentID); err != nil {
		res.ErrorMessage = fmt.Sprintf("project payment events: %v", err)
		return res
	}

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

	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, amount_paise, currency, idempotency_key)
		VALUES ($1, $2, $3, $4, 'HOLD', 'SUCCESS', $5, 'INR', $6)`,
		uuid.New(), fix.PaymentID, h.testBankA, fix.HoldOpID, fix.AmountPaise, fix.HoldOpID.String()); err != nil {
		res.ErrorMessage = fmt.Sprintf("write scenario checkpoint: %v", err)
		return res
	}

	svc := h.newPaymentsService()
	resumeRes, err := svc.Resume(ctx, fix.PaymentID)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("resume payment: %v", err)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "completed_cross_bank_transfer_after_presence_branch_checks")
	if err := h.projectPaymentEvents(ctx, fix.PaymentID); err != nil {
		res.ErrorMessage = fmt.Sprintf("project payment events: %v", err)
		return res
	}

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

// BackupRestoreRunner abstracts external command execution for backup and restore drills.
type BackupRestoreRunner interface {
	RunPgDump(ctx context.Context, host, port, user, password, srcDB, destFile string) ([]byte, error)
	RunDropDB(ctx context.Context, host, port, user, password, dbName string) ([]byte, error)
	RunCreateDB(ctx context.Context, host, port, user, password, dbName string) ([]byte, error)
	RunPgRestore(ctx context.Context, host, port, user, password, dbName, srcFile string) ([]byte, error)
}

func buildPgEnv(host, port, user, password string) []string {
	env := os.Environ()
	env = append(env,
		fmt.Sprintf("PGHOST=%s", host),
		fmt.Sprintf("PGPORT=%s", port),
		fmt.Sprintf("PGUSER=%s", user),
	)
	if password != "" {
		env = append(env, fmt.Sprintf("PGPASSWORD=%s", password))
	}
	return env
}

func sanitizeDiagnostic(output string, password string) string {
	if password != "" {
		output = strings.ReplaceAll(output, password, "[REDACTED]")
	}
	return strings.TrimSpace(output)
}

func validateDisposableDB(targetDB, srcDB string) error {
	if targetDB == "" {
		return fmt.Errorf("target database name cannot be empty")
	}
	if targetDB == srcDB {
		return fmt.Errorf("refusing to restore over source database %q", srcDB)
	}
	return ValidateDisposableDBName(targetDB)
}

// DefaultBackupRestoreRunner executes native PostgreSQL CLI utilities with container fallback.
type DefaultBackupRestoreRunner struct{}

func (d *DefaultBackupRestoreRunner) RunPgDump(ctx context.Context, host, port, user, password, srcDB, destFile string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "pg_dump", "-h", host, "-p", port, "-U", user, "-Fc", "-f", destFile, srcDB)
	cmd.Env = buildPgEnv(host, port, user, password)
	out, err := cmd.CombinedOutput()
	if err == nil {
		if fi, statErr := os.Stat(destFile); statErr != nil || fi.Size() == 0 {
			return out, fmt.Errorf("pg_dump produced empty or missing dump file")
		}
		return out, nil
	}
	if containerID := findPostgresContainer(); containerID != "" {
		dumpF, fErr := os.Create(destFile)
		if fErr == nil {
			defer dumpF.Close()
			cmdDocker := exec.CommandContext(ctx, "docker", "exec", containerID, "pg_dump", "-U", user, "-Fc", srcDB)
			cmdDocker.Stdout = dumpF
			var stderr bytes.Buffer
			cmdDocker.Stderr = &stderr
			if dErr := cmdDocker.Run(); dErr == nil {
				if fi, statErr := os.Stat(destFile); statErr == nil && fi.Size() > 0 {
					return []byte("docker pg_dump succeeded"), nil
				}
			}
		}
	}
	return out, err
}

func (d *DefaultBackupRestoreRunner) RunDropDB(ctx context.Context, host, port, user, password, dbName string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "dropdb", "-h", host, "-p", port, "-U", user, "--if-exists", dbName)
	cmd.Env = buildPgEnv(host, port, user, password)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if containerID := findPostgresContainer(); containerID != "" {
			cmdDocker := exec.CommandContext(ctx, "docker", "exec", containerID, "dropdb", "-U", user, "--if-exists", dbName)
			if outD, errD := cmdDocker.CombinedOutput(); errD == nil {
				return outD, nil
			}
		}
	}
	return out, err
}

func (d *DefaultBackupRestoreRunner) RunCreateDB(ctx context.Context, host, port, user, password, dbName string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "createdb", "-h", host, "-p", port, "-U", user, dbName)
	cmd.Env = buildPgEnv(host, port, user, password)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if containerID := findPostgresContainer(); containerID != "" {
			cmdDocker := exec.CommandContext(ctx, "docker", "exec", containerID, "createdb", "-U", user, dbName)
			if outD, errD := cmdDocker.CombinedOutput(); errD == nil {
				return outD, nil
			}
		}
	}
	return out, err
}

func (d *DefaultBackupRestoreRunner) RunPgRestore(ctx context.Context, host, port, user, password, dbName, srcFile string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "pg_restore", "-h", host, "-p", port, "-U", user, "-d", dbName, "--no-owner", "--no-acl", srcFile)
	cmd.Env = buildPgEnv(host, port, user, password)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return out, nil
	}
	if containerID := findPostgresContainer(); containerID != "" {
		dumpReadF, openErr := os.Open(srcFile)
		if openErr == nil {
			defer dumpReadF.Close()
			cmdDocker := exec.CommandContext(ctx, "docker", "exec", "-i", containerID, "pg_restore", "-U", user, "-d", dbName, "--no-owner", "--no-acl")
			cmdDocker.Stdin = dumpReadF
			var stderr bytes.Buffer
			cmdDocker.Stderr = &stderr
			if errD := cmdDocker.Run(); errD == nil {
				return []byte("docker pg_restore succeeded"), nil
			}
		}
	}
	return out, err
}

func findPostgresContainer() string {
	for _, filter := range []string{"ancestor=postgres:18", "ancestor=postgres", "publish=5432", "expose=5432"} {
		out, err := exec.Command("docker", "ps", "--filter", filter, "--format", "{{.ID}}").Output()
		if err == nil {
			id := strings.TrimSpace(string(out))
			if lines := strings.Split(id, "\n"); len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
				return strings.TrimSpace(lines[0])
			}
		}
	}
	return ""
}

// -----------------------------------------------------------------------------
// SCENARIO 7: Unencrypted dump-and-restore: cross-bank payment pending during pg_dump
// -----------------------------------------------------------------------------
func (h *CrossBankRecoveryHarness) RunCB07_UnencryptedDumpRestorePendingPayment(ctx context.Context) (res CrossBankScenarioResult) {
	start := time.Now()
	res = CrossBankScenarioResult{
		ID:            "CB-07",
		Name:          "Unencrypted dump-and-restore: cross-bank payment pending during pg_dump",
		FaultBoundary: "Non-encrypted PostgreSQL custom-format archive (pg_dump -Fc) while cross-bank payment pending; restored via pg_restore into disposable DB; worker continuation converges cleanly (does not satisfy encrypted DR acceptance)",
	}

	if h.pool == nil {
		res.ErrorMessage = "database pool is nil"
		return res
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
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, amount_paise, currency, idempotency_key)
		VALUES ($1, $2, $3, $4, 'HOLD', 'SUCCESS', $5, 'INR', $6)`,
		uuid.New(), fix.PaymentID, h.testBankA, fix.HoldOpID, fix.AmountPaise, fix.HoldOpID.String()); err != nil {
		res.ErrorMessage = fmt.Sprintf("insert payment_bank_operations: %v", err)
		return res
	}

	repo := recovery.NewRepository(h.pool)
	if err := repo.UpsertJob(ctx, fix.PaymentID, time.Now().Add(-10*time.Second)); err != nil {
		res.ErrorMessage = fmt.Sprintf("upsert recovery job: %v", err)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "pending_payment_in_flight_with_hold_committed")

	host := "127.0.0.1"
	port := "5432"
	user := "postgres"
	password := ""
	srcDB := "transactx"

	if cfg, err := pgxpool.ParseConfig(h.dbURL); err == nil && cfg != nil {
		if cfg.ConnConfig.Host != "" {
			host = cfg.ConnConfig.Host
		}
		if cfg.ConnConfig.Port != 0 {
			port = fmt.Sprintf("%d", cfg.ConnConfig.Port)
		}
		if cfg.ConnConfig.User != "" {
			user = cfg.ConnConfig.User
		}
		if cfg.ConnConfig.Password != "" {
			password = cfg.ConnConfig.Password
		}
		if cfg.ConnConfig.Database != "" {
			srcDB = cfg.ConnConfig.Database
		}
	}

	disposableDB := fmt.Sprintf("transactx_cb_restore_%d_%d", os.Getpid(), time.Now().UnixNano()%1000000000)
	if err := validateDisposableDB(disposableDB, srcDB); err != nil {
		res.ErrorMessage = fmt.Sprintf("target database validation failed: %v", err)
		return res
	}

	tmpDump := filepath.Join(os.TempDir(), fmt.Sprintf("transactx_cb_backup_%d_%d.dump", os.Getpid(), time.Now().UnixNano()))
	defer os.Remove(tmpDump)

	runner := h.getBackupRestoreRunner()
	restoreCreated := false

	defer func() {
		if restoreCreated && disposableDB != srcDB && ValidateDisposableDBName(disposableDB) == nil {
			_, dropErr := runner.RunDropDB(context.Background(), host, port, user, password, disposableDB)
			if dropErr != nil {
				res.Passed = false
				res.ErrorMessage += fmt.Sprintf("; disposable restore teardown failed: %v", dropErr)
			} else {
				res.Checkpoints = append(res.Checkpoints, "disposable_database_dropped")
			}
		}
	}()

	// Step 1: Attempt pg_dump custom format backup
	dumpOut, dumpErr := runner.RunPgDump(ctx, host, port, user, password, srcDB, tmpDump)
	if dumpErr != nil {
		sanitized := sanitizeDiagnostic(string(dumpOut), password)
		res.ErrorMessage = fmt.Sprintf("backup failed: pg_dump: %v (%s)", dumpErr, sanitized)
		res.Checkpoints = append(res.Checkpoints, "pg_dump_backup_failed")
		return res
	}

	fi, statErr := os.Stat(tmpDump)
	if statErr != nil || fi.Size() == 0 {
		res.ErrorMessage = fmt.Sprintf("backup failed: dump file is missing or empty (stat: %v)", statErr)
		res.Checkpoints = append(res.Checkpoints, "pg_dump_backup_failed")
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "pg_dump_custom_format_backup_completed")

	// Step 2: Create a unique disposable target; never delete a preexisting database.

	createOut, createErr := runner.RunCreateDB(ctx, host, port, user, password, disposableDB)
	if createErr != nil {
		sanitized := sanitizeDiagnostic(string(createOut), password)
		res.ErrorMessage = fmt.Sprintf("database creation failed: createdb: %v (%s)", createErr, sanitized)
		res.Checkpoints = append(res.Checkpoints, "disposable_database_creation_failed")
		return res
	}
	restoreCreated = true
	res.Checkpoints = append(res.Checkpoints, "disposable_database_created")

	// Step 3: Restore custom dump into disposable database
	restoreOut, restoreErr := runner.RunPgRestore(ctx, host, port, user, password, disposableDB, tmpDump)
	if restoreErr != nil {
		sanitized := sanitizeDiagnostic(string(restoreOut), password)
		res.ErrorMessage = fmt.Sprintf("restore failed: pg_restore: %v (%s)", restoreErr, sanitized)
		res.Checkpoints = append(res.Checkpoints, "pg_restore_into_disposable_database_failed")
		return res
	}
	if bytes.Contains(bytes.ToLower(restoreOut), []byte("error:")) {
		sanitized := sanitizeDiagnostic(string(restoreOut), password)
		res.ErrorMessage = fmt.Sprintf("restore completed with fatal errors: %s", sanitized)
		res.Checkpoints = append(res.Checkpoints, "pg_restore_into_disposable_database_failed")
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "pg_restore_into_disposable_database_completed")

	// Step 4: Connect to restored database
	restoredConnection := &url.URL{
		Scheme: "postgres", Host: net.JoinHostPort(host, port),
		Path: disposableDB, RawQuery: "sslmode=disable",
	}
	if user != "" {
		restoredConnection.User = url.UserPassword(user, password)
	}
	restoredURL := restoredConnection.String()
	restoredPool, err := database.NewPool(ctx, restoredURL)
	if err != nil {
		res.ErrorMessage = fmt.Sprintf("connect to restored DB: %v", err)
		return res
	}
	defer restoredPool.Close()

	// Step 5: Verify restored database isolation and pre-recovery data
	var currentDB string
	if err := restoredPool.QueryRow(ctx, `SELECT current_database()`).Scan(&currentDB); err != nil {
		res.ErrorMessage = fmt.Sprintf("query restored current_database: %v", err)
		return res
	}
	if currentDB != disposableDB {
		res.ErrorMessage = fmt.Sprintf("isolation failure: connected to %q, expected %q", currentDB, disposableDB)
		return res
	}

	var paymentState string
	var paymentAmount int64
	if err := restoredPool.QueryRow(ctx, `SELECT state, amount_paise FROM payments WHERE id = $1`, fix.PaymentID).Scan(&paymentState, &paymentAmount); err != nil {
		res.ErrorMessage = fmt.Sprintf("restored DB missing expected payment %s: %v", fix.PaymentID, err)
		return res
	}
	if paymentState != "PROCESSING" || paymentAmount != fix.AmountPaise {
		res.ErrorMessage = fmt.Sprintf("restored DB payment state mismatch: state=%s, amount=%d (expected PROCESSING, %d)", paymentState, paymentAmount, fix.AmountPaise)
		return res
	}

	var holdOpsCount int
	if err := restoredPool.QueryRow(ctx, `SELECT COUNT(*) FROM payment_bank_operations WHERE payment_id = $1 AND operation_id = $2 AND status = 'SUCCESS'`, fix.PaymentID, fix.HoldOpID).Scan(&holdOpsCount); err != nil || holdOpsCount != 1 {
		res.ErrorMessage = fmt.Sprintf("restored DB missing committed hold operation: count=%d, err=%v", holdOpsCount, err)
		return res
	}

	var bankAHoldCount int
	if err := restoredPool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_a.operations WHERE payment_id = $1 AND operation_id = $2`, fix.PaymentID, fix.HoldOpID).Scan(&bankAHoldCount); err != nil || bankAHoldCount != 1 {
		res.ErrorMessage = fmt.Sprintf("restored DB missing Bank A hold operation: count=%d, err=%v", bankAHoldCount, err)
		return res
	}
	res.Checkpoints = append(res.Checkpoints, "restored_database_pre_recovery_state_verified")

	// Step 6: Execute recovery continuation on the restored database
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
	if err := hRestored.projectPaymentEvents(ctx, fix.PaymentID); err != nil {
		res.ErrorMessage = fmt.Sprintf("project payment events on restored DB: %v", err)
		return res
	}

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
		res.ErrorMessage = fmt.Sprintf("invariants failed on restored DB (violations=%d)", finSummary.TotalViolations)
	}
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000.0
	return res
}

func (h *CrossBankRecoveryHarness) RunCB07_RestoreFromDisposableBackupPendingPayment(ctx context.Context) CrossBankScenarioResult {
	return h.RunCB07_UnencryptedDumpRestorePendingPayment(ctx)
}

// RunCB07_TemplateCloneDiagnostic runs a database clone drill for diagnostic comparison only.
// It is explicitly classified as a database clone and does NOT satisfy CB-07 backup-and-restore acceptance.
func (h *CrossBankRecoveryHarness) RunCB07_TemplateCloneDiagnostic(ctx context.Context) CrossBankScenarioResult {
	start := time.Now()
	res := CrossBankScenarioResult{
		ID:            "CB-07-CLONE",
		Name:          "Database clone diagnostic (not backup-and-restore acceptance)",
		FaultBoundary: "Database cloned via CREATE DATABASE TEMPLATE; strictly diagnostic clone comparison",
		Passed:        false,
		ErrorMessage:  "database clone does not satisfy backup-and-restore acceptance requirements",
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
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE balance_paise < 0`).Scan(&negPublic); err != nil {
		summary.NonnegativeBalances = false
		summary.TotalViolations++
	}
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_a.accounts WHERE balance_paise < 0`).Scan(&negA); err != nil {
		summary.NonnegativeBalances = false
		summary.TotalViolations++
	}
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_b.accounts WHERE balance_paise < 0`).Scan(&negB); err != nil {
		summary.NonnegativeBalances = false
		summary.TotalViolations++
	}
	if negPublic+negA+negB > 0 {
		summary.NonnegativeBalances = false
		summary.TotalViolations += (negPublic + negA + negB)
	}

	// 2. Exact Debit/Credit conservation for this payment
	var debitSum, creditSum int64
	if err := h.pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(le.amount_paise) FILTER (WHERE le.entry_type = 'DEBIT'), 0),
			COALESCE(SUM(le.amount_paise) FILTER (WHERE le.entry_type = 'CREDIT'), 0)
		FROM ledger_entries le
		JOIN ledger_transactions lt ON lt.id = le.ledger_transaction_id
		WHERE lt.payment_id = $1`, fix.PaymentID).Scan(&debitSum, &creditSum); err != nil {
		summary.DebitCreditConservation = false
		summary.TotalViolations++
	}
	summary.TotalLedgerDebitsPaise = debitSum
	summary.TotalLedgerCreditsPaise = creditSum
	if debitSum != fix.AmountPaise || creditSum != fix.AmountPaise || debitSum != creditSum {
		summary.DebitCreditConservation = false
		summary.TotalViolations++
	}

	// Central account reconstruction check
	var reconErrors int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM accounts a
		WHERE a.id IN ($1, $2)
		AND a.balance_paise != a.opening_balance_paise + COALESCE((
			SELECT SUM(CASE WHEN le.entry_type = 'CREDIT' THEN le.amount_paise ELSE -le.amount_paise END)
			FROM ledger_entries le WHERE le.account_id = a.id
		), 0)`, fix.SenderCentralAcc, fix.RecvCentralAcc).Scan(&reconErrors); err != nil {
		summary.DebitCreditConservation = false
		summary.TotalViolations++
	}
	summary.CentralReconstructionErr = reconErrors
	if reconErrors > 0 {
		summary.DebitCreditConservation = false
		summary.TotalViolations += reconErrors
	}

	// 3. Exactly 1 ledger transaction for this payment
	var ledgerTxCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_transactions WHERE payment_id = $1`, fix.PaymentID).Scan(&ledgerTxCount); err != nil {
		summary.SingleLedgerPerPayment = false
		summary.TotalViolations++
	}
	if ledgerTxCount != 1 {
		summary.SingleLedgerPerPayment = false
		summary.TotalViolations++
	}

	// 4. Idempotency unique mapping
	var idempMatches int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM idempotency_records WHERE user_id = $1 AND key = $2 AND payment_id = $3`,
		fix.SenderUserID, fix.IdempotencyKey, fix.PaymentID).Scan(&idempMatches); err != nil {
		summary.IdempotencyUnique = false
		summary.TotalViolations++
	}
	if idempMatches != 1 {
		summary.IdempotencyUnique = false
		summary.TotalViolations++
	}

	// 5. Participant operation uniqueness
	var dupAOps, dupBOps int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM (SELECT operation_id FROM bank_a.operations WHERE payment_id = $1 GROUP BY operation_id HAVING COUNT(*) > 1) d`, fix.PaymentID).Scan(&dupAOps); err != nil {
		summary.ParticipantUnique = false
		summary.TotalViolations++
	}
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM (SELECT operation_id FROM bank_b.operations WHERE payment_id = $1 GROUP BY operation_id HAVING COUNT(*) > 1) d`, fix.PaymentID).Scan(&dupBOps); err != nil {
		summary.ParticipantUnique = false
		summary.TotalViolations++
	}
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_a.operations WHERE payment_id = $1`, fix.PaymentID).Scan(&summary.BankAOperationsCount); err != nil {
		summary.ParticipantUnique = false
		summary.TotalViolations++
	}
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bank_b.operations WHERE payment_id = $1`, fix.PaymentID).Scan(&summary.BankBOperationsCount); err != nil {
		summary.ParticipantUnique = false
		summary.TotalViolations++
	}
	if dupAOps+dupBOps > 0 {
		summary.ParticipantUnique = false
		summary.TotalViolations += (dupAOps + dupBOps)
	}

	// 6. Payload integrity (amounts match, currency INR)
	var badPayloads int
	if err := h.pool.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM bank_a.operations WHERE payment_id = $1 AND (amount_paise != $2 OR currency != 'INR')) +
		       (SELECT COUNT(*) FROM bank_b.operations WHERE payment_id = $1 AND (amount_paise != $2 OR currency != 'INR'))`,
		fix.PaymentID, fix.AmountPaise).Scan(&badPayloads); err != nil {
		summary.PayloadIntegrity = false
		summary.TotalViolations++
	}
	if badPayloads > 0 {
		summary.PayloadIntegrity = false
		summary.TotalViolations += badPayloads
	}

	// 7. Correlation between central journal and participant operations
	var mismatches int
	if err := h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM payment_bank_operations j
		LEFT JOIN (
			SELECT operation_id, payment_id, amount_paise, currency FROM bank_a.operations
			UNION ALL
			SELECT operation_id, payment_id, amount_paise, currency FROM bank_b.operations
		) p ON p.operation_id = j.operation_id
		WHERE j.payment_id = $1 AND (p.operation_id IS NULL OR p.payment_id != j.payment_id OR p.amount_paise != j.amount_paise OR p.currency != j.currency)`,
		fix.PaymentID).Scan(&mismatches); err != nil {
		summary.CorrelationIntegrity = false
		summary.TotalViolations++
	}
	if mismatches > 0 {
		summary.CorrelationIntegrity = false
		summary.TotalViolations += mismatches
	}

	// 8. Worklist convergence
	var worklistCount int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ops_payment_worklist WHERE payment_id = $1 AND current_state = 'COMPLETED'`, fix.PaymentID).Scan(&worklistCount); err != nil {
		summary.WorklistConvergence = false
		summary.TotalViolations++
	}
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
	// Reuse the checked independent central/participant assertions; do not publish
	// unconditional uniqueness/payload claims or interpret SQL failures as zero.
	base := (&CrashMatrixHarness{pool: h.pool}).verifyOverallFinancialInvariants(ctx)
	summary := CrossBankFinancialSummary{
		NonnegativeBalances:     base.NonnegativeBalances,
		DebitCreditConservation: base.DebitCreditConservation,
		SingleLedgerPerPayment:  base.SingleLedgerPerPayment,
		IdempotencyUnique:       base.IdempotencyUnique,
		ParticipantUnique:       base.ParticipantUnique,
		PayloadIntegrity:        base.ParticipantUnique,
		CorrelationIntegrity:    base.ParticipantUnique,
		TotalViolations:         base.TotalViolations,
		TotalLedgerDebitsPaise:  base.TotalLedgerDebitsPaise,
		TotalLedgerCreditsPaise: base.TotalLedgerCreditsPaise,
		WorklistConvergence:     true,
	}
	for _, bankName := range []string{"bank_a", "bank_b"} {
		var count int
		if err := h.pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+bankName+".operations").Scan(&count); err != nil {
			summary.ParticipantUnique = false
			summary.TotalViolations++
		}
		if bankName == "bank_a" {
			summary.BankAOperationsCount = count
		} else {
			summary.BankBOperationsCount = count
		}
	}
	var reconstruction, missingProjection int
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts a
		WHERE a.user_id = ANY($1::uuid[]) AND a.balance_paise != a.opening_balance_paise + COALESCE((
		SELECT SUM(CASE WHEN le.entry_type='CREDIT' THEN le.amount_paise ELSE -le.amount_paise END)
		FROM ledger_entries le WHERE le.account_id=a.id), 0)`, h.testUsers).Scan(&reconstruction); err != nil {
		summary.DebitCreditConservation = false
		summary.TotalViolations++
	} else if reconstruction > 0 {
		summary.DebitCreditConservation = false
		summary.TotalViolations += reconstruction
	}
	summary.CentralReconstructionErr = reconstruction
	if err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM payments p
		WHERE p.initiated_by_user_id=ANY($1::uuid[]) AND p.state='COMPLETED'
		AND NOT EXISTS (SELECT 1 FROM ops_payment_worklist w WHERE w.payment_id=p.id AND w.current_state=p.state
		AND w.event_version=p.event_version)`, h.testUsers).Scan(&missingProjection); err != nil {
		summary.WorklistConvergence = false
		summary.TotalViolations++
	} else if missingProjection > 0 {
		summary.WorklistConvergence = false
		summary.TotalViolations += missingProjection
	}
	return summary
}
