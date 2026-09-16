package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type JWTManager struct {
	secret   []byte
	issuer   string
	lifetime time.Duration
}

type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

func NewJWTManager(secret, issuer string, lifetime time.Duration) (*JWTManager, error) {
	if len([]byte(secret)) < 32 || issuer == "" || lifetime <= 0 {
		return nil, errors.New("invalid JWT configuration")
	}
	return &JWTManager{secret: []byte(secret), issuer: issuer, lifetime: lifetime}, nil
}

func (manager *JWTManager) Issue(userID, role string, now time.Time) (string, error) {
	claims := Claims{Role: role, RegisteredClaims: jwt.RegisteredClaims{
		Subject: userID, Issuer: manager.issuer, IssuedAt: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(manager.lifetime)), ID: uuid.NewString(),
	}}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(manager.secret)
}

func (manager *JWTManager) Parse(tokenString string, now time.Time) (Claims, error) {
	parsed, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unsupported JWT signing algorithm")
		}
		return manager.secret, nil
	}, jwt.WithIssuer(manager.issuer), jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil {
		return Claims{}, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || claims.Subject == "" || claims.Role == "" || claims.ExpiresAt == nil || claims.IssuedAt == nil {
		return Claims{}, errors.New("missing required JWT claims")
	}
	if _, err := uuid.Parse(claims.Subject); err != nil {
		return Claims{}, errors.New("invalid JWT subject")
	}
	return *claims, nil
}
