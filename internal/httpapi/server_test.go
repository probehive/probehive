package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/user"
)

func TestAnonymousSetupSessionAntiforgeryLogoutFlow(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)

	anonymousToken, anonymousResponse := environment.getAntiforgery(t)
	if anonymousToken.HeaderName != antiforgeryHeaderName || anonymousToken.RequestToken == "" {
		t.Fatalf("anonymous antiforgery response = %#v", anonymousToken)
	}
	antiforgeryCookie := findCookie(t, anonymousResponse, antiforgeryCookieName)
	assertCookieProfile(t, antiforgeryCookie, http.SameSiteStrictMode, false)

	setup := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/setup/admin",
		`{"email":"admin@example.test","displayName":"Admin","password":"password-123"}`,
		environment.server.URL, anonymousToken.RequestToken,
	)
	if setup.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, body %s", setup.StatusCode, setup.Body)
	}
	sessionCookie := findCookie(t, setup, sessionCookieName)
	assertCookieProfile(t, sessionCookie, http.SameSiteLaxMode, false)
	if sessionCookie.MaxAge != 0 || !sessionCookie.Expires.IsZero() {
		t.Fatalf("new session cookie unexpectedly carries client-side renewal fields: %#v", sessionCookie)
	}
	sessionRecord := environment.sessions.only(t)
	if !sessionRecord.ExpiresAt.Equal(sessionRecord.AuthenticatedAt.Add(user.SessionLifetime)) || !sessionRecord.AuthenticatedAt.Equal(environment.clock.Now()) {
		t.Fatalf("session record = %#v", sessionRecord)
	}

	oldToken := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/organizations",
		`{"slug":"acme","displayName":"Acme"}`, environment.server.URL,
		anonymousToken.RequestToken,
	)
	assertProblem(t, oldToken, http.StatusBadRequest, antiforgeryInvalidTitle, antiforgeryInvalidDetail)

	authenticatedToken, authenticatedResponse := environment.getAntiforgery(t)
	authenticatedCookie := findCookie(t, authenticatedResponse, antiforgeryCookieName)
	assertCookieProfile(t, authenticatedCookie, http.SameSiteStrictMode, false)

	session := environment.request(t, environment.client, http.MethodGet, "/api/v1/auth/session", "", "", "")
	if session.StatusCode != http.StatusOK {
		t.Fatalf("session status = %d, body %s", session.StatusCode, session.Body)
	}
	var sessionBody api.SessionResponse
	if err := json.Unmarshal(session.Body, &sessionBody); err != nil {
		t.Fatal(err)
	}
	if sessionBody.Email != "admin@example.test" || sessionBody.DisplayName != "Admin" || sessionBody.Role != user.AdministratorRole {
		t.Fatalf("session response = %#v", sessionBody)
	}

	logout := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/auth/logout", "",
		environment.server.URL, authenticatedToken.RequestToken,
	)
	if logout.StatusCode != http.StatusNoContent || len(logout.Body) != 0 {
		t.Fatalf("logout status = %d, body %q", logout.StatusCode, logout.Body)
	}
	expiredCookie := findCookie(t, logout, sessionCookieName)
	if expiredCookie.MaxAge >= 0 || expiredCookie.Value != "" || !expiredCookie.HttpOnly || expiredCookie.SameSite != http.SameSiteLaxMode || expiredCookie.Path != "/" {
		t.Fatalf("expired session cookie = %#v", expiredCookie)
	}

	afterLogout := environment.request(t, environment.client, http.MethodGet, "/api/v1/auth/session", "", "", "")
	assertProblem(t, afterLogout, http.StatusUnauthorized, "Unauthorized", "")
	postLogoutToken, _ := environment.getAntiforgery(t)
	if postLogoutToken.RequestToken == authenticatedToken.RequestToken {
		t.Fatal("logout antiforgery refresh reused an authenticated token")
	}
}

