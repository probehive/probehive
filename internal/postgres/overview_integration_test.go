package postgres

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/overview"
)

func TestOrganizationOverviewIsTenantScopedAndBounded(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	firstOrganization, firstProject := seedTenant(t, database, 3100, "overview-one")
	secondOrganization, secondProject := seedTenant(t, database, 3200, "overview-two")
	base := testTime()

	firstMonitors := make([]monitor.Monitor, 6)
	for index := range firstMonitors {
		firstMonitors[index] = seedMonitor(
			t, database, 3110+index, firstOrganization, firstProject, base.Add(time.Duration(index)*time.Minute),
		)
	}
	setOverviewMonitorState(t, database, firstMonitors[1], "active")
	setOverviewMonitorState(t, database, firstMonitors[2], "active")
	setOverviewMonitorState(t, database, firstMonitors[3], "active")
	setOverviewMonitorState(t, database, firstMonitors[4], "paused")
	setOverviewMonitorState(t, database, firstMonitors[5], "archived")
	seedOverviewHealth(t, database, firstMonitors[1], "healthy", base.Add(time.Hour))
	seedOverviewHealth(t, database, firstMonitors[2], "down", base.Add(2*time.Hour))

	creator := seedAdministrator(t, database)
	seedOverviewIncident(
		t, database, firstMonitors[2], testUUID(3130), testUUID(3131),
		"open", "", base.Add(4*time.Hour),
	)
	seedOverviewIncident(
		t, database, firstMonitors[4], testUUID(3132), testUUID(3133),
		"acknowledged", creator, base.Add(3*time.Hour),
	)
	seedOverviewStatusPage(t, database, firstOrganization, testUUID(3140), true, base)
	seedOverviewIntegration(t, database, firstOrganization, testUUID(3150), "Primary", true, base)
	seedOverviewIntegration(t, database, firstOrganization, testUUID(3151), "Standby", false, base)

	otherMonitor := seedMonitor(t, database, 3210, secondOrganization, secondProject, base)
	setOverviewMonitorState(t, database, otherMonitor, "active")
	seedOverviewHealth(t, database, otherMonitor, "down", base)
	seedOverviewStatusPage(t, database, secondOrganization, testUUID(3240), true, base)
	seedOverviewIntegration(t, database, secondOrganization, testUUID(3250), "Other", true, base)

	store := database.Overviews()
	value, found, err := store.GetOverview(
		t.Context(), string(firstOrganization.ID), overview.ActiveIncidentPreviewLimit,
	)
	if err != nil || !found {
		t.Fatalf("GetOverview() = (%#v, %v, %v)", value, found, err)
	}
	if value.OrganizationID != string(firstOrganization.ID) ||
		value.Monitors != (overview.MonitorCounts{Total: 6, Draft: 1, Active: 3, Paused: 1, Archived: 1}) {
		t.Fatalf("Monitor counts = %#v", value.Monitors)
	}
	if value.Health != (overview.HealthCounts{NotEvaluated: 1, Healthy: 1, Down: 1}) {
		t.Fatalf("health counts = %#v", value.Health)
	}
	if value.Incidents != (overview.IncidentCounts{Active: 2, Open: 1, Acknowledged: 1}) {
		t.Fatalf("Incident counts = %#v", value.Incidents)
	}
	if len(value.ActiveIncidents) != 2 || value.ActiveIncidentsTruncated ||
		value.ActiveIncidents[0].ID != testUUID(3130) ||
		value.ActiveIncidents[0].MonitorName != firstMonitors[2].Name ||
		value.ActiveIncidents[0].UpdatedAt.Location() != time.UTC {
		t.Fatalf("active Incident preview = %#v, truncated %v", value.ActiveIncidents, value.ActiveIncidentsTruncated)
	}
	if value.Integrations != (overview.IntegrationCounts{Total: 2, Enabled: 1}) {
		t.Fatalf("Integration counts = %#v", value.Integrations)
	}
	if value.StatusPage != (overview.StatusPageState{Configured: true, Published: true}) {
		t.Fatalf("status-page state = %#v", value.StatusPage)
	}

	bounded, found, err := store.GetOverview(t.Context(), string(firstOrganization.ID), 1)
	if err != nil || !found || len(bounded.ActiveIncidents) != 1 || !bounded.ActiveIncidentsTruncated {
		t.Fatalf("bounded GetOverview() = (%#v, %v, %v)", bounded, found, err)
	}
	missing, found, err := store.GetOverview(t.Context(), testUUID(3999), 5)
	if err != nil || found || missing.OrganizationID != "" {
		t.Fatalf("missing GetOverview() = (%#v, %v, %v)", missing, found, err)
	}
}

func setOverviewMonitorState(t *testing.T, database *DB, value monitor.Monitor, state string) {
	t.Helper()
	if _, err := database.pool.Exec(t.Context(), `
UPDATE monitors
SET state = $1, latest_revision_number = 1, updated_at = $2
WHERE id = $3 AND organization_id = $4`,
		state, value.UpdatedAt.Add(time.Second), string(value.ID), value.OrganizationID,
	); err != nil {
		t.Fatalf("set overview Monitor state: %v", err)
	}
}

