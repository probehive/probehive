-- Immutable Alert intents derived from Incident lifecycle facts.

CREATE TABLE alerts (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    monitor_id uuid NOT NULL,
    incident_id uuid NOT NULL,
    incident_version bigint NOT NULL,
    kind character varying(32) NOT NULL,
    occurred_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_alerts PRIMARY KEY (id),
    CONSTRAINT ak_alerts_id_organization UNIQUE (id, organization_id),
    CONSTRAINT fk_alerts_incidents FOREIGN KEY (incident_id, organization_id)
        REFERENCES incidents (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_alerts_monitors FOREIGN KEY (monitor_id, organization_id)
        REFERENCES monitors (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_alerts_projects FOREIGN KEY (project_id, organization_id)
        REFERENCES projects (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT ux_alerts_incident_version UNIQUE (
        organization_id, incident_id, incident_version
    ),
    CONSTRAINT ck_alerts_incident_version CHECK (incident_version >= 1),
    CONSTRAINT ck_alerts_kind CHECK (
        kind IN ('incident.opened', 'incident.resolved')
    )
);

CREATE INDEX ix_alerts_monitor_occurred
    ON alerts (
        organization_id, project_id, monitor_id, occurred_at DESC, id DESC
    );