func TestSecurityFilterOrderingAndAntiforgeryFailures(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	validToken := environment.bootstrapAdministrator(t)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	unauthenticated := &http.Client{Jar: jar}
	withoutSession := environment.request(
		t, unauthenticated, http.MethodPost, "/api/v1/organizations",
		`{"slug":"acme","displayName":"Acme"}`, "", "",
	)
	assertProblem(t, withoutSession, http.StatusUnauthorized, "Unauthorized", "")

	badOrigin := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/organizations",
		`{"slug":"acme","displayName":"Acme"}`, "https://attacker.invalid", "",
	)
	assertProblem(t, badOrigin, http.StatusForbidden, originRejectedTitle, originRejectedDetail)

	missingToken := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/organizations",
		`{"slug":"acme","displayName":"Acme"}`, environment.server.URL, "",
	)
	assertProblem(t, missingToken, http.StatusBadRequest, antiforgeryInvalidTitle, antiforgeryInvalidDetail)

	wrongToken := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/organizations",
		`{"slug":"acme","displayName":"Acme"}`, environment.server.URL,
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	)
	assertProblem(t, wrongToken, http.StatusBadRequest, antiforgeryInvalidTitle, antiforgeryInvalidDetail)

	valid := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/organizations",
		`{"slug":"acme","displayName":"Acme"}`, environment.server.URL, validToken,
	)
	if valid.StatusCode != http.StatusCreated || valid.Header.Get("Location") == "" {
		t.Fatalf("valid protected request = %d, body %s", valid.StatusCode, valid.Body)
	}
}

func TestCredentialRateLimitIsFixedAndSharedBySetupAndLogin(t *testing.T) {
	environment := newTestEnvironment(t, true, 2)
	token, _ := environment.getAntiforgery(t)

	first := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/auth/login",
		`{"email":"missing@example.test","password":"wrong"}`,
		environment.server.URL, token.RequestToken,
	)
	assertProblem(t, first, http.StatusUnauthorized, user.InvalidCredentialsTitle, user.InvalidCredentialsDetail)

	second := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/setup/admin", `{}`,
		environment.server.URL, token.RequestToken,
	)
	if second.StatusCode != http.StatusBadRequest {
		t.Fatalf("second shared attempt = %d, body %s", second.StatusCode, second.Body)
	}

	third := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/auth/login",
		`{"email":"missing@example.test","password":"wrong"}`,
		environment.server.URL, token.RequestToken,
	)
	assertProblem(t, third, http.StatusTooManyRequests, "Too Many Requests", "")

	environment.clock.Advance(time.Minute)
	afterWindow := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/auth/login",
		`{"email":"missing@example.test","password":"wrong"}`,
		environment.server.URL, token.RequestToken,
	)
	assertProblem(t, afterWindow, http.StatusUnauthorized, user.InvalidCredentialsTitle, user.InvalidCredentialsDetail)
}

func TestMalformedUUIDAndValidationProblemDetails(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)

	malformed := environment.request(t, environment.client, http.MethodGet, "/api/v1/organizations/not-a-uuid", "", "", "")
	assertProblem(t, malformed, http.StatusNotFound, "Not Found", "")

	token, _ := environment.getAntiforgery(t)
	invalid := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/setup/admin", `{}`,
		environment.server.URL, token.RequestToken,
	)
	problem := decodeProblem(t, invalid)
	if problem.Status != http.StatusBadRequest || problem.Title != "One or more validation errors occurred." {
		t.Fatalf("validation problem = %#v", problem)
	}
	want := map[string]string{
		"email":       user.EmailValidationMessage,
		"displayName": user.DisplayNameValidationMessage,
		"password":    user.PasswordValidationMessage,
	}
	for field, message := range want {
		if values := problem.Errors[field]; len(values) != 1 || values[0] != message {
			t.Errorf("errors[%q] = %#v, want %q", field, values, message)
		}
	}
}

func TestTopLevelJSONNullIsRejected(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	token, _ := environment.getAntiforgery(t)

	response := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/setup/admin", `null`,
		environment.server.URL, token.RequestToken,
	)
	assertProblem(
		t, response, http.StatusBadRequest, "Bad Request",
		"The request body must be a JSON object.",
	)
}

func TestInvalidUTF8IsRejectedBeforeBinding(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	token, _ := environment.getAntiforgery(t)
	body := string(append(
		[]byte(`{"email":"admin@example.test","displayName":"Adm`),
		append([]byte{0xff}, []byte(`in","password":"password-123"}`)...)...,
	))

	response := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/setup/admin", body,
		environment.server.URL, token.RequestToken,
	)
	assertProblem(
		t, response, http.StatusBadRequest, "Bad Request",
		"The request body is not valid JSON for this endpoint.",
	)
}

