package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"encoding/json"
	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
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

func TestMembershipGovernsOrganizationAccess(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	token := environment.bootstrapAdministrator(t)
	// Setup provisions the bootstrap Organization and makes its creator a member.
	const bootstrapOrganization = "00000000-0000-7000-8000-000000000002"

	readable := environment.request(
		t, environment.client, http.MethodGet, "/api/v1/organizations/"+bootstrapOrganization, "", "", "",
	)
	if readable.StatusCode != http.StatusOK {
		t.Fatalf("member read = %d, body %s", readable.StatusCode, readable.Body)
	}

	// An instance Administrator who is not a member has no implicit access, and the
	// Organization must be indistinguishable from one that does not exist.
	environment.organizations.setMembership(
		organization.ID(bootstrapOrganization), environment.administratorID(t), organization.RoleAdministrator, false,
	)
	hidden := environment.request(
		t, environment.client, http.MethodGet, "/api/v1/organizations/"+bootstrapOrganization, "", "", "",
	)
	assertProblem(t, hidden, http.StatusNotFound, "Not Found", "")
	unknown := environment.request(
		t, environment.client, http.MethodGet,
		"/api/v1/organizations/00000000-0000-7000-8000-000000000999", "", "", "",
	)
	if string(hidden.Body) != string(unknown.Body) {
		t.Fatalf("non-member body %s differs from unknown-Organization body %s", hidden.Body, unknown.Body)
	}

	// A member whose role lacks the permission gets 403: existence is already known.
	environment.grantMembership(t, bootstrapOrganization, organization.RoleViewer)
	viewerRead := environment.request(
		t, environment.client, http.MethodGet, "/api/v1/organizations/"+bootstrapOrganization, "", "", "",
	)
	if viewerRead.StatusCode != http.StatusOK {
		t.Fatalf("Viewer read = %d, body %s", viewerRead.StatusCode, viewerRead.Body)
	}
	viewerWrite := environment.request(
		t, environment.client, http.MethodPost,
		"/api/v1/organizations/"+bootstrapOrganization+"/monitors",
		`{"name":"Checkout","checkType":"http"}`, environment.server.URL, token,
	)
	assertProblem(t, viewerWrite, http.StatusForbidden, "Forbidden", "")
}

func TestOrganizationListReturnsOnlyMemberships(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	environment.bootstrapAdministrator(t)
	const bootstrapOrganization = "00000000-0000-7000-8000-000000000002"

	listed := environment.request(t, environment.client, http.MethodGet, "/api/v1/organizations", "", "", "")
	if listed.StatusCode != http.StatusOK {
		t.Fatalf("list = %d, body %s", listed.StatusCode, listed.Body)
	}
	var organizations []api.OrganizationResponse
	if err := json.Unmarshal(listed.Body, &organizations); err != nil {
		t.Fatal(err)
	}
	if len(organizations) != 1 || organizations[0].ID != bootstrapOrganization {
		t.Fatalf("list = %#v, want only the bootstrap Organization", organizations)
	}

	environment.organizations.setMembership(
		organization.ID(bootstrapOrganization), environment.administratorID(t), organization.RoleAdministrator, false,
	)
	empty := environment.request(t, environment.client, http.MethodGet, "/api/v1/organizations", "", "", "")
	assertExactJSON(t, empty, http.StatusOK, "[]\n")
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

func TestOrganizationCreateAndGetExactWireShape(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	token := environment.bootstrapAdministrator(t)
	// Setup consumes 0001 for the administrator and 0002 for the bootstrap
	// Organization, so this one starts at 0003.
	const organizationID = "00000000-0000-7000-8000-000000000003"
	wantBody := `{"id":"` + organizationID + `","displayName":"Acme","createdAt":"2026-07-24T00:00:00Z"}` + "\n"

	created := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/organizations",
		`{"displayName":"Acme"}`, environment.server.URL, token,
	)
	assertExactJSON(t, created, http.StatusCreated, wantBody)
	if location := created.Header.Get("Location"); location != "/api/v1/organizations/"+organizationID {
		t.Fatalf("created Location = %q", location)
	}

	loaded := environment.request(
		t, environment.client, http.MethodGet, "/api/v1/organizations/"+organizationID,
		"", "", "",
	)
	assertExactJSON(t, loaded, http.StatusOK, wantBody)

	environment.organizations.mu.Lock()
	defer environment.organizations.mu.Unlock()
	if len(environment.organizations.byID) != 2 {
		t.Fatalf("creation left %d Organizations", len(environment.organizations.byID))
	}
}

