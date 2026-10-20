package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const GoogleIssuer = "https://accounts.google.com"

var ErrOIDCInvalid = errors.New("sign-in could not be verified")
var ErrOIDCLimit = errors.New("sign-in admission limit reached")

type GoogleConfig struct{ ClientID, ClientSecret, RedirectURI, FrontendOrigin string }

func (c GoogleConfig) Validate() error {
	if strings.TrimSpace(c.ClientID) == "" || strings.TrimSpace(c.ClientSecret) == "" {
		return errors.New("Google client configuration is required")
	}
	redirect, err := url.Parse(c.RedirectURI)
	if err != nil || redirect.Scheme != "https" || redirect.Host == "" || redirect.User != nil || redirect.RawQuery != "" || redirect.Fragment != "" || redirect.Path != "/api/auth/oidc/google/callback" {
		return errors.New("Google redirect must be the exact HTTPS API callback")
	}
	front, err := url.Parse(c.FrontendOrigin)
	if err != nil || front.Scheme != "https" || front.Host == "" || front.User != nil || front.RawQuery != "" || front.Fragment != "" || front.Path != "" {
		return errors.New("OAuth frontend must be a plain HTTPS origin")
	}
	return nil
}

type verifiedGoogleIdentity struct{ Subject string }
type googleIdentityProvider interface {
	AuthorizationURL(context.Context, string, string, string) (string, error)
	VerifyCode(context.Context, string, string, []byte, time.Time) (verifiedGoogleIdentity, error)
}
type googleProvider struct {
	config   GoogleConfig
	client   *http.Client
	mu       sync.Mutex
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func newGoogleProvider(c GoogleConfig) *googleProvider {
	return &googleProvider{config: c, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (p *googleProvider) discover(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.oauth != nil {
		return nil
	}
	ctx = oidc.ClientContext(ctx, p.client)
	provider, err := oidc.NewProvider(ctx, GoogleIssuer)
	if err != nil {
		return ErrOIDCInvalid
	}
	var metadata struct {
		JWKS string `json:"jwks_uri"`
	}
	if provider.Claims(&metadata) != nil || metadata.JWKS != "https://www.googleapis.com/oauth2/v3/certs" {
		return ErrOIDCInvalid
	}
	endpoint := provider.Endpoint()
	if endpoint.AuthURL != "https://accounts.google.com/o/oauth2/v2/auth" || endpoint.TokenURL != "https://oauth2.googleapis.com/token" {
		return ErrOIDCInvalid
	}
	p.oauth = &oauth2.Config{ClientID: p.config.ClientID, ClientSecret: p.config.ClientSecret, RedirectURL: p.config.RedirectURI, Scopes: []string{oidc.ScopeOpenID, "email"}, Endpoint: endpoint}
	p.verifier = provider.Verifier(&oidc.Config{ClientID: p.config.ClientID, SupportedSigningAlgs: []string{"RS256"}})
	return nil
}
func (p *googleProvider) AuthorizationURL(ctx context.Context, state, nonce, verifier string) (string, error) {
	if err := p.discover(ctx); err != nil {
		return "", err
	}
	return p.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}
func (p *googleProvider) VerifyCode(ctx context.Context, code, verifier string, nonceHash []byte, created time.Time) (verifiedGoogleIdentity, error) {
	if err := p.discover(ctx); err != nil {
		return verifiedGoogleIdentity{}, err
	}
	ctx = oidc.ClientContext(ctx, p.client)
	token, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return verifiedGoogleIdentity{}, ErrOIDCInvalid
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || len(raw) > 32<<10 {
		return verifiedGoogleIdentity{}, ErrOIDCInvalid
	}
	return verifyGoogleToken(ctx, p.verifier, raw, nonceHash, created, p.config.ClientID, time.Now())
}

// Cryptographic verification and rotating JWKS belong to go-oidc; the protocol
// nonce, authorized party, verified-email policy and issued time are checked here.
func verifyGoogleToken(ctx context.Context, verifier *oidc.IDTokenVerifier, raw string, nonceHash []byte, created time.Time, clientID string, now time.Time) (verifiedGoogleIdentity, error) {
	token, err := verifier.Verify(ctx, raw)
	if err != nil {
		return verifiedGoogleIdentity{}, ErrOIDCInvalid
	}
	var claims struct {
		AuthorizedParty string `json:"azp"`
		EmailVerified   bool   `json:"email_verified"`
	}
	nonce := sha256.Sum256([]byte(token.Nonce))
	if token.Claims(&claims) != nil || !claims.EmailVerified || token.Subject == "" || len(token.Subject) > 255 ||
		(token.Issuer != GoogleIssuer && token.Issuer != "accounts.google.com") ||
		token.IssuedAt.IsZero() || token.IssuedAt.After(now.Add(time.Minute)) || token.IssuedAt.Before(created.Add(-time.Minute)) ||
		subtle.ConstantTimeCompare(nonce[:], nonceHash) != 1 ||
		(len(token.Audience) > 1 && claims.AuthorizedParty == "") || (claims.AuthorizedParty != "" && claims.AuthorizedParty != clientID) {
		return verifiedGoogleIdentity{}, ErrOIDCInvalid
	}
	return verifiedGoogleIdentity{Subject: token.Subject}, nil
}
