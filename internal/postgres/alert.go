package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/probehive/probehive/internal/alert"
)

var _ alert.Store = (*AlertStore)(nil)

type AlertStore struct{ pool *pgxpool.Pool }

func (database *DB) Alerts() *AlertStore { return &AlertStore{pool: database.pool} }

func (store *AlertStore) ProjectIncidentTransition(
	ctx context.Context, event alert.IncidentTransitionedV1, value alert.Alert, routeIDs alert.IDGenerator,
) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Alert projection: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	processed, err := outboxEventProcessed(ctx, transaction, event.EventID, event.OrganizationID)
	if err != nil {
		return err
	}
	if processed {
		return nil
	}

	var projectID, monitorID, timelineKind string
	var incidentVersion int64
	var occurredAt time.Time
	err = transaction.QueryRow(ctx, `
SELECT i.project_id, i.monitor_id, timeline.incident_version,
       timeline.kind, timeline.occurred_at
FROM incidents AS i
JOIN incident_timeline_entries AS timeline
  ON timeline.incident_id = i.id
 AND timeline.organization_id = i.organization_id
WHERE i.id = $1
  AND i.organization_id = $2
  AND timeline.incident_version = $3
  AND timeline.kind IN ('opened', 'resolved')`,
		event.IncidentID, event.OrganizationID, event.AggregateVersion,
	).Scan(&projectID, &monitorID, &incidentVersion, &timelineKind, &occurredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return alert.ErrPayloadInvalid
	}
	if err != nil {
		return fmt.Errorf("load Alert source Incident fact: %w", err)
	}
	if projectID != event.ProjectID || monitorID != event.MonitorID ||
		incidentVersion != event.AggregateVersion || timelineKind != event.Transition ||
		!occurredAt.Equal(event.OccurredAt) ||
		value.OrganizationID != event.OrganizationID || value.ProjectID != projectID ||
		value.MonitorID != monitorID || value.IncidentID != event.IncidentID ||
		value.IncidentVersion != incidentVersion ||
		value.Kind != alert.Kind("incident."+timelineKind) ||
		!value.OccurredAt.Equal(occurredAt) {
		return alert.ErrPayloadInvalid
	}

	var lockedOrganizationID string
	if err := transaction.QueryRow(ctx, `
SELECT id
FROM organizations
WHERE id=$1
FOR UPDATE`, event.OrganizationID).Scan(&lockedOrganizationID); errors.Is(err, pgx.ErrNoRows) {
		return alert.ErrPayloadInvalid
	} else if err != nil {
		return fmt.Errorf("lock Alert routing Organization: %w", err)
	}

	tag, err := transaction.Exec(ctx, `
INSERT INTO alerts (
    id, organization_id, project_id, monitor_id, incident_id,
    incident_version, kind, occurred_at, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (organization_id, incident_id, incident_version) DO NOTHING`,
		value.ID, value.OrganizationID, value.ProjectID, value.MonitorID,
		value.IncidentID, value.IncidentVersion, string(value.Kind),
		value.OccurredAt.UTC(), value.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("insert Alert: %w", err)
	}
	if tag.RowsAffected() == 0 {
		processed, err := outboxEventProcessed(
			ctx, transaction, event.EventID, event.OrganizationID,
		)
		if err != nil {
			return err
		}
		if processed {
			return nil
		}
		return errors.New("Alert source fact already projected without its outbox marker")
	}
	if err := routeWebhookDeliveries(ctx, transaction, value, routeIDs); err != nil {
		return err
	}
	if err := markOutboxEventProcessed(
		ctx, transaction, event.EventID, event.OrganizationID,
		"incident.transitioned.v1", value.CreatedAt,
	); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Alert projection: %w", err)
	}
	return nil
}

func (store *AlertStore) ListAlerts(
	ctx context.Context, scope alert.Scope, query alert.ListQuery,
) ([]alert.Alert, bool, bool, error) {
	if query.PageSize < 1 || query.PageSize > alert.MaxPageSize {
		return nil, false, false, fmt.Errorf("an Alert page is 1 to %d rows", alert.MaxPageSize)
	}
	var exists bool
	if err := store.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM monitors
    WHERE id=$1 AND organization_id=$2 AND project_id=$3
)`, scope.MonitorID, scope.OrganizationID, scope.ProjectID).Scan(&exists); err != nil {
		return nil, false, false, fmt.Errorf("check Alert Monitor scope: %w", err)
	}
	if !exists {
		return nil, false, false, nil
	}

	var cursorTime *time.Time
	var cursorID *string
	if query.Cursor != nil {
		occurredAt := query.Cursor.OccurredAt.UTC()
		cursorTime, cursorID = &occurredAt, &query.Cursor.ID
	}
	rows, err := store.pool.Query(ctx, `
SELECT id, organization_id, project_id, monitor_id, incident_id,
       incident_version, kind, occurred_at, created_at
FROM alerts
WHERE organization_id=$1 AND project_id=$2 AND monitor_id=$3
  AND (
      $4::timestamptz IS NULL
      OR (occurred_at, id) < ($4::timestamptz, $5::uuid)
  )
ORDER BY occurred_at DESC, id DESC
LIMIT $6`, scope.OrganizationID, scope.ProjectID, scope.MonitorID,
		cursorTime, cursorID, query.PageSize+1)
	if err != nil {
		return nil, false, false, fmt.Errorf("list Alerts: %w", err)
	}
	defer rows.Close()

	values := make([]alert.Alert, 0, query.PageSize+1)
	for rows.Next() {
		var value alert.Alert
		var kind string
		if err := rows.Scan(
			&value.ID, &value.OrganizationID, &value.ProjectID, &value.MonitorID,
			&value.IncidentID, &value.IncidentVersion, &kind,
			&value.OccurredAt, &value.CreatedAt,
		); err != nil {
			return nil, false, false, fmt.Errorf("scan Alert: %w", err)
		}
		value.Kind = alert.Kind(kind)
		value.OccurredAt, value.CreatedAt = value.OccurredAt.UTC(), value.CreatedAt.UTC()
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, false, false, fmt.Errorf("scan Alerts: %w", err)
	}
	more := len(values) > query.PageSize
	if more {
		values = values[:query.PageSize]
	}
	return values, more, true, nil
}