func TestOrganizationRenameGateAndValidation(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	token := environment.bootstrapAdministrator(t)
	const bootstrapOrganization = "00000000-0000-7000-8000-000000000002"
	renamePath := "/api/v1/organizations/" + bootstrapOrganization + "/name"

	// organization.write is additive: Administrator gains it by rule, Viewer does not.
	environment.grantMembership(t, bootstrapOrganization, organization.RoleViewer)
	forbidden := environment.request(
		t, environment.client, http.MethodPut, renamePath,
		`{"displayName":"Acme"}`, environment.server.URL, token,
	)
	assertProblem(t, forbidden, http.StatusForbidden, "Forbidden", "")

	environment.grantMembership(t, bootstrapOrganization, organization.RoleAdministrator)
	renamed := environment.request(
		t, environment.client, http.MethodPut, renamePath,
		`{"displayName":"  Acme Monitoring  "}`, environment.server.URL, token,
	)
	if renamed.StatusCode != http.StatusOK {
		t.Fatalf("rename = %d, body %s", renamed.StatusCode, renamed.Body)
	}
	var after api.OrganizationResponse
	if err := json.Unmarshal(renamed.Body, &after); err != nil {
		t.Fatal(err)
	}
	if after.DisplayName != "Acme Monitoring" || after.ID != bootstrapOrganization {
		t.Fatalf("after rename = %#v, want the trimmed name and an unchanged id", after)
	}

	invalid := environment.request(
		t, environment.client, http.MethodPut, renamePath,
		`{"displayName":"   "}`, environment.server.URL, token,
	)
	problem := decodeProblem(t, invalid)
	entries := problem.Errors["displayName"]
	if invalid.StatusCode != http.StatusBadRequest || len(entries) != 1 ||
		entries[0].Code != organization.DisplayNameInvalidCode {
		t.Fatalf("blank rename = %d, body %s", invalid.StatusCode, invalid.Body)
	}
}

func TestProblemDetailsCarryStableCodes(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	token, _ := environment.getAntiforgery(t)

	// A validation problem carries one coded entry per failure, grouped by field path.
	invalid := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/setup/admin",
		`{"email":"nope","displayName":"  ","password":"short"}`,
		environment.server.URL, token.RequestToken,
	)
	problem := decodeProblem(t, invalid)
	if problem.Status != http.StatusBadRequest || problem.Code != "" {
		t.Fatalf("validation problem = %#v, want 400 with no top-level code", problem)
	}
	wantCodes := map[string]string{
		"email":       user.EmailInvalidCode,
		"displayName": user.DisplayNameInvalidCode,
		"password":    user.PasswordLengthCode,
	}
	for field, code := range wantCodes {
		entries := problem.Errors[field]
		if len(entries) != 1 || entries[0].Code != code || entries[0].Message == "" {
			t.Errorf("errors[%q] = %#v, want code %q with a non-empty message", field, entries, code)
		}
	}

	// A non-validation problem carries a top-level code instead.
	environment.bootstrapAdministrator(t)
	refreshed, _ := environment.getAntiforgery(t)
	conflict := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/setup/admin",
		`{"email":"admin@example.test","displayName":"Admin","password":"password-123"}`,
		environment.server.URL, refreshed.RequestToken,
	)
	conflictProblem := decodeProblem(t, conflict)
	if conflictProblem.Status != http.StatusConflict ||
		conflictProblem.Code != user.SetupAlreadyCompletedCode || len(conflictProblem.Errors) != 0 {
		t.Fatalf("conflict problem = %#v", conflictProblem)
	}

	// Transport-level problems are coded too, so a client never has to match prose.
	unauthorized := environment.request(
		t, environment.client, http.MethodGet, "/api/v1/organizations/not-a-uuid", "", "", "",
	)
	if notFound := decodeProblem(t, unauthorized); notFound.Code != notFoundCode {
		t.Fatalf("routing problem = %#v, want code %q", notFound, notFoundCode)
	}
}

