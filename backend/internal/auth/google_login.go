package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/transactx/backend/internal/users"
)

var oidcChallengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var oidcVerifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

const oidcLock int64 = 644091031

type GoogleLogin struct {
	service  *Service
	config   GoogleConfig
	provider googleIdentityProvider
}
type GoogleFlow struct {
	ProviderVerifier string
	NonceHash        []byte
	AppChallenge     string
	CreatedAt        time.Time
}
type GoogleExchangeResult struct {
	Token      string           `json:"token"`
	User       users.PublicUser `json:"user"`
	NewAccount bool             `json:"newAccount"`
}

func (s *Service) EnableGoogle(c GoogleConfig) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if s.provisioner == nil {
		return errors.New("Google sign-in requires atomic participant provisioning")
	}
	if s.defaultBankCode != "BANK-A" && s.defaultBankCode != "BANK-B" {
		return errors.New("Google sign-in requires a supported simulation bank")
	}
	s.google = &GoogleLogin{service: s, config: c, provider: newGoogleProvider(c)}
	return nil
}
func (s *Service) Google() *GoogleLogin       { return s.google }
func (g *GoogleLogin) FrontendOrigin() string { return g.config.FrontendOrigin }
func randomOIDC() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("cryptographic randomness unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func hashOIDC(value string) []byte { h := sha256.Sum256([]byte(value)); return h[:] }

// Admission is durable and global, independent of browser-controlled IP headers.
// Only three rows persist. Start limits and the row cap bound transient storage.
func (g *GoogleLogin) Admit(ctx context.Context, action string) error {
	limit := 12
	if action == "start" {
		limit = 6
	} else if action != "callback" && action != "exchange" {
		return ErrOIDCInvalid
	}
	var attempts int
	err := g.service.db.QueryRow(ctx, `INSERT INTO auth_oidc_admission(action,window_start,attempts) VALUES($1,CURRENT_TIMESTAMP,1)
 ON CONFLICT(action) DO UPDATE SET attempts=CASE WHEN auth_oidc_admission.window_start < CURRENT_TIMESTAMP-INTERVAL '1 minute' THEN 1 ELSE auth_oidc_admission.attempts+1 END,
 window_start=CASE WHEN auth_oidc_admission.window_start < CURRENT_TIMESTAMP-INTERVAL '1 minute' THEN CURRENT_TIMESTAMP ELSE auth_oidc_admission.window_start END
 WHERE auth_oidc_admission.attempts < $2 OR auth_oidc_admission.window_start < CURRENT_TIMESTAMP-INTERVAL '1 minute'
 RETURNING attempts`, action, limit).Scan(&attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrOIDCLimit
	}
	return err
}
func (g *GoogleLogin) Start(ctx context.Context, challenge string) (location, binding string, err error) {
	if !oidcChallengePattern.MatchString(challenge) {
		return "", "", ErrOIDCInvalid
	}
	state, nonce, verifier, binding := randomOIDC(), randomOIDC(), randomOIDC(), randomOIDC()
	location, err = g.provider.AuthorizationURL(ctx, state, nonce, verifier)
	if err != nil {
		return "", "", err
	}
	tx, err := g.service.db.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, oidcLock); err != nil {
		return "", "", err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM auth_login_flows WHERE expires_at<=CURRENT_TIMESTAMP`); err != nil {
		return "", "", err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM auth_login_flows`).Scan(&count); err != nil {
		return "", "", err
	}
	if count >= 100 {
		return "", "", ErrOIDCLimit
	}
	_, err = tx.Exec(ctx, `INSERT INTO auth_login_flows(state_hash,binding_hash,nonce_hash,provider_verifier,app_challenge,expires_at) VALUES($1,$2,$3,$4,$5,CURRENT_TIMESTAMP+INTERVAL '10 minutes')`, hashOIDC(state), hashOIDC(binding), hashOIDC(nonce), verifier, challenge)
	if err != nil {
		return "", "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", err
	}
	return location, binding, nil
}
func (g *GoogleLogin) ConsumeFlow(ctx context.Context, state, binding string) (GoogleFlow, error) {
	if !oidcChallengePattern.MatchString(state) || !oidcChallengePattern.MatchString(binding) {
		return GoogleFlow{}, ErrOIDCInvalid
	}
	var flow GoogleFlow
	err := g.service.db.QueryRow(ctx, `DELETE FROM auth_login_flows WHERE state_hash=$1 AND binding_hash=$2 AND expires_at>CURRENT_TIMESTAMP RETURNING provider_verifier,nonce_hash,app_challenge,created_at`, hashOIDC(state), hashOIDC(binding)).Scan(&flow.ProviderVerifier, &flow.NonceHash, &flow.AppChallenge, &flow.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return GoogleFlow{}, ErrOIDCInvalid
	}
	return flow, err
}
func (g *GoogleLogin) Complete(ctx context.Context, code string, flow GoogleFlow) (string, error) {
	if code == "" || len(code) > 4096 {
		return "", ErrOIDCInvalid
	}
	identity, err := g.provider.VerifyCode(ctx, code, flow.ProviderVerifier, flow.NonceHash, flow.CreatedAt)
	if err != nil {
		return "", ErrOIDCInvalid
	}
	if identity.Subject == "" || len(identity.Subject) > 255 {
		return "", ErrOIDCInvalid
	}
	return g.createExchange(ctx, identity.Subject, flow.AppChallenge)
}
func (g *GoogleLogin) createExchange(ctx context.Context, subject, challenge string) (string, error) {
	tx, err := g.service.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, oidcLock); err != nil {
		return "", err
	}
	var userID uuid.UUID
	var role string
	err = tx.QueryRow(ctx, `SELECT u.id,u.role FROM auth_external_identities i JOIN users u ON u.id=i.user_id WHERE i.issuer=$1 AND i.subject=$2`, GoogleIssuer, subject).Scan(&userID, &role)
	isNew := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !isNew {
		return "", err
	}
	if isNew {
		if _, err = tx.Exec(ctx, `INSERT INTO auth_external_identity_budget(singleton,day,daily,lifetime) VALUES(TRUE,(CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date,0,0) ON CONFLICT DO NOTHING`); err != nil {
			return "", err
		}
		var admitted bool
		err = tx.QueryRow(ctx, `UPDATE auth_external_identity_budget SET daily=CASE WHEN day=(CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date THEN daily+1 ELSE 1 END, day=(CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date,lifetime=lifetime+1
  WHERE singleton AND lifetime<50 AND (day<>(CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date OR daily<5) RETURNING TRUE`).Scan(&admitted)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrOIDCLimit
		}
		if err != nil {
			return "", err
		}
		userID = uuid.New()
		suffix := strings.ReplaceAll(userID.String(), "-", "")
		if _, err = tx.Exec(ctx, `INSERT INTO users(id,name,phone,upi_id,password_hash,role) VALUES($1,$2,NULL,$3,NULL,'CUSTOMER')`, userID, "Visitor "+suffix[:8], "visitor-"+suffix+"@transactx"); err != nil {
			return "", err
		}
		accountID := uuid.New()
		number := "TX-" + strings.ToUpper(strings.ReplaceAll(accountID.String(), "-", ""))[:16]
		var bankID uuid.UUID
		if err = tx.QueryRow(ctx, `SELECT id FROM banks WHERE code=$1 AND status='ACTIVE'`, g.service.defaultBankCode).Scan(&bankID); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO accounts(id,user_id,bank_id,bank_account_id,account_number,balance_paise,version,status) VALUES($1,$2,$3,$1,$4,0,0,'ACTIVE')`, accountID, userID, bankID, number); err != nil {
			return "", err
		}
		if err = g.service.provisioner.ProvisionAccount(ctx, tx, g.service.defaultBankCode, accountID, number); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO auth_external_identities(issuer,subject,provider,user_id) VALUES($1,$2,'google',$3)`, GoogleIssuer, subject, userID); err != nil {
			return "", err
		}
	} else if role != users.RoleCustomer {
		return "", ErrOIDCInvalid
	}
	if _, err = tx.Exec(ctx, `DELETE FROM auth_exchange_codes WHERE expires_at<=CURRENT_TIMESTAMP`); err != nil {
		return "", err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM auth_exchange_codes`).Scan(&count); err != nil {
		return "", err
	}
	if count >= 100 {
		return "", ErrOIDCLimit
	}
	appCode := randomOIDC()
	if _, err = tx.Exec(ctx, `INSERT INTO auth_exchange_codes(code_hash,user_id,app_challenge,is_new,expires_at) VALUES($1,$2,$3,$4,CURRENT_TIMESTAMP+INTERVAL '60 seconds')`, hashOIDC(appCode), userID, challenge, isNew); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return appCode, nil
}
func (g *GoogleLogin) Exchange(ctx context.Context, code, verifier string) (GoogleExchangeResult, error) {
	if !oidcChallengePattern.MatchString(code) || !oidcVerifierPattern.MatchString(verifier) {
		return GoogleExchangeResult{}, ErrOIDCInvalid
	}
	challenge := base64.RawURLEncoding.EncodeToString(hashOIDC(verifier))
	tx, err := g.service.db.Begin(ctx)
	if err != nil {
		return GoogleExchangeResult{}, err
	}
	defer tx.Rollback(ctx)
	var userID uuid.UUID
	var isNew bool
	err = tx.QueryRow(ctx, `DELETE FROM auth_exchange_codes WHERE code_hash=$1 AND app_challenge=$2 AND expires_at>CURRENT_TIMESTAMP RETURNING user_id,is_new`, hashOIDC(code), challenge).Scan(&userID, &isNew)
	if errors.Is(err, pgx.ErrNoRows) {
		return GoogleExchangeResult{}, ErrOIDCInvalid
	}
	if err != nil {
		return GoogleExchangeResult{}, err
	}
	var user users.User
	if err = tx.QueryRow(ctx, `SELECT id,name,upi_id,role FROM users WHERE id=$1`, userID).Scan(&user.ID, &user.Name, &user.PaymentID, &user.Role); err != nil || user.Role != users.RoleCustomer {
		return GoogleExchangeResult{}, ErrOIDCInvalid
	}
	token, err := g.service.jwt.Issue(user.ID.String(), user.Role, time.Now())
	if err != nil {
		return GoogleExchangeResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return GoogleExchangeResult{}, err
	}
	return GoogleExchangeResult{Token: token, User: user.Public(), NewAccount: isNew}, nil
}
