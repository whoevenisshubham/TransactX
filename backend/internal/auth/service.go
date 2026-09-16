package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/common"
	"github.com/transactx/backend/internal/users"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrDuplicateUser      = errors.New("user already exists")
	ErrBankNotFound       = errors.New("default bank not found")
)

type Service struct {
	db              *pgxpool.Pool
	users           *users.Repository
	jwt             *JWTManager
	defaultBankCode string
}

func NewService(db *pgxpool.Pool, jwtManager *JWTManager, defaultBankCode string) *Service {
	return &Service{db: db, users: users.NewRepository(db), jwt: jwtManager, defaultBankCode: defaultBankCode}
}

type RegisterInput struct {
	Name              string
	Phone             string
	PaymentIdentifier string
	Password          string
	Role              string
}

func (service *Service) Register(ctx context.Context, input RegisterInput) (users.PublicUser, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Phone = strings.TrimSpace(input.Phone)
	input.PaymentIdentifier = common.NormalizeIdentifier(input.PaymentIdentifier)
	input.Role = strings.ToUpper(strings.TrimSpace(input.Role))
	if input.Role == "" {
		input.Role = users.RoleCustomer
	}
	if !users.IsPublicRole(input.Role) || !common.ValidLength(input.Name, 1, 120) || !common.ValidLength(input.Phone, 3, 32) || !common.ValidLength(input.PaymentIdentifier, 3, 128) || len(input.Password) < 8 || len(input.Password) > 256 {
		return users.PublicUser{}, common.NewAPIError("INVALID_REQUEST", "registration details are invalid", 400)
	}
	hash, err := HashPassword(input.Password, DefaultArgon2idParams)
	if err != nil {
		return users.PublicUser{}, fmt.Errorf("hash password: %w", err)
	}
	userID, accountID := uuid.New(), uuid.New()
	accountNumber := "TX-" + strings.ToUpper(strings.ReplaceAll(accountID.String(), "-", ""))[:16]
	tx, err := service.db.Begin(ctx)
	if err != nil {
		return users.PublicUser{}, err
	}
	defer tx.Rollback(ctx)
	var bankID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM banks WHERE code = $1 AND status = 'ACTIVE'`, service.defaultBankCode).Scan(&bankID)
	if errors.Is(err, pgx.ErrNoRows) {
		return users.PublicUser{}, ErrBankNotFound
	}
	if err != nil {
		return users.PublicUser{}, err
	}
	var user users.User
	err = tx.QueryRow(ctx, `INSERT INTO users (id, name, phone, upi_id, password_hash, role) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, name, phone, upi_id, password_hash, role`, userID, input.Name, input.Phone, input.PaymentIdentifier, hash, input.Role).Scan(&user.ID, &user.Name, &user.Phone, &user.PaymentID, &user.PasswordHash, &user.Role)
	if isUniqueViolation(err) {
		return users.PublicUser{}, ErrDuplicateUser
	}
	if err != nil {
		return users.PublicUser{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, version, status) VALUES ($1, $2, $3, $4, 0, 0, 'ACTIVE')`, accountID, userID, bankID, accountNumber)
	if err != nil {
		return users.PublicUser{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return users.PublicUser{}, err
	}
	return user.Public(), nil
}

func (service *Service) Login(ctx context.Context, credential, password string, now time.Time) (string, users.PublicUser, error) {
	user, err := service.users.GetByCredential(ctx, common.NormalizeIdentifier(credential))
	if err != nil {
		return "", users.PublicUser{}, ErrInvalidCredentials
	}
	valid, _, err := VerifyPassword(password, user.PasswordHash)
	if err != nil || !valid {
		return "", users.PublicUser{}, ErrInvalidCredentials
	}
	token, err := service.jwt.Issue(user.ID.String(), user.Role, now)
	if err != nil {
		return "", users.PublicUser{}, err
	}
	return token, user.Public(), nil
}

func isUniqueViolation(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == "23505"
}
