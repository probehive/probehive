package postgres

import (
	"testing"
	"time"

	"github.com/probehive/probehive/internal/health"
)

func TestListStaleHealthUsesTimestampCutoffs(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, project := seedTenant(t, database, 1300, "stale-health-tenant")
	monitorValue := seedMonitor(t, database, 1305, organizationValue, project, testTime())
	monitorValue = appendTestRevision(
		t, database, monitorValue, 1, `{"url":"https://health.example.test"}`)
	activateMonitor(t, database, &monitorValue)

	now := testTime().Add(10 * time.Minute)
	lastDeterminate := now.Add(-4 * time.Minute)
	if _, err := database.pool.Exec(t.Context(), `
INSERT INTO monitor_health (
    monitor_id, organization_id, project_id, state, stable_state,
    policy_version, version, source_revision_number, last_scheduled_for,
    last_determinate_finished_at, configured_count, eligible_count,
    responding_count, passing_count, failing_count, location_fault_count,
    indeterminate_count, missing_count, transitioned_at, updated_at
) VALUES (
    $1, $2, $3, 'healthy', 'healthy', $4, 1, 1, $5, $5,
    1, 1, 1, 1, 0, 0, 0, 0, $5, $5
)`, string(monitorValue.ID), string(organizationValue.ID), string(project.ID),
		health.PolicyVersion, lastDeterminate); err != nil {
		t.Fatalf("insert stale Monitor health: %v", err)
	}

	targets, err := database.Health().ListStaleHealth(
		t.Context(), now, 30*time.Second, 10)
	if err != nil {
		t.Fatalf("ListStaleHealth() error = %v", err)
	}
	if len(targets) != 1 ||
		targets[0].OrganizationID != string(organizationValue.ID) ||
		targets[0].ProjectID != string(project.ID) ||
		targets[0].MonitorID != string(monitorValue.ID) || targets[0].Version != 1 {
		t.Fatalf("ListStaleHealth() = %#v", targets)
	}

	recentDeterminate := now.Add(-2 * time.Minute)
	if _, err := database.pool.Exec(t.Context(), `
UPDATE monitor_health
SET last_determinate_finished_at=$1, updated_at=$1
WHERE monitor_id=$2`, recentDeterminate, string(monitorValue.ID)); err != nil {
		t.Fatalf("update recent Monitor health: %v", err)
	}
	targets, err = database.Health().ListStaleHealth(
		t.Context(), now, 30*time.Second, 10)
	if err != nil {
		t.Fatalf("ListStaleHealth(recent) error = %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("ListStaleHealth(recent) = %#v, want no targets", targets)
	}
}
