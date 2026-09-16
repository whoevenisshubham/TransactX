package auth

import (
	"strings"
	"testing"
	"time"
)

func TestJWTClaimsAndAlgorithm(t *testing.T) {
	manager, err := NewJWTManager(strings.Repeat("s", 32), "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1700000000, 0)
	token, err := manager.Issue("11111111-1111-4111-8111-111111111111", "CUSTOMER", now)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := manager.Parse(token, now)
	if err != nil || claims.Subject == "" || claims.Role != "CUSTOMER" || claims.ExpiresAt == nil || claims.IssuedAt == nil {
		t.Fatalf("claims = %+v, err = %v", claims, err)
	}
}

func TestJWTRejectsWrongSecretAndExpiry(t *testing.T) {
	now := time.Unix(1700000000, 0)
	manager, _ := NewJWTManager(strings.Repeat("s", 32), "test", time.Minute)
	token, _ := manager.Issue("11111111-1111-4111-8111-111111111111", "CUSTOMER", now)
	wrong, _ := NewJWTManager(strings.Repeat("x", 32), "test", time.Minute)
	if _, err := wrong.Parse(token, now); err == nil {
		t.Fatal("token signed with another secret was accepted")
	}
	if _, err := manager.Parse(token, now.Add(2*time.Minute)); err == nil {
		t.Fatal("expired token was accepted")
	}
}
