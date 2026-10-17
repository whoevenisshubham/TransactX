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

	"github.com/transactx/backend/internal/app"
	"github.com/transactx/backend/internal/config"
	"github.com/transactx/backend/internal/telemetry"
)

func main() {
	jsonHandler := slog.NewJSONHandler(os.Stdout, nil)
	logger := slog.New(telemetry.NewTraceCorrelatingHandler(jsonHandler))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	env, err := app.BuildEnvironment(context.Background(), cfg, logger)
	if err != nil {
		logger.Error("build environment", "error", err)
		os.Exit(1)
	}
	defer env.Close()

	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           env.NewHTTPHandler(),
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
