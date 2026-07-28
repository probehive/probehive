package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/probehive/probehive/internal/run"
)

var _ run.MonitorSource = (*RunStore)(nil)

// MaxSchedulableMonitors bounds one scheduling read. An installation that exceeds it has
// outgrown a tick that lists every active Monitor, which ADR 0026 names as the trigger for
// introducing a due cursor; truncating silently would hide that by simply not running the
// Monitors past the bound.
const MaxSchedulableMonitors = 10000

// ListSchedulable returns every active Monitor with the revision a new Run would execute.
//
// It joins the latest revision rather than letting the scheduler ask for one per Monitor,
// because the scheduler's whole tick is one read and a Monitor without its configuration is
// not schedulable. An active Monitor always has a revision (ADR 0014), so the inner join
// excludes nothing that should have run.
func (store *RunStore) ListSchedulable(ctx context.Context) ([]run.Schedulable, error) {
	rows, err := store.pool.Query(ctx, `
SELECT monitors.organization_id, monitors.id, monitors.interval_seconds, monitors.updated_at,
       monitor_revisions.revision_number, monitor_revisions.check_type,
       monitor_revisions.check_schema_version, monitor_revisions.check_configuration
FROM monitors
JOIN monitor_revisions
  ON monitor_revisions.monitor_id = monitors.id
 AND monitor_revisions.organization_id = monitors.organization_id
 AND monitor_revisions.revision_number = monitors.latest_revision_number
WHERE monitors.state = 'active'
ORDER BY monitors.organization_id, monitors.id
LIMIT $1`, MaxSchedulableMonitors+1)
	if err != nil {
		return nil, fmt.Errorf("list schedulable Monitors: %w", err)
	}
	defer rows.Close()

	values := make([]run.Schedulable, 0)
	for rows.Next() {
		var (
			organizationID     string
			monitorID          string
			intervalSeconds    int
			updatedAt          time.Time
			revisionNumber     int
			checkType          string
			checkSchemaVersion int
			configuration      []byte
		)
		if err := rows.Scan(&organizationID, &monitorID, &intervalSeconds, &updatedAt, &revisionNumber,
			&checkType, &checkSchemaVersion, &configuration); err != nil {
			return nil, fmt.Errorf("scan schedulable Monitor: %w", err)
		}
		values = append(values, run.Schedulable{
			OrganizationID:     organizationID,
			MonitorID:          monitorID,
			RevisionNumber:     revisionNumber,
			CheckType:          checkType,
			CheckSchemaVersion: checkSchemaVersion,
			CheckConfiguration: append(json.RawMessage(nil), configuration...),
			Interval:           time.Duration(intervalSeconds) * time.Second,
			// updated_at moves when a Monitor is activated or gains a revision, so it is the
			// floor on how far back a misfire may be recorded (ADR 0026).
			NotBefore: updatedAt.UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list schedulable Monitors: %w", err)
	}
	if len(values) > MaxSchedulableMonitors {
		return nil, fmt.Errorf(
			"this installation has more than %d active Monitors, which the single-read scheduler does not support",
			MaxSchedulableMonitors)
	}
	return values, nil
}

// LoadManualTarget returns the latest revision under the complete Monitor scope. Unlike the
// scheduler and confirmation path, it deliberately does not filter on Monitor state: an
// explicit manual request may exercise a draft or paused Monitor.
func (store *RunStore) LoadManualTarget(
	ctx context.Context, scope run.Scope,
) (run.Schedulable, bool, error) {
	var (
		value           run.Schedulable
		intervalSeconds int
		updatedAt       time.Time
		configuration   []byte
	)
	err := store.pool.QueryRow(ctx, `
SELECT m.organization_id, m.id, m.interval_seconds, m.updated_at,
       r.revision_number, r.check_type, r.check_schema_version, r.check_configuration
FROM monitors AS m
JOIN monitor_revisions AS r
  ON r.monitor_id = m.id AND r.organization_id = m.organization_id
 AND r.revision_number = m.latest_revision_number
WHERE m.id = $1 AND m.project_id = $2 AND m.organization_id = $3`,
		scope.MonitorID, scope.ProjectID, scope.OrganizationID).Scan(
		&value.OrganizationID, &value.MonitorID, &intervalSeconds, &updatedAt,
		&value.RevisionNumber, &value.CheckType, &value.CheckSchemaVersion, &configuration)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Schedulable{}, false, nil
	}
	if err != nil {
		return run.Schedulable{}, false, fmt.Errorf("load manual Run target: %w", err)
	}
	value.CheckConfiguration = append(json.RawMessage(nil), configuration...)
	value.Interval = time.Duration(intervalSeconds) * time.Second
	value.NotBefore = updatedAt.UTC()
	return value, true, nil
}

// LoadConfirmationTarget returns the exact still-current revision for a pending candidate.
func (store *RunStore) LoadConfirmationTarget(
	ctx context.Context, request run.ConfirmationRequest,
) (run.Schedulable, bool, error) {
	var (
		value           run.Schedulable
		intervalSeconds int
		updatedAt       time.Time
		configuration   []byte
	)
	err := store.pool.QueryRow(ctx, `
SELECT m.organization_id, m.id, m.interval_seconds, m.updated_at,
       r.revision_number, r.check_type, r.check_schema_version, r.check_configuration
FROM health_candidates AS c
JOIN monitors AS m
  ON m.id = c.monitor_id AND m.organization_id = c.organization_id
JOIN monitor_revisions AS r
  ON r.monitor_id = m.id AND r.organization_id = m.organization_id
 AND r.revision_number = c.source_revision_number
WHERE c.id = $1 AND c.organization_id = $2 AND c.monitor_id = $3
  AND c.source_revision_number = $4 AND c.triggering_run_id = $5
  AND c.triggering_scheduled_for = $6 AND c.state = 'pending'
  AND m.state = 'active' AND m.latest_revision_number = c.source_revision_number`,
		request.CandidateID, request.OrganizationID, request.MonitorID,
		request.RevisionNumber, request.TriggeringRunID,
		request.TriggeringScheduledFor.UTC()).Scan(
		&value.OrganizationID, &value.MonitorID, &intervalSeconds, &updatedAt,
		&value.RevisionNumber, &value.CheckType, &value.CheckSchemaVersion, &configuration)
	if errors.Is(err, pgx.ErrNoRows) {
		return run.Schedulable{}, false, nil
	}
	if err != nil {
		return run.Schedulable{}, false, fmt.Errorf("load confirmation target: %w", err)
	}
	value.CheckConfiguration = append(json.RawMessage(nil), configuration...)
	value.Interval = time.Duration(intervalSeconds) * time.Second
	value.NotBefore = updatedAt.UTC()
	return value, true, nil
}

func (store *RunStore) FindConfirmation(
	ctx context.Context, organizationID, candidateID string,
) (run.Run, bool, error) {
	return scanRun(store.pool.QueryRow(ctx, `
SELECT `+runColumns+`
FROM runs
WHERE organization_id = $1 AND confirmation_candidate_id = $2
ORDER BY scheduled_for DESC
LIMIT 1`, organizationID, candidateID))
}