func TestNonCanonicalPathsReturnProblemDetailsWithoutRedirect(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	for _, requestPath := range []string{"/api//v1/setup/status", "/api/v1/../v1/setup/status", "/api/v1/setup/./status"} {
		response := environment.request(t, environment.client, http.MethodGet, requestPath, "", "", "")
		assertProblem(t, response, http.StatusNotFound, "Not Found", "")
		if location := response.Header.Get("Location"); location != "" {
			t.Errorf("%s redirected to %q", requestPath, location)
		}
	}
}

func TestRouteConstraintsPrecedeMethodMatching(t *testing.T) {
	validOrganization := "00000000-0000-7000-8000-000000000101"
	validProject := "00000000-0000-7000-8000-000000000102"
	validMonitor := "00000000-0000-7000-8000-000000000103"
	tests := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{name: "invalid Organization UUID", method: http.MethodPost, path: "/api/v1/organizations/not-a-uuid", status: http.StatusNotFound},
		{
			name: "invalid Monitor UUID", method: http.MethodDelete,
			path:   "/api/v1/organizations/" + validOrganization + "/projects/" + validProject + "/monitors/not-a-uuid",
			status: http.StatusNotFound,
		},
		{
			name: "revision outside int32", method: http.MethodPost,
			path:   "/api/v1/organizations/" + validOrganization + "/projects/" + validProject + "/monitors/" + validMonitor + "/revisions/2147483648",
			status: http.StatusNotFound,
		},
		{name: "valid constraint wrong method", method: http.MethodPost, path: "/api/v1/organizations/" + validOrganization, status: http.StatusMethodNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment := newTestEnvironment(t, true, 0)
			response := environment.request(t, environment.client, test.method, test.path, "", "", "")
			assertProblem(t, response, test.status, http.StatusText(test.status), "")
		})
	}
}

func TestConfiguredPublicOriginControlsUnsafeRequests(t *testing.T) {
	const publicOrigin = "https://probe.example"
	environment := newTestEnvironment(t, true, 0, func(config *Config) {
		config.PublicOrigin = " " + publicOrigin + " "
	})
	token, _ := environment.getAntiforgery(t)

	setup := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/setup/admin",
		`{"email":"admin@example.test","displayName":"Admin","password":"password-123"}`,
		publicOrigin, token.RequestToken,
	)
	if setup.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, body %s", setup.StatusCode, setup.Body)
	}

	refreshed, _ := environment.getAntiforgery(t)
	internalOrigin := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/organizations",
		`{"slug":"acme","displayName":"Acme"}`, environment.server.URL,
		refreshed.RequestToken,
	)
	assertProblem(t, internalOrigin, http.StatusForbidden, originRejectedTitle, originRejectedDetail)

	publicOriginResponse := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/organizations",
		`{"slug":"acme","displayName":"Acme"}`, publicOrigin,
		refreshed.RequestToken,
	)
	if publicOriginResponse.StatusCode != http.StatusCreated {
		t.Fatalf("public-origin request = %d, body %s", publicOriginResponse.StatusCode, publicOriginResponse.Body)
	}
}

func TestPublicOriginRejectsNonOrigins(t *testing.T) {
	values := []string{
		"ftp://probe.example",
		"https://user@probe.example",
		"https://probe.example/path",
		"https://probe.example?query=value",
		"https://probe.example#fragment",
		"probe.example",
	}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			if _, err := normalizePublicOrigin(value); err == nil {
				t.Fatalf("normalizePublicOrigin(%q) unexpectedly succeeded", value)
			}
		})
	}
}

