package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/statuspage"
)

func TestStatusPageDraftAPIIsAdministratorOnlyAndDisclosureSafe(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	antiforgery := environment.bootstrapAdministrator(t)
	organizationID, projectID, monitorID := seedRunQueryMonitor(t, environment)
	path := "/api/v1/organizations/" + organizationID + "/status-page/draft"

	if response := environment.request(t, &http.Client{}, http.MethodGet, path, "", "", ""); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous GET = %d, want 401", response.StatusCode)
	}
	environment.grantMembership(t, organizationID, organization.RoleViewer)
	if response := environment.request(t, environment.client, http.MethodGet, path, "", "", ""); response.StatusCode != http.StatusForbidden {
		t.Fatalf("Viewer GET = %d, want 403", response.StatusCode)
	}
	environment.grantMembership(t, organizationID, organization.RoleAdministrator)
	if response := environment.request(t, environment.client, http.MethodGet, path, "", "", ""); response.StatusCode != http.StatusNoContent {
		t.Fatalf("empty GET = %d, body %s", response.StatusCode, response.Body)
	}

	body := `{"title":"Service Status","version":0,"components":[{"monitorId":"` +
		monitorID + `","label":"Public API"}]}`
	if response := environment.request(t, environment.client, http.MethodPut, path, body, environment.server.URL, ""); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT without antiforgery = %d, want 400", response.StatusCode)
	}
	createdResponse := environment.request(
		t, environment.client, http.MethodPut, path, body, environment.server.URL, antiforgery,
	)
	if createdResponse.StatusCode != http.StatusOK {
		t.Fatalf("PUT = %d, body %s", createdResponse.StatusCode, createdResponse.Body)
	}
	var created api.StatusPageDraftResponse
	if err := json.Unmarshal(createdResponse.Body, &created); err != nil {
		t.Fatal(err)
	}
	if created.OrganizationID != organizationID || created.Title != "Service Status" ||
		created.Version != 1 || len(created.Components) != 1 ||
		created.Components[0].MonitorID != monitorID ||
		created.Components[0].Label != "Public API" || created.Components[0].Position != 0 {
		t.Fatalf("created = %#v", created)
	}
	for _, forbidden := range []string{"target", "revision", "run", "observation", "incident", "alert", "secret"} {
		if json.Valid(createdResponse.Body) && containsFold(string(createdResponse.Body), forbidden) {
			t.Fatalf("response disclosed forbidden field %q: %s", forbidden, createdResponse.Body)
		}
	}

	stale := environment.request(
		t, environment.client, http.MethodPut, path, body, environment.server.URL, antiforgery,
	)
	if stale.StatusCode != http.StatusConflict || decodeProblem(t, stale).Code != statuspage.ConcurrentUpdateCode {
		t.Fatalf("stale PUT = %d, body %s", stale.StatusCode, stale.Body)
	}

	archivedID := "00000000-0000-7000-8000-000000000099"
	archived, err := monitor.RestoreMonitor(
		monitor.ID(archivedID), organizationID, projectID, "Archived", "http",
		monitor.StateArchived, monitor.DefaultIntervalSeconds, 1,
		time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC), 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	environment.monitors.seed(archived)
	invalidBody := `{"title":"Status","version":1,"components":[{"monitorId":"` + archivedID + `","label":"Archived"}]}`
	invalid := environment.request(
		t, environment.client, http.MethodPut, path, invalidBody,
		environment.server.URL, antiforgery,
	)
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("archived Monitor PUT = %d, body %s", invalid.StatusCode, invalid.Body)
	}
	problem := decodeProblem(t, invalid)
	if problem.Errors["components"][0].Code != statuspage.MonitorUnavailableCode {
		t.Fatalf("archived problem = %#v", problem)
	}

	otherOrganization := "00000000-0000-7000-8000-000000000088"
	if response := environment.request(
		t, environment.client, http.MethodGet,
		"/api/v1/organizations/"+otherOrganization+"/status-page/draft", "", "", "",
	); response.StatusCode != http.StatusNotFound {
		t.Fatalf("non-member GET = %d, want 404", response.StatusCode)
	}
}

