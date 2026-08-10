package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/maintenance"
	"github.com/probehive/probehive/internal/organization"
)

func TestMaintenanceAPIValidatesAuthorizesListsAndCancels(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	antiforgery := environment.bootstrapAdministrator(t)
	organizationID, projectID, monitorID := seedRunQueryMonitor(t, environment)
	collection := monitorPath(organizationID, projectID, monitorID) + "/maintenance-windows"
	now := environment.clock.Now()
	startsAt := now.Add(time.Hour)
	endsAt := now.Add(2 * time.Hour)

	anonymous := environment.request(
		t, &http.Client{}, http.MethodGet, collection, "", "", "",
	)
	if anonymous.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous list status = %d, want 401", anonymous.StatusCode)
	}

	invalid := environment.request(
		t, environment.client, http.MethodPost, collection, `{}`,
		environment.server.URL, antiforgery,
	)
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid create status = %d, body %s", invalid.StatusCode, invalid.Body)
	}
	invalidProblem := decodeProblem(t, invalid)
	if invalidProblem.Errors["startsAt"][0].Code != maintenance.StartsAtInvalidCode ||
		invalidProblem.Errors["endsAt"][0].Code != maintenance.EndsAtInvalidCode {
		t.Fatalf("invalid create problem = %#v", invalidProblem)
	}

	body := fmt.Sprintf(
		`{"startsAt":%q,"endsAt":%q}`,
		startsAt.Format(time.RFC3339Nano),
		endsAt.Format(time.RFC3339Nano),
	)
	noAntiforgery := environment.request(
		t, environment.client, http.MethodPost, collection, body,
		environment.server.URL, "",
	)
	if noAntiforgery.StatusCode != http.StatusBadRequest {
		t.Fatalf("unprotected create status = %d, want 400", noAntiforgery.StatusCode)
	}

	wrongOrigin := environment.request(
		t, environment.client, http.MethodPost, collection, body,
		"https://other.example.test", antiforgery,
	)
	if wrongOrigin.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong-origin create status = %d, want 403", wrongOrigin.StatusCode)
	}

	createdResponse := environment.request(
		t, environment.client, http.MethodPost, collection, body,
		environment.server.URL, antiforgery,
	)
	if createdResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", createdResponse.StatusCode, createdResponse.Body)
	}
	var created api.MaintenanceWindowResponse
	if err := json.Unmarshal(createdResponse.Body, &created); err != nil {
		t.Fatal(err)
	}
	if created.OrganizationID != organizationID || created.ProjectID != projectID ||
		created.MonitorID != monitorID || created.Status != string(maintenance.StatusUpcoming) ||
		created.CancelledAt != nil || !created.StartsAt.Equal(startsAt) || !created.EndsAt.Equal(endsAt) {
		t.Fatalf("created response = %#v", created)
	}
	item := collection + "/" + created.ID
	if location := createdResponse.Header.Get("Location"); location != item {
		t.Fatalf("Location = %q, want %q", location, item)
	}

	overlap := environment.request(
		t, environment.client, http.MethodPost, collection, body,
		environment.server.URL, antiforgery,
	)
	if overlap.StatusCode != http.StatusConflict {
		t.Fatalf("overlap status = %d, body %s", overlap.StatusCode, overlap.Body)
	}
	if problem := decodeProblem(t, overlap); problem.Code != maintenance.OverlapCode {
		t.Fatalf("overlap code = %q", problem.Code)
	}

	environment.grantMembership(t, organizationID, organization.RoleViewer)
	listedResponse := environment.request(
		t, environment.client, http.MethodGet, collection, "", "", "",
	)
	if listedResponse.StatusCode != http.StatusOK {
		t.Fatalf("Viewer list status = %d, body %s", listedResponse.StatusCode, listedResponse.Body)
	}
	var listed []api.MaintenanceWindowResponse
	if err := json.Unmarshal(listedResponse.Body, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("Viewer list = %#v", listed)
	}

	viewerCancel := environment.request(
		t, environment.client, http.MethodPost, item+"/cancel", `{}`,
		environment.server.URL, antiforgery,
	)
	if viewerCancel.StatusCode != http.StatusForbidden {
		t.Fatalf("Viewer cancel status = %d, want 403", viewerCancel.StatusCode)
	}

	wrongScope := monitorPath(
		organizationID, "00000000-0000-7000-8000-000000000099", monitorID,
	) + "/maintenance-windows/" + created.ID
	if response := environment.request(
		t, environment.client, http.MethodGet, wrongScope, "", "", "",
	); response.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong-scope item status = %d, want 404", response.StatusCode)
	}

	environment.grantMembership(t, organizationID, organization.RoleAdministrator)
	cancelResponse := environment.request(
		t, environment.client, http.MethodPost, item+"/cancel", `{}`,
		environment.server.URL, antiforgery,
	)
	if cancelResponse.StatusCode != http.StatusOK {
		t.Fatalf("cancel status = %d, body %s", cancelResponse.StatusCode, cancelResponse.Body)
	}
	var cancelled api.MaintenanceWindowResponse
	if err := json.Unmarshal(cancelResponse.Body, &cancelled); err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != string(maintenance.StatusCancelled) || cancelled.CancelledAt == nil {
		t.Fatalf("cancelled response = %#v", cancelled)
	}

	repeated := environment.request(
		t, environment.client, http.MethodPost, item+"/cancel", `{}`,
		environment.server.URL, antiforgery,
	)
	if repeated.StatusCode != http.StatusOK {
		t.Fatalf("repeated cancel status = %d, body %s", repeated.StatusCode, repeated.Body)
	}
	var repeatedWindow api.MaintenanceWindowResponse
	if err := json.Unmarshal(repeated.Body, &repeatedWindow); err != nil {
		t.Fatal(err)
	}
	if repeatedWindow.CancelledAt == nil || !repeatedWindow.CancelledAt.Equal(*cancelled.CancelledAt) {
		t.Fatalf("repeated cancellation changed response: %#v then %#v", cancelled, repeatedWindow)
	}

	replacement := environment.request(
		t, environment.client, http.MethodPost, collection, body,
		environment.server.URL, antiforgery,
	)
	if replacement.StatusCode != http.StatusCreated {
		t.Fatalf("replacement create status = %d, body %s", replacement.StatusCode, replacement.Body)
	}
	var replacementWindow api.MaintenanceWindowResponse
	if err := json.Unmarshal(replacement.Body, &replacementWindow); err != nil {
		t.Fatal(err)
	}
	environment.clock.Advance(2 * time.Hour)
	endedCancellation := environment.request(
		t, environment.client, http.MethodPost,
		collection+"/"+replacementWindow.ID+"/cancel", `{}`,
		environment.server.URL, antiforgery,
	)
	if endedCancellation.StatusCode != http.StatusConflict {
		t.Fatalf("ended cancel status = %d, body %s", endedCancellation.StatusCode, endedCancellation.Body)
	}
	if problem := decodeProblem(t, endedCancellation); problem.Code != maintenance.WindowEndedCode {
		t.Fatalf("ended cancel code = %q", problem.Code)
	}

	afterEnd := environment.request(
		t, environment.client, http.MethodGet, collection, "", "", "",
	)
	if afterEnd.StatusCode != http.StatusOK {
		t.Fatalf("list after end status = %d, body %s", afterEnd.StatusCode, afterEnd.Body)
	}
	listed = nil
	if err := json.Unmarshal(afterEnd.Body, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("list after end = %#v, want empty", listed)
	}
}
