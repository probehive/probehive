package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/probehive/probehive/internal/health"
	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/incident"
	"github.com/probehive/probehive/internal/maintenance"
	"github.com/probehive/probehive/internal/organization"
)

func TestHealthAndIncidentAPIsEnforceScopePermissionsAndLifecycle(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	token := environment.bootstrapAdministrator(t)
	organizationID, projectID, monitorID := seedRunQueryMonitor(t, environment)
	now := environment.clock.Now()

	environment.health.mu.Lock()
	environment.health.snapshots = map[string]health.Snapshot{
		organizationID + "/" + projectID + "/" + monitorID: {
			OrganizationID: organizationID, ProjectID: projectID, MonitorID: monitorID,
			State: health.StateDown, StableState: health.StateDown,
			PolicyVersion: health.PolicyVersion, Version: 4, SourceRevisionNumber: 1,
			LastScheduledFor: now.Add(-time.Minute), LastDeterminateFinishedAt: now.Add(-50 * time.Second),
			LastRunID:           "00000000-0000-7000-8000-000000000040",
			LastRunScheduledFor: now.Add(-time.Minute), Counts: health.Counts{Configured: 1, Eligible: 1, Responding: 1, Failing: 1},
			TransitionedAt: now.Add(-50 * time.Second), UpdatedAt: now.Add(-50 * time.Second),
		},
	}
	environment.health.mu.Unlock()

	first := incident.Incident{
		ID: "00000000-0000-7000-8000-000000000030", OrganizationID: organizationID,
		ProjectID: projectID, MonitorID: monitorID, State: incident.StateOpen, Version: 1,
		OpenedTransitionID: "00000000-0000-7000-8000-000000000130",
		CreatedAt:          now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
		Timeline: []incident.TimelineEntry{{
			ID: "00000000-0000-7000-8000-000000000230", IncidentVersion: 1,
			Kind: incident.TimelineOpened, HealthTransitionID: "00000000-0000-7000-8000-000000000130",
			OldHealthState: "degraded", NewHealthState: "down", PolicyVersion: health.PolicyVersion,
			CausalRunID:           "00000000-0000-7000-8000-000000000040",
			CausalRunScheduledFor: now.Add(-time.Minute),
			Counts:                &incident.Counts{Configured: 1, Eligible: 1, Responding: 1, Failing: 1},
			OccurredAt:            now.Add(-time.Minute),
		}},
	}
	second := first
	second.ID = "00000000-0000-7000-8000-000000000029"
	second.CreatedAt, second.UpdatedAt = now.Add(-2*time.Minute), now.Add(-2*time.Minute)
	second.Timeline = nil
	resolved := second
	resolved.ID = "00000000-0000-7000-8000-000000000028"
	resolved.State, resolved.Version = incident.StateResolved, 2
	resolved.CreatedAt, resolved.UpdatedAt = now.Add(-3*time.Minute), now.Add(-time.Minute)
	resolved.ResolvedTransitionID = "00000000-0000-7000-8000-000000000128"
	resolved.ResolvedAt = now.Add(-time.Minute)
	environment.incidents.mu.Lock()
	environment.incidents.incidents = map[string]incident.Incident{
		first.ID: first, second.ID: second, resolved.ID: resolved,
	}
	environment.incidents.mu.Unlock()

	base := "/api/v1/organizations/" + organizationID + "/projects/" + projectID +
		"/monitors/" + monitorID
	healthResponse := environment.request(t, environment.client, http.MethodGet, base+"/health", "", "", "")
	var healthValue api.MonitorHealthResponse
	if err := json.Unmarshal(healthResponse.Body, &healthValue); err != nil {
		t.Fatal(err)
	}
	if healthResponse.StatusCode != http.StatusOK || healthValue.State != "down" ||
		healthValue.Counts.Failing != 1 || healthValue.LastRunID == nil {
		t.Fatalf("Monitor health = %d, %#v", healthResponse.StatusCode, healthValue)
	}

	listPath := base + "/incidents"
	pageResponse := environment.request(t, environment.client, http.MethodGet, listPath+"?pageSize=2", "", "", "")
	var firstPage api.IncidentPageResponse
	if err := json.Unmarshal(pageResponse.Body, &firstPage); err != nil {
		t.Fatal(err)
	}
	if pageResponse.StatusCode != http.StatusOK || len(firstPage.Items) != 2 ||
		firstPage.Items[0].ID != first.ID || firstPage.Items[1].ID != second.ID || firstPage.NextCursor == nil {
		t.Fatalf("first Incident page = %d, %#v", pageResponse.StatusCode, firstPage)
	}
	pageResponse = environment.request(
		t, environment.client, http.MethodGet,
		listPath+"?pageSize=2&cursor="+url.QueryEscape(*firstPage.NextCursor), "", "", "",
	)
	var nextPage api.IncidentPageResponse
	if err := json.Unmarshal(pageResponse.Body, &nextPage); err != nil {
		t.Fatal(err)
	}
	if pageResponse.StatusCode != http.StatusOK || len(nextPage.Items) != 1 ||
		nextPage.Items[0].ID != resolved.ID || nextPage.NextCursor != nil {
		t.Fatalf("second Incident page = %d, %#v", pageResponse.StatusCode, nextPage)
	}

	detailResponse := environment.request(t, environment.client, http.MethodGet, listPath+"/"+first.ID, "", "", "")
	var detail api.IncidentResponse
	if err := json.Unmarshal(detailResponse.Body, &detail); err != nil {
		t.Fatal(err)
	}
	if detailResponse.StatusCode != http.StatusOK || len(detail.Timeline) != 1 ||
		detail.Timeline[0].Counts == nil || detail.Timeline[0].Counts.Failing != 1 {
		t.Fatalf("Incident detail = %d, %#v", detailResponse.StatusCode, detail)
	}

	for _, testCase := range []struct {
		query string
		field string
		code  string
	}{
		{"?pageSize=0", "pageSize", incidentPageSizeInvalidCode},
		{"?pageSize=1&pageSize=2", "pageSize", incidentPageSizeInvalidCode},
		{"?cursor=not-a-cursor", "cursor", incidentCursorInvalidCode},
	} {
		response := environment.request(t, environment.client, http.MethodGet, listPath+testCase.query, "", "", "")
		problem := decodeProblem(t, response)
		if response.StatusCode != http.StatusBadRequest || len(problem.Errors[testCase.field]) != 1 ||
			problem.Errors[testCase.field][0].Code != testCase.code {
			t.Errorf("%s = %d, %#v", testCase.query, response.StatusCode, problem)
		}
	}

	environment.grantMembership(t, organizationID, organization.RoleViewer)
	viewerRead := environment.request(t, environment.client, http.MethodGet, listPath, "", "", "")
	if viewerRead.StatusCode != http.StatusOK {
		t.Fatalf("Viewer Incident read = %d, body %s", viewerRead.StatusCode, viewerRead.Body)
	}
	viewerWrite := environment.request(
		t, environment.client, http.MethodPost, listPath+"/"+first.ID+"/acknowledge",
		"", environment.server.URL, token,
	)
	assertProblem(t, viewerWrite, http.StatusForbidden, "Forbidden", "")

	environment.grantMembership(t, organizationID, organization.RoleAdministrator)
	missingToken := environment.request(
		t, environment.client, http.MethodPost, listPath+"/"+first.ID+"/acknowledge",
		"", environment.server.URL, "",
	)
	assertProblem(t, missingToken, http.StatusBadRequest, antiforgeryInvalidTitle, antiforgeryInvalidDetail)
	acknowledged := environment.request(
		t, environment.client, http.MethodPost, listPath+"/"+first.ID+"/acknowledge",
		"", environment.server.URL, token,
	)
	var acknowledgedValue api.IncidentResponse
	if err := json.Unmarshal(acknowledged.Body, &acknowledgedValue); err != nil {
		t.Fatal(err)
	}
	if acknowledged.StatusCode != http.StatusOK || acknowledgedValue.State != "acknowledged" ||
		acknowledgedValue.AcknowledgedBy == nil || len(acknowledgedValue.Timeline) != 2 {
		t.Fatalf("acknowledged Incident = %d, %#v", acknowledged.StatusCode, acknowledgedValue)
	}
	conflict := environment.request(
		t, environment.client, http.MethodPost, listPath+"/"+resolved.ID+"/acknowledge",
		"", environment.server.URL, token,
	)
	assertProblem(t, conflict, http.StatusConflict, "Incident acknowledgement rejected", "A resolved Incident cannot be acknowledged.")

	wrongProject := environment.request(
		t, environment.client, http.MethodGet,
		"/api/v1/organizations/"+organizationID+
			"/projects/00000000-0000-7000-8000-000000000099/monitors/"+monitorID+"/incidents/"+first.ID,
		"", "", "",
	)
	assertProblem(t, wrongProject, http.StatusNotFound, "Not Found", "")
	environment.organizations.setMembership(
		organization.ID(organizationID), environment.administratorID(t), organization.RoleAdministrator, false,
	)
	hidden := environment.request(t, environment.client, http.MethodGet, listPath, "", "", "")
	assertProblem(t, hidden, http.StatusNotFound, "Not Found", "")
}

