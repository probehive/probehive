-- Run, Observation, and outbox storage (ADR 0021, amended by ADR 0025).
--
-- Both high-volume tables are range-partitioned by month on scheduled_for. ADR 0021 named
-- started_at; ADR 0025 amended it because PostgreSQL requires a unique constraint on a
-- partitioned table to include every partition-key column, which made the idempotent run
-- identity below impossible, and because a skipped Run never started and so has no
-- started_at to partition on.
--
-- This migration creates the partitioned parents only. Individual monthly partitions are
-- created by the maintenance job, not here, so an installation's partition set follows its
-- clock rather than the instant it happened to migrate.

CREATE TABLE runs (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    monitor_id uuid NOT NULL,
    revision_number integer NOT NULL,
    location character varying(63) NOT NULL,
    scheduled_for timestamp with time zone NOT NULL,
    kind character varying(20) NOT NULL,
    outcome character varying(20),
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    lease_holder character varying(128),
    lease_expires_at timestamp with time zone,
    created_at timestamp with time zone NOT NULL,
    -- The primary key includes the partition key because PostgreSQL requires it. Identity
    -- for scheduling purposes is the slot index below, not this key (ADR 0021).
    CONSTRAINT pk_runs PRIMARY KEY (id, scheduled_for),
    -- The composite reference is how tenant identity is enforced rather than trusted: a Run
    -- cannot name a Monitor belonging to another Organization (ADR 0009).
    CONSTRAINT fk_runs_monitors FOREIGN KEY (monitor_id, organization_id)
        REFERENCES monitors (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT ck_runs_revision_number CHECK (revision_number >= 1),
    CONSTRAINT ck_runs_kind CHECK (kind IN ('scheduled', 'confirmation', 'manual')),
    CONSTRAINT ck_runs_outcome CHECK (
        outcome IS NULL
        OR outcome IN ('passed', 'failed', 'errored', 'timedout', 'cancelled', 'skipped')
    ),
    -- A Run is in flight if and only if it holds a lease, so "claimed" has exactly one
    -- spelling and a finished Run cannot keep a claim alive (ADR 0025).
    CONSTRAINT ck_runs_lease_matches_outcome CHECK (
        (outcome IS NULL) = (lease_expires_at IS NOT NULL)
        AND (lease_holder IS NULL) = (lease_expires_at IS NULL)
    ),
    -- A skipped Run never executed; every other recorded outcome did.
    CONSTRAINT ck_runs_execution_instants CHECK (
        CASE
            WHEN outcome IS NULL OR outcome = 'skipped'
                THEN started_at IS NULL AND finished_at IS NULL
            ELSE started_at IS NOT NULL AND finished_at IS NOT NULL AND finished_at >= started_at
        END
    )
) PARTITION BY RANGE (scheduled_for);

-- The idempotent run identity of ADR 0021: one Monitor Revision, one Probe Location, one due
-- instant is one execution. Duplicate lease delivery, a retry, or a restarted worker cannot
-- produce a second row. Manual Runs are exempt, as ADR 0021 requires: asking twice is a
-- request rather than a duplicate.
CREATE UNIQUE INDEX ux_runs_slot ON runs (monitor_id, revision_number, location, scheduled_for)
    WHERE kind <> 'manual';

CREATE INDEX ix_runs_organization_scheduled_for ON runs (organization_id, scheduled_for DESC);
CREATE INDEX ix_runs_monitor_scheduled_for ON runs (monitor_id, organization_id, scheduled_for DESC);
-- Partial, because reclaiming an expired lease only ever scans Runs that hold one, and that
-- set is bounded by concurrency rather than by retention.
CREATE INDEX ix_runs_lease_expires_at ON runs (lease_expires_at) WHERE lease_expires_at IS NOT NULL;

CREATE TABLE observations (
    run_id uuid NOT NULL,
    scheduled_for timestamp with time zone NOT NULL,
    organization_id uuid NOT NULL,
    -- A stable probe.* code or an outbound.* denial reason (ADR 0019, ADR 0023, ADR 0024).
    -- Null for a passed Run.
    failure_code character varying(100),
    failure_class character varying(100),
    -- Durations are microsecond integers because they are measured monotonically as integers
    -- and comparing them is arithmetic, not calendar arithmetic (ADR 0025).
    duration_microseconds bigint NOT NULL,
    connect_microseconds bigint NOT NULL,
    tls_microseconds bigint NOT NULL,
    first_byte_microseconds bigint NOT NULL,
    -- The HTTP detail group is present when a response arrived, whatever the outcome. It
    -- belongs to the first check type; a second check type brings its own (ADR 0025).
    http_status_code integer,
    http_protocol character varying(20),
    http_redirect_count integer,
    http_body_bytes bigint,
    http_body_truncated boolean,
    -- The TLS detail group is present when the first hop completed a handshake.
    tls_version character varying(64),
    tls_cipher_suite character varying(64),
    tls_certificate_expires_at timestamp with time zone,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_observations PRIMARY KEY (run_id, scheduled_for),
    CONSTRAINT fk_observations_runs FOREIGN KEY (run_id, scheduled_for)
        REFERENCES runs (id, scheduled_for) ON DELETE CASCADE,
    CONSTRAINT ck_observations_durations CHECK (
        duration_microseconds >= 0
        AND connect_microseconds >= 0
        AND tls_microseconds >= 0
        AND first_byte_microseconds >= 0
    ),
    CONSTRAINT ck_observations_denial_class CHECK (failure_class IS NULL OR failure_code IS NOT NULL),
    -- The HTTP group is present or absent as a whole, so a half-written row is not a state
    -- a reader has to interpret.
    CONSTRAINT ck_observations_http_group CHECK (
        num_nonnulls(http_status_code, http_redirect_count, http_body_bytes, http_body_truncated)
            IN (0, 4)
    ),
    CONSTRAINT ck_observations_http_status CHECK (
        http_status_code IS NULL OR (http_status_code BETWEEN 100 AND 599)
    ),
    CONSTRAINT ck_observations_http_counts CHECK (
        (http_redirect_count IS NULL OR http_redirect_count >= 0)
        AND (http_body_bytes IS NULL OR http_body_bytes >= 0)
    ),
    -- TLS detail cannot exist without the protocol exchange it was negotiated for.
    CONSTRAINT ck_observations_tls_requires_http CHECK (
        (tls_version IS NULL AND tls_cipher_suite IS NULL AND tls_certificate_expires_at IS NULL)
        OR http_status_code IS NOT NULL
    )
) PARTITION BY RANGE (scheduled_for);

CREATE INDEX ix_observations_organization_scheduled_for
    ON observations (organization_id, scheduled_for DESC);

-- The outbox is a queue, not a record: entries are drained and deleted, so it is neither
-- partitioned nor subject to a retention window (ADR 0025). Consumers are at-least-once and
-- idempotent on id.
CREATE TABLE outbox_entries (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    topic character varying(100) NOT NULL,
    payload jsonb NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    created_at timestamp with time zone NOT NULL,
    available_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_outbox_entries PRIMARY KEY (id),
    CONSTRAINT ck_outbox_entries_attempts CHECK (attempts >= 0),
    CONSTRAINT fk_outbox_entries_organizations FOREIGN KEY (organization_id)
        REFERENCES organizations (id) ON DELETE CASCADE
);

CREATE INDEX ix_outbox_entries_available_at ON outbox_entries (available_at, id);
