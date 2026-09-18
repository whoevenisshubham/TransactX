package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/config"
	"github.com/transactx/backend/internal/database"
	"github.com/transactx/backend/internal/users"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}
	password := os.Getenv("DEV_ADMIN_PASSWORD")
	if password == "" {
		logger.Error("DEV_ADMIN_PASSWORD is required to provision the development administrator")
		os.Exit(1)
	}
	if !cfg.DevelopmentMode {
		logger.Error("development provisioning requires APP_DEVELOPMENT=true")
		os.Exit(1)
	}
	db, err := database.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect to PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := provision(context.Background(), db, password, cfg.DefaultBankCode); err != nil {
		logger.Error("provision development data", "error", err)
		os.Exit(1)
	}
	logger.Info("development bank and OPS_ADMIN provisioned", "bank_code", cfg.DefaultBankCode)
}

func provision(ctx context.Context, db *pgxpool.Pool, password, bankCode string) error {
	hash, err := auth.HashPassword(password, auth.DefaultArgon2idParams)
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	bankID := uuid.NewMD5(uuid.NameSpaceOID, []byte("transactx-bank:"+bankCode))
	if err := tx.QueryRow(ctx, `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'TransactX Development Bank', 'ACTIVE') ON CONFLICT (code) DO UPDATE SET status = 'ACTIVE' RETURNING id`, bankID, bankCode).Scan(&bankID); err != nil {
		return err
	}
	adminID := uuid.NewMD5(uuid.NameSpaceOID, []byte("transactx-admin"))
	phone := strings.TrimSpace(getEnv("DEV_ADMIN_PHONE", "9999999999"))
	paymentID := strings.ToLower(strings.TrimSpace(getEnv("DEV_ADMIN_PAYMENT_ID", "admin@transactx")))
	name := strings.TrimSpace(getEnv("DEV_ADMIN_NAME", "Development Administrator"))
	if _, err := tx.Exec(ctx, `INSERT INTO users (id, name, phone, upi_id, password_hash, role) VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, phone = EXCLUDED.phone, upi_id = EXCLUDED.upi_id, password_hash = EXCLUDED.password_hash, role = EXCLUDED.role, updated_at = CURRENT_TIMESTAMP`, adminID, name, phone, paymentID, hash, users.RoleOpsAdmin); err != nil {
		return err
	}
	accountID := uuid.NewMD5(uuid.NameSpaceOID, []byte("transactx-admin-account"))
	accountNumber := "TX-ADMIN-DEV-001"
	if _, err := tx.Exec(ctx, `INSERT INTO accounts (id, user_id, bank_id, bank_account_id, account_number, balance_paise, version, status) VALUES ($1, $2, $3, $4, $5, 0, 0, 'ACTIVE') ON CONFLICT (id) DO UPDATE SET user_id = EXCLUDED.user_id, bank_id = EXCLUDED.bank_id, bank_account_id = EXCLUDED.bank_account_id, status = 'ACTIVE'`, accountID, adminID, bankID, accountID, accountNumber); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
