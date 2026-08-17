package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/probehive/probehive/internal/overview"
)

var _ overview.Store = (*OverviewStore)(nil)

type OverviewStore struct{ pool *pgxpool.Pool }

func (database *DB) Overviews() *OverviewStore { return &OverviewStore{pool: database.pool} }

func (store *OverviewStore) GetOverview(
	ctx context.Context, organizationID string, incidentLimit int,
) (overview.Summary, bool, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return overview.Summary{}, false, fmt.Errorf("begin Organization overview: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var value overview.Summary
	err = transaction.QueryRow(ctx, `
WITH monitor_counts AS (
    SELECT count(*)::integer AS total,
           count(*) FILTER (WHERE state = 'draft')::integer AS draft,
           count(*) FILTER (WHERE state = 'active')::integer AS active,
           count(*) FILTER (WHERE state = 'paused')::integer AS paused,
           count(*) FILTER (WHERE state = 'archived')::integer AS archived
    FROM monitors WHERE organization_id = $1
), health_counts AS (
    SELECT count(*) FILTER (WHERE health.monitor_id IS NULL)::integer AS not_evaluated,
           count(*) FILTER (WHERE health.state = 'unknown')::integer AS unknown,
           count(*) FILTER (WHERE health.state = 'healthy')::integer AS healthy,
           count(*) FILTER (WHERE health.state = 'degraded')::integer AS degraded,
           count(*) FILTER (WHERE health.state = 'down')::integer AS down
    FROM monitors AS monitor
    LEFT JOIN monitor_health AS health
      ON health.organization_id = monitor.organization_id
     AND health.monitor_id = monitor.id
    WHERE monitor.organization_id = $1 AND monitor.state = 'active'
), incident_counts AS (
    SELECT count(*)::integer AS active,
           count(*) FILTER (WHERE state = 'open')::integer AS open,
           count(*) FILTER (WHERE state = 'acknowledged')::integer AS acknowledged
    FROM incidents WHERE organization_id = $1 AND state <> 'resolved'
), integration_counts AS (
    SELECT count(*)::integer AS total,
           count(*) FILTER (WHERE enabled)::integer AS enabled
    FROM webhook_integrations WHERE organization_id = $1
)
SELECT organization.id,
       monitors.total, monitors.draft, monitors.active, monitors.paused, monitors.archived,
       health.not_evaluated, health.unknown, health.healthy, health.degraded, health.down,
       incidents.active, incidents.open, incidents.acknowledged,
       integrations.total, integrations.enabled,
       EXISTS (SELECT 1 FROM status_pages WHERE organization_id = $1),
       EXISTS (SELECT 1 FROM status_pages WHERE organization_id = $1 AND publication_token_hash IS NOT NULL)
FROM organizations AS organization
CROSS JOIN monitor_counts AS monitors
CROSS JOIN health_counts AS health
CROSS JOIN incident_counts AS incidents
CROSS JOIN integration_counts AS integrations
WHERE organization.id = $1`, organizationID).Scan(
		&value.OrganizationID,
		&value.Monitors.Total, &value.Monitors.Draft, &value.Monitors.Active,
		&value.Monitors.Paused, &value.Monitors.Archived,
		&value.Health.NotEvaluated, &value.Health.Unknown, &value.Health.Healthy,
		&value.Health.Degraded, &value.Health.Down,
		&value.Incidents.Active, &value.Incidents.Open, &value.Incidents.Acknowledged,
		&value.Integrations.Total, &value.Integrations.Enabled,
		&value.StatusPage.Configured, &value.StatusPage.Published,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return overview.Summary{}, false, nil
	}
	if err != nil {
		return overview.Summary{}, false, fmt.Errorf("read Organization overview counts: %w", err)
	}

	rows, err := transaction.Query(ctx, `
SELECT incident.id, incident.project_id, incident.monitor_id, monitor.name,
       incident.state, incident.updated_at
FROM incidents AS incident
JOIN monitors AS monitor
  ON monitor.organization_id = incident.organization_id AND monitor.id = incident.monitor_id
WHERE incident.organization_id = $1 AND incident.state <> 'resolved'
ORDER BY incident.updated_at DESC, incident.id DESC
LIMIT $2`, organizationID, incidentLimit)
	if err != nil {
		return overview.Summary{}, false, fmt.Errorf("read Organization overview Incidents: %w", err)
	}
	defer rows.Close()
	value.ActiveIncidents = make([]overview.ActiveIncident, 0, incidentLimit)
	for rows.Next() {
		var incident overview.ActiveIncident
		if err := rows.Scan(
			&incident.ID, &incident.ProjectID, &incident.MonitorID, &incident.MonitorName,
			&incident.State, &incident.UpdatedAt,
		); err != nil {
			return overview.Summary{}, false, fmt.Errorf("scan Organization overview Incident: %w", err)
		}
		incident.UpdatedAt = incident.UpdatedAt.UTC()
		value.ActiveIncidents = append(value.ActiveIncidents, incident)
	}
	if err := rows.Err(); err != nil {
		return overview.Summary{}, false, fmt.Errorf("read Organization overview Incident rows: %w", err)
	}
	value.ActiveIncidentsTruncated = value.Incidents.Active > len(value.ActiveIncidents)
	if err := transaction.Commit(ctx); err != nil {
		return overview.Summary{}, false, fmt.Errorf("commit Organization overview read: %w", err)
	}
	return value, true, nil
}