func seedOverviewHealth(
	t *testing.T, database *DB, value monitor.Monitor, state string, updatedAt time.Time,
) {
	t.Helper()
	if _, err := database.pool.Exec(t.Context(), `
INSERT INTO monitor_health (
    monitor_id, organization_id, project_id, state, stable_state, policy_version,
    version, source_revision_number, configured_count, eligible_count,
    responding_count, passing_count, failing_count, location_fault_count,
    indeterminate_count, missing_count, transitioned_at, updated_at
) VALUES ($1,$2,$3,$4,$4,'v1',1,1,1,1,1,$5,$6,0,0,0,$7,$7)`,
		string(value.ID), value.OrganizationID, value.ProjectID, state,
		boolCount(state == "healthy"), boolCount(state == "down"), updatedAt.UTC(),
	); err != nil {
		t.Fatalf("seed overview health: %v", err)
	}
}

func seedOverviewIncident(
	t *testing.T,
	database *DB,
	value monitor.Monitor,
	incidentID, transitionID, state, acknowledgedBy string,
	updatedAt time.Time,
) {
	t.Helper()
	if _, err := database.pool.Exec(t.Context(), `
INSERT INTO health_transitions (
    id, organization_id, project_id, monitor_id, version, old_state, new_state,
    policy_version, source_revision_number, configured_count, eligible_count,
    responding_count, passing_count, failing_count, location_fault_count,
    indeterminate_count, missing_count, occurred_at
) VALUES ($1,$2,$3,$4,1,'healthy','down','v1',1,1,1,1,0,1,0,0,0,$5)`,
		transitionID, value.OrganizationID, value.ProjectID, string(value.ID), updatedAt.UTC(),
	); err != nil {
		t.Fatalf("seed overview health transition: %v", err)
	}
	var actor any
	var acknowledgedAt any
	if acknowledgedBy != "" {
		actor, acknowledgedAt = acknowledgedBy, updatedAt.UTC()
	}
	if _, err := database.pool.Exec(t.Context(), `
INSERT INTO incidents (
    id, organization_id, project_id, monitor_id, state, version,
    opened_transition_id, acknowledged_by, acknowledged_at, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,1,$6,$7,$8,$9,$9)`,
		incidentID, value.OrganizationID, value.ProjectID, string(value.ID), state,
		transitionID, actor, acknowledgedAt, updatedAt.UTC(),
	); err != nil {
		t.Fatalf("seed overview Incident: %v", err)
	}
}

func seedOverviewStatusPage(
	t *testing.T,
	database *DB,
	organizationValue organization.Organization,
	id string,
	published bool,
	createdAt time.Time,
) {
	t.Helper()
	var tokenHash any
	var publishedAt any
	if published {
		digest := sha256.Sum256([]byte(id))
		tokenHash, publishedAt = digest[:], createdAt.UTC()
	}
	if _, err := database.pool.Exec(t.Context(), `
INSERT INTO status_pages (
    id, organization_id, title, version, created_at, updated_at,
    publication_token_hash, published_at
) VALUES ($1,$2,'Overview Status',1,$3,$3,$4,$5)`,
		id, string(organizationValue.ID), createdAt.UTC(), tokenHash, publishedAt,
	); err != nil {
		t.Fatalf("seed overview status page: %v", err)
	}
}

func seedOverviewIntegration(
	t *testing.T,
	database *DB,
	organizationValue organization.Organization,
	id, name string,
	enabled bool,
	createdAt time.Time,
) {
	t.Helper()
	transaction, err := database.pool.BeginTx(t.Context(), pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin overview Integration seed: %v", err)
	}
	defer func() { _ = transaction.Rollback(t.Context()) }()
	if _, err := transaction.Exec(t.Context(), `
INSERT INTO webhook_integrations (
    id, organization_id, name, destination_url, enabled, version,
    active_secret_version, created_at, updated_at
) VALUES ($1,$2,$3,'https://example.test/hook',$4,1,1,$5,$5)`,
		id, string(organizationValue.ID), name, enabled, createdAt.UTC(),
	); err != nil {
		t.Fatalf("seed overview Integration: %v", err)
	}
	if _, err := transaction.Exec(t.Context(), `
INSERT INTO webhook_signing_secrets (
    organization_id, integration_id, secret_version, state,
    wrapping_key_id, nonce, ciphertext, created_at, activated_at
) VALUES ($1,$2,1,'active','test',$3,$4,$5,$5)`,
		string(organizationValue.ID), id, make([]byte, 12), make([]byte, 16), createdAt.UTC(),
	); err != nil {
		t.Fatalf("seed overview Integration secret: %v", err)
	}
	if err := transaction.Commit(t.Context()); err != nil {
		t.Fatalf("commit overview Integration seed: %v", err)
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
