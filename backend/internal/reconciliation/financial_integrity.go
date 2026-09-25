package reconciliation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/payments"
)

// -----------------------------------------------------------------------------
// 1. Typed Check Registry
// -----------------------------------------------------------------------------

// IntegritySeverity represents the operational and financial impact level of an invariant failure.
type IntegritySeverity string

const (
	SeverityCritical IntegritySeverity = "CRITICAL"
	SeverityHigh     IntegritySeverity = "HIGH"
	SeverityMedium   IntegritySeverity = "MEDIUM"
	SeverityLow      IntegritySeverity = "LOW"
	SeverityInfo     IntegritySeverity = "INFO"
)

// Stable check code constants. These MUST remain stable across releases.
const (
	CheckDebitCreditConservation           = "DEBIT_CREDIT_CONSERVATION"
	CheckNonNegativeBalances               = "NON_NEGATIVE_BALANCES"
	CheckTransactionUniqueness             = "TRANSACTION_UNIQUENESS"
	CheckIdempotencyMapping                = "IDEMPOTENCY_MAPPING"
	CheckPaymentStateValidity              = "PAYMENT_STATE_VALIDITY"
	CheckCompletedPaymentLedgerCompleteness = "COMPLETED_PAYMENT_LEDGER_COMPLETENESS"
	CheckMerkleCommitmentConsistency       = "MERKLE_COMMITMENT_CONSISTENCY"
)

// IntegrityCheckDefinition defines the metadata for a registered financial invariant check.
type IntegrityCheckDefinition struct {
	Code        string            `json:"code"`
	Severity    IntegritySeverity `json:"severity"`
	Description string            `json:"description"`
}

// AuthoritativeCheckRegistry contains all registered checks in canonical evaluation order.
var AuthoritativeCheckRegistry = []IntegrityCheckDefinition{
	{
		Code:        CheckDebitCreditConservation,
		Severity:    SeverityCritical,
		Description: "Sum of debits must equal sum of credits for all ledger transactions in scope",
	},
	{
		Code:        CheckNonNegativeBalances,
		Severity:    SeverityCritical,
		Description: "Materialized and spendable account balances must never be negative",
	},
	{
		Code:        CheckTransactionUniqueness,
		Severity:    SeverityCritical,
		Description: "Payment and operation identities must be strictly unique without duplication",
	},
	{
		Code:        CheckIdempotencyMapping,
		Severity:    SeverityHigh,
		Description: "Idempotency records must map uniquely and consistently to their originating user and payment",
	},
	{
		Code:        CheckPaymentStateValidity,
		Severity:    SeverityHigh,
		Description: "Payment states must be valid and conform to permitted state transitions",
	},
	{
		Code:        CheckCompletedPaymentLedgerCompleteness,
		Severity:    SeverityCritical,
		Description: "All completed payments must have corresponding balanced ledger transactions and entries",
	},
	{
		Code:        CheckMerkleCommitmentConsistency,
		Severity:    SeverityCritical,
		Description: "Participant Merkle commitment roots must match canonical calculation of underlying records",
	},
}

// -----------------------------------------------------------------------------
// 2. Structured Result Model
// -----------------------------------------------------------------------------

// CheckStatus represents the evaluation outcome of an individual check.
type CheckStatus string

const (
	CheckStatusPass          CheckStatus = "PASS"
	CheckStatusFail          CheckStatus = "FAIL"
	CheckStatusError         CheckStatus = "ERROR"
	CheckStatusNotApplicable CheckStatus = "NOT_APPLICABLE"
)

// CheckViolation details a single invariant violation detected during check execution.
type CheckViolation struct {
	EntityID    string         `json:"entityId,omitempty"`
	Description string         `json:"description"`
	Details     map[string]any `json:"details,omitempty"`
}

// IntegrityCheckResult is the structured result for an individual invariant check.
type IntegrityCheckResult struct {
	RunID       uuid.UUID          `json:"runId"`
	Code        string             `json:"code"`
	Severity    IntegritySeverity  `json:"severity"`
	Status      CheckStatus        `json:"status"`
	Message     string             `json:"message"`
	Observed    string             `json:"observed,omitempty"`
	Violations  []CheckViolation   `json:"violations,omitempty"`
	Error       string             `json:"error,omitempty"`
	StartedAt   time.Time          `json:"startedAt"`
	CompletedAt time.Time          `json:"completedAt"`
}

// -----------------------------------------------------------------------------
// 3. Integrity Run Model & Persistence
// -----------------------------------------------------------------------------

// IntegrityRunStatus represents the overall completion status of an integrity run.
type IntegrityRunStatus string

const (
	IntegrityRunStatusRunning   IntegrityRunStatus = "RUNNING"
	IntegrityRunStatusCompleted IntegrityRunStatus = "COMPLETED"
	IntegrityRunStatusFailed    IntegrityRunStatus = "FAILED"
)

// IntegrityRunSummary summarizes check outcomes within a run.
type IntegrityRunSummary struct {
	TotalChecks   int `json:"totalChecks"`
	Passed        int `json:"passed"`
	Failed        int `json:"failed"`
	Errors        int `json:"errors"`
	NotApplicable int `json:"notApplicable"`
}

// IntegrityRunResult is the durable result of an entire financial integrity execution.
type IntegrityRunResult struct {
	RunID         uuid.UUID              `json:"runId"`
	Scope         *Scope                 `json:"scope,omitempty"`
	ParticipantID string                 `json:"participantId,omitempty"`
	Status        IntegrityRunStatus     `json:"status"`
	Summary       IntegrityRunSummary    `json:"summary"`
	Checks        []IntegrityCheckResult `json:"checks"`
	ErrorMessage  string                 `json:"errorMessage,omitempty"`
	StartedAt     time.Time              `json:"startedAt"`
	CompletedAt   time.Time              `json:"completedAt"`
}

// IntegrityRunStore defines the persistence boundary for financial integrity runs.
type IntegrityRunStore interface {
	SaveRun(ctx context.Context, run IntegrityRunResult) error
	GetRun(ctx context.Context, runID uuid.UUID) (IntegrityRunResult, error)
	ListRuns(ctx context.Context, limit, offset int) ([]IntegrityRunResult, int, error)
}

var ErrIntegrityRunNotFound = errors.New("integrity run not found")

// MemoryIntegrityRunStore is an in-memory, thread-safe implementation of IntegrityRunStore.
type MemoryIntegrityRunStore struct {
	mu   sync.RWMutex
	runs map[uuid.UUID]IntegrityRunResult
	list []uuid.UUID
}

// NewMemoryIntegrityRunStore constructs a MemoryIntegrityRunStore.
func NewMemoryIntegrityRunStore() *MemoryIntegrityRunStore {
	return &MemoryIntegrityRunStore{
		runs: make(map[uuid.UUID]IntegrityRunResult),
	}
}

func (s *MemoryIntegrityRunStore) SaveRun(_ context.Context, run IntegrityRunResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.runs[run.RunID]; !exists {
		s.list = append(s.list, run.RunID)
	}
	s.runs[run.RunID] = run
	return nil
}

func (s *MemoryIntegrityRunStore) GetRun(_ context.Context, runID uuid.UUID) (IntegrityRunResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[runID]
	if !ok {
		return IntegrityRunResult{}, ErrIntegrityRunNotFound
	}
	return run, nil
}

func (s *MemoryIntegrityRunStore) ListRuns(_ context.Context, limit, offset int) ([]IntegrityRunResult, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := len(s.list)
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []IntegrityRunResult{}, total, nil
	}

	end := offset + limit
	if end > total {
		end = total
	}

	result := make([]IntegrityRunResult, 0, end-offset)
	// Iterate in reverse (latest runs first)
	for i := total - 1 - offset; i >= 0 && len(result) < limit; i-- {
		runID := s.list[i]
		result = append(result, s.runs[runID])
	}

	return result, total, nil
}

// -----------------------------------------------------------------------------
// 4. Financial Data Model for Integrity Auditing (Read-Only)
// -----------------------------------------------------------------------------

type FinancialLedgerEntry struct {
	ID          uuid.UUID `json:"id"`
	AccountID   uuid.UUID `json:"accountId"`
	EntryType   string    `json:"entryType"` // "DEBIT" or "CREDIT"
	AmountPaise int64     `json:"amountPaise"`
	CreatedAt   time.Time `json:"createdAt"`
}

