package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/probehive/probehive/internal/maintenance"
)

var _ maintenance.Store = (*MaintenanceStore)(nil)

// MaintenanceStore persists one-time Monitor maintenance windows.
type MaintenanceStore struct {
	pool *pgxpool.Pool
}

func (database *DB) Maintenance() *MaintenanceStore {
	return &MaintenanceStore{pool: database.pool}
}

// CreateWindow serializes overlap checks on the owning Monitor row, so concurrent
// creators cannot both insert overlapping intervals.
func (store *MaintenanceStore) CreateWindow(ctx context.Context, value maintenance.Window) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin maintenance window creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var monitorID string
	err = transaction.QueryRow(ctx, `
SELECT id
FROM monitors
WHERE id = $1 AND organization_id = $2 AND project_id = $3
FOR UPDATE`,
		value.MonitorID, value.OrganizationID, value.ProjectID,
	).Scan(&monitorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return maintenance.ErrMonitorNotFound
	}
	if err != nil {
		return fmt.Errorf("lock maintenance Monitor: %w", err)
	}

	var overlaps bool
	if err = transaction.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM maintenance_windows
    WHERE organization_id = $1
      AND monitor_id = $2
      AND cancelled_at IS NULL
      AND starts_at < $3
      AND ends_at > $4
)`,
		value.OrganizationID, value.MonitorID, value.EndsAt.UTC(), value.StartsAt.UTC(),
	).Scan(&overlaps); err != nil {
		return fmt.Errorf("check maintenance overlap: %w", err)
	}
	if overlaps {
		return maintenance.ErrOverlap
	}

	if _, err = transaction.Exec(ctx, `
INSERT INTO maintenance_windows (
    id, organization_id, monitor_id, starts_at, ends_at, created_at, cancelled_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		string(value.ID), value.OrganizationID, value.MonitorID,
		value.StartsAt.UTC(), value.EndsAt.UTC(), value.CreatedAt.UTC(), value.CancelledAt,
	); err != nil {
		return fmt.Errorf("insert maintenance window: %w", err)
	}
	if err = transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit maintenance window creation: %w", err)
	}
	return nil
}

func (store *MaintenanceStore) ListWindows(
	ctx context.Context, scope maintenance.Scope, endsAfter time.Time,
) ([]maintenance.Window, bool, error) {
	var monitorExists bool
	if err := store.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM monitors
    WHERE id = $1 AND organization_id = $2 AND project_id = $3
)`, scope.MonitorID, scope.OrganizationID, scope.ProjectID).Scan(&monitorExists); err != nil {
		return nil, false, fmt.Errorf("check maintenance Monitor scope: %w", err)
	}
	if !monitorExists {
		return nil, false, nil
	}

	rows, err := store.pool.Query(ctx, `
SELECT maintenance_window.id, maintenance_window.organization_id, monitor.project_id, maintenance_window.monitor_id,
       maintenance_window.starts_at, maintenance_window.ends_at, maintenance_window.created_at, maintenance_window.cancelled_at,
       (maintenance_window.xmin::text)::bigint
FROM maintenance_windows AS maintenance_window
JOIN monitors AS monitor
  ON monitor.id = maintenance_window.monitor_id
 AND monitor.organization_id = maintenance_window.organization_id
WHERE maintenance_window.organization_id = $1
  AND maintenance_window.monitor_id = $2
  AND monitor.project_id = $3
  AND maintenance_window.ends_at > $4
ORDER BY maintenance_window.starts_at, maintenance_window.id`,
		scope.OrganizationID, scope.MonitorID, scope.ProjectID, endsAfter.UTC())
	if err != nil {
		return nil, false, fmt.Errorf("list maintenance windows: %w", err)
	}
	defer rows.Close()

	values := make([]maintenance.Window, 0)
	for rows.Next() {
		value, _, scanErr := scanMaintenanceWindow(rows)
		if scanErr != nil {
			return nil, false, scanErr
		}
		values = append(values, value)
	}
	if err = rows.Err(); err != nil {
		return nil, false, fmt.Errorf("list maintenance windows: %w", err)
	}
	return values, true, nil
}

func (store *MaintenanceStore) FindWindow(
	ctx context.Context, scope maintenance.Scope, id maintenance.ID,
) (maintenance.Window, bool, error) {
	return scanMaintenanceWindow(store.pool.QueryRow(ctx, `
SELECT maintenance_window.id, maintenance_window.organization_id, monitor.project_id, maintenance_window.monitor_id,
       maintenance_window.starts_at, maintenance_window.ends_at, maintenance_window.created_at, maintenance_window.cancelled_at,
       (maintenance_window.xmin::text)::bigint
FROM maintenance_windows AS maintenance_window
JOIN monitors AS monitor
  ON monitor.id = maintenance_window.monitor_id
 AND monitor.organization_id = maintenance_window.organization_id
WHERE maintenance_window.id = $1
  AND maintenance_window.organization_id = $2
  AND maintenance_window.monitor_id = $3
  AND monitor.project_id = $4`,
		string(id), scope.OrganizationID, scope.MonitorID, scope.ProjectID))
}

func (store *MaintenanceStore) CancelWindow(
	ctx context.Context, value maintenance.Window, expectedVersion uint32,
) error {
	result, err := store.pool.Exec(ctx, `
UPDATE maintenance_windows AS maintenance_window
SET cancelled_at = $1
WHERE maintenance_window.id = $2
  AND maintenance_window.organization_id = $3
  AND maintenance_window.monitor_id = $4
  AND (maintenance_window.xmin::text)::bigint = $5
  AND EXISTS (
      SELECT 1
      FROM monitors AS monitor
      WHERE monitor.id = maintenance_window.monitor_id
        AND monitor.organization_id = maintenance_window.organization_id
        AND monitor.project_id = $6
  )`,
		value.CancelledAt, string(value.ID), value.OrganizationID, value.MonitorID,
		uint64(expectedVersion), value.ProjectID)
	if err != nil {
		return fmt.Errorf("cancel maintenance window: %w", err)
	}
	if result.RowsAffected() == 0 {
		return maintenance.ErrConcurrentUpdate
	}
	return nil
}

func scanMaintenanceWindow(row rowScanner) (maintenance.Window, bool, error) {
	var (
		id             string
		organizationID string
		projectID      string
		monitorID      string
		startsAt       time.Time
		endsAt         time.Time
		createdAt      time.Time
		cancelledAt    *time.Time
		version        uint64
	)
	if err := row.Scan(
		&id, &organizationID, &projectID, &monitorID,
		&startsAt, &endsAt, &createdAt, &cancelledAt, &version,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return maintenance.Window{}, false, nil
		}
		return maintenance.Window{}, false, fmt.Errorf("scan maintenance window: %w", err)
	}
	if version > math.MaxUint32 {
		return maintenance.Window{}, false, fmt.Errorf(
			"restore maintenance window: xmin %d exceeds uint32", version,
		)
	}
	if cancelledAt != nil {
		value := cancelledAt.UTC()
		cancelledAt = &value
	}
	value, err := maintenance.RestoreWindow(
		maintenance.ID(id),
		maintenance.Scope{
			OrganizationID: organizationID,
			ProjectID:      projectID,
			MonitorID:      monitorID,
		},
		startsAt.UTC(), endsAt.UTC(), createdAt.UTC(), cancelledAt, uint32(version),
	)
	if err != nil {
		return maintenance.Window{}, false, fmt.Errorf("restore maintenance window: %w", err)
	}
	return value, true, nil
}
