package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/probehive/probehive/internal/monitor"
)

type MonitorInventoryStore struct{ pool *pgxpool.Pool }

func (database *DB) MonitorInventory() *MonitorInventoryStore {
	return &MonitorInventoryStore{pool: database.pool}
}

var _ monitor.InventoryStore = (*MonitorInventoryStore)(nil)

func (store *MonitorInventoryStore) ListMonitorInventory(
	ctx context.Context, organizationID string,
	query monitor.InventoryQuery, now time.Time,
) ([]monitor.InventoryItem, int, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, fmt.Errorf("begin Monitor inventory query: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	args := inventoryQueryArgs(organizationID, query, now)
	where := inventoryWhere(query)
	countQuery := `
SELECT count(*)
FROM monitors AS m
LEFT JOIN monitor_health AS health
  ON health.monitor_id = m.id AND health.organization_id = m.organization_id
LEFT JOIN LATERAL (
    SELECT r.id, r.outcome, r.scheduled_for
    FROM runs AS r
    WHERE r.monitor_id = m.id AND r.organization_id = m.organization_id
    ORDER BY r.scheduled_for DESC, r.id DESC
    LIMIT 1
) AS latest_run ON true
LEFT JOIN LATERAL (
    SELECT w.id,
           CASE WHEN w.starts_at <= $2 THEN 'active' ELSE 'upcoming' END AS state,
           w.starts_at, w.ends_at
    FROM maintenance_windows AS w
    WHERE w.monitor_id = m.id AND w.organization_id = m.organization_id
      AND w.cancelled_at IS NULL AND w.ends_at > $2
    ORDER BY CASE WHEN w.starts_at <= $2 THEN 0 ELSE 1 END, w.starts_at, w.id
    LIMIT 1
) AS maintenance ON true
` + where
	var total int
	if err := transaction.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count Monitor inventory: %w", err)
	}

	sortColumn := "lower(m.name)"
	if query.Sort == monitor.InventorySortCreatedAt {
		sortColumn = "m.created_at"
	}
	if query.Sort == monitor.InventorySortUpdatedAt {
		sortColumn = "m.updated_at"
	}
	sortDirection := "ASC"
	if query.Direction == monitor.InventoryDirectionDescending {
		sortDirection = "DESC"
	}
	dataQuery := `
SELECT m.id, m.organization_id, m.name, m.check_type, m.state,
       m.interval_seconds, m.latest_revision_number, m.created_at, m.updated_at,
       health.state, health.updated_at,
       latest_run.id, latest_run.outcome, latest_run.scheduled_for,
       maintenance.id, maintenance.state, maintenance.starts_at, maintenance.ends_at
FROM monitors AS m
LEFT JOIN monitor_health AS health
  ON health.monitor_id = m.id AND health.organization_id = m.organization_id
LEFT JOIN LATERAL (
    SELECT r.id, r.outcome, r.scheduled_for
    FROM runs AS r
    WHERE r.monitor_id = m.id AND r.organization_id = m.organization_id
    ORDER BY r.scheduled_for DESC, r.id DESC
    LIMIT 1
) AS latest_run ON true
LEFT JOIN LATERAL (
    SELECT w.id,
           CASE WHEN w.starts_at <= $2 THEN 'active' ELSE 'upcoming' END AS state,
           w.starts_at, w.ends_at
    FROM maintenance_windows AS w
    WHERE w.monitor_id = m.id AND w.organization_id = m.organization_id
      AND w.cancelled_at IS NULL AND w.ends_at > $2
    ORDER BY CASE WHEN w.starts_at <= $2 THEN 0 ELSE 1 END, w.starts_at, w.id
    LIMIT 1
) AS maintenance ON true
` + where + fmt.Sprintf(" ORDER BY %s %s, m.id %s LIMIT $8 OFFSET $9", sortColumn, sortDirection, sortDirection)
	args = append(args, query.PageSize, (query.Page-1)*query.PageSize)
	rows, err := transaction.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list Monitor inventory: %w", err)
	}
	defer rows.Close()

	items := make([]monitor.InventoryItem, 0, query.PageSize)
	for rows.Next() {
		item, err := scanMonitorInventoryItem(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list Monitor inventory: %w", err)
	}
	return items, total, nil
}

