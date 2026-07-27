package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/probehive/probehive/internal/run"
)

// partitionedRunTables are the tables ADR 0021 partitions by month and expires by dropping
// whole partitions. They share a partition key, so they share a maintenance schedule.
//
// The order is a dependency order: observations reference runs, so creation walks the list
// forwards and expiry walks it backwards, retiring the referencing table's month first.
var partitionedRunTables = []string{"runs", "observations"}

// EnsurePartitions creates the monthly partitions that should exist at an instant: the month
// it falls in, plus a bounded lookahead.
//
// ADR 0021 makes a missing future partition an operational alert rather than an insert
// failure discovered at midnight, which only holds if something creates them in advance.
// There is no default partition to fall back on (ADR 0025), so this job running is what
// keeps inserts possible.
//
// It returns the partitions it created, so a caller can report the maintenance it performed
// rather than reporting that it ran.
func (store *RunStore) EnsurePartitions(ctx context.Context, now time.Time, monthsAhead int) ([]string, error) {
	months, err := run.PartitionPlan(now, monthsAhead)
	if err != nil {
		return nil, err
	}

	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin partition maintenance: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if err := lockPartitionMaintenance(ctx, transaction); err != nil {
		return nil, err
	}

	created := make([]string, 0)
	for _, table := range partitionedRunTables {
		for _, month := range months {
			name := partitionName(table, month)
			// The existence check is separate from the create rather than folded into
			// IF NOT EXISTS, because a skipped IF NOT EXISTS reports the same command tag
			// as one that created the table and this job reports what it actually did. The
			// advisory lock above is what makes checking and creating safe as two steps.
			present, err := relationPresent(ctx, transaction, name)
			if err != nil {
				return nil, err
			}
			if present {
				continue
			}
			// Partition bounds cannot be query parameters, so they are formatted in. The
			// values come from run.Month, which is two integers, so there is nothing here a
			// caller could influence.
			statement := fmt.Sprintf(
				"CREATE TABLE %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')",
				pgx.Identifier{name}.Sanitize(),
				pgx.Identifier{table}.Sanitize(),
				month.Start().Format(time.RFC3339),
				month.End().Format(time.RFC3339),
			)
			if _, err := transaction.Exec(ctx, statement); err != nil {
				return nil, fmt.Errorf("create partition %s: %w", name, err)
			}
			created = append(created, name)
		}
	}

	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit partition maintenance: %w", err)
	}
	return created, nil
}

// DropExpiredPartitions drops every partition whose entire range is older than the retention
// window, which is ADR 0021's O(1) catalogue expiry rather than a bulk delete that leaves
// bloat behind.
//
// A partition whose name this job does not recognise is left alone: an operator who attached
// a partition by hand keeps it, and maintenance drops only what it created (ADR 0025).
func (store *RunStore) DropExpiredPartitions(ctx context.Context, retention run.Retention, now time.Time) ([]string, error) {
	if _, err := run.NewRetention(retention.Days); err != nil {
		return nil, err
	}

	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin partition expiry: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if err := lockPartitionMaintenance(ctx, transaction); err != nil {
		return nil, err
	}

	dropped := make([]string, 0)
	for index := len(partitionedRunTables) - 1; index >= 0; index-- {
		table := partitionedRunTables[index]
		names, err := listPartitions(ctx, transaction, table)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			month, recognised := partitionMonth(table, name)
			if !recognised || !retention.Expired(month, now) {
				continue
			}
			// The partition is detached before it is dropped because the foreign key on the
			// observations parent depends on every runs partition, so a direct DROP is
			// refused while the partition is still attached. DROP ... CASCADE would also
			// work and would remove whatever else happened to depend on it, which is not a
			// power a scheduled maintenance job should hold.
			if _, err := transaction.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DETACH PARTITION %s",
				pgx.Identifier{table}.Sanitize(), pgx.Identifier{name}.Sanitize())); err != nil {
				return nil, fmt.Errorf("detach expired partition %s: %w", name, err)
			}
			if _, err := transaction.Exec(ctx, "DROP TABLE "+pgx.Identifier{name}.Sanitize()); err != nil {
				return nil, fmt.Errorf("drop expired partition %s: %w", name, err)
			}
			dropped = append(dropped, name)
		}
	}

	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit partition expiry: %w", err)
	}
	return dropped, nil
}

// lockPartitionMaintenance serializes maintenance for the rest of the transaction. The lock
// releases on commit or rollback, so a crashed job does not leave maintenance blocked.
func lockPartitionMaintenance(ctx context.Context, transaction pgx.Tx) error {
	if _, err := transaction.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", partitionAdvisoryLockKey); err != nil {
		return fmt.Errorf("acquire partition maintenance lock: %w", err)
	}
	return nil
}

// listPartitions reads the current partitions of one parent from the catalogue, scoped to the
// schema the connection is using so a test schema and a production schema never see each
// other's partitions.
func listPartitions(ctx context.Context, transaction pgx.Tx, table string) ([]string, error) {
	rows, err := transaction.Query(ctx, `
SELECT child.relname
FROM pg_inherits
JOIN pg_class parent ON parent.oid = pg_inherits.inhparent
JOIN pg_class child ON child.oid = pg_inherits.inhrelid
JOIN pg_namespace parent_namespace ON parent_namespace.oid = parent.relnamespace
WHERE parent.relname = $1 AND parent_namespace.nspname = current_schema()
ORDER BY child.relname`, table)
	if err != nil {
		return nil, fmt.Errorf("list partitions of %s: %w", table, err)
	}
	defer rows.Close()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan partition of %s: %w", table, err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list partitions of %s: %w", table, err)
	}
	return names, nil
}

// relationPresent reports whether a relation exists in the schema the connection is using.
func relationPresent(ctx context.Context, transaction pgx.Tx, name string) (bool, error) {
	var relation *string
	if err := transaction.QueryRow(ctx, "SELECT to_regclass($1)::text", name).Scan(&relation); err != nil {
		return false, fmt.Errorf("check relation %s: %w", name, err)
	}
	return relation != nil, nil
}

func partitionName(table string, month run.Month) string {
	return table + "_" + month.Suffix()
}

// partitionMonth reads back the range a partition covers from its name. Deriving it from the
// name rather than parsing the catalogue's bound expression is what makes an unrecognised
// name mean "not ours" instead of meaning "unparseable" (ADR 0025).
func partitionMonth(table, name string) (run.Month, bool) {
	suffix, found := strings.CutPrefix(name, table+"_")
	if !found {
		return run.Month{}, false
	}
	return run.ParseMonthSuffix(suffix)
}
