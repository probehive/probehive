package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationAdvisoryLockKey int64 = 7355608014

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

type migration struct {
	version int64
	name    string
	sql     string
}

// Migrate applies every embedded migration in version order.
func (database *DB) Migrate(ctx context.Context) error {
	if database == nil || database.pool == nil {
		return errors.New("PostgreSQL database is not open")
	}
	return runMigrations(ctx, database.pool, embeddedMigrations)
}

func runMigrations(ctx context.Context, pool *pgxpool.Pool, migrationFS fs.FS) (returnErr error) {
	migrations, err := loadMigrations(migrationFS)
	if err != nil {
		return err
	}

	connection, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	if _, err := connection.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationAdvisoryLockKey); err != nil {
		connection.Release()
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	defer func() {
		if unlockErr := releaseMigrationLock(connection); unlockErr != nil {
			returnErr = errors.Join(returnErr, unlockErr)
		}
	}()

	if err := ensureMigrationTable(ctx, connection); err != nil {
		return err
	}
	applied, err := readAppliedMigrations(ctx, connection)
	if err != nil {
		return err
	}

	known := make(map[int64]migration, len(migrations))
	for _, item := range migrations {
		known[item.version] = item
	}
	for version, name := range applied {
		item, exists := known[version]
		if !exists {
			return fmt.Errorf("database contains unknown migration version %d", version)
		}
		if item.name != name {
			return fmt.Errorf("migration version %d name mismatch: database has %q, binary has %q", version, name, item.name)
		}
	}

	for _, item := range migrations {
		if _, exists := applied[item.version]; exists {
			continue
		}
		if err := applyMigration(ctx, connection, item); err != nil {
			return err
		}
	}
	return nil
}

func ensureMigrationTable(ctx context.Context, connection *pgxpool.Conn) error {
	transaction, err := connection.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin schema_migrations transaction: %w", err)
	}
	defer rollbackMigrationTransaction(transaction)
	if _, err := transaction.Exec(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version bigint NOT NULL,
    name text NOT NULL,
    applied_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT pk_schema_migrations PRIMARY KEY (version)
)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit schema_migrations: %w", err)
	}
	return nil
}

func readAppliedMigrations(ctx context.Context, connection *pgxpool.Conn) (map[int64]string, error) {
	rows, err := connection.Query(ctx, "SELECT version, name FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	applied := make(map[int64]string)
	for rows.Next() {
		var version int64
		var name string
		if err := rows.Scan(&version, &name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = name
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	return applied, nil
}

func applyMigration(ctx context.Context, connection *pgxpool.Conn, item migration) error {
	transaction, err := connection.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", item.name, err)
	}
	defer rollbackMigrationTransaction(transaction)
	if _, err := transaction.Exec(ctx, item.sql); err != nil {
		return fmt.Errorf("apply migration %s: %w", item.name, err)
	}
	if _, err := transaction.Exec(ctx,
		"INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", item.version, item.name); err != nil {
		return fmt.Errorf("record migration %s: %w", item.name, err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", item.name, err)
	}
	return nil
}

func rollbackMigrationTransaction(transaction pgx.Tx) {
	rollbackContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = transaction.Rollback(rollbackContext)
}

func releaseMigrationLock(connection *pgxpool.Conn) error {
	unlockContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var unlocked bool
	err := connection.QueryRow(unlockContext,
		"SELECT pg_advisory_unlock($1)", migrationAdvisoryLockKey).Scan(&unlocked)
	if err == nil && unlocked {
		connection.Release()
		return nil
	}

	underlying := connection.Hijack()
	closeErr := underlying.Close(unlockContext)
	if err != nil {
		return errors.Join(fmt.Errorf("release migration advisory lock: %w", err), closeErr)
	}
	if !unlocked {
		return errors.Join(errors.New("release migration advisory lock: lock was not held"), closeErr)
	}
	return closeErr
}

func loadMigrations(migrationFS fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	items := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".sql" {
			continue
		}
		separator := strings.IndexByte(entry.Name(), '_')
		if separator < 1 {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.ParseInt(entry.Name()[:separator], 10, 64)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("invalid migration version in %q", entry.Name())
		}
		contents, err := fs.ReadFile(migrationFS, path.Join("migrations", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		items = append(items, migration{version: version, name: entry.Name(), sql: string(contents)})
	}
	sort.Slice(items, func(left, right int) bool { return items[left].version < items[right].version })
	if len(items) == 0 {
		return nil, errors.New("no SQL migrations are embedded")
	}
	for index, item := range items {
		expected := int64(index + 1)
		if item.version != expected {
			return nil, fmt.Errorf("migration versions must be sequential: expected %d, found %d", expected, item.version)
		}
	}
	return items, nil
}
