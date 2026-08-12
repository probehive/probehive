package postgres

import (
	"errors"
	"testing"

	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/statuspage"
)

func TestStatusPageDraftPersistsOrderAndEnforcesTenantAvailability(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, project := seedTenant(t, database, 100, "status-one")
	otherOrganization, otherProject := seedTenant(t, database, 200, "status-two")
	now := testTime()
	first := seedMonitor(t, database, 110, organizationValue, project, now)
	second := seedMonitor(t, database, 111, organizationValue, project, now)
	other := seedMonitor(t, database, 210, otherOrganization, otherProject, now)

	store := database.StatusPages()
	draft, err := statuspage.RestoreDraft(
		statuspage.ID(testUUID(120)), string(organizationValue.ID), "Service Status", 1,
		now, now, []statuspage.Component{
			{ID: statuspage.ComponentID(testUUID(121)), MonitorID: string(second.ID), Label: "API", Position: 0},
			{ID: statuspage.ComponentID(testUUID(122)), MonitorID: string(first.ID), Label: "Website", Position: 1},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ReplaceDraft(t.Context(), draft, 0); err != nil {
		t.Fatalf("ReplaceDraft() create error = %v", err)
	}

	loaded, found, err := store.FindDraft(t.Context(), string(organizationValue.ID))
	if err != nil || !found {
		t.Fatalf("FindDraft() found/error = %v/%v", found, err)
	}
	if loaded.Version != 1 || len(loaded.Components) != 2 ||
		loaded.Components[0].MonitorID != string(second.ID) ||
		loaded.Components[1].MonitorID != string(first.ID) {
		t.Fatalf("loaded draft = %#v", loaded)
	}
	if _, otherFound, err := store.FindDraft(t.Context(), string(otherOrganization.ID)); err != nil || otherFound {
		t.Fatalf("other tenant FindDraft() found/error = %v/%v", otherFound, err)
	}

	updated, err := statuspage.RestoreDraft(
		draft.ID, draft.OrganizationID, "Public Services", 2, draft.CreatedAt, now.Add(1),
		[]statuspage.Component{
			{ID: draft.Components[1].ID, MonitorID: string(first.ID), Label: "Home", Position: 0},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ReplaceDraft(t.Context(), updated, 1); err != nil {
		t.Fatalf("ReplaceDraft() update error = %v", err)
	}
	loaded, found, err = database.StatusPages().FindDraft(t.Context(), string(organizationValue.ID))
	if err != nil || !found || loaded.Title != "Public Services" || loaded.Version != 2 ||
		len(loaded.Components) != 1 || loaded.Components[0].ID != draft.Components[1].ID {
		t.Fatalf("restart-visible draft = %#v, found/error = %v/%v", loaded, found, err)
	}
	if err = store.ReplaceDraft(t.Context(), updated, 1); !errors.Is(err, statuspage.ErrConcurrentUpdate) {
		t.Fatalf("stale ReplaceDraft() error = %v, want ErrConcurrentUpdate", err)
	}

	crossTenant, err := statuspage.RestoreDraft(
		draft.ID, draft.OrganizationID, "Status", 3, draft.CreatedAt, now.Add(2),
		[]statuspage.Component{{
			ID: statuspage.ComponentID(testUUID(123)), MonitorID: string(other.ID), Label: "Other", Position: 0,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ReplaceDraft(t.Context(), crossTenant, 2); !errors.Is(err, statuspage.ErrMonitorUnavailable) {
		t.Fatalf("cross-tenant ReplaceDraft() error = %v, want unavailable", err)
	}
	loaded, _, err = store.FindDraft(t.Context(), string(organizationValue.ID))
	if err != nil || loaded.Version != 2 || loaded.Components[0].MonitorID != string(first.ID) {
		t.Fatalf("failed replacement changed draft = %#v, error %v", loaded, err)
	}

	archived, monitorFound, err := database.Monitors().FindMonitor(t.Context(), monitor.Scope{
		OrganizationID: string(organizationValue.ID),
		ProjectID:      string(project.ID),
		MonitorID:      first.ID,
	})
	if err != nil || !monitorFound {
		t.Fatalf("reload Monitor found/error = %v/%v", monitorFound, err)
	}
	archived.LatestRevisionNumber = 1
	archived.State = monitor.StateArchived
	archived.UpdatedAt = now.Add(3)
	if err = database.Monitors().UpdateMonitor(t.Context(), archived, archived.Version); err != nil {
		t.Fatalf("archive Monitor: %v", err)
	}
	archivedDraft, err := statuspage.RestoreDraft(
		draft.ID, draft.OrganizationID, "Status", 3, draft.CreatedAt, now.Add(4),
		[]statuspage.Component{{
			ID: draft.Components[1].ID, MonitorID: string(first.ID), Label: "Home", Position: 0,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ReplaceDraft(t.Context(), archivedDraft, 2); !errors.Is(err, statuspage.ErrMonitorUnavailable) {
		t.Fatalf("archived ReplaceDraft() error = %v, want unavailable", err)
	}
}

func TestConcurrentStatusPageDraftCreationAllowsOnlyOneDraft(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, project := seedTenant(t, database, 300, "status-race")
	now := testTime()
	monitorValue := seedMonitor(t, database, 310, organizationValue, project, now)
	drafts := make([]statuspage.Draft, 2)
	for index := range drafts {
		draft, err := statuspage.RestoreDraft(
			statuspage.ID(testUUID(320+index*2)), string(organizationValue.ID),
			"Service Status", 1, now, now, []statuspage.Component{{
				ID:        statuspage.ComponentID(testUUID(321 + index*2)),
				MonitorID: string(monitorValue.ID),
				Label:     "API",
				Position:  0,
			}},
		)
		if err != nil {
			t.Fatal(err)
		}
		drafts[index] = draft
	}

	start := make(chan struct{})
	results := make(chan error, len(drafts))
	for _, draft := range drafts {
		go func(draft statuspage.Draft) {
			<-start
			results <- database.StatusPages().ReplaceDraft(t.Context(), draft, 0)
		}(draft)
	}
	close(start)

	created, conflicted := 0, 0
	for range drafts {
		err := <-results
		switch {
		case err == nil:
			created++
		case errors.Is(err, statuspage.ErrConcurrentUpdate):
			conflicted++
		default:
			t.Fatalf("concurrent ReplaceDraft() error = %v", err)
		}
	}
	if created != 1 || conflicted != 1 {
		t.Fatalf("concurrent results created/conflicted = %d/%d, want 1/1", created, conflicted)
	}

	loaded, found, err := database.StatusPages().FindDraft(t.Context(), string(organizationValue.ID))
	if err != nil || !found || loaded.Version != 1 || len(loaded.Components) != 1 {
		t.Fatalf("persisted draft = %#v, found/error = %v/%v", loaded, found, err)
	}
}
