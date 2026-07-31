package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/probehive/probehive/internal/incident"
)

var _ incident.Store = (*IncidentStore)(nil)

type IncidentStore struct{ pool *pgxpool.Pool }

func (database *DB) Incidents() *IncidentStore { return &IncidentStore{pool: database.pool} }

func (store *IncidentStore) ProcessHealthTransition(
	ctx context.Context, event incident.HealthTransitionedV1, ids incident.ProcessIDs, now time.Time,
) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Incident projection: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	processed, err := outboxEventProcessed(ctx, transaction, event.EventID, event.OrganizationID)
	if err != nil {
		return err
	}
	if processed {
		return nil
	}

	var projectID, monitorID, oldState, newState, policyVersion, monitorState string
	var version int64
	var occurredAt time.Time
	var causalRunID *string
	var causalRunScheduled *time.Time
	var counts incident.Counts
	err = transaction.QueryRow(ctx, `
SELECT t.project_id, t.monitor_id, t.version, t.old_state, t.new_state,
       t.policy_version, t.causal_run_id, t.causal_run_scheduled_for,
       t.occurred_at, m.state,
       t.configured_count, t.eligible_count, t.responding_count,
       t.passing_count, t.failing_count, t.location_fault_count,
       t.indeterminate_count, t.missing_count
FROM health_transitions AS t
JOIN monitors AS m ON m.id = t.monitor_id AND m.organization_id = t.organization_id
WHERE t.id = $1 AND t.organization_id = $2
FOR UPDATE OF m`, event.TransitionID, event.OrganizationID).Scan(
		&projectID, &monitorID, &version, &oldState, &newState,
		&policyVersion, &causalRunID, &causalRunScheduled, &occurredAt, &monitorState,
		&counts.Configured, &counts.Eligible, &counts.Responding,
		&counts.Passing, &counts.Failing, &counts.LocationFault,
		&counts.Indeterminate, &counts.Missing)
	if errors.Is(err, pgx.ErrNoRows) {
		return incident.ErrPayloadInvalid
	}
	if err != nil {
		return fmt.Errorf("load health transition for Incident: %w", err)
	}
	if projectID != event.ProjectID || monitorID != event.MonitorID ||
		version != event.AggregateVersion || oldState != event.OldState ||
		newState != event.NewState || policyVersion != event.PolicyVersion {
		return incident.ErrPayloadInvalid
	}

	lastVersion, cursorFound, err := loadIncidentProjectionCursor(
		ctx, transaction, event.OrganizationID, monitorID)
	if err != nil {
		return err
	}
	if cursorFound && event.AggregateVersion <= lastVersion {
		if err := markOutboxEventProcessed(
			ctx, transaction, event.EventID, event.OrganizationID,
			"health.transitioned.v1", now); err != nil {
			return err
		}
		return commitIncidentProjection(ctx, transaction)
	}
	expectedVersion := int64(1)
	if cursorFound {
		expectedVersion = lastVersion + 1
	}
	if event.AggregateVersion != expectedVersion {
		return incident.ErrVersionGap
	}

	switch newState {
	case "down":
		if monitorState != "archived" {
			active, found, err := findActiveIncidentForUpdate(ctx, transaction, event.OrganizationID, monitorID)
			if err != nil {
				return err
			}
			if !found {
				if err := insertIncidentOpened(
					ctx, transaction, event, ids, causalRunID, causalRunScheduled, counts, occurredAt, now); err != nil {
					return err
				}
			} else {
				_ = active
			}
		}
	case "healthy":
		active, found, err := findActiveIncidentForUpdate(ctx, transaction, event.OrganizationID, monitorID)
		if err != nil {
			return err
		}
		if found {
			if err := resolveIncident(
				ctx, transaction, active, event, ids.TimelineID,
				ids.AlertEventID, causalRunID, causalRunScheduled, counts, occurredAt, now); err != nil {
				return err
			}
		}
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO incident_projection_cursors (
    organization_id, monitor_id, last_transition_version, updated_at
) VALUES ($1,$2,$3,$4)
ON CONFLICT (organization_id, monitor_id) DO UPDATE SET
    last_transition_version=EXCLUDED.last_transition_version,
    updated_at=EXCLUDED.updated_at`,
		event.OrganizationID, monitorID, event.AggregateVersion, now.UTC()); err != nil {
		return fmt.Errorf("advance Incident projection cursor: %w", err)
	}
	if err := markOutboxEventProcessed(ctx, transaction, event.EventID, event.OrganizationID, "health.transitioned.v1", now); err != nil {
		return err
	}
	return commitIncidentProjection(ctx, transaction)
}

func loadIncidentProjectionCursor(
	ctx context.Context, transaction pgx.Tx, organizationID, monitorID string,
) (int64, bool, error) {
	var version int64
	err := transaction.QueryRow(ctx, `
SELECT last_transition_version
FROM incident_projection_cursors
WHERE organization_id=$1 AND monitor_id=$2
FOR UPDATE`, organizationID, monitorID).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("load Incident projection cursor: %w", err)
	}
	return version, true, nil
}

func commitIncidentProjection(ctx context.Context, transaction pgx.Tx) error {
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Incident projection: %w", err)
	}
	return nil
}

func findActiveIncidentForUpdate(
	ctx context.Context, transaction pgx.Tx, organizationID, monitorID string,
) (incident.Incident, bool, error) {
	return scanIncident(transaction.QueryRow(ctx, `
SELECT id, organization_id, project_id, monitor_id, state, version,
       opened_transition_id, acknowledged_by, acknowledged_at,
       resolved_transition_id, resolved_at, created_at, updated_at
FROM incidents
WHERE organization_id = $1 AND monitor_id = $2 AND state <> 'resolved'
FOR UPDATE`, organizationID, monitorID))
}

func insertIncidentOpened(
	ctx context.Context, transaction pgx.Tx, event incident.HealthTransitionedV1,
	ids incident.ProcessIDs, causalRunID *string, causalRunScheduled *time.Time,
	counts incident.Counts, occurredAt, queuedAt time.Time,
) error {
	if _, err := transaction.Exec(ctx, `
INSERT INTO incidents (
    id, organization_id, project_id, monitor_id, state, version,
    opened_transition_id, created_at, updated_at
) VALUES ($1,$2,$3,$4,'open',1,$5,$6,$6)`,
		ids.IncidentID, event.OrganizationID, event.ProjectID, event.MonitorID,
		event.TransitionID, occurredAt.UTC()); err != nil {
		return fmt.Errorf("open Incident: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO incident_timeline_entries (
    id, organization_id, incident_id, incident_version, kind,
    health_transition_id, old_health_state, new_health_state, policy_version,
    causal_run_id, causal_run_scheduled_for,
    configured_count, eligible_count, responding_count, passing_count,
    failing_count, location_fault_count, indeterminate_count, missing_count,
    occurred_at
) VALUES ($1,$2,$3,1,'opened',$4,$5,$6,$7,$8,$9,
          $10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		ids.TimelineID, event.OrganizationID, ids.IncidentID, event.TransitionID,
		event.OldState, event.NewState, event.PolicyVersion,
		causalRunID, causalRunScheduled,
		counts.Configured, counts.Eligible, counts.Responding, counts.Passing,
		counts.Failing, counts.LocationFault, counts.Indeterminate, counts.Missing,
		occurredAt.UTC()); err != nil {
		return fmt.Errorf("append Incident opening timeline: %w", err)
	}
	return insertIncidentTransitionedEvent(
		ctx, transaction, event, ids.IncidentID, 1, "opened",
		ids.AlertEventID, occurredAt, queuedAt,
	)
}

func resolveIncident(
	ctx context.Context, transaction pgx.Tx, value incident.Incident,
	event incident.HealthTransitionedV1, timelineID string,
	alertEventID string,
	causalRunID *string, causalRunScheduled *time.Time,
	counts incident.Counts, occurredAt, queuedAt time.Time,
) error {
	version := value.Version + 1
	if _, err := transaction.Exec(ctx, `
UPDATE incidents
SET state='resolved', version=$1, resolved_transition_id=$2,
    resolved_at=$3, updated_at=$3
WHERE id=$4 AND organization_id=$5 AND state <> 'resolved'`,
		version, event.TransitionID, occurredAt.UTC(), value.ID, value.OrganizationID); err != nil {
		return fmt.Errorf("resolve Incident: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO incident_timeline_entries (
    id, organization_id, incident_id, incident_version, kind,
    health_transition_id, old_health_state, new_health_state, policy_version,
    causal_run_id, causal_run_scheduled_for,
    configured_count, eligible_count, responding_count, passing_count,
    failing_count, location_fault_count, indeterminate_count, missing_count,
    occurred_at
) VALUES ($1,$2,$3,$4,'resolved',$5,$6,$7,$8,$9,$10,
          $11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		timelineID, value.OrganizationID, value.ID, version, event.TransitionID,
		event.OldState, event.NewState, event.PolicyVersion,
		causalRunID, causalRunScheduled,
		counts.Configured, counts.Eligible, counts.Responding, counts.Passing,
		counts.Failing, counts.LocationFault, counts.Indeterminate, counts.Missing,
		occurredAt.UTC()); err != nil {
		return fmt.Errorf("append Incident resolution timeline: %w", err)
	}
	return insertIncidentTransitionedEvent(
		ctx, transaction, event, value.ID, version, "resolved",
		alertEventID, occurredAt, queuedAt,
	)
}

func insertIncidentTransitionedEvent(
	ctx context.Context, transaction pgx.Tx, source incident.HealthTransitionedV1,
	incidentID string, version int64, transition, eventID string,
	occurredAt, queuedAt time.Time,
) error {
	if eventID == "" {
		return errors.New("an Incident Alert event identifier is required")
	}
	payload, err := json.Marshal(incident.IncidentTransitionedV1{
		EventID: eventID, OrganizationID: source.OrganizationID,
		OccurredAt: occurredAt.UTC(), AggregateType: "incident",
		AggregateID: incidentID, AggregateVersion: version,
		CausationID: source.EventID, IncidentID: incidentID,
		ProjectID: source.ProjectID, MonitorID: source.MonitorID,
		Transition: transition,
	})
	if err != nil {
		return fmt.Errorf("encode incident.transitioned.v1: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO outbox_entries (
    id, organization_id, topic, payload, attempts, created_at, available_at
) VALUES ($1,$2,$3,$4,0,$5,$5)`,
		eventID, source.OrganizationID, incident.TopicIncidentTransitionedV1,
		payload, queuedAt.UTC()); err != nil {
		return fmt.Errorf("enqueue incident.transitioned.v1: %w", err)
	}
	return nil
}

