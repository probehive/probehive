package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/webhook"
)

func TestAlertDeliveryAuditEnforcesScopeAndOmitsSensitiveMaterial(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	environment.bootstrapAdministrator(t)
	organizationID, projectID, monitorID := seedRunQueryMonitor(t, environment)
	alertID := "00000000-0000-7000-8000-000000000220"
	deliveryID := "00000000-0000-7000-8000-000000000221"
	integrationID := "00000000-0000-7000-8000-000000000222"
	suppressedDeliveryID := "00000000-0000-7000-8000-000000000223"
	maintenanceWindowID := "00000000-0000-7000-8000-000000000224"
	now := environment.clock.Now().UTC()
	firstFinished := now.Add(-30 * time.Second)
	secondFinished := now.Add(-10 * time.Second)
	firstStatus, secondStatus := http.StatusServiceUnavailable, http.StatusNoContent
	scope := webhook.DeliveryScope{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		MonitorID:      monitorID,
		AlertID:        alertID,
	}
	environment.webhooks.setAudit(scope, []webhook.DeliveryAudit{{
		DeliveryID:         deliveryID,
		IntegrationID:      integrationID,
		IntegrationVersion: 4,
		SecretVersion:      2,
		RoutedAt:           now.Add(-time.Minute),
		Attempts: []webhook.DeliveryAttempt{
			{
				Sequence: 1, StartedAt: now.Add(-40 * time.Second),
				FinishedAt: &firstFinished, Outcome: webhook.OutcomeFailed,
				HTTPStatus:  &firstStatus,
				FailureCode: webhook.FailureCodeHTTPRetryable,
			},
			{
				Sequence: 2, StartedAt: now.Add(-20 * time.Second),
				FinishedAt: &secondFinished, Outcome: webhook.OutcomeSucceeded,
				HTTPStatus: &secondStatus,
			},
		},
	}, {
		DeliveryID:          suppressedDeliveryID,
		IntegrationID:       integrationID,
		IntegrationVersion:  4,
		SecretVersion:       2,
		RoutedAt:            now.Add(-time.Minute),
		SuppressionReason:   webhook.SuppressionReasonMaintenance,
		MaintenanceWindowID: maintenanceWindowID,
	}})

	path := "/api/v1/organizations/" + organizationID + "/projects/" + projectID +
		"/monitors/" + monitorID + "/alerts/" + alertID + "/deliveries"
	response := environment.request(
		t, environment.client, http.MethodGet, path, "", "", "",
	)
	var page api.AlertDeliveryPageResponse
	if err := json.Unmarshal(response.Body, &page); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || len(page.Items) != 2 ||
		page.Items[0].ID != deliveryID ||
		page.Items[0].Channel != webhook.DeliveryChannel ||
		len(page.Items[0].Attempts) != 2 ||
		page.Items[0].Attempts[0].FailureCode == nil ||
		*page.Items[0].Attempts[0].FailureCode != webhook.FailureCodeHTTPRetryable ||
		page.Items[0].Attempts[1].FailureCode != nil ||
		page.Items[1].SuppressionReason == nil ||
		*page.Items[1].SuppressionReason != webhook.SuppressionReasonMaintenance ||
		page.Items[1].MaintenanceWindowID == nil ||
		*page.Items[1].MaintenanceWindowID != maintenanceWindowID ||
		len(page.Items[1].Attempts) != 0 {
		t.Fatalf("delivery audit = %d, %#v", response.StatusCode, page)
	}
	for _, forbidden := range []string{
		"hooks.example.test", "destinationUrl", "ciphertext",
		"signingSecret", "provider text",
	} {
		if strings.Contains(string(response.Body), forbidden) {
			t.Errorf("response exposed %q: %s", forbidden, response.Body)
		}
	}

	environment.grantMembership(t, organizationID, organization.RoleViewer)
	viewer := environment.request(
		t, environment.client, http.MethodGet, path, "", "", "",
	)
	if viewer.StatusCode != http.StatusOK {
		t.Fatalf("Viewer delivery audit = %d, body %s", viewer.StatusCode, viewer.Body)
	}
	wrongProject := environment.request(
		t, environment.client, http.MethodGet,
		"/api/v1/organizations/"+organizationID+
			"/projects/00000000-0000-7000-8000-000000000299/monitors/"+
			monitorID+"/alerts/"+alertID+"/deliveries",
		"", "", "",
	)
	assertProblem(t, wrongProject, http.StatusNotFound, "Not Found", "")

	environment.organizations.setMembership(
		organization.ID(organizationID), environment.administratorID(t),
		organization.RoleAdministrator, false,
	)
	hidden := environment.request(
		t, environment.client, http.MethodGet, path, "", "", "",
	)
	assertProblem(t, hidden, http.StatusNotFound, "Not Found", "")
}

func TestAlertDeliveryAuditRejectsInvalidIdentityAndMethod(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	environment.bootstrapAdministrator(t)
	organizationID, projectID, monitorID := seedRunQueryMonitor(t, environment)
	root := "/api/v1/organizations/" + organizationID + "/projects/" + projectID +
		"/monitors/" + monitorID + "/alerts/"
	invalid := environment.request(
		t, environment.client, http.MethodGet, root+"invalid/deliveries", "", "", "",
	)
	assertProblem(t, invalid, http.StatusNotFound, "Not Found", "")

	method := environment.request(
		t, environment.client, http.MethodPost,
		root+"00000000-0000-7000-8000-000000000220/deliveries",
		"{}", "", "",
	)
	if method.StatusCode != http.StatusMethodNotAllowed ||
		method.Header.Get("Allow") != http.MethodGet {
		t.Fatalf("POST delivery audit = %d, headers %#v", method.StatusCode, method.Header)
	}
}
