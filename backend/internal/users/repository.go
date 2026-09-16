package users

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("user not found")

type Repository struct{ db *pgxpool.Pool }

func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

func (repository *Repository) GetByCredential(ctx context.Context, credential string) (User, error) {
	return scanUser(repository.db.QueryRow(ctx, `SELECT id, name, phone, upi_id, password_hash, role FROM users WHERE phone = $1 OR upi_id = $1`, credential))
}

func (repository *Repository) GetByID(ctx context.Context, id uuid.UUID) (User, error) {
	return scanUser(repository.db.QueryRow(ctx, `SELECT id, name, phone, upi_id, password_hash, role FROM users WHERE id = $1`, id))
}

func scanUser(row pgx.Row) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Name, &user.Phone, &user.PaymentID, &user.PasswordHash, &user.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return user, err
}