type FinancialLedgerTransaction struct {
	ID        uuid.UUID              `json:"id"`
	PaymentID uuid.UUID              `json:"paymentId"`
	CreatedAt time.Time              `json:"createdAt"`
	Entries   []FinancialLedgerEntry `json:"entries"`
}

type FinancialAccount struct {
	ID                  uuid.UUID `json:"id"`
	UserID              uuid.UUID `json:"userId"`
	BankID              uuid.UUID `json:"bankId"`
	AccountNumber       string    `json:"accountNumber"`
	BalancePaise        int64     `json:"balancePaise"`
	OpeningBalancePaise int64     `json:"openingBalancePaise"`
	Status              string    `json:"status"`
}

type FinancialPayment struct {
	ID                uuid.UUID  `json:"id"`
	InitiatedByUserID uuid.UUID  `json:"initiatedByUserId"`
	SenderAccountID   uuid.UUID  `json:"senderAccountId"`
	ReceiverAccountID uuid.UUID  `json:"receiverAccountId"`
	AmountPaise       int64      `json:"amountPaise"`
	Currency          string     `json:"currency"`
	State             string     `json:"state"`
	CreatedAt         time.Time  `json:"createdAt"`
	CompletedAt       *time.Time `json:"completedAt,omitempty"`
}

type FinancialIdempotencyRecord struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"userId"`
	Key         string     `json:"key"`
	RequestHash string     `json:"requestHash"`
	PaymentID   *uuid.UUID `json:"paymentId,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type FinancialBankOperation struct {
	ID            uuid.UUID `json:"id"`
	PaymentID     uuid.UUID `json:"paymentId"`
	BankID        uuid.UUID `json:"bankId"`
	OperationID   uuid.UUID `json:"operationId"`
	OperationType string    `json:"operationType"`
	Status        string    `json:"status"`
	AmountPaise   int64     `json:"amountPaise"`
	Currency      string    `json:"currency"`
	CreatedAt     time.Time `json:"createdAt"`
}

type FinancialStateTransition struct {
	ID             uuid.UUID `json:"id,omitempty"`
	PaymentID      uuid.UUID `json:"paymentId"`
	FromState      string    `json:"fromState"`
	ToState        string    `json:"toState"`
	TransitionedAt time.Time `json:"transitionedAt"`
}

// MaintainedCommitment represents an independently maintained Merkle commitment state.
type MaintainedCommitment struct {
	ParticipantID    string        `json:"participantId"`
	Partition        string        `json:"partition"`
	BucketWidth      time.Duration `json:"bucketWidth"`
	CanonicalVersion string        `json:"canonicalVersion"`
	AlgorithmVersion string        `json:"algorithmVersion"`
	Generation       string        `json:"generation"`
	Root             []byte        `json:"root"`
	RecordCount      int           `json:"recordCount"`
	Scope            Scope         `json:"scope"`
	CapturedAt       time.Time     `json:"capturedAt"`
}

// ParticipantCommitmentSource provides access to already-maintained participant commitments without rebuilding them.
type ParticipantCommitmentSource interface {
	GetMaintainedCommitment(ctx context.Context, participantID string, scope Scope) (MaintainedCommitment, bool, error)
}

// FinancialDataStore is the read-only abstraction for observing financial state.
type FinancialDataStore interface {
	GetLedgerTransactions(ctx context.Context, scope *Scope) ([]FinancialLedgerTransaction, error)
	GetAccounts(ctx context.Context) ([]FinancialAccount, error)
	GetPayments(ctx context.Context, scope *Scope) ([]FinancialPayment, error)
	GetIdempotencyRecords(ctx context.Context) ([]FinancialIdempotencyRecord, error)
	GetBankOperations(ctx context.Context, scope *Scope) ([]FinancialBankOperation, error)
	GetStateTransitions(ctx context.Context, scope *Scope) ([]FinancialStateTransition, error)
	GetAuthoritativeRecords(ctx context.Context, participantID string, scope Scope) ([]CanonicalRecord, error)
	GetMaintainedCommitment(ctx context.Context, participantID string, scope Scope) (MaintainedCommitment, bool, error)
	GetMerkleCommitment(ctx context.Context, participantID string, scope Scope) (expectedRoot []byte, records []CanonicalRecord, err error)
}

// -----------------------------------------------------------------------------
// In-Memory FinancialDataStore (for deterministic testing & isolation)
// -----------------------------------------------------------------------------

type MemoryFinancialDataStore struct {
	mu                    sync.RWMutex
	LedgerTransactions    []FinancialLedgerTransaction
	Accounts              []FinancialAccount
	Payments              []FinancialPayment
	IdempotencyRecords    []FinancialIdempotencyRecord
	BankOperations        []FinancialBankOperation
	StateTransitions      []FinancialStateTransition
	AuthoritativeRecords  map[string][]CanonicalRecord
	MaintainedCommitments map[string]MaintainedCommitment
	CommitmentStore       IncrementalCommitmentStore
	ParticipantStore      ParticipantCommitmentSource
	MerkleRoots           map[string][]byte
	MerkleRecords         map[string][]CanonicalRecord
	SimulatedError        error
}

func NewMemoryFinancialDataStore() *MemoryFinancialDataStore {
	return &MemoryFinancialDataStore{
		AuthoritativeRecords:  make(map[string][]CanonicalRecord),
		MaintainedCommitments: make(map[string]MaintainedCommitment),
		MerkleRoots:           make(map[string][]byte),
		MerkleRecords:         make(map[string][]CanonicalRecord),
	}
}

func (m *MemoryFinancialDataStore) GetLedgerTransactions(_ context.Context, scope *Scope) ([]FinancialLedgerTransaction, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.SimulatedError != nil {
		return nil, m.SimulatedError
	}
	if scope == nil {
		return cloneLedgerTransactions(m.LedgerTransactions), nil
	}
	var out []FinancialLedgerTransaction
	for _, lt := range m.LedgerTransactions {
		if scopeContains(*scope, lt.CreatedAt) {
			out = append(out, lt)
		}
	}
	return cloneLedgerTransactions(out), nil
}

func (m *MemoryFinancialDataStore) GetAccounts(_ context.Context) ([]FinancialAccount, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.SimulatedError != nil {
		return nil, m.SimulatedError
	}
	out := make([]FinancialAccount, len(m.Accounts))
	copy(out, m.Accounts)
	return out, nil
}