func TestSetupProvisionsBootstrapOrganizationAndListsIt(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	token, _ := environment.getAntiforgery(t)
	response := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/setup/admin",
		`{"email":"admin@example.test","displayName":"Admin","password":"password-123"}`,
		environment.server.URL, token.RequestToken,
	)

	const userID = "00000000-0000-7000-8000-000000000001"
	const organizationID = "00000000-0000-7000-8000-000000000002"
	wantOrganization := `{"id":"` + organizationID + `","displayName":"Default",` +
		`"createdAt":"2026-07-24T00:00:00Z"}`
	wantSetup := `{"user":{"id":"` + userID + `","email":"admin@example.test","displayName":"Admin",` +
		`"role":"Administrator","createdAt":"2026-07-24T00:00:00Z"},"organization":` + wantOrganization + "}\n"
	assertExactJSON(t, response, http.StatusCreated, wantSetup)

	// Setup signs the administrator in, so the Organization is reachable immediately.
	if findCookie(t, response, sessionCookieName).Value == "" {
		t.Fatal("setup did not issue a session cookie")
	}
	if location := response.Header.Get("Location"); location != "" {
		t.Fatalf("setup Location = %q, want empty", location)
	}

	listed := environment.request(t, environment.client, http.MethodGet, "/api/v1/organizations", "", "", "")
	assertExactJSON(t, listed, http.StatusOK, "["+wantOrganization+"]\n")
}

func TestOrganizationListIsEmptyArrayAndRequiresAdministrator(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	anonymous := environment.request(t, environment.client, http.MethodGet, "/api/v1/organizations", "", "", "")
	assertProblem(t, anonymous, http.StatusUnauthorized, "Unauthorized", "")

	environment.bootstrapAdministrator(t)
	environment.organizations.mu.Lock()
	clear(environment.organizations.byID)
	environment.organizations.mu.Unlock()

	empty := environment.request(t, environment.client, http.MethodGet, "/api/v1/organizations", "", "", "")
	assertExactJSON(t, empty, http.StatusOK, "[]\n")
}

func TestUserAdministrationCreatesTeammatesAndSharesOrganizations(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	unauthenticated := environment.request(t, environment.client, http.MethodGet, "/api/v1/users", "", "", "")
	assertProblem(t, unauthenticated, http.StatusUnauthorized, "Unauthorized", "")

	token := environment.bootstrapAdministrator(t)
	const bootstrapOrganization = "00000000-0000-7000-8000-000000000002"
	// Setup consumes 0001-0002, so the created user takes 0003.
	const teammateID = "00000000-0000-7000-8000-000000000003"

	created := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/users",
		`{"email":" TEAMMATE@Example.Test ","password":"password-123"}`,
		environment.server.URL, token,
	)
	assertExactJSON(t, created, http.StatusCreated, `{"id":"`+teammateID+`","email":"teammate@example.test","displayName":"teammate@example.test","role":"Administrator","createdAt":"2026-07-24T00:00:00Z"}`+"\n")
	if location := created.Header.Get("Location"); location != "/api/v1/users/"+teammateID {
		t.Fatalf("created Location = %q", location)
	}

	listed := environment.request(t, environment.client, http.MethodGet, "/api/v1/users", "", "", "")
	if listed.StatusCode != http.StatusOK {
		t.Fatalf("list = %d, body %s", listed.StatusCode, listed.Body)
	}
	var accounts []api.UserResponse
	if err := json.Unmarshal(listed.Body, &accounts); err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || accounts[1].Email != "teammate@example.test" {
		t.Fatalf("users = %#v, want the administrator and the teammate in creation order", accounts)
	}

	// The teammate shares the instance: membership was granted for every Organization.
	membership, found, err := environment.organizations.FindMembership(
		context.Background(), organization.ID(bootstrapOrganization), teammateID,
	)
	if err != nil || !found || membership.Role != organization.RoleAdministrator {
		t.Fatalf("teammate membership = %#v, found %v, error %v", membership, found, err)
	}

	duplicate := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/users",
		`{"email":"teammate@example.test","password":"password-123"}`,
		environment.server.URL, token,
	)
	if problem := decodeProblem(t, duplicate); duplicate.StatusCode != http.StatusConflict ||
		problem.Code != user.EmailConflictCode {
		t.Fatalf("duplicate email = %d, body %s", duplicate.StatusCode, duplicate.Body)
	}
}

