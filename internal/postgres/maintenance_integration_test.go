package postgres

import (
	"testing"
	"time"

	"github.com/probehive/probehive/internal/maintenance"
)

func TestMaintenanceWindowPersistenceIsTenantIsolatedAndRestartVisible(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	now := testTime()
	organizationA, projectA := seedTenant(t, database, 200, "maintenance-a")
	organizationB, projectB := seedTenant(t, database, 210, "maintenance-b")
	monitorA := seedMonitor(t, database, 220, organizationA, projectA, now)

	scopeA := maintenance.Scope{
		OrganizationID: string(organizationA.ID),
		ProjectID:      string(projectA.ID),
		MonitorID:      string(monitorA.ID),
	}
	service := maintenance.NewService(
		database.Maintenance(),
		fixedClock{now},
		&sequenceUUIDs{values: []string{testUUID(221)}},
	)
	created, err := service.Create(t.Context(), maintenance.CreateCommand{
		Scope: scopeA, StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour),
	})
	if err != nil || created.Kind != maintenance.CreateCreated {
		t.Fatalf("Create() result/error = %#v/%v", created, err)
	}

	// A fresh adapter and service simulate restart/restore visibility rather than relying
	// on in-memory state retained by the creator.
	restoredService := maintenance.NewService(
		database.Maintenance(), fixedClock{now}, &sequenceUUIDs{values: []string{testUUID(222)}},
	)
	listed, found, err := restoredService.List(t.Context(), scopeA)
	if err != nil || !found || len(listed) != 1 {
		t.Fatalf("restored List() found/len/error = %v/%d/%v", found, len(listed), err)
	}
	if listed[0].ID != created.Window.ID || listed[0].StartsAt != created.Window.StartsAt {
		t.Fatalf("restored window = %#v, want %#v", listed[0], created.Window)
	}

	wrongScopes := []maintenance.Scope{
		{
			OrganizationID: string(organizationB.ID),
			ProjectID:      string(projectB.ID),
			MonitorID:      string(monitorA.ID),
		},
		{
			OrganizationID: string(organizationA.ID),
			ProjectID:      string(projectB.ID),
			MonitorID:      string(monitorA.ID),
		},
	}
	for _, scope := range wrongScopes {
		values, scopeFound, scopeErr := restoredService.List(t.Context(), scope)
		if scopeErr != nil || scopeFound || values != nil {
			t.Fatalf("wrong-scope List(%#v) = %#v/%v/%v", scope, values, scopeFound, scopeErr)
		}
		if _, windowFound, findErr := restoredService.Get(t.Context(), scope, created.Window.ID); findErr != nil || windowFound {
			t.Fatalf("wrong-scope Get(%#v) found/error = %v/%v", scope, windowFound, findErr)
		}
	}

	cancelled, err := restoredService.Cancel(t.Context(), scopeA, created.Window.ID)
	if err != nil || cancelled.Kind != maintenance.CancelCancelled || cancelled.Window.CancelledAt == nil {
		t.Fatalf("Cancel() result/error = %#v/%v", cancelled, err)
	}
	afterRestart, found, err := maintenance.NewService(
		database.Maintenance(), fixedClock{now}, &sequenceUUIDs{},
	).Get(t.Context(), scopeA, created.Window.ID)
	if err != nil || !found || afterRestart.CancelledAt == nil {
		t.Fatalf("restart-visible cancellation found/window/error = %v/%#v/%v", found, afterRestart, err)
	}
}

func TestConcurrentMaintenanceCreationAllowsOnlyOneOverlap(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	now := testTime()
	organizationValue, project := seedTenant(t, database, 230, "maintenance-race")
	monitorValue := seedMonitor(t, database, 240, organizationValue, project, now)
	scope := maintenance.Scope{
		OrganizationID: string(organizationValue.ID),
		ProjectID:      string(project.ID),
		MonitorID:      string(monitorValue.ID),
	}
	services := []*maintenance.Service{
		maintenance.NewService(
			database.Maintenance(), fixedClock{now},
			&sequenceUUIDs{values: []string{testUUID(241)}},
		),
		maintenance.NewService(
			database.Maintenance(), fixedClock{now},
			&sequenceUUIDs{values: []string{testUUID(242)}},
		),
	}

	start := make(chan struct{})
	results := make(chan maintenance.CreateResult, len(services))
	errorsByCall := make(chan error, len(services))
	for _, service := range services {
		go func(service *maintenance.Service) {
			<-start
			result, err := service.Create(t.Context(), maintenance.CreateCommand{
				Scope: scope, StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour),
			})
			results <- result
			errorsByCall <- err
		}(service)
	}
	close(start)

	kinds := map[maintenance.CreateKind]int{}
	for range services {
		if err := <-errorsByCall; err != nil {
			t.Fatalf("concurrent Create() error = %v", err)
		}
		kinds[(<-results).Kind]++
	}
	if kinds[maintenance.CreateCreated] != 1 || kinds[maintenance.CreateConflict] != 1 {
		t.Fatalf("concurrent Create() kinds = %#v, want one created and one conflict", kinds)
	}

	var count int
	if err := database.pool.QueryRow(t.Context(), `
SELECT count(*)
FROM maintenance_windows
WHERE organization_id = $1 AND monitor_id = $2`,
		scope.OrganizationID, scope.MonitorID,
	).Scan(&count); err != nil {
		t.Fatalf("count maintenance windows: %v", err)
	}
	if count != 1 {
		t.Fatalf("persisted maintenance windows = %d, want 1", count)
	}
}