func TestIncidentCursorRoundTripsAndRejectsUnknownShape(t *testing.T) {
	want := incident.Cursor{
		CreatedAt: time.Date(2026, time.July, 28, 12, 30, 0, 123, time.UTC),
		ID:        "00000000-0000-7000-8000-000000000030",
	}
	encoded, err := encodeIncidentCursor(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeIncidentCursor(encoded)
	if err != nil || got.ID != want.ID || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("Incident cursor = %#v, %v", got, err)
	}
	unknown := "eyJ2ZXJzaW9uIjoxLCJjcmVhdGVkQXQiOiIyMDI2LTA3LTI4VDEyOjMwOjAwWiIsImlkIjoiMDAwMDAwMDAtMDAwMC03MDAwLTgwMDAtMDAwMDAwMDAwMDMwIiwiZXh0cmEiOnRydWV9"
	if _, err := decodeIncidentCursor(unknown); err == nil {
		t.Fatal("Incident cursor with an unknown member was accepted")
	}
}

func TestOrganizationIncidentInboxIsScopedAndKeepsEvidenceSeparate(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	environment.bootstrapAdministrator(t)
	organizationID, projectID, monitorID := seedRunQueryMonitor(t, environment)
	now := environment.clock.Now()
	environment.health.mu.Lock()
	environment.health.snapshots = map[string]health.Snapshot{
		organizationID + "/" + projectID + "/" + monitorID: {
			OrganizationID: organizationID, ProjectID: projectID, MonitorID: monitorID,
			State: health.StateDown, StableState: health.StateDown,
			PolicyVersion: health.PolicyVersion, UpdatedAt: now.Add(-time.Minute),
			TransitionedAt: now.Add(-time.Minute),
		},
	}
	environment.health.mu.Unlock()
	window, err := maintenance.NewWindow(
		maintenance.ID("00000000-0000-7000-8000-000000000060"),
		maintenance.Scope{OrganizationID: organizationID, ProjectID: projectID, MonitorID: monitorID},
		now.Add(-time.Minute), now.Add(time.Hour), now.Add(-2*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := environment.maintenance.CreateWindow(t.Context(), window); err != nil {
		t.Fatal(err)
	}
	openingRunID := "00000000-0000-7000-8000-000000000040"
	first := incident.Incident{
		ID: "00000000-0000-7000-8000-000000000030", OrganizationID: organizationID,
		ProjectID: projectID, MonitorID: monitorID, State: incident.StateOpen, Version: 1,
		OpenedTransitionID: "00000000-0000-7000-8000-000000000130",
		CreatedAt:          now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
		Timeline: []incident.TimelineEntry{{
			ID: "00000000-0000-7000-8000-000000000230", IncidentVersion: 1,
			Kind: incident.TimelineOpened, CausalRunID: openingRunID,
			CausalRunScheduledFor: now.Add(-2 * time.Minute), OccurredAt: now.Add(-time.Minute),
		}},
	}
	second := first
	second.ID, second.State = "00000000-0000-7000-8000-000000000029", incident.StateAcknowledged
	second.CreatedAt, second.UpdatedAt = now.Add(-2*time.Minute), now.Add(-time.Minute)
	second.Timeline = nil
	resolved := first
	resolved.ID, resolved.State = "00000000-0000-7000-8000-000000000028", incident.StateResolved
	resolved.CreatedAt, resolved.UpdatedAt = now.Add(-3*time.Minute), now.Add(-time.Minute)
	resolved.Timeline = nil
	environment.incidents.mu.Lock()
	environment.incidents.incidents = map[string]incident.Incident{
		first.ID: first, second.ID: second, resolved.ID: resolved,
	}
	environment.incidents.mu.Unlock()

	path := "/api/v1/organizations/" + organizationID + "/incidents?state=active&pageSize=1"
	response := environment.request(t, environment.client, http.MethodGet, path, "", "", "")
	var page api.IncidentInboxPageResponse
	if err := json.Unmarshal(response.Body, &page); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || len(page.Items) != 1 ||
		page.Items[0].Incident.ID != first.ID || page.NextCursor == nil {
		t.Fatalf("first inbox page = %d, %#v", response.StatusCode, page)
	}
	item := page.Items[0]
	if item.Monitor.Name != "Checkout" || item.Monitor.State != "active" ||
		item.Health == nil || item.Health.State != "down" ||
		item.Maintenance == nil || item.Maintenance.State != "active" ||
		item.OpeningRun == nil || item.OpeningRun.ID != openingRunID || item.OpeningRun.Available {
		t.Fatalf("inbox operational facts = %#v", item)
	}
	next := environment.request(
		t, environment.client, http.MethodGet,
		"/api/v1/organizations/"+organizationID+"/incidents?state=active&pageSize=1&cursor="+url.QueryEscape(*page.NextCursor),
		"", "", "",
	)
	var nextPage api.IncidentInboxPageResponse
	if err := json.Unmarshal(next.Body, &nextPage); err != nil {
		t.Fatal(err)
	}
	if next.StatusCode != http.StatusOK || len(nextPage.Items) != 1 ||
		nextPage.Items[0].Incident.ID != second.ID {
		t.Fatalf("second inbox page = %d, %#v", next.StatusCode, nextPage)
	}
	resolvedResponse := environment.request(
		t, environment.client, http.MethodGet,
		"/api/v1/organizations/"+organizationID+"/incidents?state=resolved", "", "", "",
	)
	var resolvedPage api.IncidentInboxPageResponse
	if err := json.Unmarshal(resolvedResponse.Body, &resolvedPage); err != nil {
		t.Fatal(err)
	}
	if resolvedResponse.StatusCode != http.StatusOK || len(resolvedPage.Items) != 1 ||
		resolvedPage.Items[0].Incident.ID != resolved.ID {
		t.Fatalf("resolved inbox page = %d, %#v", resolvedResponse.StatusCode, resolvedPage)
	}
	invalid := environment.request(
		t, environment.client, http.MethodGet,
		"/api/v1/organizations/"+organizationID+"/incidents?state=closed", "", "", "",
	)
	problem := decodeProblem(t, invalid)
	if invalid.StatusCode != http.StatusBadRequest ||
		problem.Errors["state"][0].Code != incidentInboxStateInvalidCode {
		t.Fatalf("invalid inbox state = %d, %#v", invalid.StatusCode, problem)
	}
	hidden := environment.request(
		t, environment.client, http.MethodGet,
		"/api/v1/organizations/00000000-0000-7000-8000-000000000099/incidents", "", "", "",
	)
	assertProblem(t, hidden, http.StatusNotFound, "Not Found", "")
}
