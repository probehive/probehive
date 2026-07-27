CREATE TABLE organization_members (
    organization_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role character varying(50) NOT NULL,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_organization_members PRIMARY KEY (organization_id, user_id),
    CONSTRAINT fk_organization_members_organizations FOREIGN KEY (organization_id)
        REFERENCES organizations (id) ON DELETE CASCADE,
    CONSTRAINT fk_organization_members_users FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX ix_organization_members_user_id ON organization_members (user_id);

-- Backfill so an installation that predates membership does not lose access to its own
-- data the moment enforcement lands (ADR 0017). The original creator was never recorded,
-- so every instance Administrator becomes an Organization Administrator of every existing
-- Organization. This is a one-time upgrade path, not ongoing behavior: after this
-- migration the only writer of memberships is the provisioning use case.
--
-- now() appears here because a migration has no application clock. It is not a column
-- default; application inserts continue to supply their own UTC timestamps.
INSERT INTO organization_members (organization_id, user_id, role, created_at)
SELECT organizations.id, users.id, 'Administrator', now()
FROM organizations
CROSS JOIN users
WHERE users.role = 'Administrator'
ON CONFLICT ON CONSTRAINT pk_organization_members DO NOTHING;