func inventoryQueryArgs(organizationID string, query monitor.InventoryQuery, now time.Time) []any {
	return []any{organizationID, now.UTC(), query.Search, string(query.State), string(query.Health), string(query.RunOutcome), string(query.Maintenance)}
}

func inventoryWhere(query monitor.InventoryQuery) string {
	return `
WHERE m.organization_id = $1
  AND ($3 = '' OR strpos(lower(m.name), lower($3)) > 0)
  AND ($4 = '' OR m.state = $4)
  AND (
      $5 = ''
      OR ($5 = 'notEvaluated' AND health.monitor_id IS NULL)
      OR ($5 <> 'notEvaluated' AND health.state = $5)
  )
  AND (
      $6 = ''
      OR ($6 = 'notRun' AND latest_run.id IS NULL)
      OR ($6 = 'inProgress' AND latest_run.id IS NOT NULL AND latest_run.outcome IS NULL)
      OR latest_run.outcome = $6
  )
  AND (
      $7 = ''
      OR ($7 = 'none' AND maintenance.id IS NULL)
      OR ($7 <> 'none' AND maintenance.state = $7)
  )`
}

func scanMonitorInventoryItem(row interface{ Scan(...any) error }) (monitor.InventoryItem, error) {
	var (
		id, organizationID, name, checkType, state string
		intervalSeconds, latestRevisionNumber      int
		createdAt, updatedAt                       time.Time
		healthState                                *string
		healthUpdatedAt                            *time.Time
		runID, runOutcome                          *string
		runScheduledFor                            *time.Time
		maintenanceID, maintenanceState            *string
		maintenanceStartsAt, maintenanceEndsAt     *time.Time
	)
	if err := row.Scan(
		&id, &organizationID, &name, &checkType, &state,
		&intervalSeconds, &latestRevisionNumber, &createdAt, &updatedAt,
		&healthState, &healthUpdatedAt, &runID, &runOutcome, &runScheduledFor,
		&maintenanceID, &maintenanceState, &maintenanceStartsAt, &maintenanceEndsAt,
	); err != nil {
		return monitor.InventoryItem{}, fmt.Errorf("scan Monitor inventory: %w", err)
	}
	value, err := monitor.RestoreMonitor(monitor.ID(id), organizationID, name, checkType, monitor.State(state), intervalSeconds, latestRevisionNumber, createdAt.UTC(), updatedAt.UTC(), 0)
	if err != nil {
		return monitor.InventoryItem{}, fmt.Errorf("restore Monitor inventory item: %w", err)
	}
	item := monitor.InventoryItem{Monitor: value, Maintenance: monitor.InventoryMaintenance{State: monitor.InventoryMaintenanceNone}}
	if healthState != nil && healthUpdatedAt != nil {
		item.Health = &monitor.InventoryHealth{State: *healthState, UpdatedAt: healthUpdatedAt.UTC()}
	}
	if runID != nil && runScheduledFor != nil {
		item.LastRun = &monitor.InventoryRun{ID: *runID, ScheduledFor: runScheduledFor.UTC()}
		if runOutcome != nil {
			item.LastRun.Outcome = *runOutcome
		}
	}
	if maintenanceID != nil && maintenanceState != nil && maintenanceStartsAt != nil && maintenanceEndsAt != nil {
		item.Maintenance = monitor.InventoryMaintenance{State: monitor.InventoryMaintenanceFilter(*maintenanceState), WindowID: *maintenanceID, StartsAt: maintenanceStartsAt.UTC(), EndsAt: maintenanceEndsAt.UTC()}
	}
	return item, nil
}
