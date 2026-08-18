package postgres

import (
	"testing"
	"time"

	"github.com/probehive/probehive/internal/incident"
)

func TestIncidentInboxIsTenantScopedFilteredAndKeysetPaginated(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, project := seedTenant(t, database, 4300, "incident-inbox-one")
	otherOrganization, otherProject := seedTenant(t, database, 4400, "incident-inbox-two")
	base := testTime()
	monitorValue := seedMonitor(t, database, 4310, organizationValue, project, base)
	acknowledgedMonitor := seedMonitor(t, database, 4311, organizationValue, project, base)
	otherMonitor := seedMonitor(t, database, 4410, otherOrganization, otherProject, base)
	if _, err := database.pool.Exec(t.Context(), `
UPDATE monitors SET name='Checkout API' WHERE id=$1`, string(monitorValue.ID)); err != nil {
		t.Fatal(err)
	}
	seedOverviewHealth(t, database, monitorValue, "down", base.Add(time.Hour))
	seedOverviewIncident(
		t, database, monitorValue, testUUID(4320), testUUID(4321), "open", "",
		base.Add(3*time.Hour),
	)
	creator := seedAdministrator(t, database)
	seedOverviewIncident(
		t, database, acknowledgedMonitor, testUUID(4322), testUUID(4323), "acknowledged", creator,
		base.Add(2*time.Hour),
	)
	seedOverviewIncident(
		t, database, otherMonitor, testUUID(4420), testUUID(4421), "open", "",
		base.Add(4*time.Hour),
	)
	openingRunID := testUUID(4330)
	openingRunScheduledFor := base.Add(3*time.Hour - time.Minute)
	if _, err := database.pool.Exec(t.Context(), `
INSERT INTO incident_timeline_entries (
    id, organization_id, incident_id, incident_version, kind,
    causal_run_id, causal_run_scheduled_for, occurred_at
) VALUES ($1,$2,$3,1,'opened',$4,$5,$6)`,
		testUUID(4331), string(organizationValue.ID), testUUID(4320),
		openingRunID, openingRunScheduledFor, base.Add(3*time.Hour),
	); err != nil {
		t.Fatal(err)
	}
	now := base.Add(4 * time.Hour)
	if _, err := database.pool.Exec(t.Context(), `
INSERT INTO maintenance_windows (
    id, organization_id, monitor_id, starts_at, ends_at, created_at
) VALUES ($1,$2,$3,$4,$5,$6)`,
		testUUID(4340), string(organizationValue.ID), string(monitorValue.ID),
		now.Add(-time.Hour), now.Add(time.Hour), base,
	); err != nil {
		t.Fatal(err)
	}

	store := database.Incidents()
	first, more, found, err := store.ListInbox(
		t.Context(), string(organizationValue.ID),
		incident.InboxQuery{State: incident.InboxStateActive, PageSize: 1}, now,
	)
	if err != nil || !found || !more || len(first) != 1 ||
		first[0].Incident.ID != testUUID(4320) {
		t.Fatalf("first Incident inbox page = %#v, more/found/error %v/%v/%v", first, more, found, err)
	}
	item := first[0]
	if item.MonitorName != "Checkout API" || item.Health == nil || item.Health.State != "down" ||
		item.Maintenance == nil || item.Maintenance.State != "active" ||
		item.OpeningRun == nil || item.OpeningRun.ID != openingRunID ||
		!item.OpeningRun.ScheduledFor.Equal(openingRunScheduledFor) || item.OpeningRun.Available {
		t.Fatalf("Incident inbox facts = %#v", item)
	}
	next, more, found, err := store.ListInbox(
		t.Context(), string(organizationValue.ID),
		incident.InboxQuery{
			State: incident.InboxStateActive, PageSize: 1,
			Cursor: &incident.Cursor{CreatedAt: item.Incident.CreatedAt, ID: item.Incident.ID},
		}, now,
	)
	if err != nil || !found || more || len(next) != 1 ||
		next[0].Incident.ID != testUUID(4322) {
		t.Fatalf("second Incident inbox page = %#v, more/found/error %v/%v/%v", next, more, found, err)
	}
	openOnly, more, found, err := store.ListInbox(
		t.Context(), string(organizationValue.ID),
		incident.InboxQuery{State: incident.InboxStateOpen, PageSize: 10}, now,
	)
	if err != nil || !found || more || len(openOnly) != 1 ||
		openOnly[0].Incident.ID != testUUID(4320) {
		t.Fatalf("open Incident inbox = %#v, more/found/error %v/%v/%v", openOnly, more, found, err)
	}
}
