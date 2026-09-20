package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Address         string
	DatabaseURL     string
	JWTSecret       string
	JWTIssuer       string
	JWTLifetime     time.Duration
	DefaultBankCode string
	DevelopmentMode bool
}

func Load() (Config, error) {
	developmentMode, err := strconv.ParseBool(getEnv("APP_DEVELOPMENT", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("APP_DEVELOPMENT must be true or false: %w", err)
	}

	jwtLifetime, err := time.ParseDuration(getEnv("JWT_LIFETIME", "15m"))
	if err != nil || jwtLifetime <= 0 {
		return Config{}, errors.New("JWT_LIFETIME must be a positive duration")
	}

	cfg := Config{
		Address:         getEnv("APP_ADDR", ":8080"),
		DatabaseURL:     getEnv("DATABASE_URL", ""),
		JWTSecret:       os.Getenv("JWT_SECRET"),
		JWTIssuer:       getEnv("JWT_ISSUER", "transactx-api"),
		JWTLifetime:     jwtLifetime,
		DefaultBankCode: getEnv("DEFAULT_BANK_CODE", "BANK-DEV-001"),
		DevelopmentMode: developmentMode,
	}
	if !developmentMode && len([]byte(cfg.JWTSecret)) < 32 {
		return Config{}, errors.New("JWT_SECRET must contain at least 32 bytes outside development mode")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
