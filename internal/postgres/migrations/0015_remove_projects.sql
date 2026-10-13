-- Monitors hang directly off their Organization; the single-project layer carried no
-- operator-visible meaning, so this drops the layer everywhere it was persisted.
-- Old persisted outbox payloads that still carry a projectId field keep decoding:
-- unknown fields are ignored.

ALTER TABLE monitors DROP CONSTRAINT fk_monitors_projects;
ALTER TABLE alerts DROP CONSTRAINT fk_alerts_projects;
ALTER TABLE health_candidates DROP CONSTRAINT fk_health_candidates_projects;
ALTER TABLE monitor_health DROP CONSTRAINT fk_monitor_health_projects;
ALTER TABLE health_transitions DROP CONSTRAINT fk_health_transitions_projects;
ALTER TABLE incidents DROP CONSTRAINT fk_incidents_projects;

-- Column drops remove the composite indexes that included project_id; recreate the
-- tenant lookup indexes that remain meaningful without it.
CREATE INDEX ix_monitors_organization_id ON monitors (organization_id);
CREATE INDEX ix_alerts_organization_monitor
    ON alerts (organization_id, monitor_id, occurred_at DESC, id DESC);
CREATE INDEX ix_monitor_health_organization_monitor
    ON monitor_health (organization_id, monitor_id);
CREATE INDEX ix_incidents_organization_monitor
    ON incidents (organization_id, monitor_id, created_at DESC, id DESC);

ALTER TABLE monitors DROP COLUMN project_id;
ALTER TABLE alerts DROP COLUMN project_id;
ALTER TABLE health_candidates DROP COLUMN project_id;
ALTER TABLE monitor_health DROP COLUMN project_id;
ALTER TABLE health_transitions DROP COLUMN project_id;
ALTER TABLE incidents DROP COLUMN project_id;

DROP TABLE projects;
