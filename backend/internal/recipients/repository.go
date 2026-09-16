package recipients

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("recipient not found")

type Repository struct{ db *pgxpool.Pool }

func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

func (repository *Repository) Resolve(ctx context.Context, paymentID string) (Recipient, error) {
	var recipient Recipient
	err := repository.db.QueryRow(ctx, `
		SELECT a.id, u.id, u.name, u.upi_id, a.status
		FROM users u
		JOIN accounts a ON a.user_id = u.id
		WHERE u.upi_id = $1
		ORDER BY a.created_at ASC
		LIMIT 1`, paymentID).Scan(&recipient.AccountID, &recipient.UserID, &recipient.Name, &recipient.PaymentID, &recipient.AccountStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return Recipient{}, ErrNotFound
	}
	return recipient, err
}

func (repository *Repository) ResolveActive(ctx context.Context, paymentID string) (Recipient, error) {
	recipient, err := repository.Resolve(ctx, paymentID)
	if err != nil {
		return Recipient{}, err
	}
	if recipient.AccountStatus != "ACTIVE" {
		return Recipient{}, ErrNotFound
	}
	return recipient, nil
}
