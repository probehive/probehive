package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/probehive/probehive/internal/health"
)

var _ health.Store = (*HealthStore)(nil)

type HealthStore struct{ pool *pgxpool.Pool }

func (database *DB) Health() *HealthStore { return &HealthStore{pool: database.pool} }

func (store *HealthStore) ProcessRunRecorded(
	ctx context.Context,
	event health.RunRecordedV1,
	ids health.ProcessIDs,
	now time.Time,
) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin health evaluation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	processed, err := outboxEventProcessed(ctx, transaction, event.EventID, event.OrganizationID)
	if err != nil {
		return err
	}
	if processed {
		return nil
	}

	var projectID, monitorState, runMonitorID, location, kind, outcome string
	var revisionNumber, latestRevisionNumber int
	var scheduledFor time.Time
	var finishedAt *time.Time
	var failureCode, candidateID *string
	err = transaction.QueryRow(ctx, `
SELECT m.project_id, m.latest_revision_number, m.state,
       r.monitor_id, r.revision_number, r.location, r.scheduled_for,
       r.kind, r.outcome, r.finished_at, o.failure_code, r.confirmation_candidate_id
FROM runs AS r
JOIN monitors AS m ON m.id = r.monitor_id AND m.organization_id = r.organization_id
LEFT JOIN observations AS o
  ON o.run_id = r.id AND o.scheduled_for = r.scheduled_for
WHERE r.id = $1 AND r.scheduled_for = $2 AND r.organization_id = $3
FOR UPDATE OF m`,
		event.RunID, event.ScheduledFor.UTC(), event.OrganizationID).Scan(
		&projectID, &latestRevisionNumber, &monitorState,
		&runMonitorID, &revisionNumber, &location, &scheduledFor,
		&kind, &outcome, &finishedAt, &failureCode, &candidateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return health.ErrPayloadInvalid
	}
	if err != nil {
		return fmt.Errorf("load Run for health evaluation: %w", err)
	}
	if runMonitorID != event.MonitorID || revisionNumber != event.RevisionNumber ||
		location != event.Location || kind != event.Kind || outcome != event.Outcome ||
		!scheduledFor.Equal(event.ScheduledFor) {
		return health.ErrPayloadInvalid
	}
	if monitorState != "active" {
		if err := markOutboxEventProcessed(ctx, transaction, event.EventID, event.OrganizationID, health.TopicRunRecordedV1, now); err != nil {
			return err
		}
		return commitHealth(ctx, transaction)
	}

	current, found, err := loadHealthForUpdate(ctx, transaction, event.OrganizationID, projectID, event.MonitorID)
	if err != nil {
		return err
	}
	if !found {
		current, err = health.Initial(event.OrganizationID, projectID, event.MonitorID, now)
		if err != nil {
			return err
		}
	}
	input := health.Input{
		EventID: event.EventID, RunID: event.RunID, Kind: kind, Outcome: outcome,
		RevisionNumber: revisionNumber, LatestRevisionNumber: latestRevisionNumber,
		ScheduledFor: scheduledFor.UTC(), Now: now, NewCandidateID: ids.CandidateID,
	}
	if finishedAt != nil {
		input.FinishedAt = finishedAt.UTC()
	}
	if failureCode != nil {
		input.FailureCode = *failureCode
	}
	if candidateID != nil {
		input.CandidateID = *candidateID
	}
	decision, err := health.Evaluate(current, input)
	if err != nil {
		return fmt.Errorf("evaluate Monitor health: %w", err)
	}

	if decision.CandidateCompleted != nil {
		if _, err := transaction.Exec(ctx, `
UPDATE health_candidates SET state = $1, completed_at = $2
WHERE id = $3 AND organization_id = $4 AND state = 'pending'`,
			string(decision.CandidateCompleted.State), now.UTC(),
			decision.CandidateCompleted.ID, event.OrganizationID); err != nil {
			return fmt.Errorf("complete health candidate: %w", err)
		}
	}
	if decision.CandidateCreated != nil {
		candidate := decision.CandidateCreated
		if _, err := transaction.Exec(ctx, `
INSERT INTO health_candidates (
    id, organization_id, project_id, monitor_id, source_revision_number,
    direction, expected_evidence, state, triggering_run_id,
    triggering_scheduled_for, triggering_event_id, requested_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (
    organization_id, monitor_id, source_revision_number, direction,
    triggering_run_id, triggering_scheduled_for
) DO NOTHING`,
			candidate.ID, candidate.OrganizationID, candidate.ProjectID, candidate.MonitorID,
			candidate.SourceRevisionNumber, string(candidate.Direction),
			string(candidate.ExpectedEvidence), string(candidate.State),
			candidate.TriggeringRunID, candidate.TriggeringScheduledFor.UTC(),
			candidate.TriggeringEventID, candidate.RequestedAt.UTC()); err != nil {
			return fmt.Errorf("insert health candidate: %w", err)
		}
		if err := insertConfirmationRequest(ctx, transaction, event, *candidate, ids.ConfirmationEventID, location, now); err != nil {
			return err
		}
	}
	if err := upsertHealth(ctx, transaction, decision.Snapshot); err != nil {
		return err
	}
	if decision.Transitioned {
		if err := insertHealthTransition(ctx, transaction, event, decision, ids, now); err != nil {
			return err
		}
	}
	if err := markOutboxEventProcessed(ctx, transaction, event.EventID, event.OrganizationID, health.TopicRunRecordedV1, now); err != nil {
		return err
	}
	return commitHealth(ctx, transaction)
}

