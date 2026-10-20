// demo-seed initializes ONLY an explicitly acknowledged empty portfolio DB.
// It never resets accounts, rewrites history, or restores missing transitions.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/database"
)

func main() {
	if err := run(); err != nil {
		slog.Error("demo initialization failed", "error", err)
		os.Exit(1)
	}
	slog.Info("fresh demo initialized; no historical transition data imported; public roles contain no administrator")
}

func run() error {
	if os.Getenv("TX_DEMO_INITIALIZE") != "I_ACKNOWLEDGE_NEW_EMPTY_DEMO_DATABASE" {
		return fmt.Errorf("explicit fresh-demo acknowledgment required")
	}
	expected := os.Getenv("TX_DEMO_EXPECTED_DATABASE")
	if expected == "" || expected == "transactx" || expected == "postgres" {
		return fmt.Errorf("explicit non-source expected database required")
	}
	password := os.Getenv("TX_DEMO_PASSWORD")
	if len(password) < 12 {
		return fmt.Errorf("TX_DEMO_PASSWORD must contain at least 12 characters")
	}
	hash, err := auth.HashPassword(password, auth.DefaultArgon2idParams)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.NewPool(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("connect to demo database failed")
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(782346192)`); err != nil {
		return err
	}
	var actual string
	var populated bool
	if err = tx.QueryRow(ctx, `SELECT current_database(), EXISTS(SELECT 1 FROM users) OR EXISTS(SELECT 1 FROM accounts) OR EXISTS(SELECT 1 FROM payments) OR EXISTS(SELECT 1 FROM bank_a.accounts) OR EXISTS(SELECT 1 FROM bank_b.accounts)`).Scan(&actual, &populated); err != nil {
		return err
	}
	if actual != expected || populated {
		return fmt.Errorf("database name mismatch or populated database: refusing initialization")
	}
	bankIDs := map[string]uuid.UUID{}
	for _, code := range []string{"BANK-A", "BANK-B"} {
		id := uuid.NewMD5(uuid.NameSpaceOID, []byte("transactx-bank:"+code))
		if err = tx.QueryRow(ctx, `INSERT INTO banks(id,code,name,status) VALUES($1,$2,$3,'ACTIVE') ON CONFLICT(code) DO UPDATE SET name=EXCLUDED.name RETURNING id`, id, code, "TransactX Demo "+code).Scan(&id); err != nil {
			return err
		}
		bankIDs[code] = id
	}
	fixtures := []struct {
		name, phone, paymentID, role, bank string
		balance                            int64
	}{
		{"Alice Demo", "9990000001", "alice@transactx", "CUSTOMER", "BANK-A", 1000000},
		{"Bob Demo", "9990000002", "bob@transactx", "CUSTOMER", "BANK-B", 100000},
		{"Demo Store", "9990000003", "store@transactx", "MERCHANT", "BANK-B", 0},
		{"Demo Operator", "8888888888", "viewer@transactx", "OPS_VIEWER", "BANK-A", 0},
	}
	for _, f := range fixtures {
		userID, accountID := uuid.New(), uuid.New()
		number := "TX-" + strings.ToUpper(strings.ReplaceAll(accountID.String(), "-", ""))[:16]
		if _, err = tx.Exec(ctx, `INSERT INTO users(id,name,phone,upi_id,password_hash,role) VALUES($1,$2,$3,$4,$5,$6)`, userID, f.name, f.phone, f.paymentID, hash, f.role); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO accounts(id,user_id,bank_id,bank_account_id,account_number,balance_paise,opening_balance_paise,version,status) VALUES($1,$2,$3,$1,$4,$5,$5,0,'ACTIVE')`, accountID, userID, bankIDs[f.bank], number, f.balance); err != nil {
			return err
		}
		// Schema names are fixed literals, never user input.
		query := `INSERT INTO bank_a.accounts(id,account_number,balance_paise,version,status) VALUES($1,$2,$3,0,'ACTIVE')`
		if f.bank == "BANK-B" {
			query = `INSERT INTO bank_b.accounts(id,account_number,balance_paise,version,status) VALUES($1,$2,$3,0,'ACTIVE')`
		}
		if _, err = tx.Exec(ctx, query, accountID, number, f.balance); err != nil {
			return err
		}
	}
	// A demo-only, persistent admission budget survives sleep/redeploy and bounds
	// amplification into immutable histories. No financial record is deleted.
	_, err = tx.Exec(ctx, `
	CREATE SCHEMA demo_limits;
	CREATE TABLE demo_limits.budgets(kind text PRIMARY KEY, total bigint NOT NULL DEFAULT 0, day date NOT NULL DEFAULT (CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date, today integer NOT NULL DEFAULT 0);
	INSERT INTO demo_limits.budgets(kind) VALUES('payments'),('investigation_jobs');
	CREATE FUNCTION demo_limits.admit() RETURNS trigger LANGUAGE plpgsql SET timezone='UTC' AS $fn$
	DECLARE allowed boolean;
	BEGIN
	  UPDATE demo_limits.budgets SET total=total+1,
	    today=CASE WHEN day=CURRENT_DATE THEN today+1 ELSE 1 END, day=CURRENT_DATE
	    WHERE kind=TG_TABLE_NAME AND total<1000 AND (day<>CURRENT_DATE OR today<50)
	    RETURNING true INTO allowed;
	  IF allowed IS DISTINCT FROM true THEN RAISE EXCEPTION 'public demo capacity reached; owner maintenance required' USING ERRCODE='P0001'; END IF;
	  RETURN NEW;
	END; $fn$;
	CREATE TRIGGER public_demo_payment_capacity BEFORE INSERT ON public.payments FOR EACH ROW EXECUTE FUNCTION demo_limits.admit();
	CREATE TRIGGER public_demo_investigation_capacity BEFORE INSERT ON public.investigation_jobs FOR EACH ROW EXECUTE FUNCTION demo_limits.admit();`)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
