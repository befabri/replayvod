package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/befabri/replayvod/server/internal/config"
)

// NewPostgresPool creates a new PostgreSQL connection pool.
func NewPostgresPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	dsn := cfg.GetPostgresDSN()

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse postgres config: %w", err)
	}

	pool := cfg.App.PostgresPool
	if pool.MaxConns > 0 {
		poolConfig.MaxConns = pool.MaxConns
	}
	if pool.MinConns > 0 {
		poolConfig.MinConns = pool.MinConns
	}
	if pool.MaxConnLifetimeMs > 0 {
		poolConfig.MaxConnLifetime = time.Duration(pool.MaxConnLifetimeMs) * time.Millisecond
	}
	if pool.MaxConnIdleTimeMs > 0 {
		poolConfig.MaxConnIdleTime = time.Duration(pool.MaxConnIdleTimeMs) * time.Millisecond
	}
	if pool.HealthCheckPeriodMs > 0 {
		poolConfig.HealthCheckPeriod = time.Duration(pool.HealthCheckPeriodMs) * time.Millisecond
	}

	p, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create postgres pool: %w", err)
	}

	if err := p.Ping(ctx); err != nil {
		p.Close()
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return p, nil
}
