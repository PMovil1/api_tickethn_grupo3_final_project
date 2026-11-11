package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const startupTimeout = 5 * time.Second

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// Connection errors can contain credentials or infrastructure details.
		return nil, errors.New("could not parse DATABASE_URL or its TLS settings")
	}
	config.ConnConfig.ConnectTimeout = startupTimeout
	config.MaxConns = 5
	config.MaxConnIdleTime = 5 * time.Minute

	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("could not initialize the PostgreSQL pool (%T)", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New("initial PostgreSQL check timed out")
		}
		return nil, fmt.Errorf("initial PostgreSQL check failed (%T)", err)
	}
	return pool, nil
}
