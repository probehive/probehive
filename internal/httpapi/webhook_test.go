package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/webhook"
)

func TestWebhookIntegrationAPIProtectsAndEncryptsConfiguration(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	token := environment.bootstrapAdministrator(t)

	const organizationID = "00000000-0000-7000-8000-000000000002"
	path := "/api/v1/organizations/" + organizationID + "/webhook-integrations"

	anonymous := environment.request(t, &http.Client{}, http.MethodGet, path, "", "", "")
	assertProblem(t, anonymous, http.StatusUnauthorized, "Unauthorized", "")

	environment.grantMembership(t, organizationID, organization.RoleViewer)
	viewer := environment.request(t, environment.client, http.MethodGet, path, "", "", "")
	assertProblem(t, viewer, http.StatusForbidden, "Forbidden", "")
	environment.grantMembership(t, organizationID, organization.RoleAdministrator)

	invalid := environment.request(
		t, environment.client, http.MethodPost, path,
		`{"name":"","destinationUrl":"https://hooks.example.test/events?secret=value"}`,
		environment.server.URL, token,
	)
	problem := decodeProblem(t, invalid)
	if invalid.StatusCode != http.StatusBadRequest ||
		problem.Errors["name"][0].Code != webhook.NameInvalidCode ||
		problem.Errors["destinationUrl"][0].Code != webhook.DestinationInvalidCode {
		t.Fatalf("invalid Webhook problem = %#v", problem)
	}

	created := environment.request(
		t, environment.client, http.MethodPost, path,
		`{"name":"Primary receiver","destinationUrl":"https://hooks.example.test/events"}`,
		environment.server.URL, token,
	)
	if created.StatusCode != http.StatusCreated || created.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("create status = %d, body %s", created.StatusCode, created.Body)
	}
	var response api.CreateWebhookIntegrationResponse
	if err := json.Unmarshal(created.Body, &response); err != nil {
		t.Fatal(err)
	}
	if response.Integration.OrganizationID != organizationID ||
		response.Integration.Name != "Primary receiver" ||
		response.Integration.DestinationURL != "https://hooks.example.test/events" ||
		response.Integration.Enabled || response.Integration.Version != 1 ||
		response.Integration.ActiveSecretVersion != 1 ||
		!strings.HasPrefix(response.SigningSecret, "phwh_") {
		t.Fatalf("create response = %#v", response)
	}
	stored := environment.webhooks.onlySecret(t)
	if stored.Envelope.KeyID != "test" ||
		bytes.Contains(stored.Envelope.Ciphertext, []byte(response.SigningSecret)) {
		t.Fatalf("stored envelope exposes the one-time secret: %#v", stored.Envelope)
	}

	listed := environment.request(t, environment.client, http.MethodGet, path, "", "", "")
	if listed.StatusCode != http.StatusOK || bytes.Contains(listed.Body, []byte("signingSecret")) ||
		bytes.Contains(listed.Body, []byte(response.SigningSecret)) {
		t.Fatalf("list status/body = %d/%s", listed.StatusCode, listed.Body)
	}
	var integrations []api.WebhookIntegrationResponse
	if err := json.Unmarshal(listed.Body, &integrations); err != nil {
		t.Fatal(err)
	}
	if len(integrations) != 1 || integrations[0] != response.Integration {
		t.Fatalf("listed Integrations = %#v", integrations)
	}

	conflict := environment.request(
		t, environment.client, http.MethodPost, path,
		`{"name":"Primary receiver","destinationUrl":"https://other.example.test/events"}`,
		environment.server.URL, token,
	)
	conflictProblem := decodeProblem(t, conflict)
	if conflict.StatusCode != http.StatusConflict ||
		conflictProblem.Code != webhook.NameConflictCode {
		t.Fatalf("conflict problem = %#v", conflictProblem)
	}
}

func TestWebhookIntegrationCreateRequiresOperatorKeyring(t *testing.T) {
	environment := newTestEnvironment(t, true, 0, func(config *Config) {
		config.Webhooks = webhook.NewService(
			newMemoryWebhookStore(),
			&testClock{value: time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)},
			&testUUIDGenerator{}, &testRandomReader{}, nil,
		)
	})
	token := environment.bootstrapAdministrator(t)
	const organizationID = "00000000-0000-7000-8000-000000000002"
	response := environment.request(
		t, environment.client, http.MethodPost,
		"/api/v1/organizations/"+organizationID+"/webhook-integrations",
		`{"name":"Receiver","destinationUrl":"https://hooks.example.test/events"}`,
		environment.server.URL, token,
	)
	problem := decodeProblem(t, response)
	if response.StatusCode != http.StatusServiceUnavailable ||
		problem.Code != webhook.KeyringUnavailableCode {
		t.Fatalf("unavailable problem = %#v", problem)
	}
}
