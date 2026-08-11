-- Event-time maintenance attribution for routed Webhook deliveries.
-- A suppressed route is durable completion evidence, not a network attempt.

ALTER TABLE webhook_deliveries
    ADD COLUMN suppression_reason character varying(32),
    ADD COLUMN maintenance_window_id uuid,
    ADD CONSTRAINT fk_webhook_deliveries_maintenance_window FOREIGN KEY (
        maintenance_window_id, organization_id
    ) REFERENCES maintenance_windows (id, organization_id),
    ADD CONSTRAINT ck_webhook_deliveries_suppression CHECK (
        (
            suppression_reason IS NULL
            AND maintenance_window_id IS NULL
        )
        OR
        (
            suppression_reason = 'maintenance'
            AND maintenance_window_id IS NOT NULL
            AND completed_at IS NOT NULL
            AND attempt_count = 0
            AND lease_holder IS NULL
            AND lease_expires_at IS NULL
        )
    );

CREATE INDEX ix_webhook_deliveries_maintenance_window
    ON webhook_deliveries (
        organization_id, maintenance_window_id, alert_id
    )
    WHERE maintenance_window_id IS NOT NULL;
