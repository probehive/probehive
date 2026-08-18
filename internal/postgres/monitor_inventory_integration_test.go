package postgres

import (
	"testing"
	"time"

	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/run"
)

func TestMonitorInventoryIsScopedFilteredSortedAndPaginated(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, project := seedTenant(t, database, 4100, "inventory-one")
	otherOrganization, otherProject := seedTenant(t, database, 4200, "inventory-two")
	base := testTime()
	alpha := seedMonitor(t, database, 4110, organizationValue, project, base)
	beta := seedMonitor(t, database, 4111, organizationValue, project, base.Add(time.Minute))
	gamma := seedMonitor(t, database, 4112, organizationValue, project, base.Add(2*time.Minute))
	other := seedMonitor(t, database, 4210, otherOrganization, otherProject, base)
	setOverviewMonitorState(t, database, alpha, "paused")
	setOverviewMonitorState(t, database, beta, "active")
	setOverviewMonitorState(t, database, gamma, "active")
	setOverviewMonitorState(t, database, other, "active")
	if _, err := database.pool.Exec(t.Context(), `UPDATE monitors SET name = 'Alpha API' WHERE id = $1`, alpha.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.pool.Exec(t.Context(), `UPDATE monitors SET name = 'Beta API' WHERE id = $1`, beta.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.pool.Exec(t.Context(), `UPDATE monitors SET name = 'Gamma Site' WHERE id = $1`, gamma.ID); err != nil {
		t.Fatal(err)
	}
	seedOverviewHealth(t, database, beta, "down", base.Add(3*time.Hour))
	seedOverviewHealth(t, database, other, "down", base.Add(3*time.Hour))

	if _, err := database.Runs().EnsurePartitions(t.Context(), base, run.DefaultPartitionsAhead); err != nil {
		t.Fatal(err)
	}
	runAt := base.Add(4 * time.Hour)
	if _, err := database.pool.Exec(t.Context(), `
INSERT INTO runs (
    id, organization_id, monitor_id, revision_number, location, scheduled_for,
    kind, outcome, started_at, finished_at, created_at
) VALUES ($1,$2,$3,1,'local',$4,'manual','failed',$4,$4,$4)`,
		testUUID(4130), beta.OrganizationID, beta.ID, runAt); err != nil {
		t.Fatal(err)
	}
	now := base.Add(5 * time.Hour)
	if _, err := database.pool.Exec(t.Context(), `
INSERT INTO maintenance_windows (
    id, organization_id, monitor_id, starts_at, ends_at, created_at
) VALUES ($1,$2,$3,$4,$5,$6)`,
		testUUID(4140), beta.OrganizationID, beta.ID, now.Add(-time.Hour), now.Add(time.Hour), base); err != nil {
		t.Fatal(err)
	}

	store := database.MonitorInventory()
	first, total, err := store.ListMonitorInventory(t.Context(), string(organizationValue.ID), string(project.ID), monitor.InventoryQuery{
		Sort: monitor.InventorySortName, Direction: monitor.InventoryDirectionAscending, Page: 1, PageSize: 2,
	}, now)
	if err != nil || total != 3 || len(first) != 2 || first[0].Monitor.Name != "Alpha API" || first[1].Monitor.Name != "Beta API" {
		t.Fatalf("first page = %#v, total %d, error %v", first, total, err)
	}
	second, total, err := store.ListMonitorInventory(t.Context(), string(organizationValue.ID), string(project.ID), monitor.InventoryQuery{
		Sort: monitor.InventorySortName, Direction: monitor.InventoryDirectionAscending, Page: 2, PageSize: 2,
	}, now)
	if err != nil || total != 3 || len(second) != 1 || second[0].Monitor.Name != "Gamma Site" {
		t.Fatalf("second page = %#v, total %d, error %v", second, total, err)
	}
	literal, total, err := store.ListMonitorInventory(t.Context(), string(organizationValue.ID), string(project.ID), monitor.InventoryQuery{
		Search: "%", Sort: monitor.InventorySortName, Direction: monitor.InventoryDirectionAscending, Page: 1, PageSize: 25,
	}, now)
	if err != nil || total != 0 || len(literal) != 0 {
		t.Fatalf("literal wildcard search = %#v, total %d, error %v", literal, total, err)
	}
	combined, total, err := store.ListMonitorInventory(t.Context(), string(organizationValue.ID), string(project.ID), monitor.InventoryQuery{
		Search: "api", State: monitor.StateActive, Health: monitor.InventoryHealthDown,
		RunOutcome: monitor.InventoryRunFailed, Maintenance: monitor.InventoryMaintenanceActive,
		Sort: monitor.InventorySortUpdatedAt, Direction: monitor.InventoryDirectionDescending,
		Page: 1, PageSize: 25,
	}, now)
	if err != nil || total != 1 || len(combined) != 1 || combined[0].Monitor.ID != beta.ID {
		t.Fatalf("combined query = %#v, total %d, error %v", combined, total, err)
	}
	item := combined[0]
	if item.Health == nil || item.Health.State != "down" || item.LastRun == nil || item.LastRun.Outcome != "failed" || item.Maintenance.State != monitor.InventoryMaintenanceActive {
		t.Fatalf("combined facts = %#v", item)
	}
}
