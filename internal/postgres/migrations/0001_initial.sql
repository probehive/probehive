CREATE TABLE organizations (
    id uuid NOT NULL,
    slug character varying(63) NOT NULL,
    display_name character varying(100) NOT NULL,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_organizations PRIMARY KEY (id)
);

CREATE UNIQUE INDEX ux_organizations_slug ON organizations (slug);

CREATE TABLE projects (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    name character varying(100) NOT NULL,
    is_default boolean NOT NULL,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_projects PRIMARY KEY (id),
    CONSTRAINT ak_projects_id_organization_id UNIQUE (id, organization_id),
    CONSTRAINT fk_projects_organizations FOREIGN KEY (organization_id)
        REFERENCES organizations (id) ON DELETE CASCADE
);

CREATE INDEX ix_projects_organization_id ON projects (organization_id);
CREATE UNIQUE INDEX ux_projects_organization_default ON projects (organization_id) WHERE is_default;

CREATE TABLE users (
    id uuid NOT NULL,
    email character varying(254) NOT NULL,
    display_name character varying(100) NOT NULL,
    role character varying(50) NOT NULL,
    password_hash text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_users PRIMARY KEY (id)
);

CREATE UNIQUE INDEX ux_users_email ON users (email);

CREATE TABLE monitors (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    name character varying(100) NOT NULL,
    check_type character varying(50) NOT NULL,
    state character varying(20) NOT NULL,
    latest_revision_number integer NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_monitors PRIMARY KEY (id),
    CONSTRAINT ak_monitors_id_organization_id UNIQUE (id, organization_id),
    CONSTRAINT fk_monitors_organizations FOREIGN KEY (organization_id)
        REFERENCES organizations (id) ON DELETE CASCADE,
    CONSTRAINT fk_monitors_projects FOREIGN KEY (project_id, organization_id)
        REFERENCES projects (id, organization_id) ON DELETE CASCADE
);

CREATE INDEX ix_monitors_organization_project ON monitors (organization_id, project_id);
CREATE INDEX "IX_monitors_project_id_organization_id" ON monitors (project_id, organization_id);

CREATE TABLE monitor_revisions (
    id uuid NOT NULL,
    monitor_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    revision_number integer NOT NULL,
    check_type character varying(50) NOT NULL,
    check_schema_version integer NOT NULL,
    check_configuration jsonb NOT NULL,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_monitor_revisions PRIMARY KEY (id),
    CONSTRAINT fk_monitor_revisions_monitors FOREIGN KEY (monitor_id, organization_id)
        REFERENCES monitors (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_monitor_revisions_organizations FOREIGN KEY (organization_id)
        REFERENCES organizations (id) ON DELETE CASCADE
);

CREATE INDEX "IX_monitor_revisions_organization_id" ON monitor_revisions (organization_id);
CREATE UNIQUE INDEX ux_monitor_revisions_monitor_number
    ON monitor_revisions (monitor_id, revision_number);

CREATE TABLE sessions (
    token_hash bytea NOT NULL,
    user_id uuid NOT NULL,
    authenticated_at timestamp with time zone NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_sessions PRIMARY KEY (token_hash),
    CONSTRAINT ck_sessions_token_hash_length CHECK (octet_length(token_hash) = 32),
    CONSTRAINT ck_sessions_expiry CHECK (expires_at > authenticated_at),
    CONSTRAINT fk_sessions_users FOREIGN KEY (user_id)
        REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX ix_sessions_user_id ON sessions (user_id);
CREATE INDEX ix_sessions_expires_at ON sessions (expires_at);

CREATE TABLE antiforgery_tokens (
    selector_hash bytea NOT NULL,
    request_token_hash bytea NOT NULL,
    session_token_hash bytea NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_antiforgery_tokens PRIMARY KEY (selector_hash),
    CONSTRAINT ux_antiforgery_tokens_request_hash UNIQUE (request_token_hash),
    CONSTRAINT ck_antiforgery_selector_hash_length CHECK (octet_length(selector_hash) = 32),
    CONSTRAINT ck_antiforgery_request_hash_length CHECK (octet_length(request_token_hash) = 32),
    CONSTRAINT ck_antiforgery_session_hash_length CHECK (
        session_token_hash IS NULL OR octet_length(session_token_hash) = 32
    ),
    CONSTRAINT fk_antiforgery_tokens_sessions FOREIGN KEY (session_token_hash)
        REFERENCES sessions (token_hash) ON DELETE CASCADE
);

CREATE INDEX ix_antiforgery_tokens_session_hash ON antiforgery_tokens (session_token_hash);
CREATE INDEX ix_antiforgery_tokens_expires_at ON antiforgery_tokens (expires_at);
