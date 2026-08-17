package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/overview"
)

func TestOrganizationOverviewReturnsBoundedPermissionAwareSummary(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	environment.bootstrapAdministrator(t)
	organizationID, projectID := onlyOrganizationScope(t, environment)
	environment.overview.seed(overview.Summary{
		OrganizationID: organizationID,
		Monitors:       overview.MonitorCounts{Total: 6, Draft: 1, Active: 3, Paused: 1, Archived: 1},
		Health:         overview.HealthCounts{NotEvaluated: 1, Healthy: 1, Down: 1},
		Incidents:      overview.IncidentCounts{Active: 2, Open: 1, Acknowledged: 1},
		ActiveIncidents: []overview.ActiveIncident{
			{
				ID: "00000000-0000-7000-8000-000000000101", ProjectID: projectID,
				MonitorID:   "00000000-0000-7000-8000-000000000201",
				MonitorName: "Checkout", State: "open",
				UpdatedAt: time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC),
			},
			{
				ID: "00000000-0000-7000-8000-000000000102", ProjectID: projectID,
				MonitorID:   "00000000-0000-7000-8000-000000000202",
				MonitorName: "API", State: "acknowledged",
				UpdatedAt: time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC),
			},
		},
		Integrations: overview.IntegrationCounts{Total: 2, Enabled: 1},
		StatusPage:   overview.StatusPageState{Configured: true, Published: true},
	})

	path := "/api/v1/organizations/" + organizationID + "/overview"
	response := environment.request(t, environment.client, http.MethodGet, path, "", "", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("overview status = %d, body %s", response.StatusCode, response.Body)
	}
	var body api.OrganizationOverviewResponse
	if err := json.Unmarshal(response.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.OrganizationID != organizationID || body.Monitors == nil || body.Monitors.Total != 6 ||
		body.Health == nil || body.Health.NotEvaluated != 1 || body.Health.Down != 1 {
		t.Fatalf("monitoring summary = %#v", body)
	}
	if body.Incidents == nil || body.Incidents.Active != 2 ||
		len(body.Incidents.ActivePreview) != 2 || body.Incidents.ActivePreviewTruncated {
		t.Fatalf("Incident summary = %#v", body.Incidents)
	}
	if body.Incidents.ActivePreview[0].MonitorName != "Checkout" ||
		body.Incidents.ActivePreview[0].UpdatedAt.Location() != time.UTC {
		t.Fatalf("Incident preview = %#v", body.Incidents.ActivePreview)
	}
	if body.Integrations == nil || body.Integrations.Total != 2 || body.Integrations.Enabled != 1 ||
		body.StatusPage == nil || !body.StatusPage.Configured || !body.StatusPage.Published {
		t.Fatalf("administrative summary = %#v", body)
	}
	if !body.Capabilities.ManageOrganization || !body.Capabilities.ManageIntegrations ||
		!body.Capabilities.ManageStatusPage {
		t.Fatalf("administrator capabilities = %#v", body.Capabilities)
	}

	environment.grantMembership(t, organizationID, organization.RoleViewer)
	response = environment.request(t, environment.client, http.MethodGet, path, "", "", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Viewer overview status = %d, body %s", response.StatusCode, response.Body)
	}
	if err := json.Unmarshal(response.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.Monitors == nil || body.Health == nil || body.Incidents == nil {
		t.Fatalf("Viewer lost read evidence: %#v", body)
	}
	if body.Integrations != nil || body.StatusPage != nil ||
		body.Capabilities.ManageOrganization || body.Capabilities.ManageIntegrations ||
		body.Capabilities.ManageStatusPage {
		t.Fatalf("Viewer received administrative state: %#v", body)
	}
}

func TestOrganizationOverviewPreservesAuthenticationAndTenantNonDisclosure(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	environment.bootstrapAdministrator(t)
	organizationID, _ := onlyOrganizationScope(t, environment)
	path := "/api/v1/organizations/" + organizationID + "/overview"

	anonymous := environment.request(t, &http.Client{}, http.MethodGet, path, "", "", "")
	assertProblem(t, anonymous, http.StatusUnauthorized, "Unauthorized", "")

	environment.grantMembership(t, organizationID, organization.RoleViewer)
	environment.organizations.setMembership(
		organization.ID(organizationID), environment.administratorID(t), organization.RoleViewer, false,
	)
	nonMember := environment.request(t, environment.client, http.MethodGet, path, "", "", "")
	assertProblem(t, nonMember, http.StatusNotFound, "Not Found", "")

	wrongMethod := environment.request(t, environment.client, http.MethodPost, path, "", "", "")
	assertProblem(t, wrongMethod, http.StatusMethodNotAllowed, "Method Not Allowed", "")
}

func onlyOrganizationScope(t *testing.T, environment *testEnvironment) (string, string) {
	t.Helper()
	environment.organizations.mu.Lock()
	defer environment.organizations.mu.Unlock()
	if len(environment.organizations.byID) != 1 {
		t.Fatalf("Organization count = %d, want 1", len(environment.organizations.byID))
	}
	for organizationID := range environment.organizations.byID {
		for projectID, project := range environment.organizations.projects {
			if project.OrganizationID == organizationID && project.IsDefault {
				return string(organizationID), string(projectID)
			}
		}
	}
	t.Fatal("bootstrapped Organization has no default Project")
	return "", ""
}