func loadHealthForUpdate(
	ctx context.Context, transaction pgx.Tx, organizationID, projectID, monitorID string,
) (health.Snapshot, bool, error) {
	var value health.Snapshot
	var state, stableState, policyVersion string
	var sourceRevision *int
	var lastScheduled, lastDeterminate, lastRunScheduled *time.Time
	var lastRunID, candidateID *string
	err := transaction.QueryRow(ctx, `
SELECT state, stable_state, policy_version, version, source_revision_number,
       last_scheduled_for, last_determinate_finished_at, last_run_id,
       last_run_scheduled_for, candidate_id,
       configured_count, eligible_count, responding_count, passing_count,
       failing_count, location_fault_count, indeterminate_count, missing_count,
       transitioned_at, updated_at
FROM monitor_health
WHERE monitor_id = $1 AND organization_id = $2 AND project_id = $3
FOR UPDATE`, monitorID, organizationID, projectID).Scan(
		&state, &stableState, &policyVersion, &value.Version, &sourceRevision,
		&lastScheduled, &lastDeterminate, &lastRunID, &lastRunScheduled, &candidateID,
		&value.Counts.Configured, &value.Counts.Eligible, &value.Counts.Responding,
		&value.Counts.Passing, &value.Counts.Failing, &value.Counts.LocationFault,
		&value.Counts.Indeterminate, &value.Counts.Missing,
		&value.TransitionedAt, &value.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return health.Snapshot{}, false, nil
	}
	if err != nil {
		return health.Snapshot{}, false, fmt.Errorf("load Monitor health: %w", err)
	}
	value.OrganizationID, value.ProjectID, value.MonitorID = organizationID, projectID, monitorID
	value.State, value.StableState, value.PolicyVersion = health.State(state), health.State(stableState), policyVersion
	if sourceRevision != nil {
		value.SourceRevisionNumber = *sourceRevision
	}
	if lastScheduled != nil {
		value.LastScheduledFor = lastScheduled.UTC()
	}
	if lastDeterminate != nil {
		value.LastDeterminateFinishedAt = lastDeterminate.UTC()
	}
	if lastRunID != nil {
		value.LastRunID = *lastRunID
	}
	if lastRunScheduled != nil {
		value.LastRunScheduledFor = lastRunScheduled.UTC()
	}
	value.TransitionedAt, value.UpdatedAt = value.TransitionedAt.UTC(), value.UpdatedAt.UTC()
	if candidateID != nil {
		candidate, found, err := loadHealthCandidate(ctx, transaction, organizationID, *candidateID)
		if err != nil {
			return health.Snapshot{}, false, err
		}
		if !found {
			return health.Snapshot{}, false, errors.New("Monitor health names a missing candidate")
		}
		value.Candidate = &candidate
	}
	return value, true, nil
}

