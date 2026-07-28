-- Evaluated health, confirmation causation, Incidents, and the durable outbox
-- dispatcher (ADR 0027, ADR 0028).

ALTER TABLE runs
    ADD COLUMN confirmation_candidate_id uuid,
    ADD COLUMN triggering_run_id uuid,
    ADD COLUMN triggering_scheduled_for timestamp with time zone,
    ADD COLUMN causation_event_id uuid,
    ADD COLUMN policy_version character varying(32),
    ADD CONSTRAINT ck_runs_confirmation_causation CHECK (
        CASE
            WHEN kind = 'confirmation' THEN
                confirmation_candidate_id IS NOT NULL
                AND triggering_run_id IS NOT NULL
                AND triggering_scheduled_for IS NOT NULL
                AND causation_event_id IS NOT NULL
                AND policy_version IS NOT NULL
            ELSE
                confirmation_candidate_id IS NULL
                AND triggering_run_id IS NULL
                AND triggering_scheduled_for IS NULL
                AND causation_event_id IS NULL
                AND policy_version IS NULL
        END
    );

CREATE UNIQUE INDEX ux_runs_confirmation_candidate
    ON runs (confirmation_candidate_id, scheduled_for)
    WHERE kind = 'confirmation';

ALTER TABLE outbox_entries
    ADD COLUMN lease_holder character varying(128),
    ADD COLUMN lease_expires_at timestamp with time zone,
    ADD COLUMN last_failure_code character varying(100),
    ADD COLUMN gap_first_seen_at timestamp with time zone,
    ADD CONSTRAINT ck_outbox_entries_lease CHECK (
        (lease_holder IS NULL) = (lease_expires_at IS NULL)
    );

DROP INDEX ix_outbox_entries_available_at;
CREATE INDEX ix_outbox_entries_claim
    ON outbox_entries (available_at, id)
    WHERE lease_expires_at IS NULL;

CREATE TABLE processed_outbox_events (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    topic character varying(100) NOT NULL,
    processed_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_processed_outbox_events PRIMARY KEY (organization_id, id),
    CONSTRAINT fk_processed_outbox_events_organizations FOREIGN KEY (organization_id)
        REFERENCES organizations (id) ON DELETE CASCADE
);

CREATE INDEX ix_processed_outbox_events_processed_at
    ON processed_outbox_events (processed_at);

CREATE TABLE dead_letter_outbox_entries (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    topic character varying(100) NOT NULL,
    payload jsonb NOT NULL,
    attempts integer NOT NULL,
    created_at timestamp with time zone NOT NULL,
    final_failure_code character varying(100) NOT NULL,
    dead_lettered_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_dead_letter_outbox_entries PRIMARY KEY (organization_id, id),
    CONSTRAINT ck_dead_letter_outbox_entries_attempts CHECK (attempts >= 1),
    CONSTRAINT fk_dead_letter_outbox_entries_organizations FOREIGN KEY (organization_id)
        REFERENCES organizations (id) ON DELETE CASCADE
);

CREATE INDEX ix_dead_letter_outbox_entries_dead_lettered_at
    ON dead_letter_outbox_entries (dead_lettered_at);

