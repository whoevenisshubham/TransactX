package main

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/transactx/backend/internal/database"
)

func TestInitializationRequiresExplicitFreshDatabase(t *testing.T) {
	t.Setenv("TX_DEMO_INITIALIZE", "")
	if run() == nil {
		t.Fatal("missing acknowledgment accepted")
	}
	t.Setenv("TX_DEMO_INITIALIZE", "I_ACKNOWLEDGE_NEW_EMPTY_DEMO_DATABASE")
	for _, name := range []string{"", "transactx", "postgres"} {
		t.Setenv("TX_DEMO_EXPECTED_DATABASE", name)
		if run() == nil {
			t.Fatalf("unsafe database accepted: %q", name)
		}
	}
}

func TestSeedAndCapacityOnExplicitDisposableDatabase(t *testing.T) {
	dsn := os.Getenv("TX_DEMO_SEED_TEST_DATABASE")
	if dsn == "" {
		t.Skip("requires a new migrated disposable database; no local/production fallback")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasPrefix(strings.TrimPrefix(u.Path, "/"), "transactx_disposable_") {
		t.Fatal("refusing non-disposable database")
	}
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("TX_DEMO_EXPECTED_DATABASE", strings.TrimPrefix(u.Path, "/"))
	t.Setenv("TX_DEMO_INITIALIZE", "I_ACKNOWLEDGE_NEW_EMPTY_DEMO_DATABASE")
	t.Setenv("TX_DEMO_PASSWORD", "Disposable-demo-password-2026!")
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if err := run(); err == nil {
		t.Fatal("populated database was reseeded")
	}
	ctx := context.Background()
	pool, err := database.NewPool(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var accounts, admins int
	if err = pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE role='OPS_ADMIN') FROM users`).Scan(&accounts, &admins); err != nil || accounts != 4 || admins != 0 {
		t.Fatalf("unsafe demo roles: users=%d admins=%d err=%v", accounts, admins, err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// Exercise actual admission trigger/rollback with synthetic CREATED payments.
	insert := `INSERT INTO payments(id,initiated_by_user_id,sender_account_id,receiver_account_id,amount_paise,currency,state)
	 SELECT gen_random_uuid(),s.user_id,s.id,r.id,1,'INR','CREATED' FROM accounts s JOIN users su ON su.id=s.user_id
	 CROSS JOIN accounts r JOIN users ru ON ru.id=r.user_id WHERE su.upi_id='alice@transactx' AND ru.upi_id='bob@transactx'`
	if _, err = tx.Exec(ctx, `UPDATE demo_limits.budgets SET total=999,today=0,day=CURRENT_DATE WHERE kind='payments'`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, insert); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SAVEPOINT before_limit`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, insert); err == nil {
		t.Fatal("lifetime capacity bypassed")
	}
	if _, err = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT before_limit`); err != nil {
		t.Fatal(err)
	}
	var total int
	if err = tx.QueryRow(ctx, `SELECT total FROM demo_limits.budgets WHERE kind='payments'`).Scan(&total); err != nil || total != 1000 {
		t.Fatalf("counter corrupted: %d %v", total, err)
	}
	if _, err = tx.Exec(ctx, `UPDATE demo_limits.budgets SET total=100,today=50,day=(CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date WHERE kind='payments'`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `SAVEPOINT before_daily_limit`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, insert); err == nil {
		t.Fatal("daily capacity bypassed")
	}
	if _, err = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT before_daily_limit`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE demo_limits.budgets SET total=100,today=50,day=CURRENT_DATE-1 WHERE kind='payments'`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, insert); err != nil {
		t.Fatalf("new day did not admit: %v", err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT total FROM demo_limits.budgets WHERE kind='payments'`).Scan(&total); err != nil || total != 0 {
		t.Fatal("rolled-back capacity consumed allowance")
	}
}
