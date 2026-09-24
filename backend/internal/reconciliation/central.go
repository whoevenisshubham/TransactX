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
			pbo.operation_type, 
			pbo.amount_paise, 
			pbo.currency, 
			pbo.created_at
		FROM payment_bank_operations pbo
		JOIN banks b ON b.id = pbo.bank_id
		WHERE b.code = $1 
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

// NewCentralRepositoryParticipant builds a ReconciliationParticipant backed by
// central PostgreSQL for the given bank code.
func NewCentralRepositoryParticipant(db *pgxpool.Pool, bankCode, partition string, bucketWidth time.Duration) (*RepositoryParticipant, error) {
	source := NewCentralLedgerSnapshotSource(db, bankCode)
	return NewRepositoryParticipant(source, bankCode, partition, bucketWidth)
}