func TestUserItemReplacesProfileAndKeepsSessions(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	unauthenticated := environment.request(
		t, environment.client, http.MethodPut, "/api/v1/users/00000000-0000-7000-8000-000000000001",
		`{"email":"operator@example.test"}`, "", "",
	)
	assertProblem(t, unauthenticated, http.StatusUnauthorized, "Unauthorized", "")

	token := environment.bootstrapAdministrator(t)
	administratorID := environment.administratorID(t)

	invalid := environment.request(
		t, environment.client, http.MethodPut, "/api/v1/users/"+administratorID,
		`{"email":"not-an-email"}`, environment.server.URL, token,
	)
	refreshed, _ := environment.getAntiforgery(t)
	token = refreshed.RequestToken
	problem := decodeProblem(t, invalid)
	if invalid.StatusCode != http.StatusBadRequest || problem.Errors["email"] == nil {
		t.Fatalf("invalid email = %d, body %s", invalid.StatusCode, invalid.Body)
	}

	// Setup consumes UUIDs 0001 (user) and 0002 (Organization).
	updated := environment.request(
		t, environment.client, http.MethodPut, "/api/v1/users/"+administratorID,
		`{"email":" OPERATOR@Example.Test ","displayName":"  Ops  "}`, environment.server.URL, token,
	)
	refreshed, _ = environment.getAntiforgery(t)
	token = refreshed.RequestToken
	assertExactJSON(t, updated, http.StatusOK, `{"id":"`+administratorID+`","email":"operator@example.test","displayName":"Ops","role":"Administrator","createdAt":"2026-07-24T00:00:00Z"}`+"\n")

	// The same session keeps working because sessions bind to the immutable user id.
	session := environment.request(t, environment.client, http.MethodGet, "/api/v1/auth/session", "", "", "")
	if session.StatusCode != http.StatusOK {
		t.Fatalf("session after profile update = %d, body %s", session.StatusCode, session.Body)
	}

	// The new email is immediately the login identifier.
	login := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/auth/login",
		`{"email":"operator@example.test","password":"password-123"}`, environment.server.URL, token,
	)
	refreshed, _ = environment.getAntiforgery(t)
	token = refreshed.RequestToken
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login with new email = %d, body %s", login.StatusCode, login.Body)
	}

	defaulted := environment.request(
		t, environment.client, http.MethodPut, "/api/v1/users/"+administratorID,
		`{"email":"owner@example.test","displayName":"   "}`, environment.server.URL, token,
	)
	refreshed, _ = environment.getAntiforgery(t)
	token = refreshed.RequestToken
	assertExactJSON(t, defaulted, http.StatusOK, `{"id":"`+administratorID+`","email":"owner@example.test","displayName":"owner@example.test","role":"Administrator","createdAt":"2026-07-24T00:00:00Z"}`+"\n")

	created := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/users",
		`{"email":"teammate@example.test","password":"password-123"}`, environment.server.URL, token,
	)
	refreshed, _ = environment.getAntiforgery(t)
	token = refreshed.RequestToken
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create teammate = %d, body %s", created.StatusCode, created.Body)
	}
	conflict := environment.request(
		t, environment.client, http.MethodPut, "/api/v1/users/"+administratorID,
		`{"email":" TEAMMATE@example.test "}`, environment.server.URL, token,
	)
	if duplicate := decodeProblem(t, conflict); conflict.StatusCode != http.StatusConflict ||
		duplicate.Code != user.EmailConflictCode {
		t.Fatalf("duplicate email = %d, body %s", conflict.StatusCode, conflict.Body)
	}

	missing := environment.request(
		t, environment.client, http.MethodPut,
		"/api/v1/users/00000000-0000-7000-8000-0000000000ff",
		`{"email":"nobody@example.test"}`, environment.server.URL, token,
	)
	assertProblem(t, missing, http.StatusNotFound, "Not Found", "")

	method := environment.request(
		t, environment.client, http.MethodDelete, "/api/v1/users/"+administratorID, "", environment.server.URL, token,
	)
	assertProblem(t, method, http.StatusMethodNotAllowed, "Method Not Allowed", "")
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