func (m *MemoryFinancialDataStore) GetPayments(_ context.Context, scope *Scope) ([]FinancialPayment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.SimulatedError != nil {
		return nil, m.SimulatedError
	}
	if scope == nil {
		out := make([]FinancialPayment, len(m.Payments))
		copy(out, m.Payments)
		return out, nil
	}
	var out []FinancialPayment
	for _, p := range m.Payments {
		if scopeContains(*scope, p.CreatedAt) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *MemoryFinancialDataStore) GetIdempotencyRecords(_ context.Context) ([]FinancialIdempotencyRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.SimulatedError != nil {
		return nil, m.SimulatedError
	}
	out := make([]FinancialIdempotencyRecord, len(m.IdempotencyRecords))
	copy(out, m.IdempotencyRecords)
	return out, nil
}

func (m *MemoryFinancialDataStore) GetBankOperations(_ context.Context, scope *Scope) ([]FinancialBankOperation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.SimulatedError != nil {
		return nil, m.SimulatedError
	}
	if scope == nil {
		out := make([]FinancialBankOperation, len(m.BankOperations))
		copy(out, m.BankOperations)
		return out, nil
	}
	var out []FinancialBankOperation
	for _, op := range m.BankOperations {
		if scopeContains(*scope, op.CreatedAt) {
			out = append(out, op)
		}
	}
	return out, nil
}

func (m *MemoryFinancialDataStore) GetStateTransitions(_ context.Context, scope *Scope) ([]FinancialStateTransition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.SimulatedError != nil {
		return nil, m.SimulatedError
	}
	if scope == nil {
		out := make([]FinancialStateTransition, len(m.StateTransitions))
		copy(out, m.StateTransitions)
		return out, nil
	}
	var out []FinancialStateTransition
	for _, st := range m.StateTransitions {
		if scopeContains(*scope, st.TransitionedAt) {
			out = append(out, st)
		}
	}
	return out, nil
}

func (m *MemoryFinancialDataStore) GetAuthoritativeRecords(_ context.Context, participantID string, scope Scope) ([]CanonicalRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.SimulatedError != nil {
		return nil, m.SimulatedError
	}
	key := fmt.Sprintf("%s:%d:%d", participantID, scope.From.UnixNano(), scope.To.UnixNano())
	if recs, ok := m.AuthoritativeRecords[key]; ok {
		return cloneCanonicalRecords(recs), nil
	}
	if recs, ok := m.MerkleRecords[key]; ok {
		return cloneCanonicalRecords(recs), nil
	}
	return nil, nil
}

func (m *MemoryFinancialDataStore) GetMaintainedCommitment(ctx context.Context, participantID string, scope Scope) (MaintainedCommitment, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.SimulatedError != nil {
		return MaintainedCommitment{}, false, m.SimulatedError
	}
	key := fmt.Sprintf("%s:%d:%d", participantID, scope.From.UnixNano(), scope.To.UnixNano())
	if mc, ok := m.MaintainedCommitments[key]; ok {
		return mc, true, nil
	}
	if m.ParticipantStore != nil {
		return m.ParticipantStore.GetMaintainedCommitment(ctx, participantID, scope)
	}
	if m.CommitmentStore != nil {
		if finder, ok := m.CommitmentStore.(interface {
			FindState(context.Context, string, Scope) (IncrementalCommitmentState, bool, error)
		}); ok {
			st, found, err := finder.FindState(ctx, participantID, scope)
			if err != nil {
				return MaintainedCommitment{}, false, err
			}
			if found {
				return MaintainedCommitment{
					ParticipantID:    participantID,
					Partition:        st.Partition,
					BucketWidth:      st.BucketWidth,
					CanonicalVersion: st.CanonicalVersion,
					AlgorithmVersion: st.AlgorithmVersion,
					Generation:       st.Generation,
					Root:             append([]byte(nil), st.Root...),
					RecordCount:      st.RecordCount,
					Scope:            st.Scope,
					CapturedAt:       st.CapturedAt,
				}, true, nil
			}
		}
	}
	// Fallback to legacy MerkleRoots map for backward compatibility with existing tests
	if root, ok := m.MerkleRoots[key]; ok {
		recs := m.MerkleRecords[key]
		return MaintainedCommitment{
			ParticipantID:    participantID,
			Partition:        participantID,
			BucketWidth:      1 * time.Hour,
			CanonicalVersion: CanonicalVersion,
			AlgorithmVersion: MerkleAlgorithmVersion,
			Generation:       uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)).String(),
			Root:             append([]byte(nil), root...),
			RecordCount:      len(recs),
			Scope:            scope,
			CapturedAt:       time.Now().UTC(),
		}, true, nil
	}
	return MaintainedCommitment{}, false, nil
}

func (m *MemoryFinancialDataStore) GetMerkleCommitment(ctx context.Context, participantID string, scope Scope) ([]byte, []CanonicalRecord, error) {
	mc, ok, err := m.GetMaintainedCommitment(ctx, participantID, scope)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, errors.New("merkle commitment not found for scope")
	}
	records, err := m.GetAuthoritativeRecords(ctx, participantID, scope)
	if err != nil {
		return nil, nil, err
	}
	return mc.Root, records, nil
}

