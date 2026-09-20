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

	"github.com/transactx/backend/internal/bankservice"
	"github.com/transactx/backend/internal/database"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	databaseURL := env("BANK_B_DATABASE_URL", os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		logger.Error("BANK_B_DATABASE_URL or DATABASE_URL is required")
		os.Exit(1)
	}
	db, err := database.NewPool(context.Background(), databaseURL)
	if err != nil {
		logger.Error("connect to PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	server := &http.Server{
		Addr:              env("BANK_B_ADDR", ":8082"),
		Handler:           bankservice.Handler(bankservice.NewParticipantService(db, "BANK-B", "bank_b")),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("Bank B listening", "address", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("Bank B stopped", "error", err)
			os.Exit(1)
		}
	case <-shutdownContext.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("Bank B shutdown failed", "error", err)
			os.Exit(1)
		}
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
