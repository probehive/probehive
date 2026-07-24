package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/probehive/probehive/internal/user"
)

func TestSetupStatusTransitionAndDuplicateSetupConflict(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	before := environment.request(t, environment.client, http.MethodGet, "/api/v1/setup/status", "", "", "")
	assertExactJSON(t, before, http.StatusOK, "{\"setupComplete\":false}\n")

	token, _ := environment.getAntiforgery(t)
	setupBody := `{"email":"admin@example.test","displayName":"Admin","password":"password-123"}`
	created := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/setup/admin",
		setupBody, environment.server.URL, token.RequestToken,
	)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, body %s", created.StatusCode, created.Body)
	}

	after := environment.request(t, environment.client, http.MethodGet, "/api/v1/setup/status", "", "", "")
	assertExactJSON(t, after, http.StatusOK, "{\"setupComplete\":true}\n")

	refreshed, _ := environment.getAntiforgery(t)
	duplicate := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/setup/admin",
		setupBody, environment.server.URL, refreshed.RequestToken,
	)
	assertProblem(
		t, duplicate, http.StatusConflict,
		user.SetupAlreadyCompletedTitle, user.SetupAlreadyCompletedDetail,
	)
}

func TestSessionExpiresAtFixedBoundary(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	environment.bootstrapAdministrator(t)

	environment.clock.Advance(user.SessionLifetime - time.Nanosecond)
	beforeExpiry := environment.request(
		t, environment.client, http.MethodGet, "/api/v1/auth/session", "", "", "",
	)
	if beforeExpiry.StatusCode != http.StatusOK {
		t.Fatalf("session before expiry = %d, body %s", beforeExpiry.StatusCode, beforeExpiry.Body)
	}

	environment.clock.Advance(time.Nanosecond)
	expired := environment.request(
		t, environment.client, http.MethodGet, "/api/v1/auth/session", "", "", "",
	)
	assertProblem(t, expired, http.StatusUnauthorized, "Unauthorized", "")

	environment.sessions.mu.Lock()
	defer environment.sessions.mu.Unlock()
	if len(environment.sessions.entries) != 0 {
		t.Fatalf("expired session retained %d records", len(environment.sessions.entries))
	}
}

func TestAuthenticatedNonAdministratorIsForbidden(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	environment.bootstrapAdministrator(t)

	environment.users.mu.Lock()
	mutated := false
	for id, account := range environment.users.byID {
		account.Role = "Viewer"
		environment.users.byID[id] = account
		mutated = true
	}
	environment.users.mu.Unlock()
	if !mutated {
		t.Fatal("setup created no authenticated user")
	}

	authenticated := environment.request(
		t, environment.client, http.MethodGet, "/api/v1/auth/session", "", "", "",
	)
	if authenticated.StatusCode != http.StatusOK {
		t.Fatalf("non-administrator session = %d, body %s", authenticated.StatusCode, authenticated.Body)
	}
	forbidden := environment.request(
		t, environment.client, http.MethodGet,
		"/api/v1/organizations/00000000-0000-7000-8000-000000000099", "", "", "",
	)
	assertProblem(t, forbidden, http.StatusForbidden, "Forbidden", "")
}

func TestOriginNullIsRejectedAndMissingOriginSucceeds(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	token, _ := environment.getAntiforgery(t)
	setupBody := `{"email":"admin@example.test","displayName":"Admin","password":"password-123"}`

	rejected := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/setup/admin",
		setupBody, "null", token.RequestToken,
	)
	assertProblem(t, rejected, http.StatusForbidden, originRejectedTitle, originRejectedDetail)

	allowed := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/setup/admin",
		setupBody, "", token.RequestToken,
	)
	if allowed.StatusCode != http.StatusCreated {
		t.Fatalf("missing-Origin setup = %d, body %s", allowed.StatusCode, allowed.Body)
	}
}

func TestOrganizationReplayAndGetExactWireShape(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	token := environment.bootstrapAdministrator(t)
	const organizationID = "00000000-0000-7000-8000-000000000002"
	const projectID = "00000000-0000-7000-8000-000000000003"
	const organizationBody = `{"slug":"acme","displayName":"Acme"}`
	wantBody := `{"id":"` + organizationID + `","slug":"acme","displayName":"Acme","createdAt":"2026-07-24T00:00:00Z","defaultProject":{"id":"` +
		projectID + `","organizationId":"` + organizationID + `","name":"Default","isDefault":true,"createdAt":"2026-07-24T00:00:00Z"}}` + "\n"

	created := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/organizations",
		organizationBody, environment.server.URL, token,
	)
	assertExactJSON(t, created, http.StatusCreated, wantBody)
	if location := created.Header.Get("Location"); location != "/api/v1/organizations/"+organizationID {
		t.Fatalf("created Location = %q", location)
	}

	replayed := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/organizations",
		organizationBody, environment.server.URL, token,
	)
	assertExactJSON(t, replayed, http.StatusOK, wantBody)
	if location := replayed.Header.Get("Location"); location != "" {
		t.Fatalf("replay Location = %q, want empty", location)
	}

	loaded := environment.request(
		t, environment.client, http.MethodGet, "/api/v1/organizations/"+organizationID,
		"", "", "",
	)
	assertExactJSON(t, loaded, http.StatusOK, wantBody)

	environment.organizations.mu.Lock()
	defer environment.organizations.mu.Unlock()
	if len(environment.organizations.byID) != 1 || len(environment.organizations.projects) != 1 {
		t.Fatalf(
			"idempotent replay retained %d Organizations and %d Projects",
			len(environment.organizations.byID), len(environment.organizations.projects),
		)
	}
}

func TestReadinessFailureReturnsPlainTextServiceUnavailable(t *testing.T) {
	readyCalls := 0
	environment := newTestEnvironment(t, true, 0, func(config *Config) {
		config.Ready = func(_ context.Context) error {
			readyCalls++
			return errors.New("PostgreSQL unavailable")
		}
	})

	response := environment.request(t, environment.client, http.MethodGet, "/readyz", "", "", "")
	if response.StatusCode != http.StatusServiceUnavailable || string(response.Body) != "Unhealthy" {
		t.Fatalf("readiness response = %d, body %q", response.StatusCode, response.Body)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "text/plain; charset=utf-8" {
		t.Fatalf("readiness Content-Type = %q", contentType)
	}
	if readyCalls != 1 {
		t.Fatalf("readiness check called %d times", readyCalls)
	}
}

func assertExactJSON(t *testing.T, response recordedResponse, status int, body string) {
	t.Helper()
	if response.StatusCode != status || string(response.Body) != body {
		t.Fatalf("JSON response = %d, body %s; want %d, body %s", response.StatusCode, response.Body, status, body)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("JSON Content-Type = %q", contentType)
	}
}
