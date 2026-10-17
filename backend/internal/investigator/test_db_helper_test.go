package investigator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func findRepoMigrationsDir(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"../../migrations",
		"../migrations",
		"migrations",
		"backend/migrations",
	}
	for _, c := range candidates {
		clean := filepath.Clean(c)
		if info, err := os.Stat(clean); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(clean)
			return abs
		}
	}
	t.Fatal("could not find migrations directory from test")
	return ""
}

func setupDisposableDB(t *testing.T, databaseURL string) (string, func()) {
	t.Helper()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("failed to parse database URL: %v", err)
	}

	origDB := config.ConnConfig.Database
	adminPool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("failed to connect to admin database: %v", err)
	}

	dbName := "tx_corpus_conf_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = adminPool.Exec(context.Background(), fmt.Sprintf("CREATE DATABASE %s;", dbName))
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to create disposable database %s: %v", dbName, err)
	}

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = adminPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);", dbName))
		adminPool.Close()
	}

	config.ConnConfig.Database = dbName
	testDBURL := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		config.ConnConfig.User,
		config.ConnConfig.Password,
		config.ConnConfig.Host,
		config.ConnConfig.Port,
		dbName,
	)

	_ = origDB
	return testDBURL, cleanup
}
