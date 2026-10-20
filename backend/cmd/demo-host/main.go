// demo-host is a single-replica, quota-conscious public portfolio gateway.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/transactx/backend/internal/demohost"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// Validate required configuration without ever logging secret values.
	db, err := url.Parse(os.Getenv("DATABASE_URL"))
	if err != nil || db == nil || (db.Scheme != "postgres" && db.Scheme != "postgresql") || db.Hostname() == "" || strings.Contains(db.Hostname(), "-pooler") {
		logger.Error("DATABASE_URL must be a direct PostgreSQL connection, not a transaction pooler")
		os.Exit(1)
	}
	if strings.HasSuffix(db.Hostname(), "neon.tech") && db.Query().Get("sslmode") != "verify-full" && db.Query().Get("sslmode") != "require" {
		logger.Error("Neon connection must require TLS")
		os.Exit(1)
	}
	expected := os.Getenv("TX_DEMO_EXPECTED_DATABASE")
	if expected == "" || expected == "transactx" || expected == "postgres" || strings.TrimPrefix(db.Path, "/") != expected {
		logger.Error("TX_DEMO_EXPECTED_DATABASE must match a separate non-source demo database")
		os.Exit(1)
	}
	for _, key := range []string{"JWT_SECRET", "BANK_A_SERVICE_TOKEN", "BANK_B_SERVICE_TOKEN"} {
		if len(os.Getenv(key)) < 32 {
			logger.Error("missing or short demo secret", "key", key)
			os.Exit(1)
		}
	}
	origins := strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",")
	for _, value := range origins {
		u, e := url.Parse(strings.TrimSpace(value))
		if e != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			logger.Error("configure exact HTTPS CORS_ALLOWED_ORIGINS")
			os.Exit(1)
		}
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "10000"
	}
	if n, e := strconv.Atoi(port); e != nil || n < 1024 || n > 65535 {
		logger.Error("invalid PORT")
		os.Exit(1)
	}
	bin := os.Getenv("TX_DEMO_BIN_DIR")
	if bin == "" {
		bin = "/app/bin"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	common := []string{"APP_DEVELOPMENT=false", "TX_SIMULATION_MODE=true", "TX_ALLOW_REGISTRATION=false", "TX_CHAOS_ENABLED=false", "TX_RECOVERY_CONTROL_ENABLED=false", "TX_PARTICIPANT_PROVISIONING=false", "TX_DB_MAX_CONNS=4", "TX_PAYMENT_CONCURRENCY=1", "TX_RECOVERY_CONCURRENCY=1", "BANK_A_ADDR=127.0.0.1:18081", "BANK_B_ADDR=127.0.0.1:18082", "BANK_A_URL=http://127.0.0.1:18081", "BANK_B_URL=http://127.0.0.1:18082", "APP_ADDR=127.0.0.1:18080"}
	// Do not inherit an unrelated bank DSN from a developer's shell.
	common = append(common, "BANK_A_DATABASE_URL="+os.Getenv("DATABASE_URL"), "BANK_B_DATABASE_URL="+os.Getenv("DATABASE_URL"))
	if sha := os.Getenv("RENDER_GIT_COMMIT"); sha != "" {
		common = append(common, "TX_BUILD_SHA="+sha)
	}
	services := []demohost.Service{
		{Name: "bank-a", Address: "127.0.0.1:18081", Env: common},
		{Name: "bank-b", Address: "127.0.0.1:18082", Env: common},
		{Name: "api", Address: "127.0.0.1:18080", Env: common},
		{Name: "worker", Env: append(append([]string{}, common...), "TX_RECOVERY_ENABLED=true")},
	}
	processes := demohost.NewProcesses(ctx, bin, services, 90*time.Second, logger)
	target, _ := url.Parse("http://127.0.0.1:18080")
	server := &http.Server{Addr: ":" + port, Handler: demohost.NewGateway(target, processes, origins), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	logger.Info("demo gateway listening", "port", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("demo gateway stopped")
		stop()
	}
	stop()
	processes.Wait()
}
