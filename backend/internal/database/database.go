package database

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	maxConns := 4
	if envMax := os.Getenv("TX_DB_MAX_CONNS"); envMax != "" {
		if parsed, err := strconv.Atoi(envMax); err == nil && parsed > 0 {
			maxConns = parsed
		}
	}
	return NewPoolWithConns(ctx, databaseURL, maxConns, 1)
}

func NewPoolWithConns(ctx context.Context, databaseURL string, maxConns, minConns int) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	if maxConns > 0 {
		poolConfig.MaxConns = int32(maxConns)
	}
	if minConns > 0 {
		poolConfig.MinConns = int32(minConns)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}

	pingContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingContext); err != nil {
		pool.Close()
		return nil, err
	}

	return pool, nil
}