CREATE TABLE health_candidates (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    monitor_id uuid NOT NULL,
    source_revision_number integer NOT NULL,
    direction character varying(16) NOT NULL,
    expected_evidence character varying(16) NOT NULL,
    state character varying(16) NOT NULL,
    triggering_run_id uuid NOT NULL,
    triggering_scheduled_for timestamp with time zone NOT NULL,
    triggering_event_id uuid NOT NULL,
    requested_at timestamp with time zone NOT NULL,
    confirmation_run_id uuid,
    confirmation_scheduled_for timestamp with time zone,
    completed_at timestamp with time zone,
    CONSTRAINT pk_health_candidates PRIMARY KEY (id),
    CONSTRAINT ak_health_candidates_id_organization UNIQUE (id, organization_id),
    CONSTRAINT fk_health_candidates_monitors FOREIGN KEY (monitor_id, organization_id)
        REFERENCES monitors (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_health_candidates_projects FOREIGN KEY (project_id, organization_id)
        REFERENCES projects (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT ck_health_candidates_revision CHECK (source_revision_number >= 1),
    CONSTRAINT ck_health_candidates_direction CHECK (direction IN ('failure', 'recovery')),
    CONSTRAINT ck_health_candidates_expected CHECK (expected_evidence IN ('passing', 'failing')),
    CONSTRAINT ck_health_candidates_state CHECK (
        state IN ('pending', 'confirmed', 'contradicted', 'superseded', 'stale')
    ),
    CONSTRAINT ck_health_candidates_confirmation CHECK (
        (confirmation_run_id IS NULL) = (confirmation_scheduled_for IS NULL)
    )
);

CREATE UNIQUE INDEX ux_health_candidates_trigger
    ON health_candidates (
        organization_id, monitor_id, source_revision_number, direction,
        triggering_run_id, triggering_scheduled_for
    );

CREATE TABLE monitor_health (
    monitor_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    state character varying(16) NOT NULL,
    stable_state character varying(16) NOT NULL,
    policy_version character varying(32) NOT NULL,
    version bigint NOT NULL,
    source_revision_number integer,
    last_scheduled_for timestamp with time zone,
    last_determinate_finished_at timestamp with time zone,
    last_run_id uuid,
    last_run_scheduled_for timestamp with time zone,
    candidate_id uuid,
    configured_count integer NOT NULL,
    eligible_count integer NOT NULL,
    responding_count integer NOT NULL,
    passing_count integer NOT NULL,
    failing_count integer NOT NULL,
    location_fault_count integer NOT NULL,
    indeterminate_count integer NOT NULL,
    missing_count integer NOT NULL,
    transitioned_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_monitor_health PRIMARY KEY (monitor_id),
    CONSTRAINT ak_monitor_health_monitor_organization UNIQUE (monitor_id, organization_id),
    CONSTRAINT fk_monitor_health_monitors FOREIGN KEY (monitor_id, organization_id)
        REFERENCES monitors (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_monitor_health_projects FOREIGN KEY (project_id, organization_id)
        REFERENCES projects (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_monitor_health_candidates FOREIGN KEY (candidate_id, organization_id)
        REFERENCES health_candidates (id, organization_id),
    CONSTRAINT ck_monitor_health_state CHECK (state IN ('unknown', 'healthy', 'degraded', 'down')),
    CONSTRAINT ck_monitor_health_stable_state CHECK (
        stable_state IN ('unknown', 'healthy', 'down')
    ),
    CONSTRAINT ck_monitor_health_version CHECK (version >= 0),
    CONSTRAINT ck_monitor_health_revision CHECK (
        source_revision_number IS NULL OR source_revision_number >= 1
    ),
    CONSTRAINT ck_monitor_health_run_pointer CHECK (
        (last_run_id IS NULL) = (last_run_scheduled_for IS NULL)
    ),
    CONSTRAINT ck_monitor_health_counts CHECK (
        configured_count >= 0
        AND eligible_count >= 0
        AND responding_count >= 0
        AND passing_count >= 0
        AND failing_count >= 0
        AND location_fault_count >= 0
        AND indeterminate_count >= 0
        AND missing_count >= 0
    )
);

CREATE INDEX ix_monitor_health_organization_project
    ON monitor_health (organization_id, project_id, monitor_id);
CREATE INDEX ix_monitor_health_staleness
    ON monitor_health (last_determinate_finished_at)
    WHERE state <> 'unknown';

CREATE TABLE health_transitions (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    monitor_id uuid NOT NULL,
    version bigint NOT NULL,
    old_state character varying(16) NOT NULL,
    new_state character varying(16) NOT NULL,
    policy_version character varying(32) NOT NULL,
    source_revision_number integer,
    causal_run_id uuid,
    causal_run_scheduled_for timestamp with time zone,
    configured_count integer NOT NULL,
    eligible_count integer NOT NULL,
    responding_count integer NOT NULL,
    passing_count integer NOT NULL,
    failing_count integer NOT NULL,
    location_fault_count integer NOT NULL,
    indeterminate_count integer NOT NULL,
    missing_count integer NOT NULL,
    occurred_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_health_transitions PRIMARY KEY (id),
    CONSTRAINT ak_health_transitions_id_organization UNIQUE (id, organization_id),
    CONSTRAINT fk_health_transitions_monitors FOREIGN KEY (monitor_id, organization_id)
        REFERENCES monitors (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_health_transitions_projects FOREIGN KEY (project_id, organization_id)
        REFERENCES projects (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT ux_health_transitions_monitor_version UNIQUE (monitor_id, version),
    CONSTRAINT ck_health_transitions_version CHECK (version >= 1),
    CONSTRAINT ck_health_transitions_old_state CHECK (
        old_state IN ('unknown', 'healthy', 'degraded', 'down')
    ),
    CONSTRAINT ck_health_transitions_new_state CHECK (
        new_state IN ('unknown', 'healthy', 'degraded', 'down')
    ),
    CONSTRAINT ck_health_transitions_changed CHECK (old_state <> new_state),
    CONSTRAINT ck_health_transitions_run_pointer CHECK (
        (causal_run_id IS NULL) = (causal_run_scheduled_for IS NULL)
    )
);

CREATE INDEX ix_health_transitions_monitor_occurred
    ON health_transitions (organization_id, monitor_id, occurred_at DESC, id DESC);

CREATE TABLE incident_projection_cursors (
    organization_id uuid NOT NULL,
    monitor_id uuid NOT NULL,
    last_transition_version bigint NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_incident_projection_cursors PRIMARY KEY (organization_id, monitor_id),
    CONSTRAINT fk_incident_projection_cursors_monitors FOREIGN KEY (
        monitor_id, organization_id
    ) REFERENCES monitors (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT ck_incident_projection_cursors_version CHECK (
        last_transition_version >= 1
    )
);

CREATE TABLE incidents (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    monitor_id uuid NOT NULL,
    state character varying(20) NOT NULL,
    version bigint NOT NULL,
    opened_transition_id uuid NOT NULL,
    acknowledged_by uuid,
    acknowledged_at timestamp with time zone,
    resolved_transition_id uuid,
    resolved_at timestamp with time zone,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_incidents PRIMARY KEY (id),
    CONSTRAINT ak_incidents_id_organization UNIQUE (id, organization_id),
    CONSTRAINT fk_incidents_monitors FOREIGN KEY (monitor_id, organization_id)
        REFERENCES monitors (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_incidents_projects FOREIGN KEY (project_id, organization_id)
        REFERENCES projects (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_incidents_opened_transition FOREIGN KEY (opened_transition_id, organization_id)
        REFERENCES health_transitions (id, organization_id),
    CONSTRAINT fk_incidents_resolved_transition FOREIGN KEY (resolved_transition_id, organization_id)
        REFERENCES health_transitions (id, organization_id),
    CONSTRAINT fk_incidents_acknowledged_by FOREIGN KEY (acknowledged_by)
        REFERENCES users (id),
    CONSTRAINT ck_incidents_state CHECK (state IN ('open', 'acknowledged', 'resolved')),
    CONSTRAINT ck_incidents_version CHECK (version >= 1),
    CONSTRAINT ck_incidents_acknowledgement CHECK (
        (acknowledged_by IS NULL) = (acknowledged_at IS NULL)
    ),
    CONSTRAINT ck_incidents_resolution CHECK (
        (state = 'resolved') = (resolved_transition_id IS NOT NULL)
        AND (resolved_transition_id IS NULL) = (resolved_at IS NULL)
    )
);

CREATE UNIQUE INDEX ux_incidents_unresolved_monitor
    ON incidents (monitor_id)
    WHERE state <> 'resolved';
CREATE INDEX ix_incidents_monitor_created
    ON incidents (organization_id, project_id, monitor_id, created_at DESC, id DESC);

CREATE TABLE incident_timeline_entries (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    incident_id uuid NOT NULL,
    incident_version bigint NOT NULL,
    kind character varying(20) NOT NULL,
    health_transition_id uuid,
    actor_user_id uuid,
    old_health_state character varying(16),
    new_health_state character varying(16),
    policy_version character varying(32),
    causal_run_id uuid,
    causal_run_scheduled_for timestamp with time zone,
    configured_count integer,
    eligible_count integer,
    responding_count integer,
    passing_count integer,
    failing_count integer,
    location_fault_count integer,
    indeterminate_count integer,
    missing_count integer,
    occurred_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_incident_timeline_entries PRIMARY KEY (id),
    CONSTRAINT fk_incident_timeline_incidents FOREIGN KEY (incident_id, organization_id)
        REFERENCES incidents (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_incident_timeline_health_transition FOREIGN KEY (
        health_transition_id, organization_id
    ) REFERENCES health_transitions (id, organization_id),
    CONSTRAINT fk_incident_timeline_actor FOREIGN KEY (actor_user_id)
        REFERENCES users (id),
    CONSTRAINT ux_incident_timeline_version UNIQUE (incident_id, incident_version),
    CONSTRAINT ck_incident_timeline_version CHECK (incident_version >= 1),
    CONSTRAINT ck_incident_timeline_kind CHECK (
        kind IN ('opened', 'acknowledged', 'resolved')
    ),
    CONSTRAINT ck_incident_timeline_run_pointer CHECK (
        (causal_run_id IS NULL) = (causal_run_scheduled_for IS NULL)
    ),
    CONSTRAINT ck_incident_timeline_counts CHECK (
        (configured_count IS NULL
         AND eligible_count IS NULL
         AND responding_count IS NULL
         AND passing_count IS NULL
         AND failing_count IS NULL
         AND location_fault_count IS NULL
         AND indeterminate_count IS NULL
         AND missing_count IS NULL)
        OR
        (configured_count IS NOT NULL
         AND eligible_count IS NOT NULL
         AND responding_count IS NOT NULL
         AND passing_count IS NOT NULL
         AND failing_count IS NOT NULL
         AND location_fault_count IS NOT NULL
         AND indeterminate_count IS NOT NULL
         AND missing_count IS NOT NULL
         AND configured_count >= 0 AND eligible_count >= 0 AND responding_count >= 0
         AND passing_count >= 0 AND failing_count >= 0 AND location_fault_count >= 0
         AND indeterminate_count >= 0 AND missing_count >= 0)
    )
);

CREATE INDEX ix_incident_timeline_incident_occurred
    ON incident_timeline_entries (organization_id, incident_id, occurred_at, id);
