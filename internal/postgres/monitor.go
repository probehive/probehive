package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/probehive/probehive/internal/monitor"
)

var _ monitor.Store = (*MonitorStore)(nil)

const revisionNumberUniqueIndex = "ux_monitor_revisions_monitor_number"

// MonitorStore persists Monitors and immutable revisions.
type MonitorStore struct {
	pool *pgxpool.Pool
}

// Monitors returns the Monitor persistence adapter.
func (database *DB) Monitors() *MonitorStore {
	return &MonitorStore{pool: database.pool}
}

// ProjectExists checks Project ownership with explicit Organization scope.
func (store *MonitorStore) ProjectExists(ctx context.Context, organizationID, projectID string) (bool, error) {
	var exists bool
	if err := store.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM projects WHERE id = $1 AND organization_id = $2
)`, projectID, organizationID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check Project scope: %w", err)
	}
	return exists, nil
}

// FindMonitor loads a Monitor only through its complete tenant scope.
func (store *MonitorStore) FindMonitor(ctx context.Context, scope monitor.Scope) (monitor.Monitor, bool, error) {
	return scanMonitor(store.pool.QueryRow(ctx, `
SELECT id, organization_id, project_id, name, check_type, state,
       latest_revision_number, created_at, updated_at, (xmin::text)::bigint
FROM monitors
WHERE id = $1 AND project_id = $2 AND organization_id = $3`,
		string(scope.MonitorID), scope.ProjectID, scope.OrganizationID))
}

// ListMonitors returns creation order with UUID as a deterministic tie-breaker.
func (store *MonitorStore) ListMonitors(ctx context.Context, organizationID, projectID string) ([]monitor.Monitor, error) {
	rows, err := store.pool.Query(ctx, `
SELECT id, organization_id, project_id, name, check_type, state,
       latest_revision_number, created_at, updated_at, (xmin::text)::bigint
FROM monitors
WHERE project_id = $1 AND organization_id = $2
ORDER BY created_at, id`, projectID, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Monitors: %w", err)
	}
	defer rows.Close()

	values := make([]monitor.Monitor, 0)
	for rows.Next() {
		value, _, scanErr := scanMonitor(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list Monitors: %w", err)
	}
	return values, nil
}

// CreateMonitor inserts a new draft Monitor.
func (store *MonitorStore) CreateMonitor(ctx context.Context, value monitor.Monitor) error {
	if _, err := store.pool.Exec(ctx, `
INSERT INTO monitors (
    id, organization_id, project_id, name, check_type, state,
    latest_revision_number, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		string(value.ID), value.OrganizationID, value.ProjectID, value.Name, value.CheckType,
		string(value.State), value.LatestRevisionNumber, value.CreatedAt.UTC(), value.UpdatedAt.UTC()); err != nil {
		return fmt.Errorf("insert Monitor: %w", err)
	}
	return nil
}

// UpdateMonitor applies a mutable snapshot only when xmin still matches.
func (store *MonitorStore) UpdateMonitor(ctx context.Context, value monitor.Monitor, expectedVersion uint32) error {
	result, err := store.pool.Exec(ctx, `
UPDATE monitors
SET name = $1, state = $2, latest_revision_number = $3, updated_at = $4
WHERE id = $5 AND organization_id = $6 AND project_id = $7
  AND (xmin::text)::bigint = $8`,
		value.Name, string(value.State), value.LatestRevisionNumber, value.UpdatedAt.UTC(),
		string(value.ID), value.OrganizationID, value.ProjectID, uint64(expectedVersion))
	if err != nil {
		return fmt.Errorf("update Monitor: %w", err)
	}
	if result.RowsAffected() == 0 {
		return monitor.ErrConcurrentUpdate
	}
	return nil
}

