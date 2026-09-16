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
	databaseURL := env("BANK_A_DATABASE_URL", os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		logger.Error("BANK_A_DATABASE_URL or DATABASE_URL is required")
		os.Exit(1)
	}
	db, err := database.NewPool(context.Background(), databaseURL)
	if err != nil {
		logger.Error("connect to Bank A database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	server := &http.Server{
		Addr:              env("BANK_A_ADDR", ":8081"),
		Handler:           bankservice.Handler(bankservice.NewService(db)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("Bank A listening", "address", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("Bank A stopped", "error", err)
			os.Exit(1)
		}
	case <-shutdownContext.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			logger.Error("shutdown Bank A", "error", err)
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
