package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/transactx/backend/internal/app"
	"github.com/transactx/backend/internal/config"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
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

	worker := app.NewWorker(env)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := worker.Run(ctx); err != nil {
		logger.Error("worker terminated with error", "error", err)
		os.Exit(1)
	}
}