func loadHealthCandidate(ctx context.Context, transaction pgx.Tx, organizationID, id string) (health.Candidate, bool, error) {
	var value health.Candidate
	var direction, expected, state string
	err := transaction.QueryRow(ctx, `
SELECT id, organization_id, project_id, monitor_id, source_revision_number,
       direction, expected_evidence, state, triggering_run_id,
       triggering_scheduled_for, triggering_event_id, requested_at
FROM health_candidates WHERE id = $1 AND organization_id = $2`, id, organizationID).Scan(
		&value.ID, &value.OrganizationID, &value.ProjectID, &value.MonitorID,
		&value.SourceRevisionNumber, &direction, &expected, &state,
		&value.TriggeringRunID, &value.TriggeringScheduledFor,
		&value.TriggeringEventID, &value.RequestedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return health.Candidate{}, false, nil
	}
	if err != nil {
		return health.Candidate{}, false, fmt.Errorf("load health candidate: %w", err)
	}
	value.Direction, value.ExpectedEvidence, value.State =
		health.Direction(direction), health.Evidence(expected), health.CandidateState(state)
	value.TriggeringScheduledFor, value.RequestedAt = value.TriggeringScheduledFor.UTC(), value.RequestedAt.UTC()
	return value, true, nil
}

func upsertHealth(ctx context.Context, transaction pgx.Tx, value health.Snapshot) error {
	var candidateID any
	if value.Candidate != nil {
		candidateID = value.Candidate.ID
	}
	_, err := transaction.Exec(ctx, `
INSERT INTO monitor_health (
    monitor_id, organization_id, project_id, state, stable_state, policy_version,
    version, source_revision_number, last_scheduled_for, last_determinate_finished_at,
    last_run_id, last_run_scheduled_for, candidate_id,
    configured_count, eligible_count, responding_count, passing_count, failing_count,
    location_fault_count, indeterminate_count, missing_count, transitioned_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,0),$9,$10,NULLIF($11::text,'')::uuid,$12,$13,
          $14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
ON CONFLICT (monitor_id) DO UPDATE SET
    state=EXCLUDED.state, stable_state=EXCLUDED.stable_state,
    policy_version=EXCLUDED.policy_version, version=EXCLUDED.version,
    source_revision_number=EXCLUDED.source_revision_number,
    last_scheduled_for=EXCLUDED.last_scheduled_for,
    last_determinate_finished_at=EXCLUDED.last_determinate_finished_at,
    last_run_id=EXCLUDED.last_run_id, last_run_scheduled_for=EXCLUDED.last_run_scheduled_for,
    candidate_id=EXCLUDED.candidate_id, configured_count=EXCLUDED.configured_count,
    eligible_count=EXCLUDED.eligible_count, responding_count=EXCLUDED.responding_count,
    passing_count=EXCLUDED.passing_count, failing_count=EXCLUDED.failing_count,
    location_fault_count=EXCLUDED.location_fault_count,
    indeterminate_count=EXCLUDED.indeterminate_count, missing_count=EXCLUDED.missing_count,
    transitioned_at=EXCLUDED.transitioned_at, updated_at=EXCLUDED.updated_at`,
		value.MonitorID, value.OrganizationID, value.ProjectID, string(value.State),
		string(value.StableState), value.PolicyVersion, value.Version, value.SourceRevisionNumber,
		nullTime(value.LastScheduledFor), nullTime(value.LastDeterminateFinishedAt),
		value.LastRunID, nullTime(value.LastRunScheduledFor), candidateID,
		value.Counts.Configured, value.Counts.Eligible, value.Counts.Responding,
		value.Counts.Passing, value.Counts.Failing, value.Counts.LocationFault,
		value.Counts.Indeterminate, value.Counts.Missing,
		value.TransitionedAt.UTC(), value.UpdatedAt.UTC())
	if err != nil {
		return fmt.Errorf("upsert Monitor health: %w", err)
	}
	return nil
}

func insertConfirmationRequest(
	ctx context.Context, transaction pgx.Tx, source health.RunRecordedV1,
	candidate health.Candidate, eventID, location string, now time.Time,
) error {
	event := health.ConfirmationRequestedV1{
		EventID: eventID, OrganizationID: candidate.OrganizationID, OccurredAt: now,
		AggregateType: "healthCandidate", AggregateID: candidate.ID, AggregateVersion: 1,
		CausationID: source.EventID, CandidateID: candidate.ID, MonitorID: candidate.MonitorID,
		RevisionNumber: candidate.SourceRevisionNumber, Location: location,
		TriggeringRunID:        candidate.TriggeringRunID,
		TriggeringScheduledFor: candidate.TriggeringScheduledFor,
		RequestedFor:           now, ExpectedEvidence: string(candidate.ExpectedEvidence),
		PolicyVersion: health.PolicyVersion,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal confirmation request: %w", err)
	}
	return insertRawOutbox(ctx, transaction, eventID, candidate.OrganizationID,
		health.TopicConfirmationRequestedV1, payload, now)
}

