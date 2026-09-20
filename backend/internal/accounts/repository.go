package accounts

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("account not found")
var ErrInsufficientFunds = errors.New("insufficient funds")
var ErrAmbiguousPrimary = errors.New("multiple active primary accounts")

type Repository struct{ db *pgxpool.Pool }

func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

const accountColumns = `id, user_id, bank_id, bank_account_id, account_number, balance_paise, opening_balance_paise, version, status, created_at, updated_at`

func (repository *Repository) Debit(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, amountPaise int64) error {
	result, err := tx.Exec(ctx, `
		UPDATE accounts
		SET balance_paise = balance_paise - $2, version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = 'ACTIVE' AND balance_paise >= $2`, accountID, amountPaise)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrInsufficientFunds
	}
	return nil
}

func (repository *Repository) Credit(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, amountPaise int64) error {
	result, err := tx.Exec(ctx, `
		UPDATE accounts
		SET balance_paise = balance_paise + $2, version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = 'ACTIVE'`, accountID, amountPaise)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

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

func (repository *Repository) GetPrimaryOwned(ctx context.Context, userID uuid.UUID) (Account, error) {
	rows, err := repository.db.Query(ctx, `SELECT `+accountColumns+` FROM accounts WHERE user_id = $1 AND status = 'ACTIVE' ORDER BY created_at ASC, id ASC LIMIT 2`, userID)
	if err != nil {
		return Account{}, err
	}
	defer rows.Close()
	var result []Account
	for rows.Next() {
		account, scanErr := scanAccount(rows)
		if scanErr != nil {
			return Account{}, scanErr
		}
		result = append(result, account)
	}
	if err := rows.Err(); err != nil {
		return Account{}, err
	}
	if len(result) == 0 {
		return Account{}, ErrNotFound
	}
	if len(result) > 1 {
		return Account{}, ErrAmbiguousPrimary
	}
	return result[0], nil
}

func scanAccount(row pgx.Row) (Account, error) {
	var account Account
	var bankAccountID *uuid.UUID
	err := row.Scan(&account.ID, &account.UserID, &account.BankID, &bankAccountID, &account.AccountNumber, &account.BalancePaise, &account.OpeningBalancePaise, &account.Version, &account.Status, &account.CreatedAt, &account.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err == nil {
		account.BankAccountID = account.ID
		if bankAccountID != nil {
			account.BankAccountID = *bankAccountID
		}
	}
	return account, err
}
