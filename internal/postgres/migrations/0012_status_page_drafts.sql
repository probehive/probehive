-- Private Organization-owned status configuration. Publication is intentionally absent.

CREATE TABLE status_pages (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    title character varying(100) NOT NULL,
    version bigint NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_status_pages PRIMARY KEY (id),
    CONSTRAINT ak_status_pages_id_organization UNIQUE (id, organization_id),
    CONSTRAINT ux_status_pages_organization UNIQUE (organization_id),
    CONSTRAINT fk_status_pages_organization FOREIGN KEY (organization_id)
        REFERENCES organizations (id) ON DELETE CASCADE,
    CONSTRAINT ck_status_pages_title CHECK (char_length(btrim(title)) BETWEEN 1 AND 100),
    CONSTRAINT ck_status_pages_version CHECK (version > 0),
    CONSTRAINT ck_status_pages_timestamps CHECK (updated_at >= created_at)
);

CREATE TABLE status_page_components (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    status_page_id uuid NOT NULL,
    monitor_id uuid NOT NULL,
    label character varying(100) NOT NULL,
    position smallint NOT NULL,
    CONSTRAINT pk_status_page_components PRIMARY KEY (id),
    CONSTRAINT fk_status_page_components_page FOREIGN KEY (status_page_id, organization_id)
        REFERENCES status_pages (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_status_page_components_monitor FOREIGN KEY (monitor_id, organization_id)
        REFERENCES monitors (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT ck_status_page_components_label CHECK (
        char_length(btrim(label)) BETWEEN 1 AND 100
    ),
    CONSTRAINT ck_status_page_components_position CHECK (position BETWEEN 0 AND 49),
    CONSTRAINT ux_status_page_components_position UNIQUE (status_page_id, position),
    CONSTRAINT ux_status_page_components_monitor UNIQUE (status_page_id, monitor_id)
);

CREATE INDEX ix_status_page_components_organization
    ON status_page_components (organization_id, status_page_id);
