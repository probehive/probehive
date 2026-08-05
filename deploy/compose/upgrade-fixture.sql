CREATE TABLE runs_2026_08 PARTITION OF runs
    FOR VALUES FROM ('2026-08-01T00:00:00Z') TO ('2026-09-01T00:00:00Z');
CREATE TABLE observations_2026_08 PARTITION OF observations
    FOR VALUES FROM ('2026-08-01T00:00:00Z') TO ('2026-09-01T00:00:00Z');

INSERT INTO organizations (id, slug, display_name, created_at)
VALUES (
    '00000000-0000-7000-8000-000000000001',
    'upgrade-fixture',
    'Upgrade Fixture',
    '2026-08-05T00:00:00Z'
);

INSERT INTO projects (id, organization_id, name, is_default, created_at)
VALUES (
    '00000000-0000-7000-8000-000000000002',
    '00000000-0000-7000-8000-000000000001',
    'Default',
    true,
    '2026-08-05T00:00:00Z'
);

INSERT INTO users (id, email, display_name, role, password_hash, created_at)
VALUES (
    '00000000-0000-7000-8000-000000000003',
    'upgrade-admin@example.test',
    'Upgrade Administrator',
    'Administrator',
    'fixture:not-a-login-hash',
    '2026-08-05T00:00:00Z'
);

INSERT INTO organization_members (organization_id, user_id, role, created_at)
VALUES (
    '00000000-0000-7000-8000-000000000001',
    '00000000-0000-7000-8000-000000000003',
    'Administrator',
    '2026-08-05T00:00:00Z'
);

INSERT INTO monitors (
    id, organization_id, project_id, name, check_type, state,
    latest_revision_number, created_at, updated_at, interval_seconds
) VALUES (
    '00000000-0000-7000-8000-000000000004',
    '00000000-0000-7000-8000-000000000001',
    '00000000-0000-7000-8000-000000000002',
    'Upgrade evidence',
    'http',
    'draft',
    1,
    '2026-08-05T00:00:00Z',
    '2026-08-05T00:00:00Z',
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
    '2026-08-05T00:00:00Z'
);

INSERT INTO runs (
    id, organization_id, monitor_id, revision_number, location, scheduled_for,
    kind, outcome, started_at, finished_at, created_at
) VALUES (
    '00000000-0000-7000-8000-000000000006',
    '00000000-0000-7000-8000-000000000001',
    '00000000-0000-7000-8000-000000000004',
    1,
    'upgrade-fixture',
    '2026-08-05T00:00:00Z',
    'manual',
    'passed',
    '2026-08-05T00:00:01Z',
    '2026-08-05T00:00:02Z',
    '2026-08-05T00:00:00Z'
);

INSERT INTO observations (
    run_id, scheduled_for, organization_id, duration_microseconds,
    connect_microseconds, tls_microseconds, first_byte_microseconds,
    http_status_code, http_protocol, http_redirect_count, http_body_bytes,
    http_body_truncated, created_at
) VALUES (
    '00000000-0000-7000-8000-000000000006',
    '2026-08-05T00:00:00Z',
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
    '2026-08-05T00:00:02Z'
);
