package reconciliation

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/bank"
)

// CentralLedgerSnapshotSource reads authoritative operations from central
// PostgreSQL. It implements LedgerSnapshotSource for a specific bank code,
// reading from payment_bank_operations joined with banks.
type CentralLedgerSnapshotSource struct {
	db       *pgxpool.Pool
	bankCode string
}

// ProjectedCentralLedgerSnapshotSource retains central operation facts while
// taking the participant-attributed occurrence time for the same operation ID.
// BankAdapter does not return occurrence timestamps on operation responses, so
// this join is the explicit projection boundary needed for identical records.
type ProjectedCentralLedgerSnapshotSource struct {
	central     *CentralLedgerSnapshotSource
	participant LedgerSnapshotSource
}

func NewProjectedCentralLedgerSnapshotSource(db *pgxpool.Pool, participantID string, participant LedgerSnapshotSource) *ProjectedCentralLedgerSnapshotSource {
	return &ProjectedCentralLedgerSnapshotSource{central: NewCentralLedgerSnapshotSource(db, participantID), participant: participant}
}

func (source *ProjectedCentralLedgerSnapshotSource) GetLedgerSnapshot(ctx context.Context, scope bank.LedgerScope) (bank.LedgerSnapshot, error) {
	central, err := source.central.GetLedgerSnapshot(ctx, scope)
	if err != nil || source.participant == nil {
		return central, err
	}
	participant, err := source.participant.GetLedgerSnapshot(ctx, scope)
	if err != nil {
		return bank.LedgerSnapshot{}, fmt.Errorf("read participant occurrence projection: %w", err)
	}
	occurred := make(map[uuid.UUID]time.Time, len(participant.Entries))
	for _, entry := range participant.Entries {
		occurred[entry.OperationID] = entry.OccurredAt.UTC()
	}
	for i := range central.Entries {
		if value, ok := occurred[central.Entries[i].OperationID]; ok {
			central.Entries[i].OccurredAt = value
		}
	}
	return central, nil
}

// NewCentralLedgerSnapshotSource creates a snapshot source for the central ledger.
func NewCentralLedgerSnapshotSource(db *pgxpool.Pool, bankCode string) *CentralLedgerSnapshotSource {
	return &CentralLedgerSnapshotSource{
		db:       db,
		bankCode: bankCode,
	}
}

// GetLedgerSnapshot queries central PostgreSQL for the authoritative operations
// recorded for this bank participant within [scope.From, scope.To).
func (s *CentralLedgerSnapshotSource) GetLedgerSnapshot(ctx context.Context, scope bank.LedgerScope) (bank.LedgerSnapshot, error) {
	if s.db == nil {
		return bank.LedgerSnapshot{BankID: s.bankCode, SnapshotID: uuid.New(), CapturedAt: time.Now().UTC()}, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT 
			pbo.operation_id, 
			pbo.payment_id, 
			COALESCE(pbo.account_id, '00000000-0000-0000-0000-000000000000'::uuid) AS account_id, 
			CASE pbo.operation_type
				WHEN 'FINALIZE_CREDIT' THEN 'FINAL_CREDIT'
				WHEN 'RELEASE_HOLD' THEN 'RELEASE'
				ELSE pbo.operation_type
			END AS entry_type,
			pbo.amount_paise, 
			pbo.currency, 
			pbo.created_at
		FROM payment_bank_operations pbo
		JOIN banks b ON b.id = pbo.bank_id
		WHERE b.code = $1 
		  AND pbo.status = 'SUCCEEDED'
		  AND pbo.operation_type IN ('HOLD', 'PROVISIONAL_CREDIT', 'FINALIZE_CREDIT', 'RELEASE_HOLD', 'REVERSE_CREDIT')
		  AND pbo.created_at >= $2 
		  AND pbo.created_at < $3
		ORDER BY pbo.created_at, pbo.operation_id
	`, s.bankCode, scope.From, scope.To)
	if err != nil {
		return bank.LedgerSnapshot{}, fmt.Errorf("query central operations for %q: %w", s.bankCode, err)
	}
	defer rows.Close()

	snapshot := bank.LedgerSnapshot{
		BankID:     s.bankCode,
		SnapshotID: uuid.New(),
		CapturedAt: time.Now().UTC(),
	}
	for rows.Next() {
		var entry bank.LedgerEntry
		if err := rows.Scan(
			&entry.OperationID,
			&entry.PaymentID,
			&entry.AccountID,
			&entry.EntryType,
			&entry.AmountPaise,
			&entry.Currency,
			&entry.OccurredAt,
		); err != nil {
			return bank.LedgerSnapshot{}, fmt.Errorf("scan central operation: %w", err)
		}
		snapshot.Entries = append(snapshot.Entries, entry)
	}
	return snapshot, rows.Err()
}

// Compile-time check that CentralLedgerSnapshotSource satisfies LedgerSnapshotSource.
var _ LedgerSnapshotSource = (*CentralLedgerSnapshotSource)(nil)
var _ LedgerSnapshotSource = (*ProjectedCentralLedgerSnapshotSource)(nil)

// NewCentralRepositoryParticipant builds a ReconciliationParticipant backed by
// central PostgreSQL for the given bank code.
func NewCentralRepositoryParticipant(db *pgxpool.Pool, bankCode, partition string, bucketWidth time.Duration) (*RepositoryParticipant, error) {
	source := NewCentralLedgerSnapshotSource(db, bankCode)
	return NewRepositoryParticipant(source, bankCode, partition, bucketWidth)
}

// NewDurableCentralRepositoryParticipant builds a read-only participant over
// explicitly maintained canonical commitments.
func NewDurableCentralRepositoryParticipant(db *pgxpool.Pool, bankCode string, bucketWidth time.Duration) (*RepositoryParticipant, error) {
	source := NewCentralLedgerSnapshotSource(db, bankCode)
	store := NewPostgresIncrementalCommitmentStoreForOwner(db, "canonical:"+bankCode)
	return NewRepositoryParticipantWithCommitmentStore(source, bankCode, bankCode, bucketWidth, store)
}
