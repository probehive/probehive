package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/webhook"
)

func TestWebhookSigningSecretRotationAPIEnforcesOrderAndOneTimeDisclosure(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	token := environment.bootstrapAdministrator(t)
	const organizationID = "00000000-0000-7000-8000-000000000002"
	environment.grantMembership(t, organizationID, organization.RoleAdministrator)

	root := "/api/v1/organizations/" + organizationID + "/webhook-integrations"
	created := environment.request(
		t, environment.client, http.MethodPost, root,
		`{"name":"Rotating receiver","destinationUrl":"https://hooks.example.test/events"}`,
		environment.server.URL, token,
	)
	var createdResponse api.CreateWebhookIntegrationResponse
	if err := json.Unmarshal(created.Body, &createdResponse); err != nil {
		t.Fatal(err)
	}
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status/body = %d/%s", created.StatusCode, created.Body)
	}
	rotationRoot := root + "/" + createdResponse.Integration.ID + "/signing-secrets"

	environment.grantMembership(t, organizationID, organization.RoleViewer)
	forbidden := environment.request(
		t, environment.client, http.MethodPost, rotationRoot+"/prepare",
		`{"version":1}`, environment.server.URL, token,
	)
	assertProblem(t, forbidden, http.StatusForbidden, "Forbidden", "")
	environment.grantMembership(t, organizationID, organization.RoleAdministrator)

	invalid := environment.request(
		t, environment.client, http.MethodPost, rotationRoot+"/prepare",
		`{"version":0}`, environment.server.URL, token,
	)
	invalidProblem := decodeProblem(t, invalid)
	if invalid.StatusCode != http.StatusBadRequest ||
		invalidProblem.Errors["version"][0].Code != webhook.VersionInvalidCode {
		t.Fatalf("invalid rotation problem = %#v", invalidProblem)
	}

	prepared := environment.request(
		t, environment.client, http.MethodPost, rotationRoot+"/prepare",
		`{"version":1}`, environment.server.URL, token,
	)
	if prepared.StatusCode != http.StatusCreated ||
		prepared.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("prepare status/body = %d/%s", prepared.StatusCode, prepared.Body)
	}
	var preparedResponse api.PrepareWebhookSigningSecretResponse
	if err := json.Unmarshal(prepared.Body, &preparedResponse); err != nil {
		t.Fatal(err)
	}
	if preparedResponse.Integration.Version != 2 ||
		preparedResponse.Integration.ActiveSecretVersion != 1 ||
		preparedResponse.SecretVersion != 2 ||
		!strings.HasPrefix(preparedResponse.SigningSecret, "phwh_") ||
		preparedResponse.SigningSecret == createdResponse.SigningSecret {
		t.Fatalf("prepare response = %#v", preparedResponse)
	}
	environment.webhooks.mu.Lock()
	pending := cloneStoredSecret(environment.webhooks.secrets[1])
	environment.webhooks.mu.Unlock()
	if pending.State != "pending" ||
		bytes.Contains(pending.Envelope.Ciphertext, []byte(preparedResponse.SigningSecret)) {
		t.Fatalf("pending secret exposes plaintext: %#v", pending)
	}

	listed := environment.request(t, environment.client, http.MethodGet, root, "", "", "")
	if bytes.Contains(listed.Body, []byte(preparedResponse.SigningSecret)) ||
		bytes.Contains(listed.Body, []byte("signingSecret")) {
		t.Fatalf("list exposes prepared secret: %s", listed.Body)
	}

	stale := environment.request(
		t, environment.client, http.MethodPost, rotationRoot+"/activate",
		`{"version":1}`, environment.server.URL, token,
	)
	if problem := decodeProblem(t, stale); stale.StatusCode != http.StatusConflict ||
		problem.Code != webhook.ConcurrentUpdateCode {
		t.Fatalf("stale activation = %d/%#v", stale.StatusCode, problem)
	}
	activated := environment.request(
		t, environment.client, http.MethodPost, rotationRoot+"/activate",
		`{"version":2}`, environment.server.URL, token,
	)
	var activatedResponse api.WebhookIntegrationResponse
	if err := json.Unmarshal(activated.Body, &activatedResponse); err != nil {
		t.Fatal(err)
	}
	if activated.StatusCode != http.StatusOK ||
		activatedResponse.Version != 3 || activatedResponse.ActiveSecretVersion != 2 {
		t.Fatalf("activation status/response = %d/%#v", activated.StatusCode, activatedResponse)
	}

	inProgress := environment.request(
		t, environment.client, http.MethodPost, rotationRoot+"/prepare",
		`{"version":3}`, environment.server.URL, token,
	)
	if problem := decodeProblem(t, inProgress); inProgress.StatusCode != http.StatusConflict ||
		problem.Code != webhook.RotationInProgressCode {
		t.Fatalf("in-progress prepare = %d/%#v", inProgress.StatusCode, problem)
	}

	retired := environment.request(
		t, environment.client, http.MethodPost, rotationRoot+"/retire",
		`{"version":3}`, environment.server.URL, token,
	)
	var retiredResponse api.WebhookIntegrationResponse
	if err := json.Unmarshal(retired.Body, &retiredResponse); err != nil {
		t.Fatal(err)
	}
	if retired.StatusCode != http.StatusOK ||
		retiredResponse.Version != 4 || retiredResponse.ActiveSecretVersion != 2 {
		t.Fatalf("retirement status/response = %d/%#v", retired.StatusCode, retiredResponse)
	}
	environment.webhooks.mu.Lock()
	oldSecret := cloneStoredSecret(environment.webhooks.secrets[0])
	environment.webhooks.mu.Unlock()
	if oldSecret.State != "retired" ||
		len(oldSecret.Envelope.Nonce) != 0 || len(oldSecret.Envelope.Ciphertext) != 0 {
		t.Fatalf("retired secret retains material: %#v", oldSecret)
	}

	unprepared := environment.request(
		t, environment.client, http.MethodPost, rotationRoot+"/activate",
		`{"version":4}`, environment.server.URL, token,
	)
	if problem := decodeProblem(t, unprepared); unprepared.StatusCode != http.StatusConflict ||
		problem.Code != webhook.PendingSecretMissingCode {
		t.Fatalf("unprepared activation = %d/%#v", unprepared.StatusCode, problem)
	}

	missing := environment.request(
		t, environment.client, http.MethodPost,
		root+"/00000000-0000-7000-8000-000000000099/signing-secrets/prepare",
		`{"version":1}`, environment.server.URL, token,
	)
	assertProblem(t, missing, http.StatusNotFound, "Not Found", "")
}
