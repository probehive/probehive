-- Execution interval on the Monitor (ADR 0026).
--
-- The interval is a Monitor column rather than a field inside check_configuration so the
-- scheduler can select due work without decoding a check-type-specific JSON document, and so
-- every future check type does not have to redefine the same field in its own schema.
--
-- The default exists only to backfill Monitors created before this column. Application
-- inserts continue to supply the value explicitly, so it is dropped immediately afterwards:
-- a column default that outlives its backfill is a second place where the product's default
-- lives, and the two drift.
ALTER TABLE monitors
    ADD COLUMN interval_seconds integer NOT NULL DEFAULT 60;

ALTER TABLE monitors
    ALTER COLUMN interval_seconds DROP DEFAULT;

ALTER TABLE monitors
    ADD CONSTRAINT ck_monitors_interval_seconds
        CHECK (interval_seconds BETWEEN 30 AND 86400);

-- The scheduler's read is "every active Monitor", so the index carries the columns that
-- answer it. Organization identity leads because every tenant-scoped query carries it
-- explicitly (ADR 0009).
CREATE INDEX ix_monitors_active ON monitors (organization_id, id)
    WHERE state = 'active';