// AppendRevision inserts one immutable revision and advances its Monitor atomically.
func (store *MonitorStore) AppendRevision(
	ctx context.Context,
	value monitor.Monitor,
	revision monitor.Revision,
	expectedVersion uint32,
) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Monitor revision append: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	result, err := transaction.Exec(ctx, `
UPDATE monitors
SET latest_revision_number = $1, updated_at = $2
WHERE id = $3 AND organization_id = $4 AND project_id = $5
  AND (xmin::text)::bigint = $6`,
		value.LatestRevisionNumber, value.UpdatedAt.UTC(), string(value.ID),
		value.OrganizationID, value.ProjectID, uint64(expectedVersion))
	if err != nil {
		return fmt.Errorf("advance Monitor revision counter: %w", err)
	}
	if result.RowsAffected() == 0 {
		return monitor.ErrConcurrentUpdate
	}

	if _, err := transaction.Exec(ctx, `
INSERT INTO monitor_revisions (
    id, monitor_id, organization_id, revision_number, check_type,
    check_schema_version, check_configuration, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		string(revision.ID), string(revision.MonitorID), revision.OrganizationID,
		revision.RevisionNumber, revision.CheckType, revision.CheckSchemaVersion,
		string(revision.CheckConfiguration), revision.CreatedAt.UTC()); err != nil {
		if isConstraintViolation(err, uniqueViolation, revisionNumberUniqueIndex) {
			return monitor.ErrConcurrentUpdate
		}
		return fmt.Errorf("insert Monitor revision: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		if isConstraintViolation(err, uniqueViolation, revisionNumberUniqueIndex) {
			return monitor.ErrConcurrentUpdate
		}
		return fmt.Errorf("commit Monitor revision append: %w", err)
	}
	return nil
}

// FindRevision loads one immutable revision under explicit Organization scope.
func (store *MonitorStore) FindRevision(ctx context.Context, organizationID string, monitorID monitor.ID, revisionNumber int) (monitor.Revision, bool, error) {
	return scanRevision(store.pool.QueryRow(ctx, `
SELECT id, monitor_id, organization_id, revision_number, check_type,
       check_schema_version, check_configuration, created_at
FROM monitor_revisions
WHERE monitor_id = $1 AND organization_id = $2 AND revision_number = $3`,
		string(monitorID), organizationID, revisionNumber))
}

// ListRevisions returns immutable revisions in ascending revision-number order.
func (store *MonitorStore) ListRevisions(ctx context.Context, organizationID string, monitorID monitor.ID) ([]monitor.Revision, error) {
	rows, err := store.pool.Query(ctx, `
SELECT id, monitor_id, organization_id, revision_number, check_type,
       check_schema_version, check_configuration, created_at
FROM monitor_revisions
WHERE monitor_id = $1 AND organization_id = $2
ORDER BY revision_number`, string(monitorID), organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Monitor revisions: %w", err)
	}
	defer rows.Close()

	values := make([]monitor.Revision, 0)
	for rows.Next() {
		value, _, scanErr := scanRevision(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list Monitor revisions: %w", err)
	}
	return values, nil
}

func scanMonitor(row rowScanner) (monitor.Monitor, bool, error) {
	var (
		id                   string
		organizationID       string
		projectID            string
		name                 string
		checkType            string
		state                string
		latestRevisionNumber int
		createdAt            time.Time
		updatedAt            time.Time
		version              uint64
	)
	if err := row.Scan(
		&id, &organizationID, &projectID, &name, &checkType, &state,
		&latestRevisionNumber, &createdAt, &updatedAt, &version,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return monitor.Monitor{}, false, nil
		}
		return monitor.Monitor{}, false, fmt.Errorf("scan Monitor: %w", err)
	}
	if version > math.MaxUint32 {
		return monitor.Monitor{}, false, fmt.Errorf("restore Monitor: xmin %d exceeds uint32", version)
	}
	value, err := monitor.RestoreMonitor(
		monitor.ID(id), organizationID, projectID, name, checkType, monitor.State(state),
		latestRevisionNumber, createdAt.UTC(), updatedAt.UTC(), uint32(version),
	)
	if err != nil {
		return monitor.Monitor{}, false, fmt.Errorf("restore Monitor: %w", err)
	}
	return value, true, nil
}

func scanRevision(row rowScanner) (monitor.Revision, bool, error) {
	var (
		id                     string
		monitorID              string
		organizationID         string
		revisionNumber         int
		checkType              string
		checkSchemaVersion     int
		checkConfigurationJSON []byte
		createdAt              time.Time
	)
	if err := row.Scan(
		&id, &monitorID, &organizationID, &revisionNumber, &checkType,
		&checkSchemaVersion, &checkConfigurationJSON, &createdAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return monitor.Revision{}, false, nil
		}
		return monitor.Revision{}, false, fmt.Errorf("scan Monitor revision: %w", err)
	}
	value, err := monitor.NewRevision(
		monitor.RevisionID(id), monitor.ID(monitorID), organizationID, revisionNumber,
		checkType, checkSchemaVersion, json.RawMessage(checkConfigurationJSON), createdAt.UTC(),
	)
	if err != nil {
		return monitor.Revision{}, false, fmt.Errorf("restore Monitor revision: %w", err)
	}
	return value, true, nil
}
