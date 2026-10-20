package auth

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDemoRejectsArbitraryPersonasBeforeDatabaseAccess(t *testing.T) {
	service := &Service{}
	for _, persona := range []string{"", "admin", "OPS_ADMIN", "viewer@transactx", "alice@transactx", "CUSTOMER", " customer", "customer\x00"} {
		if _, _, err := service.DemoLogin(context.Background(), persona, time.Now()); err != ErrInvalidCredentials {
			t.Fatalf("arbitrary persona accepted: %q", persona)
		}
	}
}

// This test deliberately requires the V3 disposable database, never the
// historical/source DATABASE_URL. Initialize it with the real demo-seed command.
func TestDemoLoginDisposablePostgres(t *testing.T) {
	dsn := os.Getenv("TX_EXPERIENCE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TX_EXPERIENCE_TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasPrefix(u.Path, "/txv3_") || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("requires explicitly named loopback V3 disposable database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("open disposable pool failed")
	}
	defer pool.Close()
	manager, err := NewJWTManager(strings.Repeat("d", 32), "v3-demo-test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(pool, manager, "BANK-A")
	for _, persona := range []string{"customer", "recipient", "merchant", "operator"} {
		token, user, err := service.DemoLogin(ctx, persona, time.Now())
		if err != nil {
			t.Fatalf("%s sign-in failed: %v", persona, err)
		}
		identifier, role := demoIdentity(persona)
		claims, err := manager.Parse(token, time.Now())
		if err != nil || claims.Subject != user.ID.String() || claims.Role != role || user.Role != role || user.PaymentID != identifier {
			t.Fatalf("%s returned wrong stored identity or JWT", persona)
		}
		var coherent bool
		err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts a JOIN banks b ON b.id=a.bank_id WHERE a.user_id=$1 AND ((b.code='BANK-A' AND EXISTS(SELECT 1 FROM bank_a.accounts ba WHERE ba.id=a.bank_account_id AND ba.balance_paise=a.balance_paise)) OR (b.code='BANK-B' AND EXISTS(SELECT 1 FROM bank_b.accounts bb WHERE bb.id=a.bank_account_id AND bb.balance_paise=a.balance_paise))))`, user.ID).Scan(&coherent)
		if err != nil || !coherent {
			t.Fatal("demo identity has incoherent central/participant account")
		}
	}
	// Fail closed even when an expected identifier has been given a privileged role.
	if _, err = pool.Exec(ctx, `UPDATE users SET role='OPS_ADMIN' WHERE upi_id='viewer@transactx'`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, `UPDATE users SET role='OPS_VIEWER' WHERE upi_id='viewer@transactx'`); err != nil {
			t.Error("restore disposable viewer role failed")
		}
	}()
	if _, _, err = service.DemoLogin(ctx, "operator", time.Now()); err != ErrInvalidCredentials {
		t.Fatal("escalated stored role was accepted")
	}
}
