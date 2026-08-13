package postgres

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/probehive/probehive/internal/maintenance"
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

func TestConcurrentStatusPagePublicationActivatesOnlyOneCapability(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, project := seedTenant(t, database, 350, "status-publish-race")
	now := testTime()
	monitorValue := seedMonitor(t, database, 360, organizationValue, project, now)
	draft, err := statuspage.RestoreDraft(
		statuspage.ID(testUUID(370)), string(organizationValue.ID), "Service Status", 1,
		now, now, []statuspage.Component{{
			ID: statuspage.ComponentID(testUUID(371)), MonitorID: string(monitorValue.ID),
			Label: "API", Position: 0,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.StatusPages().ReplaceDraft(t.Context(), draft, 0); err != nil {
		t.Fatal(err)
	}

	publications := make([]statuspage.Publication, 2)
	for index, token := range []string{
		base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")),
		base64.RawURLEncoding.EncodeToString([]byte("abcdef0123456789abcdef0123456789")),
	} {
		tokenHash, valid := statuspage.HashPublicationToken(token)
		if !valid {
			t.Fatal("test publication token is invalid")
		}
		publications[index] = statuspage.Publication{TokenHash: tokenHash, PublishedAt: now.Add(time.Duration(index))}
	}
	start := make(chan struct{})
	results := make(chan error, len(publications))
	for _, publication := range publications {
		go func(publication statuspage.Publication) {
			<-start
			results <- database.StatusPages().Publish(t.Context(), string(organizationValue.ID), publication)
		}(publication)
	}
	close(start)

	published, conflicted := 0, 0
	for range publications {
		switch err = <-results; {
		case err == nil:
			published++
		case errors.Is(err, statuspage.ErrAlreadyPublished):
			conflicted++
		default:
			t.Fatalf("concurrent Publish() error = %v", err)
		}
	}
	if published != 1 || conflicted != 1 {
		t.Fatalf("concurrent publication results published/conflicted = %d/%d, want 1/1", published, conflicted)
	}
}

func TestStatusPagePublicationProjectsCurrentStateAndRevokesImmediately(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, project := seedTenant(t, database, 400, "status-public")
	now := testTime()
	monitorValue := seedMonitor(t, database, 410, organizationValue, project, now)
	draft, err := statuspage.RestoreDraft(
		statuspage.ID(testUUID(420)), string(organizationValue.ID), "Service Status", 1,
		now, now, []statuspage.Component{{
			ID: statuspage.ComponentID(testUUID(421)), MonitorID: string(monitorValue.ID),
			Label: "Public API", Position: 0,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	store := database.StatusPages()
	if err = store.ReplaceDraft(t.Context(), draft, 0); err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	tokenHash, valid := statuspage.HashPublicationToken(token)
	if !valid {
		t.Fatal("test token is invalid")
	}
	publication := statuspage.Publication{TokenHash: tokenHash, PublishedAt: now}
	if err = store.Publish(t.Context(), string(organizationValue.ID), publication); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	var persistedHash []byte
	if err = database.pool.QueryRow(t.Context(), `
SELECT publication_token_hash FROM status_pages WHERE organization_id = $1`,
		string(organizationValue.ID)).Scan(&persistedHash); err != nil {
		t.Fatal(err)
	}
	if string(persistedHash) == token || len(persistedHash) != 32 {
		t.Fatalf("persisted publication digest length/raw match = %d/%v", len(persistedHash), string(persistedHash) == token)
	}

	if _, err = database.pool.Exec(t.Context(), `
UPDATE monitors SET state = 'active', latest_revision_number = 1 WHERE id = $1 AND organization_id = $2`,
		string(monitorValue.ID), string(organizationValue.ID)); err != nil {
		t.Fatalf("activate Monitor: %v", err)
	}

	if _, err = database.pool.Exec(t.Context(), `
INSERT INTO monitor_health (
    monitor_id, organization_id, project_id, state, stable_state, policy_version,
    version, source_revision_number, last_scheduled_for, last_determinate_finished_at,
    last_run_id, last_run_scheduled_for, candidate_id,
    configured_count, eligible_count, responding_count, passing_count, failing_count,
    location_fault_count, indeterminate_count, missing_count, transitioned_at, updated_at
) VALUES ($1,$2,$3,'down','down','phase1.v1',1,1,$4,$4,NULL,NULL,NULL,1,1,1,0,1,0,0,0,$4,$4)`,
		string(monitorValue.ID), string(organizationValue.ID), string(project.ID), now.Add(time.Minute)); err != nil {
		t.Fatalf("seed health: %v", err)
	}
	window, err := maintenance.NewWindow(
		maintenance.ID(testUUID(422)), maintenance.Scope{
			OrganizationID: string(organizationValue.ID), ProjectID: string(project.ID),
			MonitorID: string(monitorValue.ID),
		}, now.Add(2*time.Minute), now.Add(10*time.Minute), now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Maintenance().CreateWindow(t.Context(), window); err != nil {
		t.Fatalf("CreateWindow() error = %v", err)
	}

	projected, found, err := database.StatusPages().FindPublicPage(
		t.Context(), tokenHash, now.Add(3*time.Minute),
	)
	if err != nil || !found || projected.Title != "Service Status" || len(projected.Components) != 1 {
		t.Fatalf("FindPublicPage() = %#v, %v, %v", projected, found, err)
	}
	component := projected.Components[0]
	if component.Label != "Public API" || component.State != "down" || !component.Maintenance ||
		!component.UpdatedAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("public component = %#v", component)
	}

	if _, err = database.pool.Exec(t.Context(), `
UPDATE monitors SET state = 'paused', updated_at = $1 WHERE id = $2 AND organization_id = $3`,
		now.Add(4*time.Minute), string(monitorValue.ID), string(organizationValue.ID)); err != nil {
		t.Fatalf("pause Monitor: %v", err)
	}
	projected, found, err = database.StatusPages().FindPublicPage(
		t.Context(), tokenHash, now.Add(5*time.Minute),
	)
	if err != nil || !found || projected.Components[0].State != "unknown" ||
		projected.Components[0].Maintenance ||
		!projected.Components[0].UpdatedAt.Equal(now.Add(4*time.Minute)) {
		t.Fatalf("paused public projection = %#v, %v, %v", projected, found, err)
	}

	loaded, found, err := database.StatusPages().FindDraft(t.Context(), string(organizationValue.ID))
	if err != nil || !found || loaded.Publication == nil || loaded.Publication.TokenHash != tokenHash {
		t.Fatalf("restart-visible publication = %#v, %v, %v", loaded.Publication, found, err)
	}
	if err = database.StatusPages().Revoke(t.Context(), string(organizationValue.ID)); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if _, found, err = database.StatusPages().FindPublicPage(t.Context(), tokenHash, now.Add(5*time.Minute)); err != nil || found {
		t.Fatalf("revoked FindPublicPage() found/error = %v/%v", found, err)
	}
}
