package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/transactx/backend/internal/database"
)

func findMigrationsDir(userSpecified string) string {
	if userSpecified != "" {
		return userSpecified
	}
	if envDir := os.Getenv("TX_MIGRATIONS_DIR"); envDir != "" {
		return envDir
	}

	candidates := []string{
		"migrations",
		"./migrations",
		"../migrations",
		"backend/migrations",
		"../../backend/migrations",
	}

	for _, c := range candidates {
		clean := filepath.Clean(c)
		if info, err := os.Stat(clean); err == nil && info.IsDir() {
			return clean
		}
	}
	return "migrations"
}

func main() {
	var (
		databaseURL string
		dirFlag     string
		timeoutFlag time.Duration
		targetDB    string
	)

	flag.StringVar(&databaseURL, "database-url", "", "PostgreSQL database connection URL (falls back to DATABASE_URL env)")
	flag.StringVar(&dirFlag, "dir", "", "Path to migrations directory (falls back to TX_MIGRATIONS_DIR or ./migrations)")
	flag.DurationVar(&timeoutFlag, "timeout", 15*time.Second, "Lock acquisition timeout")
	flag.StringVar(&targetDB, "target-db", "", "Optional target database name validation")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: migrate [flags] <command> [arguments]\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  up                  Apply all pending migrations\n")
		fmt.Fprintf(os.Stderr, "  status              Display current migration status and pending files\n")
		fmt.Fprintf(os.Stderr, "  verify              Verify applied migration checksums and sequence\n")
		fmt.Fprintf(os.Stderr, "  baseline [version]  Establish baseline without executing SQL (default: 000021)\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	command := flag.Arg(0)

	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		slog.Error("database URL is required via -database-url or DATABASE_URL environment variable")
		os.Exit(1)
	}

	migrationsDir := findMigrationsDir(dirFlag)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pool, err := database.NewPool(ctx, databaseURL)
	if err != nil {
		slog.Error("failed to connect to database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	opts := []database.MigratorOption{
		database.WithLockTimeout(timeoutFlag),
	}
	if targetDB != "" {
		opts = append(opts, database.WithTargetDatabase(targetDB))
	}

	migrator, err := database.NewMigrator(pool, migrationsDir, opts...)
	if err != nil {
		slog.Error("failed to initialize migrator", slog.String("error", err.Error()))
		os.Exit(1)
	}

	switch command {
	case "up":
		applied, err := migrator.Up(ctx)
		if err != nil {
			slog.Error("migration failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		if applied == 0 {
			slog.Info("database is up to date (0 pending migrations)")
		} else {
			slog.Info("migrations completed successfully", slog.Int("applied", applied))
		}

	case "status":
		status, err := migrator.Status(ctx)
		if err != nil {
			slog.Error("failed to retrieve migration status", slog.String("error", err.Error()))
			os.Exit(1)
		}

		fmt.Printf("Migration Status (Total Files: %d)\n", status.TotalFiles)
		fmt.Printf("Current Version: %s\n\n", status.CurrentVersion)

		fmt.Printf("Applied Migrations (%d):\n", len(status.Applied))
		for _, rec := range status.Applied {
			fmt.Printf("  [✓] %s - %s (%s, took %dms)\n",
				rec.Version, rec.Name, rec.AppliedAt.Format(time.RFC3339), rec.ExecutionTimeMs)
		}

		fmt.Printf("\nPending Migrations (%d):\n", len(status.Pending))
		for _, file := range status.Pending {
			fmt.Printf("  [ ] %s - %s\n", file.Version, file.Name)
		}

	case "verify":
		if err := migrator.Verify(ctx); err != nil {
			slog.Error("migration verification failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		slog.Info("all applied migrations verified successfully")

	case "baseline":
		baselineVer := "000021"
		if flag.NArg() >= 2 {
			baselineVer = strings.TrimSpace(flag.Arg(1))
		}
		if err := migrator.Baseline(ctx, baselineVer); err != nil {
			slog.Error("baseline operation failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		slog.Info("baseline established successfully", slog.String("baselineVersion", baselineVer))

	default:
		slog.Error("unknown command", slog.String("command", command))
		flag.Usage()
		os.Exit(1)
	}
}
