package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixtureGoogle struct{}

func (fixtureGoogle) AuthorizationURL(_ context.Context, state, nonce, verifier string) (string, error) {
	return "https://accounts.google.com/o/oauth2/v2/auth?" + url.Values{"state": {state}, "nonce": {nonce}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hashOIDC(verifier))}, "code_challenge_method": {"S256"}}.Encode(), nil
}
func (fixtureGoogle) VerifyCode(_ context.Context, code, verifier string, nonce []byte, created time.Time) (verifiedGoogleIdentity, error) {
	if code != "fixture-valid" {
		return verifiedGoogleIdentity{}, ErrOIDCInvalid
	}
	return verifiedGoogleIdentity{Subject: "fixture-subject"}, nil
}

type failedGoogleProvisioner struct{}

func (failedGoogleProvisioner) ProvisionAccount(context.Context, pgx.Tx, string, uuid.UUID, string) error {
	return errors.New("controlled provisioning failure")
}

func TestGoogleDurableFlowsAndProvisioning(t *testing.T) {
	dsn := os.Getenv("OIDC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("OIDC_TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Hostname() != "127.0.0.1" || !strings.HasPrefix(parsed.Path, "/txv3_oidc_") {
		t.Fatal("OIDC tests require an explicitly isolated loopback txv3_oidc_ database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	var preexisting int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&preexisting); err != nil || preexisting != 0 {
		t.Fatal("OIDC fixture requires an empty disposable user namespace")
	}
	bankID := uuid.New()
	exec(`INSERT INTO banks(id,code,name,status) VALUES($1,'BANK-A','OIDC fixture bank','ACTIVE') ON CONFLICT(code) DO NOTHING`, bankID)
	jwtManager, err := NewJWTManager(strings.Repeat("g", 32), "oidc-test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(pool, jwtManager, "BANK-A").WithParticipantProvisioner(NewSimulationParticipantAccountProvisioner())
	if err := service.EnableGoogle(GoogleConfig{ClientID: "fixture-client", ClientSecret: "fixture-only", RedirectURI: "https://api.example.test/api/auth/oidc/google/callback", FrontendOrigin: "https://front.example.test"}); err != nil {
		t.Fatal(err)
	}
	g := service.Google()
	g.provider = fixtureGoogle{}
	verifier := randomOIDC()
	challenge := base64.RawURLEncoding.EncodeToString(hashOIDC(verifier))
	t.Cleanup(func() {
		for _, sql := range []string{`DELETE FROM auth_exchange_codes`, `DELETE FROM auth_login_flows`, `DELETE FROM auth_external_identities`, `DELETE FROM bank_a.accounts WHERE id IN(SELECT id FROM accounts)`, `DELETE FROM accounts`, `DELETE FROM users`, `DELETE FROM auth_external_identity_budget`, `DELETE FROM auth_oidc_admission`} {
			if _, err := pool.Exec(ctx, sql); err != nil {
				t.Errorf("disposable OIDC cleanup failed: %v", err)
			}
		}
	})
	t.Run("one-time state cookie binding and expiry", func(t *testing.T) {
		location, binding, err := g.Start(ctx, challenge)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(location)
		state := u.Query().Get("state")
		if u.Query().Get("code_challenge_method") != "S256" {
			t.Fatal("provider PKCE absent")
		}
		if _, err := g.ConsumeFlow(ctx, state, randomOIDC()); !errors.Is(err, ErrOIDCInvalid) {
			t.Fatal("wrong cookie accepted")
		}
		flow, err := g.ConsumeFlow(ctx, state, binding)
		if err != nil {
			t.Fatal(err)
		}
		if base64.RawURLEncoding.EncodeToString(hashOIDC(flow.ProviderVerifier)) != u.Query().Get("code_challenge") {
			t.Fatal("provider verifier mismatch")
		}
		if flow.ProviderVerifier == verifier {
			t.Fatal("provider and application verifiers reused")
		}
		if _, err := g.ConsumeFlow(ctx, state, binding); !errors.Is(err, ErrOIDCInvalid) {
			t.Fatal("state replay accepted")
		}
		location, binding, err = g.Start(ctx, challenge)
		if err != nil {
			t.Fatal(err)
		}
		u, _ = url.Parse(location)
		state = u.Query().Get("state")
		exec(`UPDATE auth_login_flows SET expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second' WHERE state_hash=$1`, hashOIDC(state))
		if _, err := g.ConsumeFlow(ctx, state, binding); !errors.Is(err, ErrOIDCInvalid) {
			t.Fatal("expired state accepted")
		}
		if _, _, err := g.Start(ctx, "//evil.test"); err == nil {
			t.Fatal("invalid challenge accepted")
		}
	})
	t.Run("concurrent first identity and atomic exchange", func(t *testing.T) {
		var wg sync.WaitGroup
		codes := make(chan string, 8)
		failures := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				code, err := g.createExchange(ctx, "fixture-subject", challenge)
				if err != nil {
					failures <- err
				} else {
					codes <- code
				}
			}()
		}
		wg.Wait()
		close(codes)
		close(failures)
		for err := range failures {
			t.Fatal(err)
		}
		var code string
		for value := range codes {
			if code == "" {
				code = value
			}
		}
		if _, err := g.Exchange(ctx, code, randomOIDC()); !errors.Is(err, ErrOIDCInvalid) {
			t.Fatal("wrong application verifier accepted")
		}
		var successes atomic.Int32
		results := make(chan GoogleExchangeResult, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				result, err := g.Exchange(ctx, code, verifier)
				if err == nil {
					successes.Add(1)
					results <- result
				} else if !errors.Is(err, ErrOIDCInvalid) {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		close(results)
		if successes.Load() != 1 {
			t.Fatalf("one-time exchange successes=%d", successes.Load())
		}
		result := <-results
		if result.User.Role != "CUSTOMER" || !strings.HasPrefix(result.User.Name, "Visitor ") {
			t.Fatal("unsafe public identity")
		}
		identity, err := jwtManager.Parse(result.Token, time.Now())
		if err != nil || identity.Role != "CUSTOMER" {
			t.Fatalf("ordinary customer JWT: %v", err)
		}
		var users, identities, accounts int
		var central, opening, participant int64
		var nullPhone, nullPassword bool
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM auth_external_identities),(SELECT count(*) FROM accounts),a.balance_paise,a.opening_balance_paise,b.balance_paise,u.phone IS NULL,u.password_hash IS NULL FROM accounts a JOIN users u ON u.id=a.user_id JOIN bank_a.accounts b ON b.id=a.bank_account_id WHERE u.id=$1`, result.User.ID).Scan(&users, &identities, &accounts, &central, &opening, &participant, &nullPhone, &nullPassword); err != nil {
			t.Fatal(err)
		}
		if users != 1 || identities != 1 || accounts != 1 || central != 0 || opening != 0 || participant != 0 || !nullPhone || !nullPassword {
			t.Fatal("provisioning invariant failed")
		}
		if _, _, err := service.Login(ctx, result.User.PaymentID, "", time.Now()); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal("passwordless identity accepted a local password")
		}
	})
	t.Run("quota existing login and failed provisioning rollback", func(t *testing.T) {
		exec(`UPDATE auth_external_identity_budget SET daily=5,lifetime=1`)
		if _, err := g.createExchange(ctx, "daily-over-cap", challenge); !errors.Is(err, ErrOIDCLimit) {
			t.Fatal("daily cap bypassed")
		}
		exec(`UPDATE auth_external_identity_budget SET daily=1,lifetime=50`)
		if _, err := g.createExchange(ctx, "lifetime-over-cap", challenge); !errors.Is(err, ErrOIDCLimit) {
			t.Fatal("lifetime cap bypassed")
		}
		exec(`UPDATE auth_external_identity_budget SET daily=5,lifetime=50`)
		if _, err := g.createExchange(ctx, "fixture-subject", challenge); err != nil {
			t.Fatalf("existing identity blocked by new account cap: %v", err)
		}
		if _, err := g.createExchange(ctx, "new-over-cap", challenge); !errors.Is(err, ErrOIDCLimit) {
			t.Fatal("new identity cap bypassed")
		}
		exec(`UPDATE auth_external_identity_budget SET daily=1,lifetime=1`)
		service.provisioner = failedGoogleProvisioner{}
		if _, err := g.createExchange(ctx, "failed-provision", challenge); err == nil {
			t.Fatal("failed participant provisioning committed")
		}
		service.provisioner = NewSimulationParticipantAccountProvisioner()
		var users, daily, lifetime int
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users),daily,lifetime FROM auth_external_identity_budget WHERE singleton`).Scan(&users, &daily, &lifetime); err != nil {
			t.Fatal(err)
		}
		if users != 1 || daily != 1 || lifetime != 1 {
			t.Fatal("failed provisioning consumed quota or left a user")
		}
		exec(`UPDATE users SET role='OPS_ADMIN' WHERE id IN(SELECT user_id FROM auth_external_identities)`)
		if _, err := g.createExchange(ctx, "fixture-subject", challenge); !errors.Is(err, ErrOIDCInvalid) {
			t.Fatal("Google elevated role accepted")
		}
		exec(`UPDATE users SET role='CUSTOMER' WHERE id IN(SELECT user_id FROM auth_external_identities)`)
	})
	t.Run("expired code and existing password sign-in", func(t *testing.T) {
		code, err := g.createExchange(ctx, "fixture-subject", challenge)
		if err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE auth_exchange_codes SET expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second' WHERE code_hash=$1`, hashOIDC(code))
		if _, err := g.Exchange(ctx, code, verifier); !errors.Is(err, ErrOIDCInvalid) {
			t.Fatal("expired application code accepted")
		}
		user, err := service.Register(ctx, RegisterInput{Name: "Password Fixture", Phone: "fixture-phone", PaymentIdentifier: "password-fixture@transactx", Password: "fixture-password-123", Role: "CUSTOMER"})
		if err != nil {
			t.Fatal(err)
		}
		if _, got, err := service.Login(ctx, user.PaymentID, "fixture-password-123", time.Now()); err != nil || got.ID != user.ID {
			t.Fatal("existing password login regressed")
		}
	})
	t.Run("durable admission ignores process-local reset", func(t *testing.T) {
		for i := 0; i < 6; i++ {
			if err := g.Admit(ctx, "start"); err != nil {
				t.Fatal(err)
			}
		}
		another := *g
		if !errors.Is(another.Admit(ctx, "start"), ErrOIDCLimit) {
			t.Fatal("new service instance reset admission")
		}
	})
}
