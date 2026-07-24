// Package postgres implements ProbeHive persistence with PostgreSQL and pgx.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB owns the shared PostgreSQL connection pool.
type DB struct {
	pool *pgxpool.Pool
}

// Open creates and verifies a PostgreSQL connection pool.
func Open(ctx context.Context, databaseURL string) (*DB, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("PostgreSQL database URL is required")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL database URL: %w", err)
	}
	if _, configured := config.ConnConfig.RuntimeParams["timezone"]; !configured {
		config.ConnConfig.RuntimeParams["timezone"] = "UTC"
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL pool: %w", err)
	}
	database := &DB{pool: pool}
	if err := database.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return database, nil
}

// Ping verifies that PostgreSQL is reachable.
func (database *DB) Ping(ctx context.Context) error {
	if database == nil || database.pool == nil {
		return errors.New("PostgreSQL database is not open")
	}
	if err := database.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return nil
}

// Close releases all pooled PostgreSQL connections.
func (database *DB) Close() {
	if database != nil && database.pool != nil {
		database.pool.Close()
	}
}
