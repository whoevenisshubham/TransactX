package config

import "os"

type Config struct {
	Address     string
	DatabaseURL string
}

func Load() (Config, error) {
	return Config{
		Address:     getEnv("APP_ADDR", ":8080"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
	}, nil
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