func (store *IncidentStore) ListIncidents(
	ctx context.Context, scope incident.Scope, query incident.ListQuery,
) ([]incident.Incident, bool, bool, error) {
	if query.PageSize < 1 || query.PageSize > incident.MaxPageSize {
		return nil, false, false, fmt.Errorf("an Incident page is 1 to %d rows", incident.MaxPageSize)
	}
	var exists bool
	if err := store.pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM monitors WHERE id=$1 AND organization_id=$2 AND project_id=$3)`,
		scope.MonitorID, scope.OrganizationID, scope.ProjectID).Scan(&exists); err != nil {
		return nil, false, false, fmt.Errorf("check Incident Monitor scope: %w", err)
	}
	if !exists {
		return nil, false, false, nil
	}
	var cursorTime *time.Time
	var cursorID *string
	if query.Cursor != nil {
		createdAt := query.Cursor.CreatedAt.UTC()
		cursorTime, cursorID = &createdAt, &query.Cursor.ID
	}
	rows, err := store.pool.Query(ctx, `
SELECT id, organization_id, project_id, monitor_id, state, version,
       opened_transition_id, acknowledged_by, acknowledged_at,
       resolved_transition_id, resolved_at, created_at, updated_at
FROM incidents
WHERE monitor_id=$1 AND organization_id=$2 AND project_id=$3
  AND (
      $4::timestamptz IS NULL
      OR (created_at, id) < ($4::timestamptz, $5::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT $6`, scope.MonitorID, scope.OrganizationID, scope.ProjectID,
		cursorTime, cursorID, query.PageSize+1)
	if err != nil {
		return nil, false, false, fmt.Errorf("list Incidents: %w", err)
	}
	defer rows.Close()
	values := make([]incident.Incident, 0, query.PageSize+1)
	for rows.Next() {
		value, _, err := scanIncident(rows)
		if err != nil {
			return nil, false, false, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, false, false, fmt.Errorf("list Incidents: %w", err)
	}
	more := len(values) > query.PageSize
	if more {
		values = values[:query.PageSize]
	}
	return values, more, true, nil
}

func (store *IncidentStore) GetIncident(
	ctx context.Context, scope incident.Scope, id string,
) (incident.Incident, bool, error) {
	value, found, err := scanIncident(store.pool.QueryRow(ctx, `
SELECT id, organization_id, project_id, monitor_id, state, version,
       opened_transition_id, acknowledged_by, acknowledged_at,
       resolved_transition_id, resolved_at, created_at, updated_at
FROM incidents
WHERE id=$1 AND monitor_id=$2 AND organization_id=$3 AND project_id=$4`,
		id, scope.MonitorID, scope.OrganizationID, scope.ProjectID))
	if err != nil || !found {
		return incident.Incident{}, found, err
	}
	timeline, err := store.listTimeline(ctx, scope.OrganizationID, id)
	if err != nil {
		return incident.Incident{}, false, err
	}
	value.Timeline = timeline
	return value, true, nil
}

func (store *IncidentStore) AcknowledgeIncident(
	ctx context.Context, scope incident.Scope, id, actorID, timelineID string, now time.Time,
) (incident.Incident, bool, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return incident.Incident{}, false, err
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	value, found, err := scanIncident(transaction.QueryRow(ctx, `
SELECT id, organization_id, project_id, monitor_id, state, version,
       opened_transition_id, acknowledged_by, acknowledged_at,
       resolved_transition_id, resolved_at, created_at, updated_at
FROM incidents
WHERE id=$1 AND monitor_id=$2 AND organization_id=$3 AND project_id=$4
FOR UPDATE`, id, scope.MonitorID, scope.OrganizationID, scope.ProjectID))
	if err != nil || !found {
		return incident.Incident{}, found, err
	}
	if value.State == incident.StateResolved {
		return incident.Incident{}, true, incident.ErrConflict
	}
	if value.State == incident.StateOpen {
		version := value.Version + 1
		if _, err := transaction.Exec(ctx, `
UPDATE incidents SET state='acknowledged', version=$1, acknowledged_by=$2,
       acknowledged_at=$3, updated_at=$3
WHERE id=$4 AND organization_id=$5`,
			version, actorID, now.UTC(), id, scope.OrganizationID); err != nil {
			return incident.Incident{}, false, fmt.Errorf("acknowledge Incident: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
INSERT INTO incident_timeline_entries (
    id, organization_id, incident_id, incident_version, kind, actor_user_id, occurred_at
) VALUES ($1,$2,$3,$4,'acknowledged',$5,$6)`,
			timelineID, scope.OrganizationID, id, version, actorID, now.UTC()); err != nil {
			return incident.Incident{}, false, fmt.Errorf("append Incident acknowledgement timeline: %w", err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return incident.Incident{}, false, err
	}
	return store.GetIncident(ctx, scope, id)
}

func (store *IncidentStore) listTimeline(ctx context.Context, organizationID, incidentID string) ([]incident.TimelineEntry, error) {
	rows, err := store.pool.Query(ctx, `
SELECT id, incident_id, incident_version, kind, health_transition_id, actor_user_id,
       old_health_state, new_health_state, policy_version,
       causal_run_id, causal_run_scheduled_for,
       configured_count, eligible_count, responding_count, passing_count,
       failing_count, location_fault_count, indeterminate_count, missing_count,
       occurred_at
FROM incident_timeline_entries
WHERE organization_id=$1 AND incident_id=$2
ORDER BY incident_version`, organizationID, incidentID)
	if err != nil {
		return nil, fmt.Errorf("list Incident timeline: %w", err)
	}
	defer rows.Close()
	values := make([]incident.TimelineEntry, 0)
	for rows.Next() {
		var value incident.TimelineEntry
		var kind string
		var transitionID, actorID, oldState, newState, policy, runID *string
		var runScheduled *time.Time
		var configured, eligible, responding, passing, failing *int
		var locationFault, indeterminate, missing *int
		if err := rows.Scan(&value.ID, &value.IncidentID, &value.IncidentVersion, &kind,
			&transitionID, &actorID, &oldState, &newState, &policy, &runID, &runScheduled,
			&configured, &eligible, &responding, &passing, &failing,
			&locationFault, &indeterminate, &missing,
			&value.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan Incident timeline: %w", err)
		}
		value.Kind = incident.TimelineKind(kind)
		value.HealthTransitionID, value.ActorUserID = textValue(transitionID), textValue(actorID)
		value.OldHealthState, value.NewHealthState = textValue(oldState), textValue(newState)
		value.PolicyVersion, value.CausalRunID = textValue(policy), textValue(runID)
		if runScheduled != nil {
			value.CausalRunScheduledFor = runScheduled.UTC()
		}
		if configured != nil {
			value.Counts = &incident.Counts{
				Configured: *configured, Eligible: *eligible, Responding: *responding,
				Passing: *passing, Failing: *failing, LocationFault: *locationFault,
				Indeterminate: *indeterminate, Missing: *missing,
			}
		}
		value.OccurredAt = value.OccurredAt.UTC()
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func scanIncident(row rowScanner) (incident.Incident, bool, error) {
	var value incident.Incident
	var state string
	var acknowledgedBy, resolvedTransitionID *string
	var acknowledgedAt, resolvedAt *time.Time
	err := row.Scan(&value.ID, &value.OrganizationID, &value.ProjectID, &value.MonitorID,
		&state, &value.Version, &value.OpenedTransitionID, &acknowledgedBy, &acknowledgedAt,
		&resolvedTransitionID, &resolvedAt, &value.CreatedAt, &value.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return incident.Incident{}, false, nil
	}
	if err != nil {
		return incident.Incident{}, false, fmt.Errorf("scan Incident: %w", err)
	}
	value.State = incident.State(state)
	value.AcknowledgedBy, value.ResolvedTransitionID = textValue(acknowledgedBy), textValue(resolvedTransitionID)
	if acknowledgedAt != nil {
		value.AcknowledgedAt = acknowledgedAt.UTC()
	}
	if resolvedAt != nil {
		value.ResolvedAt = resolvedAt.UTC()
	}
	value.CreatedAt, value.UpdatedAt = value.CreatedAt.UTC(), value.UpdatedAt.UTC()
	return value, true, nil
}
