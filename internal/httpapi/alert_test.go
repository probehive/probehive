package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/probehive/probehive/internal/alert"
	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
)

func TestAlertAPIEnforcesScopePermissionAndKeysetPagination(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	environment.bootstrapAdministrator(t)
	organizationID, projectID, monitorID := seedRunQueryMonitor(t, environment)
	now := environment.clock.Now()
	environment.alerts.mu.Lock()
	environment.alerts.alerts = []alert.Alert{
		{
			ID:             "00000000-0000-7000-8000-000000000033",
			OrganizationID: organizationID, ProjectID: projectID, MonitorID: monitorID,
			IncidentID:      "00000000-0000-7000-8000-000000000133",
			IncidentVersion: 3, Kind: alert.KindIncidentResolved,
			OccurredAt: now.Add(-time.Minute), CreatedAt: now,
		},
		{
			ID:             "00000000-0000-7000-8000-000000000032",
			OrganizationID: organizationID, ProjectID: projectID, MonitorID: monitorID,
			IncidentID:      "00000000-0000-7000-8000-000000000132",
			IncidentVersion: 1, Kind: alert.KindIncidentOpened,
			OccurredAt: now.Add(-2 * time.Minute), CreatedAt: now,
		},
	}
	environment.alerts.mu.Unlock()

	path := "/api/v1/organizations/" + organizationID + "/projects/" + projectID +
		"/monitors/" + monitorID + "/alerts"
	firstResponse := environment.request(
		t, environment.client, http.MethodGet, path+"?pageSize=1", "", "", "",
	)
	var first api.AlertPageResponse
	if err := json.Unmarshal(firstResponse.Body, &first); err != nil {
		t.Fatal(err)
	}
	if firstResponse.StatusCode != http.StatusOK || len(first.Items) != 1 ||
		first.Items[0].Kind != string(alert.KindIncidentResolved) || first.NextCursor == nil {
		t.Fatalf("first Alert page = %d, %#v", firstResponse.StatusCode, first)
	}
	secondResponse := environment.request(
		t, environment.client, http.MethodGet,
		path+"?pageSize=1&cursor="+url.QueryEscape(*first.NextCursor), "", "", "",
	)
	var second api.AlertPageResponse
	if err := json.Unmarshal(secondResponse.Body, &second); err != nil {
		t.Fatal(err)
	}
	if secondResponse.StatusCode != http.StatusOK || len(second.Items) != 1 ||
		second.Items[0].Kind != string(alert.KindIncidentOpened) || second.NextCursor != nil {
		t.Fatalf("second Alert page = %d, %#v", secondResponse.StatusCode, second)
	}

	for _, query := range []string{"?pageSize=0", "?pageSize=1&pageSize=2", "?cursor=invalid"} {
		response := environment.request(t, environment.client, http.MethodGet, path+query, "", "", "")
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("%s status = %d, body %s", query, response.StatusCode, response.Body)
		}
	}

	environment.grantMembership(t, organizationID, organization.RoleViewer)
	viewer := environment.request(t, environment.client, http.MethodGet, path, "", "", "")
	if viewer.StatusCode != http.StatusOK {
		t.Fatalf("Viewer Alert read = %d, body %s", viewer.StatusCode, viewer.Body)
	}
	wrongProject := environment.request(
		t, environment.client, http.MethodGet,
		"/api/v1/organizations/"+organizationID+
			"/projects/00000000-0000-7000-8000-000000000099/monitors/"+monitorID+"/alerts",
		"", "", "",
	)
	assertProblem(t, wrongProject, http.StatusNotFound, "Not Found", "")
	environment.organizations.setMembership(
		organization.ID(organizationID), environment.administratorID(t),
		organization.RoleAdministrator, false,
	)
	hidden := environment.request(t, environment.client, http.MethodGet, path, "", "", "")
	assertProblem(t, hidden, http.StatusNotFound, "Not Found", "")
}

func TestAlertCursorRoundTripsAndRejectsUnknownShape(t *testing.T) {
	want := alert.Cursor{
		OccurredAt: time.Date(2026, time.July, 31, 12, 30, 0, 123, time.UTC),
		ID:         "00000000-0000-7000-8000-000000000033",
	}
	encoded, err := encodeAlertCursor(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeAlertCursor(encoded)
	if err != nil || got.ID != want.ID || !got.OccurredAt.Equal(want.OccurredAt) {
		t.Fatalf("Alert cursor = %#v, %v", got, err)
	}
	unknown := "eyJ2ZXJzaW9uIjoxLCJvY2N1cnJlZEF0IjoiMjAyNi0wNy0zMVQxMjozMDowMFoiLCJpZCI6IjAwMDAwMDAwLTAwMDAtNzAwMC04MDAwLTAwMDAwMDAwMDAzMyIsImV4dHJhIjp0cnVlfQ"
	if _, err := decodeAlertCursor(unknown); err == nil {
		t.Fatal("Alert cursor with an unknown member was accepted")
	}
}