func insertHealthTransition(
	ctx context.Context, transaction pgx.Tx, source health.RunRecordedV1,
	decision health.Decision, ids health.ProcessIDs, now time.Time,
) error {
	value := decision.Snapshot
	if _, err := transaction.Exec(ctx, `
INSERT INTO health_transitions (
    id, organization_id, project_id, monitor_id, version, old_state, new_state,
    policy_version, source_revision_number, causal_run_id, causal_run_scheduled_for,
    configured_count, eligible_count, responding_count, passing_count, failing_count,
    location_fault_count, indeterminate_count, missing_count, occurred_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,0),$10,$11,
          $12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		ids.TransitionID, value.OrganizationID, value.ProjectID, value.MonitorID,
		value.Version, string(decision.PreviousState), string(value.State), value.PolicyVersion,
		value.SourceRevisionNumber, source.RunID, source.ScheduledFor.UTC(),
		value.Counts.Configured, value.Counts.Eligible, value.Counts.Responding,
		value.Counts.Passing, value.Counts.Failing, value.Counts.LocationFault,
		value.Counts.Indeterminate, value.Counts.Missing, now.UTC()); err != nil {
		return fmt.Errorf("insert health transition: %w", err)
	}
	event := health.HealthTransitionedV1{
		EventID: ids.TransitionEventID, OrganizationID: value.OrganizationID, OccurredAt: now,
		AggregateType: "monitorHealth", AggregateID: value.MonitorID,
		AggregateVersion: value.Version, CausationID: source.EventID,
		TransitionID: ids.TransitionID, MonitorID: value.MonitorID, ProjectID: value.ProjectID,
		OldState: string(decision.PreviousState), NewState: string(value.State),
		PolicyVersion: value.PolicyVersion,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal health transition: %w", err)
	}
	return insertRawOutbox(ctx, transaction, event.EventID, value.OrganizationID,
		health.TopicHealthTransitionedV1, payload, now)
}

func insertRawOutbox(
	ctx context.Context, transaction pgx.Tx,
	id, organizationID, topic string, payload []byte, now time.Time,
) error {
	if _, err := transaction.Exec(ctx, `
INSERT INTO outbox_entries (id, organization_id, topic, payload, attempts, created_at, available_at)
VALUES ($1,$2,$3,$4,0,$5,$5)`, id, organizationID, topic, payload, now.UTC()); err != nil {
		return fmt.Errorf("insert outbox entry: %w", err)
	}
	return nil
}

func commitHealth(ctx context.Context, transaction pgx.Tx) error {
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit health evaluation: %w", err)
	}
	return nil
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func (store *HealthStore) GetHealth(ctx context.Context, scope health.Scope) (health.Snapshot, bool, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return health.Snapshot{}, false, fmt.Errorf("begin health query: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var createdAt time.Time
	err = transaction.QueryRow(ctx, `
SELECT created_at FROM monitors
WHERE id = $1 AND organization_id = $2 AND project_id = $3`,
		scope.MonitorID, scope.OrganizationID, scope.ProjectID).Scan(&createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return health.Snapshot{}, false, nil
	}
	if err != nil {
		return health.Snapshot{}, false, fmt.Errorf("check Monitor health scope: %w", err)
	}
	value, found, err := loadHealthForUpdate(ctx, transaction, scope.OrganizationID, scope.ProjectID, scope.MonitorID)
	if err != nil {
		return health.Snapshot{}, false, err
	}
	if !found {
		value, err = health.Initial(scope.OrganizationID, scope.ProjectID, scope.MonitorID, createdAt.UTC())
		if err != nil {
			return health.Snapshot{}, false, err
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return health.Snapshot{}, false, err
	}
	return value, true, nil
}
func (store *HealthStore) ListStaleHealth(
	ctx context.Context, now time.Time, executionCeiling time.Duration, limit int,
) ([]health.StaleTarget, error) {
	if executionCeiling <= 0 || limit < 1 || limit > 100 {
		return nil, errors.New("stale health query requires positive bounded settings")
	}
	rows, err := store.pool.Query(ctx, `
SELECT h.organization_id, h.project_id, h.monitor_id, h.version
FROM monitor_health AS h
JOIN monitors AS m ON m.id=h.monitor_id AND m.organization_id=h.organization_id
WHERE m.state='active' AND h.state <> 'unknown'
  AND h.last_determinate_finished_at IS NOT NULL
  AND h.last_determinate_finished_at <= $1::timestamptz - ($2::bigint * interval '1 microsecond')
  AND h.last_determinate_finished_at <= $1::timestamptz - make_interval(secs => m.interval_seconds * 3)
ORDER BY h.last_determinate_finished_at, h.monitor_id
LIMIT $3`, now.UTC(), (2 * executionCeiling).Microseconds(), limit)
	if err != nil {
		return nil, fmt.Errorf("list stale Monitor health: %w", err)
	}
	defer rows.Close()
	values := make([]health.StaleTarget, 0, limit)
	for rows.Next() {
		var value health.StaleTarget
		if err := rows.Scan(&value.OrganizationID, &value.ProjectID, &value.MonitorID, &value.Version); err != nil {
			return nil, fmt.Errorf("scan stale Monitor health: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan stale Monitor health: %w", err)
	}
	return values, nil
}

func (store *HealthStore) MarkHealthStale(
	ctx context.Context, target health.StaleTarget, transitionID, eventID string,
	now time.Time, executionCeiling time.Duration,
) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin stale health transition: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	current, found, err := loadHealthForUpdate(
		ctx, transaction, target.OrganizationID, target.ProjectID, target.MonitorID)
	if err != nil {
		return err
	}
	if !found || current.Version != target.Version || current.State == health.StateUnknown {
		return commitHealth(ctx, transaction)
	}
	var monitorState string
	var intervalSeconds int
	if err := transaction.QueryRow(ctx, `
SELECT state, interval_seconds FROM monitors
WHERE id=$1 AND organization_id=$2 AND project_id=$3
FOR UPDATE`, target.MonitorID, target.OrganizationID, target.ProjectID).Scan(
		&monitorState, &intervalSeconds); err != nil {
		return fmt.Errorf("load Monitor for stale health transition: %w", err)
	}
	if monitorState != "active" {
		return commitHealth(ctx, transaction)
	}
	staleAfter, err := health.StaleAfter(
		time.Duration(intervalSeconds)*time.Second, executionCeiling)
	if err != nil {
		return err
	}
	if current.LastDeterminateFinishedAt.IsZero() ||
		now.Before(current.LastDeterminateFinishedAt.Add(staleAfter)) {
		return commitHealth(ctx, transaction)
	}
	decision, err := health.MarkStale(current, now)
	if err != nil {
		return err
	}
	if decision.CandidateCompleted != nil {
		if _, err := transaction.Exec(ctx, `
UPDATE health_candidates SET state='stale', completed_at=$1
WHERE id=$2 AND organization_id=$3 AND state='pending'`,
			now.UTC(), decision.CandidateCompleted.ID, target.OrganizationID); err != nil {
			return fmt.Errorf("stale health candidate: %w", err)
		}
	}
	if err := upsertHealth(ctx, transaction, decision.Snapshot); err != nil {
		return err
	}
	value := decision.Snapshot
	if _, err := transaction.Exec(ctx, `
INSERT INTO health_transitions (
    id, organization_id, project_id, monitor_id, version, old_state, new_state,
    policy_version, source_revision_number, causal_run_id, causal_run_scheduled_for,
    configured_count, eligible_count, responding_count, passing_count, failing_count,
    location_fault_count, indeterminate_count, missing_count, occurred_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,0),NULL,NULL,
          $10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		transitionID, value.OrganizationID, value.ProjectID, value.MonitorID,
		value.Version, string(decision.PreviousState), string(value.State), value.PolicyVersion,
		value.SourceRevisionNumber, value.Counts.Configured, value.Counts.Eligible,
		value.Counts.Responding, value.Counts.Passing, value.Counts.Failing,
		value.Counts.LocationFault, value.Counts.Indeterminate, value.Counts.Missing,
		now.UTC()); err != nil {
		return fmt.Errorf("insert stale health transition: %w", err)
	}
	event := health.HealthTransitionedV1{
		EventID: eventID, OrganizationID: value.OrganizationID, OccurredAt: now,
		AggregateType: "monitorHealth", AggregateID: value.MonitorID,
		AggregateVersion: value.Version, TransitionID: transitionID,
		MonitorID: value.MonitorID, ProjectID: value.ProjectID,
		OldState: string(decision.PreviousState), NewState: string(value.State),
		PolicyVersion: value.PolicyVersion,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal stale health transition: %w", err)
	}
	if err := insertRawOutbox(ctx, transaction, eventID, value.OrganizationID,
		health.TopicHealthTransitionedV1, payload, now); err != nil {
		return err
	}
	return commitHealth(ctx, transaction)
}
