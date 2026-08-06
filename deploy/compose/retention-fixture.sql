DO $$
DECLARE
    current_month timestamp with time zone := date_trunc('month', now());
    expired_month timestamp with time zone := current_month - interval '3 months';
    table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY['runs', 'observations']
    LOOP
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
            table_name || '_' || to_char(expired_month, 'YYYY_MM'),
            table_name,
            expired_month,
            expired_month + interval '1 month'
        );
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
            table_name || '_' || to_char(current_month, 'YYYY_MM'),
            table_name,
            current_month,
            current_month + interval '1 month'
        );
    END LOOP;
END
$$;

INSERT INTO organizations (id, slug, display_name, created_at)
VALUES (
    '00000000-0000-7000-8000-000000000001',
    'retention-fixture',
    'Retention Fixture',
    now()
);

INSERT INTO projects (id, organization_id, name, is_default, created_at)
VALUES (
    '00000000-0000-7000-8000-000000000002',
    '00000000-0000-7000-8000-000000000001',
    'Default',
    true,
    now()
);

INSERT INTO users (id, email, display_name, role, password_hash, created_at)
VALUES (
    '00000000-0000-7000-8000-000000000003',
    'retention-admin@example.test',
    'Retention Administrator',
    'Administrator',
    'fixture:not-a-login-hash',
    now()
);

INSERT INTO organization_members (organization_id, user_id, role, created_at)
VALUES (
    '00000000-0000-7000-8000-000000000001',
    '00000000-0000-7000-8000-000000000003',
    'Administrator',
    now()
);

INSERT INTO monitors (
    id, organization_id, project_id, name, check_type, state,
    latest_revision_number, created_at, updated_at, interval_seconds
) VALUES (
    '00000000-0000-7000-8000-000000000004',
    '00000000-0000-7000-8000-000000000001',
    '00000000-0000-7000-8000-000000000002',
    'Retention evidence',
    'http',
    'draft',
    1,
    now(),
    now(),
    60
);

INSERT INTO monitor_revisions (
    id, monitor_id, organization_id, revision_number, check_type,
    check_schema_version, check_configuration, created_at
) VALUES (
    '00000000-0000-7000-8000-000000000005',
    '00000000-0000-7000-8000-000000000004',
    '00000000-0000-7000-8000-000000000001',
    1,
    'http',
    1,
    '{"url":"https://example.test"}',
    now()
);

WITH schedule AS (
    SELECT
        date_trunc('month', now()) - interval '3 months' + interval '1 day'
            AS expired_at,
        date_trunc('month', now()) + interval '1 day' AS retained_at
)
INSERT INTO runs (
    id, organization_id, monitor_id, revision_number, location, scheduled_for,
    kind, outcome, started_at, finished_at, created_at
)
SELECT
    fixture.id,
    '00000000-0000-7000-8000-000000000001',
    '00000000-0000-7000-8000-000000000004',
    1,
    'retention-fixture',
    fixture.scheduled_for,
    'manual',
    'passed',
    fixture.scheduled_for + interval '1 second',
    fixture.scheduled_for + interval '2 seconds',
    fixture.scheduled_for
FROM schedule
CROSS JOIN LATERAL (
    VALUES
        ('00000000-0000-7000-8000-000000000006'::uuid, schedule.expired_at),
        ('00000000-0000-7000-8000-000000000007'::uuid, schedule.retained_at)
) AS fixture(id, scheduled_for);

WITH schedule AS (
    SELECT
        date_trunc('month', now()) - interval '3 months' + interval '1 day'
            AS expired_at,
        date_trunc('month', now()) + interval '1 day' AS retained_at
)
INSERT INTO observations (
    run_id, scheduled_for, organization_id, duration_microseconds,
    connect_microseconds, tls_microseconds, first_byte_microseconds,
    http_status_code, http_protocol, http_redirect_count, http_body_bytes,
    http_body_truncated, created_at
)
SELECT
    fixture.id,
    fixture.scheduled_for,
    '00000000-0000-7000-8000-000000000001',
    2000,
    500,
    700,
    1200,
    200,
    'HTTP/1.1',
    0,
    2,
    false,
    fixture.scheduled_for + interval '2 seconds'
FROM schedule
CROSS JOIN LATERAL (
    VALUES
        ('00000000-0000-7000-8000-000000000006'::uuid, schedule.expired_at),
        ('00000000-0000-7000-8000-000000000007'::uuid, schedule.retained_at)
) AS fixture(id, scheduled_for);

WITH schedule AS (
    SELECT date_trunc('month', now()) - interval '3 months' + interval '1 day'
        AS expired_at
)
INSERT INTO health_transitions (
    id, organization_id, project_id, monitor_id, version, old_state, new_state,
    policy_version, source_revision_number, causal_run_id,
    causal_run_scheduled_for, configured_count, eligible_count, responding_count,
    passing_count, failing_count, location_fault_count, indeterminate_count,
    missing_count, occurred_at
)
SELECT
    '00000000-0000-7000-8000-000000000008',
    '00000000-0000-7000-8000-000000000001',
    '00000000-0000-7000-8000-000000000002',
    '00000000-0000-7000-8000-000000000004',
    1,
    'unknown',
    'down',
    'phase1.v1',
    1,
    '00000000-0000-7000-8000-000000000006',
    schedule.expired_at,
    1, 1, 1, 0, 1, 0, 0, 0,
    schedule.expired_at + interval '2 seconds'
FROM schedule;

INSERT INTO incidents (
    id, organization_id, project_id, monitor_id, state, version,
    opened_transition_id, created_at, updated_at
) VALUES (
    '00000000-0000-7000-8000-000000000009',
    '00000000-0000-7000-8000-000000000001',
    '00000000-0000-7000-8000-000000000002',
    '00000000-0000-7000-8000-000000000004',
    'open',
    1,
    '00000000-0000-7000-8000-000000000008',
    now(),
    now()
);

INSERT INTO incident_timeline_entries (
    id, organization_id, incident_id, incident_version, kind,
    health_transition_id, old_health_state, new_health_state, policy_version,
    causal_run_id, causal_run_scheduled_for, configured_count, eligible_count,
    responding_count, passing_count, failing_count, location_fault_count,
    indeterminate_count, missing_count, occurred_at
)
SELECT
    '00000000-0000-7000-8000-000000000010',
    '00000000-0000-7000-8000-000000000001',
    '00000000-0000-7000-8000-000000000009',
    1,
    'opened',
    '00000000-0000-7000-8000-000000000008',
    'unknown',
    'down',
    'phase1.v1',
    '00000000-0000-7000-8000-000000000006',
    date_trunc('month', now()) - interval '3 months' + interval '1 day',
    1, 1, 1, 0, 1, 0, 0, 0,
    now();

INSERT INTO alerts (
    id, organization_id, project_id, monitor_id, incident_id,
    incident_version, kind, occurred_at, created_at
) VALUES (
    '00000000-0000-7000-8000-000000000011',
    '00000000-0000-7000-8000-000000000001',
    '00000000-0000-7000-8000-000000000002',
    '00000000-0000-7000-8000-000000000004',
    '00000000-0000-7000-8000-000000000009',
    1,
    'incident.opened',
    now(),
    now()
);
