-- Durable one-time Monitor maintenance windows. Cancellation preserves the interval as evidence.

CREATE TABLE maintenance_windows (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    monitor_id uuid NOT NULL,
    starts_at timestamp with time zone NOT NULL,
    ends_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone NOT NULL,
    cancelled_at timestamp with time zone,
    CONSTRAINT pk_maintenance_windows PRIMARY KEY (id),
    CONSTRAINT ak_maintenance_windows_id_organization UNIQUE (id, organization_id),
    CONSTRAINT fk_maintenance_windows_monitor FOREIGN KEY (monitor_id, organization_id)
        REFERENCES monitors (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT ck_maintenance_windows_bounds CHECK (ends_at > starts_at),
    CONSTRAINT ck_maintenance_windows_duration CHECK (
        ends_at - starts_at <= interval '30 days'
    ),
    CONSTRAINT ck_maintenance_windows_creation CHECK (created_at <= starts_at),
    CONSTRAINT ck_maintenance_windows_cancellation CHECK (
        cancelled_at IS NULL
        OR (cancelled_at >= created_at AND cancelled_at < ends_at)
    )
);

CREATE INDEX ix_maintenance_windows_monitor_start
    ON maintenance_windows (organization_id, monitor_id, starts_at, id);

CREATE INDEX ix_maintenance_windows_active_overlap
    ON maintenance_windows (organization_id, monitor_id, starts_at, ends_at)
    WHERE cancelled_at IS NULL;
