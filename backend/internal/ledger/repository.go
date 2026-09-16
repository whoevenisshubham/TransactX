package ledger

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ db *pgxpool.Pool }

func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

func (repository *Repository) CreateTransaction(ctx context.Context, tx pgx.Tx, paymentID uuid.UUID) (uuid.UUID, error) {
	id := uuid.New()
	_, err := tx.Exec(ctx, `INSERT INTO ledger_transactions (id, payment_id) VALUES ($1, $2)`, id, paymentID)
	return id, err
}

func (repository *Repository) CreateEntry(ctx context.Context, tx pgx.Tx, entry Entry) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO ledger_entries (id, ledger_transaction_id, account_id, entry_type, amount_paise)
		VALUES ($1, $2, $3, $4, $5)`, entry.ID, entry.LedgerTransactionID, entry.AccountID, entry.EntryType, entry.AmountPaise)
	return err
}

func (repository *Repository) ReconstructBalance(ctx context.Context, accountID uuid.UUID) (int64, error) {
	var balance int64
	err := repository.db.QueryRow(ctx, `
		SELECT a.opening_balance_paise + COALESCE(SUM(CASE WHEN le.entry_type = 'CREDIT' THEN le.amount_paise ELSE -le.amount_paise END), 0)
		FROM accounts a
		LEFT JOIN ledger_entries le ON le.account_id = a.id
		WHERE a.id = $1
		GROUP BY a.opening_balance_paise`, accountID).Scan(&balance)
	return balance, err
}