func cloneLedgerTransactions(in []FinancialLedgerTransaction) []FinancialLedgerTransaction {
	out := make([]FinancialLedgerTransaction, len(in))
	for i, lt := range in {
		out[i] = lt
		if len(lt.Entries) > 0 {
			out[i].Entries = make([]FinancialLedgerEntry, len(lt.Entries))
			copy(out[i].Entries, lt.Entries)
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// PostgreSQL FinancialDataStore (Production Read-Only)
// -----------------------------------------------------------------------------

type PostgresFinancialDataStore struct {
	pool             *pgxpool.Pool
	commitments      IncrementalCommitmentStore
	participantStore ParticipantCommitmentSource
}

func NewPostgresFinancialDataStore(pool *pgxpool.Pool, commitmentStore ...IncrementalCommitmentStore) *PostgresFinancialDataStore {
	var store IncrementalCommitmentStore
	if len(commitmentStore) > 0 && commitmentStore[0] != nil {
		store = commitmentStore[0]
	} else if pool != nil {
		store = NewPostgresIncrementalCommitmentStore(pool)
	}
	return &PostgresFinancialDataStore{
		pool:        pool,
		commitments: store,
	}
}

// NewProductionPostgresFinancialDataStore constructs a PostgresFinancialDataStore explicitly configured
// with a real PostgreSQL-backed durable commitment store (NewPostgresIncrementalCommitmentStore).
func NewProductionPostgresFinancialDataStore(pool *pgxpool.Pool) *PostgresFinancialDataStore {
	var store IncrementalCommitmentStore
	if pool != nil {
		store = NewPostgresIncrementalCommitmentStore(pool)
	}
	return &PostgresFinancialDataStore{
		pool:        pool,
		commitments: store,
	}
}

// WithCommitmentStore injects an IncrementalCommitmentStore for maintained commitment resolution.
func (p *PostgresFinancialDataStore) WithCommitmentStore(store IncrementalCommitmentStore) *PostgresFinancialDataStore {
	p.commitments = store
	return p
}

// WithCommitmentSource injects an independent ParticipantCommitmentSource.
func (p *PostgresFinancialDataStore) WithCommitmentSource(source ParticipantCommitmentSource) *PostgresFinancialDataStore {
	p.participantStore = source
	return p
}

func (p *PostgresFinancialDataStore) GetLedgerTransactions(ctx context.Context, scope *Scope) ([]FinancialLedgerTransaction, error) {
	query := `
		SELECT lt.id, lt.payment_id, lt.created_at,
		       le.id, le.account_id, le.entry_type, le.amount_paise, le.created_at
		FROM ledger_transactions lt
		LEFT JOIN ledger_entries le ON le.ledger_transaction_id = lt.id
	`
	var args []any
	if scope != nil {
		query += " WHERE lt.created_at >= $1 AND lt.created_at < $2"
		args = append(args, scope.From, scope.To)
	}
	query += " ORDER BY lt.created_at ASC, lt.id ASC"

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	txMap := make(map[uuid.UUID]*FinancialLedgerTransaction)
	var orderedIDs []uuid.UUID

	for rows.Next() {
		var (
			ltID        uuid.UUID
			paymentID   uuid.UUID
			ltCreatedAt time.Time
			leID        *uuid.UUID
			accountID   *uuid.UUID
			entryType   *string
			amountPaise *int64
			leCreatedAt *time.Time
		)
		if err := rows.Scan(&ltID, &paymentID, &ltCreatedAt, &leID, &accountID, &entryType, &amountPaise, &leCreatedAt); err != nil {
			return nil, err
		}
		lt, exists := txMap[ltID]
		if !exists {
			lt = &FinancialLedgerTransaction{
				ID:        ltID,
				PaymentID: paymentID,
				CreatedAt: ltCreatedAt.UTC(),
				Entries:   make([]FinancialLedgerEntry, 0, 2),
			}
			txMap[ltID] = lt
			orderedIDs = append(orderedIDs, ltID)
		}
		if leID != nil && accountID != nil && entryType != nil && amountPaise != nil && leCreatedAt != nil {
			lt.Entries = append(lt.Entries, FinancialLedgerEntry{
				ID:          *leID,
				AccountID:   *accountID,
				EntryType:   *entryType,
				AmountPaise: *amountPaise,
				CreatedAt:   leCreatedAt.UTC(),
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]FinancialLedgerTransaction, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		result = append(result, *txMap[id])
	}
	return result, nil
}

func (p *PostgresFinancialDataStore) GetAccounts(ctx context.Context) ([]FinancialAccount, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, user_id, bank_id, account_number, balance_paise, opening_balance_paise, status
		FROM accounts ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []FinancialAccount
	for rows.Next() {
		var a FinancialAccount
		if err := rows.Scan(&a.ID, &a.UserID, &a.BankID, &a.AccountNumber, &a.BalancePaise, &a.OpeningBalancePaise, &a.Status); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

func (p *PostgresFinancialDataStore) GetPayments(ctx context.Context, scope *Scope) ([]FinancialPayment, error) {
	query := `
		SELECT id, initiated_by_user_id, sender_account_id, receiver_account_id,
		       amount_paise, currency, state, created_at, completed_at
		FROM payments
	`
	var args []any
	if scope != nil {
		query += " WHERE created_at >= $1 AND created_at < $2"
		args = append(args, scope.From, scope.To)
	}
	query += " ORDER BY created_at ASC, id ASC"

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var payments []FinancialPayment
	for rows.Next() {
		var pay FinancialPayment
		var compAt *time.Time
		if err := rows.Scan(&pay.ID, &pay.InitiatedByUserID, &pay.SenderAccountID, &pay.ReceiverAccountID,
			&pay.AmountPaise, &pay.Currency, &pay.State, &pay.CreatedAt, &compAt); err != nil {
			return nil, err
		}
		pay.CreatedAt = pay.CreatedAt.UTC()
		if compAt != nil {
			t := compAt.UTC()
			pay.CompletedAt = &t
		}
		payments = append(payments, pay)
	}
	return payments, rows.Err()
}

func (p *PostgresFinancialDataStore) GetIdempotencyRecords(ctx context.Context) ([]FinancialIdempotencyRecord, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, user_id, key, request_hash, payment_id, created_at
		FROM idempotency_records ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []FinancialIdempotencyRecord
	for rows.Next() {
		var r FinancialIdempotencyRecord
		var payID *uuid.UUID
		if err := rows.Scan(&r.ID, &r.UserID, &r.Key, &r.RequestHash, &payID, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.PaymentID = payID
		r.CreatedAt = r.CreatedAt.UTC()
		records = append(records, r)
	}
	return records, rows.Err()
}

func (p *PostgresFinancialDataStore) GetBankOperations(ctx context.Context, scope *Scope) ([]FinancialBankOperation, error) {
	query := `
		SELECT id, payment_id, bank_id, operation_id, operation_type, status, amount_paise, currency, created_at
		FROM payment_bank_operations
	`
	var args []any
	if scope != nil {
		query += " WHERE created_at >= $1 AND created_at < $2"
		args = append(args, scope.From, scope.To)
	}
	query += " ORDER BY created_at ASC, id ASC"

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ops []FinancialBankOperation
	for rows.Next() {
		var op FinancialBankOperation
		if err := rows.Scan(&op.ID, &op.PaymentID, &op.BankID, &op.OperationID, &op.OperationType, &op.Status, &op.AmountPaise, &op.Currency, &op.CreatedAt); err != nil {
			return nil, err
		}
		op.CreatedAt = op.CreatedAt.UTC()
		ops = append(ops, op)
	}
	return ops, rows.Err()
}

func (p *PostgresFinancialDataStore) GetStateTransitions(ctx context.Context, scope *Scope) ([]FinancialStateTransition, error) {
	query := `
		SELECT id, payment_id, from_state, to_state, transitioned_at
		FROM payment_state_transitions
	`
	var (
		args  []any
		conds []string
	)
	if scope != nil {
		if !scope.From.IsZero() {
			args = append(args, scope.From)
			conds = append(conds, fmt.Sprintf("transitioned_at >= $%d", len(args)))
		}
		if !scope.To.IsZero() {
			args = append(args, scope.To)
			conds = append(conds, fmt.Sprintf("transitioned_at < $%d", len(args)))
		}
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY payment_id ASC, transitioned_at ASC, id ASC"

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transitions []FinancialStateTransition
	for rows.Next() {
		var st FinancialStateTransition
		if err := rows.Scan(&st.ID, &st.PaymentID, &st.FromState, &st.ToState, &st.TransitionedAt); err != nil {
			return nil, err
		}
		st.TransitionedAt = st.TransitionedAt.UTC()
		transitions = append(transitions, st)
	}
	return transitions, rows.Err()
}

func (p *PostgresFinancialDataStore) GetAuthoritativeRecords(ctx context.Context, participantID string, scope Scope) ([]CanonicalRecord, error) {
	source := NewCentralLedgerSnapshotSource(p.pool, participantID)
	snapshot, err := source.GetLedgerSnapshot(ctx, bank.LedgerScope{From: scope.From, To: scope.To})
	if err != nil {
		return nil, err
	}
	records := make([]CanonicalRecord, 0, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		if scopeContains(scope, entry.OccurredAt) {
			records = append(records, FromLedgerEntry(entry))
		}
	}
	return records, nil
}

func (p *PostgresFinancialDataStore) GetMaintainedCommitment(ctx context.Context, participantID string, scope Scope) (MaintainedCommitment, bool, error) {
	if p.participantStore != nil {
		return p.participantStore.GetMaintainedCommitment(ctx, participantID, scope)
	}
	if p.commitments != nil {
		if finder, ok := p.commitments.(interface {
			FindState(context.Context, string, Scope) (IncrementalCommitmentState, bool, error)
		}); ok {
			st, found, err := finder.FindState(ctx, participantID, scope)
			if err != nil {
				return MaintainedCommitment{}, false, err
			}
			if found {
				return MaintainedCommitment{
					ParticipantID:    participantID,
					Partition:        st.Partition,
					BucketWidth:      st.BucketWidth,
					CanonicalVersion: st.CanonicalVersion,
					AlgorithmVersion: st.AlgorithmVersion,
					Generation:       st.Generation,
					Root:             append([]byte(nil), st.Root...),
					RecordCount:      st.RecordCount,
					Scope:            st.Scope,
					CapturedAt:       st.CapturedAt,
				}, true, nil
			}
		}
	}
	return MaintainedCommitment{}, false, nil
}

func (p *PostgresFinancialDataStore) GetMerkleCommitment(ctx context.Context, participantID string, scope Scope) ([]byte, []CanonicalRecord, error) {
	mc, ok, err := p.GetMaintainedCommitment(ctx, participantID, scope)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, errors.New("maintained merkle commitment not found for scope")
	}
	records, err := p.GetAuthoritativeRecords(ctx, participantID, scope)
	if err != nil {
		return nil, nil, err
	}
	return mc.Root, records, nil
}

// -----------------------------------------------------------------------------
// PostgreSQL IntegrityRunStore (Production Run Persistence)
// -----------------------------------------------------------------------------

type PostgresIntegrityRunStore struct {
	pool *pgxpool.Pool
}

func NewPostgresIntegrityRunStore(pool *pgxpool.Pool) *PostgresIntegrityRunStore {
	return &PostgresIntegrityRunStore{pool: pool}
}

func (s *PostgresIntegrityRunStore) SaveRun(ctx context.Context, run IntegrityRunResult) error {
	// Bounded, safe insert into integrity_runs and integrity_check_results if tables exist
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var scopeFrom, scopeTo *time.Time
	if run.Scope != nil {
		f := run.Scope.From
		t := run.Scope.To
		scopeFrom = &f
		scopeTo = &t
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO integrity_runs (
			id, scope_from, scope_to, participant_id, status,
			total_checks, passed_checks, failed_checks, error_checks, na_checks,
			error_message, started_at, completed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			total_checks = EXCLUDED.total_checks,
			passed_checks = EXCLUDED.passed_checks,
			failed_checks = EXCLUDED.failed_checks,
			error_checks = EXCLUDED.error_checks,
			na_checks = EXCLUDED.na_checks,
			error_message = EXCLUDED.error_message,
			completed_at = EXCLUDED.completed_at
	`, run.RunID, scopeFrom, scopeTo, run.ParticipantID, string(run.Status),
		run.Summary.TotalChecks, run.Summary.Passed, run.Summary.Failed, run.Summary.Errors, run.Summary.NotApplicable,
		run.ErrorMessage, run.StartedAt, run.CompletedAt)
	if err != nil {
		return err
	}

	for _, check := range run.Checks {
		violations := check.Violations
		if violations == nil {
			violations = []CheckViolation{}
		}
		violationsJSON, err := json.Marshal(violations)
		if err != nil {
			violationsJSON = []byte("[]")
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO integrity_check_results (
				id, run_id, check_code, severity, status, message, observed, error_text, violations, started_at, completed_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		`, uuid.New(), run.RunID, check.Code, string(check.Severity), string(check.Status),
			check.Message, check.Observed, check.Error, violationsJSON, check.StartedAt, check.CompletedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *PostgresIntegrityRunStore) GetRun(ctx context.Context, runID uuid.UUID) (IntegrityRunResult, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, scope_from, scope_to, participant_id, status,
		       total_checks, passed_checks, failed_checks, error_checks, na_checks,
		       error_message, started_at, completed_at
		FROM integrity_runs WHERE id = $1
	`, runID)

	var (
		res       IntegrityRunResult
		scopeFrom *time.Time
		scopeTo   *time.Time
		statusStr string
	)
	err := row.Scan(
		&res.RunID, &scopeFrom, &scopeTo, &res.ParticipantID, &statusStr,
		&res.Summary.TotalChecks, &res.Summary.Passed, &res.Summary.Failed,
		&res.Summary.Errors, &res.Summary.NotApplicable,
		&res.ErrorMessage, &res.StartedAt, &res.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return IntegrityRunResult{}, ErrIntegrityRunNotFound
	}
	if err != nil {
		return IntegrityRunResult{}, err
	}
	res.Status = IntegrityRunStatus(statusStr)
	if scopeFrom != nil && scopeTo != nil {
		res.Scope = &Scope{From: *scopeFrom, To: *scopeTo}
	}

	checkRows, err := s.pool.Query(ctx, `
		SELECT check_code, severity, status, message, observed, error_text, violations, started_at, completed_at
		FROM integrity_check_results WHERE run_id = $1 ORDER BY started_at ASC
	`, runID)
	if err != nil {
		return IntegrityRunResult{}, err
	}
	defer checkRows.Close()

	for checkRows.Next() {
		var (
			cr             IntegrityCheckResult
			sevStr         string
			statStr        string
			errText        string
			obsText        string
			violationsJSON []byte
		)
		cr.RunID = runID
		if err := checkRows.Scan(&cr.Code, &sevStr, &statStr, &cr.Message, &obsText, &errText, &violationsJSON, &cr.StartedAt, &cr.CompletedAt); err != nil {
			return IntegrityRunResult{}, err
		}
		cr.Severity = IntegritySeverity(sevStr)
		cr.Status = CheckStatus(statStr)
		cr.Observed = obsText
		cr.Error = errText
		if len(violationsJSON) > 0 {
			var viols []CheckViolation
			if err := json.Unmarshal(violationsJSON, &viols); err == nil {
				cr.Violations = viols
			}
		}
		res.Checks = append(res.Checks, cr)
	}

	return res, checkRows.Err()
}

func (s *PostgresIntegrityRunStore) ListRuns(ctx context.Context, limit, offset int) ([]IntegrityRunResult, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM integrity_runs`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, scope_from, scope_to, participant_id, status,
		       total_checks, passed_checks, failed_checks, error_checks, na_checks,
		       error_message, started_at, completed_at
		FROM integrity_runs ORDER BY started_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var runs []IntegrityRunResult
	for rows.Next() {
		var (
			res       IntegrityRunResult
			scopeFrom *time.Time
			scopeTo   *time.Time
			statusStr string
		)
		if err := rows.Scan(
			&res.RunID, &scopeFrom, &scopeTo, &res.ParticipantID, &statusStr,
			&res.Summary.TotalChecks, &res.Summary.Passed, &res.Summary.Failed,
			&res.Summary.Errors, &res.Summary.NotApplicable,
			&res.ErrorMessage, &res.StartedAt, &res.CompletedAt,
		); err != nil {
			return nil, 0, err
		}
		res.Status = IntegrityRunStatus(statusStr)
		if scopeFrom != nil && scopeTo != nil {
			res.Scope = &Scope{From: *scopeFrom, To: *scopeTo}
		}
		runs = append(runs, res)
	}

	return runs, total, rows.Err()
}

// -----------------------------------------------------------------------------
// 5. Invariant Checker Implementations (Observational / Read-Only)
// -----------------------------------------------------------------------------

// checkDebitCreditConservation verifies sum(DEBIT) == sum(CREDIT) for each transaction and across the scope.
func checkDebitCreditConservation(ctx context.Context, runID uuid.UUID, store FinancialDataStore, scope *Scope) IntegrityCheckResult {
	start := time.Now().UTC()
	def := findCheckDef(CheckDebitCreditConservation)

	txs, err := store.GetLedgerTransactions(ctx, scope)
	if err != nil {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusError,
			Message: "failed to retrieve ledger transactions", Error: err.Error(),
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	var violations []CheckViolation
	var globalDebits int64
	var globalCredits int64

	for _, tx := range txs {
		var txDebits int64
		var txCredits int64
		for _, entry := range tx.Entries {
			if entry.AmountPaise <= 0 {
				violations = append(violations, CheckViolation{
					EntityID:    entry.ID.String(),
					Description: "non-positive ledger entry amount",
					Details: map[string]any{
						"transactionId": tx.ID.String(),
						"amountPaise":   entry.AmountPaise,
						"entryType":     entry.EntryType,
					},
				})
			}
			switch entry.EntryType {
			case "DEBIT":
				txDebits += entry.AmountPaise
				globalDebits += entry.AmountPaise
			case "CREDIT":
				txCredits += entry.AmountPaise
				globalCredits += entry.AmountPaise
			default:
				violations = append(violations, CheckViolation{
					EntityID:    entry.ID.String(),
					Description: fmt.Sprintf("invalid ledger entry type: %s", entry.EntryType),
					Details: map[string]any{
						"transactionId": tx.ID.String(),
						"entryType":     entry.EntryType,
					},
				})
			}
		}
		if txDebits != txCredits {
			violations = append(violations, CheckViolation{
				EntityID:    tx.ID.String(),
				Description: fmt.Sprintf("transaction debit/credit imbalance: debits=%d, credits=%d", txDebits, txCredits),
				Details: map[string]any{
					"paymentId":   tx.PaymentID.String(),
					"debitsPaise": txDebits,
					"creditsPaise": txCredits,
					"diffPaise":   txDebits - txCredits,
				},
			})
		}
	}

	if globalDebits != globalCredits {
		violations = append(violations, CheckViolation{
			EntityID:    "GLOBAL_SCOPE",
			Description: fmt.Sprintf("aggregate debit/credit conservation violated: totalDebits=%d, totalCredits=%d", globalDebits, globalCredits),
			Details: map[string]any{
				"totalDebitsPaise":  globalDebits,
				"totalCreditsPaise": globalCredits,
				"netDiscrepancy":    globalDebits - globalCredits,
			},
		})
	}

	observed := fmt.Sprintf("transactions=%d, totalDebits=%d, totalCredits=%d", len(txs), globalDebits, globalCredits)
	if len(violations) > 0 {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusFail,
			Message: fmt.Sprintf("debit/credit conservation violated with %d issue(s)", len(violations)),
			Observed: observed, Violations: violations, StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	return IntegrityCheckResult{
		RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusPass,
		Message: "debit/credit conservation invariant satisfied", Observed: observed,
		StartedAt: start, CompletedAt: time.Now().UTC(),
	}
}

// checkNonNegativeBalances detects accounts with materialized or spendable balance < 0.
func checkNonNegativeBalances(ctx context.Context, runID uuid.UUID, store FinancialDataStore) IntegrityCheckResult {
	start := time.Now().UTC()
	def := findCheckDef(CheckNonNegativeBalances)

	accounts, err := store.GetAccounts(ctx)
	if err != nil {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusError,
			Message: "failed to retrieve accounts", Error: err.Error(),
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	var violations []CheckViolation
	for _, acc := range accounts {
		if acc.BalancePaise < 0 {
			violations = append(violations, CheckViolation{
				EntityID:    acc.ID.String(),
				Description: fmt.Sprintf("negative account balance detected: %d paise", acc.BalancePaise),
				Details: map[string]any{
					"accountNumber": acc.AccountNumber,
					"balancePaise":  acc.BalancePaise,
					"status":        acc.Status,
				},
			})
		}
	}

	observed := fmt.Sprintf("accountsEvaluated=%d, negativeCount=%d", len(accounts), len(violations))
	if len(violations) > 0 {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusFail,
			Message: fmt.Sprintf("non-negative balance invariant violated for %d account(s)", len(violations)),
			Observed: observed, Violations: violations, StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	return IntegrityCheckResult{
		RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusPass,
		Message: "non-negative balance invariant satisfied across all accounts", Observed: observed,
		StartedAt: start, CompletedAt: time.Now().UTC(),
	}
}

// checkTransactionUniqueness detects duplicate logical payment and operation identities.
func checkTransactionUniqueness(ctx context.Context, runID uuid.UUID, store FinancialDataStore, scope *Scope) IntegrityCheckResult {
	start := time.Now().UTC()
	def := findCheckDef(CheckTransactionUniqueness)

	paymentsList, err := store.GetPayments(ctx, scope)
	if err != nil {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusError,
			Message: "failed to retrieve payments", Error: err.Error(),
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	ops, err := store.GetBankOperations(ctx, scope)
	if err != nil {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusError,
			Message: "failed to retrieve bank operations", Error: err.Error(),
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	var violations []CheckViolation
	seenPayments := make(map[uuid.UUID]int)
	for _, p := range paymentsList {
		seenPayments[p.ID]++
		if seenPayments[p.ID] == 2 {
			violations = append(violations, CheckViolation{
				EntityID:    p.ID.String(),
				Description: "duplicate payment identity detected",
				Details: map[string]any{
					"paymentId": p.ID.String(),
				},
			})
		}
	}

	seenOps := make(map[string]int)
	for _, op := range ops {
		key := fmt.Sprintf("%s:%s", op.BankID, op.OperationID)
		seenOps[key]++
		if seenOps[key] == 2 {
			violations = append(violations, CheckViolation{
				EntityID:    op.OperationID.String(),
				Description: "duplicate bank operation identity detected for bank",
				Details: map[string]any{
					"bankId":      op.BankID.String(),
					"operationId": op.OperationID.String(),
					"paymentId":   op.PaymentID.String(),
				},
			})
		}
	}

	observed := fmt.Sprintf("paymentsEvaluated=%d, bankOperationsEvaluated=%d, duplicates=%d", len(paymentsList), len(ops), len(violations))
	if len(violations) > 0 {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusFail,
			Message: fmt.Sprintf("transaction uniqueness violated with %d duplicate(s)", len(violations)),
			Observed: observed, Violations: violations, StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	return IntegrityCheckResult{
		RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusPass,
		Message: "transaction uniqueness invariant satisfied", Observed: observed,
		StartedAt: start, CompletedAt: time.Now().UTC(),
	}
}

// checkIdempotencyMapping detects invalid idempotency mappings according to central and participant contracts.
func checkIdempotencyMapping(ctx context.Context, runID uuid.UUID, store FinancialDataStore) IntegrityCheckResult {
	start := time.Now().UTC()
	def := findCheckDef(CheckIdempotencyMapping)

	records, err := store.GetIdempotencyRecords(ctx)
	if err != nil {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusError,
			Message: "failed to retrieve idempotency records", Error: err.Error(),
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	paymentsList, err := store.GetPayments(ctx, nil)
	if err != nil {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusError,
			Message: "failed to retrieve payments for idempotency verification", Error: err.Error(),
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	paymentsMap := make(map[uuid.UUID]FinancialPayment, len(paymentsList))
	for _, p := range paymentsList {
		paymentsMap[p.ID] = p
	}

	var violations []CheckViolation
	seenKeys := make(map[string]uuid.UUID)

	for _, rec := range records {
		userKey := fmt.Sprintf("%s:%s", rec.UserID, rec.Key)
		if existingPayID, seen := seenKeys[userKey]; seen {
			if rec.PaymentID != nil && *rec.PaymentID != existingPayID {
				violations = append(violations, CheckViolation{
					EntityID:    rec.ID.String(),
					Description: "idempotency key mapped to multiple different payment identities",
					Details: map[string]any{
						"userId":         rec.UserID.String(),
						"key":            rec.Key,
						"firstPaymentId": existingPayID.String(),
						"newPaymentId":   rec.PaymentID.String(),
					},
				})
			}
		} else if rec.PaymentID != nil {
			seenKeys[userKey] = *rec.PaymentID
		}

		if rec.PaymentID != nil {
			pay, exists := paymentsMap[*rec.PaymentID]
			if !exists {
				violations = append(violations, CheckViolation{
					EntityID:    rec.ID.String(),
					Description: "idempotency record points to non-existent payment",
					Details: map[string]any{
						"userId":    rec.UserID.String(),
						"key":       rec.Key,
						"paymentId": rec.PaymentID.String(),
					},
				})
			} else if pay.InitiatedByUserID != rec.UserID {
				violations = append(violations, CheckViolation{
					EntityID:    rec.ID.String(),
					Description: "idempotency record user does not match payment initiator",
					Details: map[string]any{
						"idempotencyUserId": rec.UserID.String(),
						"paymentUserId":     pay.InitiatedByUserID.String(),
						"paymentId":         pay.ID.String(),
					},
				})
			}
		}
	}

	observed := fmt.Sprintf("idempotencyRecords=%d, violations=%d", len(records), len(violations))
	if len(violations) > 0 {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusFail,
			Message: fmt.Sprintf("idempotency mapping invariant violated with %d issue(s)", len(violations)),
			Observed: observed, Violations: violations, StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	return IntegrityCheckResult{
		RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusPass,
		Message: "idempotency mapping invariant satisfied", Observed: observed,
		StartedAt: start, CompletedAt: time.Now().UTC(),
	}
}

// checkPaymentStateValidity validates payment states and explicit transitions against the authoritative state machine.
func checkPaymentStateValidity(ctx context.Context, runID uuid.UUID, store FinancialDataStore, scope *Scope) IntegrityCheckResult {
	start := time.Now().UTC()
	def := findCheckDef(CheckPaymentStateValidity)

	paymentsList, err := store.GetPayments(ctx, scope)
	if err != nil {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusError,
			Message: "failed to retrieve payments", Error: err.Error(),
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	var violations []CheckViolation

	// Permitted states recognized by TransactX state machine
	permittedStates := map[string]bool{
		payments.StateCreated:                   true,
		payments.StateValidating:                true,
		payments.StateLocalSettlement:           true,
		payments.StateRouting:                   true,
		payments.StateProcessing:                true,
		payments.StateBankSettledCentralPending: true,
		payments.StateCommitted:                 true,
		payments.StateCompleted:                 true,
		payments.StateFailed:                    true,
		payments.StatePendingReconciliation:     true,
		payments.StateReversed:                  true,
		payments.StateOfflineCaptured:           true,
		payments.StateQueued:                    true,
		payments.StateSyncing:                   true,
		payments.StateReplayFailed:              true,
	}

	for _, p := range paymentsList {
		if !permittedStates[p.State] {
			violations = append(violations, CheckViolation{
				EntityID:    p.ID.String(),
				Description: fmt.Sprintf("unrecognized payment state: %q", p.State),
				Details: map[string]any{
					"paymentId": p.ID.String(),
					"state":     p.State,
				},
			})
		}
	}

	// If explicit immutable transitions exist, validate them against payments.CanTransition
	transitions, err := store.GetStateTransitions(ctx, scope)
	if err != nil {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusError,
			Message: "failed to retrieve state transitions", Error: err.Error(),
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	for _, tr := range transitions {
		if !payments.CanTransition(tr.FromState, tr.ToState) {
			violations = append(violations, CheckViolation{
				EntityID:    tr.PaymentID.String(),
				Description: fmt.Sprintf("illegal state transition from %s to %s", tr.FromState, tr.ToState),
				Details: map[string]any{
					"paymentId": tr.PaymentID.String(),
					"fromState": tr.FromState,
					"toState":   tr.ToState,
				},
			})
		}
	}

	observed := fmt.Sprintf("paymentsEvaluated=%d, transitionsEvaluated=%d, violations=%d", len(paymentsList), len(transitions), len(violations))
	if len(violations) > 0 {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusFail,
			Message: fmt.Sprintf("payment state validity violated with %d issue(s)", len(violations)),
			Observed: observed, Violations: violations, StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	return IntegrityCheckResult{
		RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusPass,
		Message: "payment state validity invariant satisfied", Observed: observed,
		StartedAt: start, CompletedAt: time.Now().UTC(),
	}
}

// checkCompletedPaymentLedgerCompleteness verifies that completed payments have balanced ledger entries.
func checkCompletedPaymentLedgerCompleteness(ctx context.Context, runID uuid.UUID, store FinancialDataStore, scope *Scope) IntegrityCheckResult {
	start := time.Now().UTC()
	def := findCheckDef(CheckCompletedPaymentLedgerCompleteness)

	paymentsList, err := store.GetPayments(ctx, scope)
	if err != nil {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusError,
			Message: "failed to retrieve payments", Error: err.Error(),
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	txs, err := store.GetLedgerTransactions(ctx, scope)
	if err != nil {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusError,
			Message: "failed to retrieve ledger transactions", Error: err.Error(),
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	txByPayment := make(map[uuid.UUID]FinancialLedgerTransaction, len(txs))
	for _, tx := range txs {
		txByPayment[tx.PaymentID] = tx
	}

	var violations []CheckViolation
	var completedCount int

	for _, p := range paymentsList {
		if p.State != payments.StateCompleted {
			continue
		}
		completedCount++

		lt, exists := txByPayment[p.ID]
		if !exists {
			violations = append(violations, CheckViolation{
				EntityID:    p.ID.String(),
				Description: "completed payment is missing ledger transaction representation",
				Details: map[string]any{
					"paymentId":   p.ID.String(),
					"amountPaise": p.AmountPaise,
					"state":       p.State,
				},
			})
			continue
		}

		// Check entries: must contain matching DEBIT for sender and CREDIT for receiver
		var hasDebit, hasCredit bool
		var debitAmount, creditAmount int64
		for _, e := range lt.Entries {
			if e.EntryType == "DEBIT" && e.AccountID == p.SenderAccountID {
				hasDebit = true
				debitAmount += e.AmountPaise
			}
			if e.EntryType == "CREDIT" && e.AccountID == p.ReceiverAccountID {
				hasCredit = true
				creditAmount += e.AmountPaise
			}
		}

		if !hasDebit || debitAmount != p.AmountPaise {
			violations = append(violations, CheckViolation{
				EntityID:    p.ID.String(),
				Description: fmt.Sprintf("completed payment ledger missing matching sender debit (expected %d, observed %d)", p.AmountPaise, debitAmount),
				Details: map[string]any{
					"paymentId":       p.ID.String(),
					"senderAccountId": p.SenderAccountID.String(),
					"expectedPaise":   p.AmountPaise,
					"observedPaise":   debitAmount,
				},
			})
		}

		if !hasCredit || creditAmount != p.AmountPaise {
			violations = append(violations, CheckViolation{
				EntityID:    p.ID.String(),
				Description: fmt.Sprintf("completed payment ledger missing matching receiver credit (expected %d, observed %d)", p.AmountPaise, creditAmount),
				Details: map[string]any{
					"paymentId":         p.ID.String(),
					"receiverAccountId": p.ReceiverAccountID.String(),
					"expectedPaise":     p.AmountPaise,
					"observedPaise":     creditAmount,
				},
			})
		}
	}

	observed := fmt.Sprintf("completedPaymentsEvaluated=%d, violations=%d", completedCount, len(violations))
	if len(violations) > 0 {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusFail,
			Message: fmt.Sprintf("completed payment ledger completeness violated with %d issue(s)", len(violations)),
			Observed: observed, Violations: violations, StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	return IntegrityCheckResult{
		RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusPass,
		Message: "completed payment ledger completeness invariant satisfied", Observed: observed,
		StartedAt: start, CompletedAt: time.Now().UTC(),
	}
}

// checkMerkleCommitmentConsistency verifies that the maintained Merkle commitment matches canonical calculation of underlying records.
func checkMerkleCommitmentConsistency(ctx context.Context, runID uuid.UUID, store FinancialDataStore, participantID string, scope *Scope, req IntegrityRunRequest) IntegrityCheckResult {
	start := time.Now().UTC()
	def := findCheckDef(CheckMerkleCommitmentConsistency)

	if participantID == "" || scope == nil {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusNotApplicable,
			Message: "merkle commitment check requires specific participant ID and scope",
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	maintained, found, err := store.GetMaintainedCommitment(ctx, participantID, *scope)
	if err != nil {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusError,
			Message: "failed to retrieve maintained merkle commitment", Error: err.Error(),
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}
	if !found {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusFail,
			Message: "no maintained Merkle commitment found for participant and scope",
			Observed: fmt.Sprintf("participantId=%s, scope=[%s, %s)", participantID, scope.From.Format(time.RFC3339), scope.To.Format(time.RFC3339)),
			Violations: []CheckViolation{
				{
					EntityID:    participantID,
					Description: "missing maintained Merkle commitment for requested scope; commitments must be maintained prior to verification",
					Details: map[string]any{
						"participantId": participantID,
						"scope":         fmt.Sprintf("[%s, %s)", scope.From.Format(time.RFC3339), scope.To.Format(time.RFC3339)),
					},
				},
			},
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	var violations []CheckViolation

	// Configuration checks
	if maintained.BucketWidth <= 0 {
		violations = append(violations, CheckViolation{
			EntityID:    participantID,
			Description: fmt.Sprintf("configuration mismatch: non-positive bucket width %v", maintained.BucketWidth),
			Details: map[string]any{
				"participantId": participantID,
				"bucketWidth":   maintained.BucketWidth.String(),
			},
		})
	}
	if maintained.Partition == "" {
		violations = append(violations, CheckViolation{
			EntityID:    participantID,
			Description: "configuration mismatch: empty partition",
			Details: map[string]any{
				"participantId": participantID,
			},
		})
	}
	if req.ExpectedBucketWidth > 0 && maintained.BucketWidth != req.ExpectedBucketWidth {
		violations = append(violations, CheckViolation{
			EntityID:    participantID,
			Description: fmt.Sprintf("configuration mismatch: bucket width %v != expected %v", maintained.BucketWidth, req.ExpectedBucketWidth),
			Details: map[string]any{
				"participantId": participantID,
				"observedWidth": maintained.BucketWidth.String(),
				"expectedWidth": req.ExpectedBucketWidth.String(),
			},
		})
	}
	if req.ExpectedPartition != "" && maintained.Partition != req.ExpectedPartition {
		violations = append(violations, CheckViolation{
			EntityID:    participantID,
			Description: fmt.Sprintf("configuration mismatch: partition %q != expected %q", maintained.Partition, req.ExpectedPartition),
			Details: map[string]any{
				"participantId":     participantID,
				"observedPartition": maintained.Partition,
				"expectedPartition": req.ExpectedPartition,
			},
		})
	}
	if maintained.CanonicalVersion != "" && maintained.CanonicalVersion != CanonicalVersion {
		violations = append(violations, CheckViolation{
			EntityID:    participantID,
			Description: fmt.Sprintf("configuration mismatch: canonical version %q != expected %q", maintained.CanonicalVersion, CanonicalVersion),
			Details: map[string]any{
				"participantId":   participantID,
				"observedVersion": maintained.CanonicalVersion,
				"expectedVersion": CanonicalVersion,
			},
		})
	}
	if maintained.AlgorithmVersion != "" && maintained.AlgorithmVersion != MerkleAlgorithmVersion {
		violations = append(violations, CheckViolation{
			EntityID:    participantID,
			Description: fmt.Sprintf("configuration mismatch: algorithm version %q != expected %q", maintained.AlgorithmVersion, MerkleAlgorithmVersion),
			Details: map[string]any{
				"participantId":   participantID,
				"observedVersion": maintained.AlgorithmVersion,
				"expectedVersion": MerkleAlgorithmVersion,
			},
		})
	}

	// Generation checks
	if maintained.Generation == "" {
		violations = append(violations, CheckViolation{
			EntityID:    participantID,
			Description: "wrong generation: maintained commitment has empty generation",
			Details: map[string]any{
				"participantId": participantID,
			},
		})
	} else if req.ExpectedGeneration != "" && maintained.Generation != req.ExpectedGeneration {
		violations = append(violations, CheckViolation{
			EntityID:    participantID,
			Description: fmt.Sprintf("wrong generation: maintained generation %q != expected %q", maintained.Generation, req.ExpectedGeneration),
			Details: map[string]any{
				"participantId":      participantID,
				"observedGeneration": maintained.Generation,
				"expectedGeneration": req.ExpectedGeneration,
			},
		})
	}

	// Fetch authoritative records independently
	records, err := store.GetAuthoritativeRecords(ctx, participantID, *scope)
	if err != nil {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusError,
			Message: "failed to retrieve authoritative records for participant and scope", Error: err.Error(),
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	// Stale check: record count mismatch
	if maintained.RecordCount != len(records) {
		violations = append(violations, CheckViolation{
			EntityID:    participantID,
			Description: fmt.Sprintf("stale maintained commitment: record count %d != authoritative record count %d", maintained.RecordCount, len(records)),
			Details: map[string]any{
				"participantId":            participantID,
				"maintainedRecordCount":    maintained.RecordCount,
				"authoritativeRecordCount": len(records),
			},
		})
	}

	// Recompute canonical Merkle root using the maintained commitment's actual configuration (bucket width & partition)
	bucketWidth := maintained.BucketWidth
	if bucketWidth <= 0 {
		bucketWidth = 1 * time.Hour
	}
	partition := maintained.Partition
	if partition == "" {
		partition = participantID
	}

	ledger, err := NewIncrementalMerkleLedger(partition, bucketWidth, *scope)
	if err != nil {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusError,
			Message: "failed to initialize merkle ledger for validation", Error: err.Error(),
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}
	rebuild, err := ledger.Bootstrap(ctx, records)
	if err != nil {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusError,
			Message: "failed to recompute merkle tree from records", Error: err.Error(),
			StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}
	calculatedRoot := rebuild.ResultingRoot

	// Root mismatch check (tampered maintained root or authoritative record mutation)
	if !bytes.Equal(maintained.Root, calculatedRoot) {
		violations = append(violations, CheckViolation{
			EntityID:    participantID,
			Description: "maintained Merkle commitment root differs from canonical calculation",
			Details: map[string]any{
				"participantId":  participantID,
				"scope":          fmt.Sprintf("[%s, %s)", scope.From.Format(time.RFC3339), scope.To.Format(time.RFC3339)),
				"maintainedRoot": fmt.Sprintf("%x", maintained.Root),
				"calculatedRoot": fmt.Sprintf("%x", calculatedRoot),
				"recordCount":    len(records),
			},
		})
	}

	observed := fmt.Sprintf("records=%d, maintainedRecords=%d, rootMatch=%t, violations=%d",
		len(records), maintained.RecordCount, bytes.Equal(maintained.Root, calculatedRoot), len(violations))
	if len(violations) > 0 {
		return IntegrityCheckResult{
			RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusFail,
			Message: fmt.Sprintf("Merkle commitment consistency violated with %d issue(s)", len(violations)),
			Observed: observed, Violations: violations, StartedAt: start, CompletedAt: time.Now().UTC(),
		}
	}

	return IntegrityCheckResult{
		RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusPass,
		Message: "Merkle commitment consistency invariant satisfied", Observed: observed,
		StartedAt: start, CompletedAt: time.Now().UTC(),
	}
}

func findCheckDef(code string) IntegrityCheckDefinition {
	for _, def := range AuthoritativeCheckRegistry {
		if def.Code == code {
			return def
		}
	}
	return IntegrityCheckDefinition{Code: code, Severity: SeverityHigh, Description: code}
}

// -----------------------------------------------------------------------------
// 6. Runtime Financial Integrity Engine Entry Point
// -----------------------------------------------------------------------------

// IntegrityRunRequest contains the parameters for an integrity run.
type IntegrityRunRequest struct {
	Scope               *Scope        `json:"scope,omitempty"`
	ParticipantID       string        `json:"participantId,omitempty"`
	CheckCodes          []string      `json:"checkCodes,omitempty"` // empty implies all registered checks
	ExpectedGeneration  string        `json:"expectedGeneration,omitempty"`
	ExpectedPartition   string        `json:"expectedPartition,omitempty"`
	ExpectedBucketWidth time.Duration `json:"expectedBucketWidth,omitempty"`
}

// RuntimeIntegrityEngine coordinates and executes observational financial integrity checks.
type RuntimeIntegrityEngine struct {
	dataStore FinancialDataStore
	runStore  IntegrityRunStore
}

// NewRuntimeIntegrityEngine constructs a RuntimeIntegrityEngine.
func NewRuntimeIntegrityEngine(dataStore FinancialDataStore, runStore IntegrityRunStore) *RuntimeIntegrityEngine {
	if runStore == nil {
		runStore = NewMemoryIntegrityRunStore()
	}
	return &RuntimeIntegrityEngine{
		dataStore: dataStore,
		runStore:  runStore,
	}
}

// Run executes all requested integrity checks, records results, and persists run history.
// Crucially, it does NOT abort on the first failure, correctly differentiates FAIL from ERROR,
// and guarantees zero mutation of financial state.
func (e *RuntimeIntegrityEngine) Run(ctx context.Context, req IntegrityRunRequest) (IntegrityRunResult, error) {
	if e.dataStore == nil {
		return IntegrityRunResult{}, errors.New("financial data store is required")
	}

	runID := uuid.New()
	start := time.Now().UTC()

	var selectedCodes map[string]bool
	if len(req.CheckCodes) > 0 {
		selectedCodes = make(map[string]bool, len(req.CheckCodes))
		for _, c := range req.CheckCodes {
			selectedCodes[c] = true
		}
	}

	var results []IntegrityCheckResult
	var summary IntegrityRunSummary

	// Canonical execution order of registered checks
	for _, def := range AuthoritativeCheckRegistry {
		if selectedCodes != nil && !selectedCodes[def.Code] {
			continue
		}

		var checkRes IntegrityCheckResult
		switch def.Code {
		case CheckDebitCreditConservation:
			checkRes = checkDebitCreditConservation(ctx, runID, e.dataStore, req.Scope)
		case CheckNonNegativeBalances:
			checkRes = checkNonNegativeBalances(ctx, runID, e.dataStore)
		case CheckTransactionUniqueness:
			checkRes = checkTransactionUniqueness(ctx, runID, e.dataStore, req.Scope)
		case CheckIdempotencyMapping:
			checkRes = checkIdempotencyMapping(ctx, runID, e.dataStore)
		case CheckPaymentStateValidity:
			checkRes = checkPaymentStateValidity(ctx, runID, e.dataStore, req.Scope)
		case CheckCompletedPaymentLedgerCompleteness:
			checkRes = checkCompletedPaymentLedgerCompleteness(ctx, runID, e.dataStore, req.Scope)
		case CheckMerkleCommitmentConsistency:
			checkRes = checkMerkleCommitmentConsistency(ctx, runID, e.dataStore, req.ParticipantID, req.Scope, req)
		default:
			checkRes = IntegrityCheckResult{
				RunID: runID, Code: def.Code, Severity: def.Severity, Status: CheckStatusNotApplicable,
				Message: fmt.Sprintf("unimplemented check code: %s", def.Code),
				StartedAt: time.Now().UTC(), CompletedAt: time.Now().UTC(),
			}
		}

		summary.TotalChecks++
		switch checkRes.Status {
		case CheckStatusPass:
			summary.Passed++
		case CheckStatusFail:
			summary.Failed++
		case CheckStatusError:
			summary.Errors++
		case CheckStatusNotApplicable:
			summary.NotApplicable++
		}

		results = append(results, checkRes)
	}

	// Determine overall run status
	overallStatus := IntegrityRunStatusCompleted
	if summary.Errors > 0 && summary.Passed == 0 && summary.Failed == 0 {
		overallStatus = IntegrityRunStatusFailed
	}

	runResult := IntegrityRunResult{
		RunID:         runID,
		Scope:         req.Scope,
		ParticipantID: req.ParticipantID,
		Status:        overallStatus,
		Summary:       summary,
		Checks:        results,
		StartedAt:     start,
		CompletedAt:   time.Now().UTC(),
	}

	// Persist the run
	if err := e.runStore.SaveRun(ctx, runResult); err != nil {
		// Even if persistence fails, return the collected run results with error noted
		runResult.ErrorMessage = fmt.Sprintf("persistence error: %v", err)
	}

	return runResult, nil
}
