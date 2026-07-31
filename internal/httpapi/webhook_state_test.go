package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/webhook"
)

func TestWebhookIntegrationStateAPIUsesAntiforgeryAuthorizationAndVersion(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	token := environment.bootstrapAdministrator(t)
	const organizationID = "00000000-0000-7000-8000-000000000002"
	environment.grantMembership(t, organizationID, organization.RoleAdministrator)
	root := "/api/v1/organizations/" + organizationID + "/webhook-integrations"

	created := environment.request(
		t, environment.client, http.MethodPost, root,
		`{"name":"State receiver","destinationUrl":"https://hooks.example.test/events"}`,
		environment.server.URL, token,
	)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status/body = %d/%s", created.StatusCode, created.Body)
	}
	var createdResponse api.CreateWebhookIntegrationResponse
	if err := json.Unmarshal(created.Body, &createdResponse); err != nil {
		t.Fatal(err)
	}
	statePath := root + "/" + createdResponse.Integration.ID + "/state"

	environment.grantMembership(t, organizationID, organization.RoleViewer)
	forbidden := environment.request(
		t, environment.client, http.MethodPut, statePath,
		`{"enabled":true,"version":1}`, environment.server.URL, token,
	)
	assertProblem(t, forbidden, http.StatusForbidden, "Forbidden", "")
	environment.grantMembership(t, organizationID, organization.RoleAdministrator)

	invalid := environment.request(
		t, environment.client, http.MethodPut, statePath,
		`{"version":0}`, environment.server.URL, token,
	)
	invalidProblem := decodeProblem(t, invalid)
	if invalid.StatusCode != http.StatusBadRequest ||
		invalidProblem.Errors["enabled"][0].Code != webhook.EnabledInvalidCode ||
		invalidProblem.Errors["version"][0].Code != webhook.VersionInvalidCode {
		t.Fatalf("invalid state problem = %#v", invalidProblem)
	}

	enabled := environment.request(
		t, environment.client, http.MethodPut, statePath,
		`{"enabled":true,"version":1}`, environment.server.URL, token,
	)
	var enabledResponse api.WebhookIntegrationResponse
	if err := json.Unmarshal(enabled.Body, &enabledResponse); err != nil {
		t.Fatal(err)
	}
	if enabled.StatusCode != http.StatusOK || !enabledResponse.Enabled ||
		enabledResponse.Version != 2 {
		t.Fatalf("enable status/response = %d/%#v", enabled.StatusCode, enabledResponse)
	}

	idempotent := environment.request(
		t, environment.client, http.MethodPut, statePath,
		`{"enabled":true,"version":2}`, environment.server.URL, token,
	)
	var idempotentResponse api.WebhookIntegrationResponse
	if err := json.Unmarshal(idempotent.Body, &idempotentResponse); err != nil {
		t.Fatal(err)
	}
	if idempotent.StatusCode != http.StatusOK || !idempotentResponse.Enabled ||
		idempotentResponse.Version != 2 {
		t.Fatalf("idempotent state/response = %d/%#v", idempotent.StatusCode, idempotentResponse)
	}

	stale := environment.request(
		t, environment.client, http.MethodPut, statePath,
		`{"enabled":false,"version":1}`, environment.server.URL, token,
	)
	staleProblem := decodeProblem(t, stale)
	if stale.StatusCode != http.StatusConflict ||
		staleProblem.Code != webhook.ConcurrentUpdateCode {
		t.Fatalf("stale state = %d/%#v", stale.StatusCode, staleProblem)
	}

	disabled := environment.request(
		t, environment.client, http.MethodPut, statePath,
		`{"enabled":false,"version":2}`, environment.server.URL, token,
	)
	var disabledResponse api.WebhookIntegrationResponse
	if err := json.Unmarshal(disabled.Body, &disabledResponse); err != nil {
		t.Fatal(err)
	}
	if disabled.StatusCode != http.StatusOK || disabledResponse.Enabled ||
		disabledResponse.Version != 3 {
		t.Fatalf("disable status/response = %d/%#v", disabled.StatusCode, disabledResponse)
	}
}
