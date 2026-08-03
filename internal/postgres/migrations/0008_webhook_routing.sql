-- Stable Webhook routing snapshots created with Alert projection.
-- These rows are delivery identities, not evidence of an external call.

CREATE TABLE webhook_deliveries (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    alert_id uuid NOT NULL,
    integration_id uuid NOT NULL,
    integration_version bigint NOT NULL,
    secret_version bigint NOT NULL,
    routed_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_webhook_deliveries PRIMARY KEY (id),
    CONSTRAINT ak_webhook_deliveries_id_organization UNIQUE (id, organization_id),
    CONSTRAINT ux_webhook_deliveries_alert_integration UNIQUE (
        organization_id, alert_id, integration_id
    ),
    CONSTRAINT fk_webhook_deliveries_alerts FOREIGN KEY (alert_id, organization_id)
        REFERENCES alerts (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_webhook_deliveries_integrations FOREIGN KEY (
        integration_id, organization_id
    ) REFERENCES webhook_integrations (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_webhook_deliveries_secrets FOREIGN KEY (
        organization_id, integration_id, secret_version
    ) REFERENCES webhook_signing_secrets (
        organization_id, integration_id, secret_version
    ),
    CONSTRAINT ck_webhook_deliveries_integration_version CHECK (
        integration_version >= 1
    ),
    CONSTRAINT ck_webhook_deliveries_secret_version CHECK (secret_version >= 1)
);

CREATE INDEX ix_webhook_deliveries_integration_routed
    ON webhook_deliveries (
        organization_id, integration_id, routed_at, id
    );