func TestStatusPagePublicationLifecycleIsProtectedAndDisclosureSafe(t *testing.T) {
	environment := newTestEnvironment(t, true, 0, func(config *Config) {
		config.PublicStatusReadsPerMinute = 20
	})
	antiforgery := environment.bootstrapAdministrator(t)
	organizationID, _, monitorID := seedRunQueryMonitor(t, environment)
	publicationPath := "/api/v1/organizations/" + organizationID + "/status-page/publication"
	draftPath := "/api/v1/organizations/" + organizationID + "/status-page/draft"

	if response := environment.request(t, &http.Client{}, http.MethodPost, publicationPath, "", environment.server.URL, antiforgery); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous publish = %d, want 401", response.StatusCode)
	}
	environment.grantMembership(t, organizationID, organization.RoleAdministrator)
	if response := environment.request(t, environment.client, http.MethodPost, publicationPath, "", environment.server.URL, antiforgery); response.StatusCode != http.StatusConflict || decodeProblem(t, response).Code != statuspage.DraftMissingCode {
		t.Fatalf("publish without draft = %d, body %s", response.StatusCode, response.Body)
	}
	draftBody := `{"title":"Service Status","version":0,"components":[{"monitorId":"` + monitorID + `","label":"Public API"}]}`
	if response := environment.request(t, environment.client, http.MethodPut, draftPath, draftBody, environment.server.URL, antiforgery); response.StatusCode != http.StatusOK {
		t.Fatalf("create draft = %d, body %s", response.StatusCode, response.Body)
	}
	if response := environment.request(t, environment.client, http.MethodPost, publicationPath, "", "https://wrong.example", antiforgery); response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin publish = %d, want 403", response.StatusCode)
	}
	if response := environment.request(t, environment.client, http.MethodPost, publicationPath, "", environment.server.URL, ""); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("publish without antiforgery = %d, want 400", response.StatusCode)
	}
	published := environment.request(t, environment.client, http.MethodPost, publicationPath, "", environment.server.URL, antiforgery)
	if published.StatusCode != http.StatusCreated {
		t.Fatalf("publish = %d, body %s", published.StatusCode, published.Body)
	}
	var publication api.PublishStatusPageResponse
	if err := json.Unmarshal(published.Body, &publication); err != nil {
		t.Fatal(err)
	}
	publicURL, err := url.Parse(publication.PublicURL)
	if err != nil || publicURL.Host == "" || !containsFold(publicURL.Path, "/status/") {
		t.Fatalf("public URL = %q, error %v", publication.PublicURL, err)
	}
	token := publicURL.Path[len("/status/"):]
	apiPath := "/api/v1/status-pages/" + token

	draftResponse := environment.request(t, environment.client, http.MethodGet, draftPath, "", "", "")
	if containsFold(string(draftResponse.Body), token) || containsFold(string(draftResponse.Body), "publicUrl") {
		t.Fatalf("draft disclosed publication capability: %s", draftResponse.Body)
	}
	environment.statusPages.mu.Lock()
	environment.statusPages.publicPage = statuspage.PublicPage{
		Title: "Service Status",
		Components: []statuspage.PublicComponent{{
			Label: "Public API", State: "down", UpdatedAt: environment.clock.Now(), Maintenance: true,
		}},
	}
	environment.statusPages.mu.Unlock()
	anonymous := environment.request(t, &http.Client{}, http.MethodGet, apiPath, "", "", "")
	if anonymous.StatusCode != http.StatusOK || anonymous.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("anonymous read = %d, headers %#v, body %s", anonymous.StatusCode, anonymous.Header, anonymous.Body)
	}
	var page api.PublicStatusPageResponse
	if err = json.Unmarshal(anonymous.Body, &page); err != nil {
		t.Fatal(err)
	}
	if page.Title != "Service Status" || len(page.Components) != 1 ||
		page.Components[0].Label != "Public API" || page.Components[0].State != "down" ||
		!page.Components[0].Maintenance {
		t.Fatalf("public page = %#v", page)
	}
	for _, forbidden := range []string{"organizationId", "monitorId", "target", "runId", "incident", "alert", "secret", "token"} {
		if containsFold(string(anonymous.Body), forbidden) {
			t.Fatalf("anonymous response disclosed %q: %s", forbidden, anonymous.Body)
		}
	}
	if response := environment.request(t, environment.client, http.MethodPost, publicationPath, "", environment.server.URL, antiforgery); response.StatusCode != http.StatusConflict || decodeProblem(t, response).Code != statuspage.AlreadyPublishedCode {
		t.Fatalf("duplicate publish = %d, body %s", response.StatusCode, response.Body)
	}
	if response := environment.request(t, environment.client, http.MethodDelete, publicationPath, "", "https://wrong.example", antiforgery); response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin revoke = %d, want 403", response.StatusCode)
	}
	if response := environment.request(t, environment.client, http.MethodDelete, publicationPath, "", environment.server.URL, antiforgery); response.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke = %d, body %s", response.StatusCode, response.Body)
	}
	if response := environment.request(t, &http.Client{}, http.MethodGet, apiPath, "", "", ""); response.StatusCode != http.StatusNotFound || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("revoked read = %d, headers %#v", response.StatusCode, response.Header)
	}
	republished := environment.request(t, environment.client, http.MethodPost, publicationPath, "", environment.server.URL, antiforgery)
	var rotated api.PublishStatusPageResponse
	if err = json.Unmarshal(republished.Body, &rotated); err != nil {
		t.Fatal(err)
	}
	if republished.StatusCode != http.StatusCreated || rotated.PublicURL == publication.PublicURL {
		t.Fatalf("republish = %d, %#v", republished.StatusCode, rotated)
	}
}

func TestPublicStatusPageReadIsRateLimitedAndLogPathIsRedacted(t *testing.T) {
	environment := newTestEnvironment(t, true, 0, func(config *Config) {
		config.PublicStatusReadsPerMinute = 1
	})
	path := "/api/v1/status-pages/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	first := environment.request(t, &http.Client{}, http.MethodGet, path, "", "", "")
	second := environment.request(t, &http.Client{}, http.MethodGet, path, "", "", "")
	if first.StatusCode != http.StatusNotFound || second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("rate-limited reads = %d, %d", first.StatusCode, second.StatusCode)
	}
	request, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := requestLogPath(request); got != "/api/v1/status-pages/{publicationToken}" || containsFold(got, "AAAA") {
		t.Fatalf("requestLogPath() = %q", got)
	}
}

func containsFold(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		match := true
		for offset := range len(substring) {
			left, right := value[index+offset], substring[offset]
			if left >= 'A' && left <= 'Z' {
				left += 'a' - 'A'
			}
			if right >= 'A' && right <= 'Z' {
				right += 'a' - 'A'
			}
			if left != right {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
