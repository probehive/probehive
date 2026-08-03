-- Organization-scoped signed Webhook Integration configuration and encrypted secrets.
-- Integrations are created disabled; delivery is a later slice.

CREATE TABLE webhook_integrations (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    name character varying(100) NOT NULL,
    destination_url character varying(2048) NOT NULL,
    enabled boolean NOT NULL DEFAULT false,
    version bigint NOT NULL,
    active_secret_version bigint NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_webhook_integrations PRIMARY KEY (id),
    CONSTRAINT ak_webhook_integrations_id_organization UNIQUE (id, organization_id),
    CONSTRAINT fk_webhook_integrations_organizations FOREIGN KEY (organization_id)
        REFERENCES organizations (id) ON DELETE CASCADE,
    CONSTRAINT ck_webhook_integrations_version CHECK (version >= 1),
    CONSTRAINT ck_webhook_integrations_active_secret_version CHECK (active_secret_version >= 1),
    CONSTRAINT ck_webhook_integrations_timestamps CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX ux_webhook_integrations_organization_name
    ON webhook_integrations (organization_id, name);

CREATE INDEX ix_webhook_integrations_organization_created
    ON webhook_integrations (organization_id, created_at, id);

CREATE TABLE webhook_signing_secrets (
    organization_id uuid NOT NULL,
    integration_id uuid NOT NULL,
    secret_version bigint NOT NULL,
    state character varying(16) NOT NULL,
    wrapping_key_id character varying(32),
    nonce bytea,
    ciphertext bytea,
    created_at timestamp with time zone NOT NULL,
    activated_at timestamp with time zone,
    retired_at timestamp with time zone,
    CONSTRAINT pk_webhook_signing_secrets PRIMARY KEY (
        organization_id, integration_id, secret_version
    ),
    CONSTRAINT fk_webhook_signing_secrets_integration FOREIGN KEY (
        integration_id, organization_id
    ) REFERENCES webhook_integrations (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT ck_webhook_signing_secrets_version CHECK (secret_version >= 1),
    CONSTRAINT ck_webhook_signing_secrets_state CHECK (
        state IN ('pending', 'active', 'retiring', 'retired')
    ),
    CONSTRAINT ck_webhook_signing_secrets_material CHECK (
        (state = 'retired' AND wrapping_key_id IS NULL AND nonce IS NULL AND ciphertext IS NULL)
        OR
        (state <> 'retired' AND wrapping_key_id IS NOT NULL
            AND octet_length(nonce) = 12 AND octet_length(ciphertext) >= 16)
    )
);

CREATE UNIQUE INDEX ux_webhook_signing_secrets_active
    ON webhook_signing_secrets (organization_id, integration_id)
    WHERE state = 'active';

CREATE UNIQUE INDEX ux_webhook_signing_secrets_pending
    ON webhook_signing_secrets (organization_id, integration_id)
    WHERE state = 'pending';

CREATE UNIQUE INDEX ux_webhook_signing_secrets_retiring
    ON webhook_signing_secrets (organization_id, integration_id)
    WHERE state = 'retiring';

ALTER TABLE webhook_integrations
    ADD CONSTRAINT fk_webhook_integrations_active_secret
    FOREIGN KEY (organization_id, id, active_secret_version)
    REFERENCES webhook_signing_secrets (
        organization_id, integration_id, secret_version
    )
    DEFERRABLE INITIALLY DEFERRED;
