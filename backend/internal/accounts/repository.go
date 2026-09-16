package accounts

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("account not found")

type Repository struct{ db *pgxpool.Pool }

func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

const accountColumns = `id, user_id, bank_id, account_number, balance_paise, version, status, created_at, updated_at`

func (repository *Repository) ListOwned(ctx context.Context, userID uuid.UUID) ([]Account, error) {
	rows, err := repository.db.Query(ctx, `SELECT `+accountColumns+` FROM accounts WHERE user_id = $1 ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Account
	for rows.Next() {
		account, scanErr := scanAccount(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, account)
	}
	return result, rows.Err()
}

func (repository *Repository) GetOwned(ctx context.Context, userID, accountID uuid.UUID) (Account, error) {
	return scanAccount(repository.db.QueryRow(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id = $1 AND user_id = $2`, accountID, userID))
}

func scanAccount(row pgx.Row) (Account, error) {
	var account Account
	err := row.Scan(&account.ID, &account.UserID, &account.BankID, &account.AccountNumber, &account.BalancePaise, &account.Version, &account.Status, &account.CreatedAt, &account.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return account, err
}
