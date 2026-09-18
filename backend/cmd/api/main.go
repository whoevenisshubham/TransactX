package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/config"
	"github.com/transactx/backend/internal/database"
	apihttp "github.com/transactx/backend/internal/http"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	db, err := database.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect to PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	jwtManager, err := auth.NewJWTManager(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTLifetime)
	if err != nil {
		logger.Error("configure JWT", "error", err)
		os.Exit(1)
	}
	authService := auth.NewService(db, jwtManager, cfg.DefaultBankCode)

	adapters := make(map[uuid.UUID]bank.BankAdapter)
	configureBank := func(url, code string) {
		if url == "" {
			return
		}
		var bankID uuid.UUID
		if err := db.QueryRow(context.Background(), `SELECT id FROM banks WHERE code = $1 AND status = 'ACTIVE'`, code).Scan(&bankID); err != nil {
			logger.Error("resolve configured bank", "code", code, "error", err)
			os.Exit(1)
		}
		adapter, clientErr := bank.NewHTTPClient(url, nil)
		if clientErr != nil {
			logger.Error("configure bank adapter", "code", code, "error", clientErr)
			os.Exit(1)
		}
		adapters[bankID] = adapter
	}
	configureBank(os.Getenv("BANK_A_URL"), getEnv("BANK_A_CODE", "BANK-A"))
	configureBank(os.Getenv("BANK_B_URL"), getEnv("BANK_B_CODE", "BANK-B"))

	var handler http.Handler
	if len(adapters) == 0 {
		handler = apihttp.NewHandler(db, logger, authService, jwtManager)
	} else {
		handler = apihttp.NewHandlerWithBankAdapters(db, logger, authService, jwtManager, adapters)
	}
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("API listening", "address", cfg.Address)
		serverErrors <- server.ListenAndServe()
	}()

	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("API server stopped", "error", err)
			os.Exit(1)
		}
	case <-shutdownContext.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			logger.Error("shutdown API server", "error", err)
			os.Exit(1)
		}
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
