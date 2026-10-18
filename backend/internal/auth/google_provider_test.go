package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

func TestGoogleSignedTokenValidationAndKeyRotation(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	second, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	public := jose.JSONWebKey{Key: &key.PublicKey, KeyID: "one", Algorithm: "RS256", Use: "sig"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if err := json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{public}}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	verifier := oidc.NewVerifier(GoogleIssuer, oidc.NewRemoteKeySet(context.Background(), server.URL), &oidc.Config{ClientID: "client", SupportedSigningAlgs: []string{"RS256"}})
	now := time.Now().Truncate(time.Second)
	valid := func() jwt.MapClaims {
		return jwt.MapClaims{"iss": GoogleIssuer, "sub": "subject-one", "aud": "client", "azp": "client", "iat": now.Unix(), "exp": now.Add(time.Minute).Unix(), "nonce": "fixture-nonce", "email_verified": true}
	}
	sign := func(claims jwt.MapClaims, key *rsa.PrivateKey, kid string) string {
		t.Helper()
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		token.Header["kid"] = kid
		raw, err := token.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	if identity, err := verifyGoogleToken(context.Background(), verifier, sign(valid(), key, "one"), hashOIDC("fixture-nonce"), now, "client", now); err != nil || identity.Subject != "subject-one" {
		t.Fatalf("valid token: %v", err)
	}
	cases := map[string]func(jwt.MapClaims){
		"issuer":                         func(c jwt.MapClaims) { c["iss"] = "https://attacker.invalid" },
		"audience":                       func(c jwt.MapClaims) { c["aud"] = "another-client" },
		"authorized party":               func(c jwt.MapClaims) { c["azp"] = "another-client" },
		"multiple audiences without azp": func(c jwt.MapClaims) { c["aud"] = []string{"client", "other"}; delete(c, "azp") },
		"nonce":                          func(c jwt.MapClaims) { c["nonce"] = "wrong" },
		"missing nonce":                  func(c jwt.MapClaims) { delete(c, "nonce") },
		"expired":                        func(c jwt.MapClaims) { c["exp"] = now.Add(-time.Minute).Unix() },
		"future issued":                  func(c jwt.MapClaims) { c["iat"] = now.Add(2 * time.Minute).Unix() },
		"old issued":                     func(c jwt.MapClaims) { c["iat"] = now.Add(-2 * time.Minute).Unix() },
		"empty subject":                  func(c jwt.MapClaims) { c["sub"] = "" },
		"unverified email":               func(c jwt.MapClaims) { c["email_verified"] = false },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := valid()
			mutate(c)
			if _, err := verifyGoogleToken(context.Background(), verifier, sign(c, key, "one"), hashOIDC("fixture-nonce"), now, "client", now); err == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}
	if _, err := verifyGoogleToken(context.Background(), verifier, sign(valid(), second, "one"), hashOIDC("fixture-nonce"), now, "client", now); err == nil {
		t.Fatal("wrong signature accepted")
	}
	mu.Lock()
	public = jose.JSONWebKey{Key: &second.PublicKey, KeyID: "two", Algorithm: "RS256", Use: "sig"}
	mu.Unlock()
	if _, err := verifyGoogleToken(context.Background(), verifier, sign(valid(), second, "two"), hashOIDC("fixture-nonce"), now, "client", now); err != nil {
		t.Fatalf("key rotation: %v", err)
	}
}

func TestGoogleConfigurationRejectsRedirectInjection(t *testing.T) {
	valid := GoogleConfig{ClientID: "client", ClientSecret: "fixture-only", RedirectURI: "https://api.example.test/api/auth/oidc/google/callback", FrontendOrigin: "https://front.example.test"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"http://api.example.test/api/auth/oidc/google/callback", "https://api.example.test/other", "https://user:secret@api.example.test/api/auth/oidc/google/callback", "https://api.example.test/api/auth/oidc/google/callback?next=https://evil.test"} {
		c := valid
		c.RedirectURI = bad
		if c.Validate() == nil {
			t.Fatal("unsafe callback accepted")
		}
	}
	for _, bad := range []string{"https://front.example.test/", "https://front.example.test/path", "http://front.example.test", "https://user@front.example.test", "https://front.example.test#fragment"} {
		c := valid
		c.FrontendOrigin = bad
		if c.Validate() == nil {
			t.Fatal("unsafe origin accepted")
		}
	}
}
