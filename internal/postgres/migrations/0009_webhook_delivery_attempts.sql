-- Durable signed Webhook calls, leases, retry scheduling, and append-only audit evidence.
ALTER TABLE webhook_deliveries
    ADD COLUMN available_at timestamp with time zone,
    ADD COLUMN attempt_count bigint NOT NULL DEFAULT 0,
    ADD COLUMN lease_holder uuid,
    ADD COLUMN lease_expires_at timestamp with time zone,
    ADD COLUMN completed_at timestamp with time zone;

UPDATE webhook_deliveries
SET available_at = routed_at;

ALTER TABLE webhook_deliveries
    ALTER COLUMN available_at SET NOT NULL,
    ADD CONSTRAINT ck_webhook_deliveries_attempt_count CHECK (
        attempt_count BETWEEN 0 AND 5
    ),
    ADD CONSTRAINT ck_webhook_deliveries_lease CHECK (
        (lease_holder IS NULL AND lease_expires_at IS NULL)
        OR
        (lease_holder IS NOT NULL AND lease_expires_at IS NOT NULL)
    ),
    ADD CONSTRAINT ck_webhook_deliveries_completion CHECK (
        completed_at IS NULL
        OR
        (lease_holder IS NULL AND lease_expires_at IS NULL)
    );

CREATE INDEX ix_webhook_deliveries_available
    ON webhook_deliveries (available_at, id)
    WHERE completed_at IS NULL;

CREATE TABLE webhook_delivery_attempts (
    organization_id uuid NOT NULL,
    delivery_id uuid NOT NULL,
    alert_id uuid NOT NULL,
    integration_id uuid NOT NULL,
    integration_version bigint NOT NULL,
    secret_version bigint NOT NULL,
    sequence bigint NOT NULL,
    started_at timestamp with time zone NOT NULL,
    finished_at timestamp with time zone,
    outcome character varying(16) NOT NULL,
    http_status integer,
    failure_code character varying(80),
    CONSTRAINT pk_webhook_delivery_attempts PRIMARY KEY (
        delivery_id, sequence
    ),
    CONSTRAINT fk_webhook_delivery_attempts_delivery FOREIGN KEY (
        delivery_id, organization_id
    ) REFERENCES webhook_deliveries (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_webhook_delivery_attempts_alert FOREIGN KEY (
        alert_id, organization_id
    ) REFERENCES alerts (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_webhook_delivery_attempts_integration FOREIGN KEY (
        integration_id, organization_id
    ) REFERENCES webhook_integrations (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_webhook_delivery_attempts_secret FOREIGN KEY (
        organization_id, integration_id, secret_version
    ) REFERENCES webhook_signing_secrets (
        organization_id, integration_id, secret_version
    ),
    CONSTRAINT ck_webhook_delivery_attempts_versions CHECK (
        integration_version >= 1 AND secret_version >= 1
    ),
    CONSTRAINT ck_webhook_delivery_attempts_sequence CHECK (
        sequence BETWEEN 1 AND 5
    ),
    CONSTRAINT ck_webhook_delivery_attempts_http_status CHECK (
        http_status IS NULL OR http_status BETWEEN 100 AND 599
    ),
    CONSTRAINT ck_webhook_delivery_attempts_timestamps CHECK (
        finished_at IS NULL OR finished_at >= started_at
    ),
    CONSTRAINT ck_webhook_delivery_attempts_result CHECK (
        (
            outcome = 'inProgress'
            AND finished_at IS NULL
            AND http_status IS NULL
            AND failure_code IS NULL
        )
        OR
        (
            outcome = 'succeeded'
            AND finished_at IS NOT NULL
            AND http_status BETWEEN 200 AND 299
            AND failure_code IS NULL
        )
        OR
        (
            outcome = 'failed'
            AND finished_at IS NOT NULL
            AND failure_code IS NOT NULL
        )
        OR
        (
            outcome = 'cancelled'
            AND finished_at IS NOT NULL
            AND http_status IS NULL
            AND failure_code = 'webhook.delivery.cancelled'
        )
    )
);

CREATE INDEX ix_webhook_delivery_attempts_alert
    ON webhook_delivery_attempts (
        organization_id, alert_id, delivery_id, sequence
    );