func TestEmptyBrowserMetadataIsTreatedAsAbsent(t *testing.T) {
	t.Parallel()
	server := &Server{}
	tests := []struct {
		name    string
		headers http.Header
		want    bool
	}{
		{name: "empty Origin", headers: http.Header{"Origin": {""}}, want: true},
		{name: "empty Referer", headers: http.Header{"Referer": {""}}, want: true},
		{
			name: "empty Origin falls through to matching Referer",
			headers: http.Header{
				"Origin":  {""},
				"Referer": {"http://example.test/path"},
			},
			want: true,
		},
		{
			name: "empty Origin falls through to mismatched Referer",
			headers: http.Header{
				"Origin":  {""},
				"Referer": {"https://attacker.invalid/path"},
			},
			want: false,
		},
		{name: "multiple empty Origins", headers: http.Header{"Origin": {"", ""}}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://example.test/api/v1/auth/login", nil)
			request.Header = test.headers
			if got := server.acceptableBrowserOrigin(request); got != test.want {
				t.Fatalf("acceptableBrowserOrigin() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestMonitorCrossScopeIsHiddenAndConcurrencyIsConflict(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	token := environment.bootstrapAdministrator(t)
	organizationID := "00000000-0000-7000-8000-000000000101"
	projectID := "00000000-0000-7000-8000-000000000102"
	monitorID := "00000000-0000-7000-8000-000000000103"
	createdAt := environment.clock.Now()
	value, err := monitor.RestoreMonitor(
		monitor.ID(monitorID), organizationID, projectID, "API", "http",
		monitor.StateDraft, 0, createdAt, createdAt, 7,
	)
	if err != nil {
		t.Fatal(err)
	}
	environment.monitors.seed(value)

	wrongOrganization := "00000000-0000-7000-8000-000000000999"
	crossScope := environment.request(
		t, environment.client, http.MethodGet,
		"/api/v1/organizations/"+wrongOrganization+"/projects/"+projectID+"/monitors/"+monitorID,
		"", "", "",
	)
	assertProblem(t, crossScope, http.StatusNotFound, "Not Found", "")

	environment.monitors.mu.Lock()
	environment.monitors.updateError = monitor.ErrConcurrentUpdate
	environment.monitors.mu.Unlock()
	conflict := environment.request(
		t, environment.client, http.MethodPut,
		"/api/v1/organizations/"+organizationID+"/projects/"+projectID+"/monitors/"+monitorID+"/name",
		`{"name":"Renamed"}`, environment.server.URL, token,
	)
	assertProblem(t, conflict, http.StatusConflict, monitor.RenameRejectedTitle, monitor.ConcurrentUpdateDetail)
}

func TestRevisionNumberUsesLegacyInt32RouteRange(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	path := "/api/v1/organizations/00000000-0000-7000-8000-000000000101" +
		"/projects/00000000-0000-7000-8000-000000000102" +
		"/monitors/00000000-0000-7000-8000-000000000103/revisions/2147483648"
	response := environment.request(t, environment.client, http.MethodGet, path, "", "", "")
	assertProblem(t, response, http.StatusNotFound, "Not Found", "")
}

func TestCookiesAreSecureOutsideDevelopment(t *testing.T) {
	environment := newTestEnvironment(t, false, 0)
	token, antiforgeryResponse := environment.getAntiforgery(t)
	antiforgeryCookie := findCookie(t, antiforgeryResponse, antiforgeryCookieName)
	assertCookieProfile(t, antiforgeryCookie, http.SameSiteStrictMode, true)

	setup := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/setup/admin",
		`{"email":"admin@example.test","displayName":"Admin","password":"password-123"}`,
		environment.server.URL, token.RequestToken, antiforgeryCookie,
	)
	if setup.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, body %s", setup.StatusCode, setup.Body)
	}
	assertCookieProfile(t, findCookie(t, setup, sessionCookieName), http.SameSiteLaxMode, true)
}

func assertCookieProfile(t *testing.T, cookie *http.Cookie, sameSite http.SameSite, secure bool) {
	t.Helper()
	if cookie.Path != "/" || cookie.Domain != "" || !cookie.HttpOnly || cookie.SameSite != sameSite || cookie.Secure != secure {
		t.Fatalf("cookie profile = %#v, want Path=/ host-only HttpOnly SameSite=%v Secure=%v", cookie, sameSite, secure)
	}
}

func assertProblem(t *testing.T, response recordedResponse, status int, title, detail string) {
	t.Helper()
	if response.StatusCode != status {
		t.Fatalf("status = %d, want %d; body %s", response.StatusCode, status, response.Body)
	}
	problem := decodeProblem(t, response)
	if problem.Status != status || problem.Title != title || problem.Detail != detail {
		t.Fatalf("problem = %#v, want status=%d title=%q detail=%q", problem, status, title, detail)
	}
}

var _ organization.Store = (*memoryOrganizationStore)(nil)
