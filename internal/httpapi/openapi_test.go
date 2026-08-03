package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestOpenAPIDocumentDescribesEveryRoute(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	response := environment.request(t, environment.client, http.MethodGet, "/openapi/v1.json", "", "", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("OpenAPI status = %d, body %s", response.StatusCode, response.Body)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("OpenAPI Content-Type = %q", contentType)
	}

	var document struct {
		OpenAPI string                                `json:"openapi"`
		Paths   map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(response.Body, &document); err != nil {
		t.Fatalf("decode OpenAPI document: %v", err)
	}
	if document.OpenAPI != "3.1.0" {
		t.Fatalf("OpenAPI version = %q", document.OpenAPI)
	}

	expected := map[string][]string{
		"/healthz":                                    {"get", "head"},
		"/readyz":                                     {"get", "head"},
		"/openapi/v1.json":                            {"get", "head"},
		"/api/v1/setup/status":                        {"get"},
		"/api/v1/setup/admin":                         {"post"},
		"/api/v1/auth/antiforgery":                    {"get"},
		"/api/v1/auth/login":                          {"post"},
		"/api/v1/auth/logout":                         {"post"},
		"/api/v1/auth/session":                        {"get"},
		"/api/v1/organizations":                       {"get", "post"},
		"/api/v1/organizations/{organizationId}":      {"get"},
		"/api/v1/organizations/{organizationId}/name": {"put"},
		"/api/v1/organizations/{organizationId}/webhook-integrations":                                                         {"get", "post"},
		"/api/v1/organizations/{organizationId}/webhook-integrations/{integrationId}/state":                                   {"put"},
		"/api/v1/organizations/{organizationId}/webhook-integrations/{integrationId}/signing-secrets/prepare":                 {"post"},
		"/api/v1/organizations/{organizationId}/webhook-integrations/{integrationId}/signing-secrets/activate":                {"post"},
		"/api/v1/organizations/{organizationId}/webhook-integrations/{integrationId}/signing-secrets/retire":                  {"post"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors":                                                {"get", "post"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}":                                    {"get"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/name":                               {"put"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/state":                              {"put"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/interval":                           {"put"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions":                          {"get", "post"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions/{revisionNumber}":         {"get"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/alerts/{alertId}/deliveries":        {"get"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/health":                             {"get"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/incidents":                          {"get"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/alerts":                             {"get"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/incidents/{incidentId}":             {"get"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/incidents/{incidentId}/acknowledge": {"post"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/runs":                               {"get", "post"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/runs/{runId}":                       {"get"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/runs/{runId}/observation":           {"get"},
	}
	httpMethods := map[string]struct{}{
		"get": {}, "put": {}, "post": {}, "delete": {}, "options": {}, "head": {}, "patch": {}, "trace": {},
	}
	if len(document.Paths) != len(expected) {
		t.Fatalf("OpenAPI path count = %d, want %d", len(document.Paths), len(expected))
	}
	for path, methods := range expected {
		operations, found := document.Paths[path]
		if !found {
			t.Errorf("OpenAPI omits %s", path)
			continue
		}
		actualMethodCount := 0
		for operation := range operations {
			if _, isMethod := httpMethods[operation]; isMethod {
				actualMethodCount++
			}
		}
		if actualMethodCount != len(methods) {
			t.Errorf("OpenAPI method count for %s = %d, want %d", path, actualMethodCount, len(methods))
		}
		for _, method := range methods {
			if len(operations[method]) == 0 {
				t.Errorf("OpenAPI omits %s %s", method, path)
			}
		}
	}
}

func TestOpenAPIDocumentLocksValidationBoundaries(t *testing.T) {
	type property struct {
		Format               string          `json:"format"`
		Minimum              int             `json:"minimum"`
		Pattern              string          `json:"pattern"`
		Maximum              int             `json:"maximum"`
		Default              json.RawMessage `json:"default"`
		MinLength            int             `json:"minLength"`
		MaxBytes             int             `json:"x-probehive-max-bytes"`
		LengthUnit           string          `json:"x-probehive-length-unit"`
		UniqueNameComparison string          `json:"x-probehive-unique-name-comparison"`
		ForbiddenNames       []string        `json:"x-probehive-forbidden-names"`
	}
	type schema struct {
		PropertyNameComparison string              `json:"x-probehive-property-name-comparison"`
		Properties             map[string]property `json:"properties"`
	}
	var document struct {
		Components struct {
			Parameters map[string]struct {
				Required bool     `json:"required"`
				Schema   property `json:"schema"`
			} `json:"parameters"`
			Schemas map[string]schema `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(openAPIDocument, &document); err != nil {
		t.Fatalf("decode embedded OpenAPI document: %v", err)
	}

	revision := document.Components.Parameters["RevisionNumber"].Schema
	if revision.Format != "int32" || revision.Maximum != 2147483647 {
		t.Fatalf("RevisionNumber schema = %#v", revision)
	}

	notBefore := document.Components.Parameters["RunNotBefore"]
	if !notBefore.Required || notBefore.Schema.Format != "date-time" {
		t.Fatalf("RunNotBefore parameter = %#v", notBefore)
	}
	pageSize := document.Components.Parameters["RunPageSize"].Schema
	if pageSize.Minimum != 1 || pageSize.Maximum != 500 || string(pageSize.Default) != "50" {
		t.Fatalf("RunPageSize schema = %#v", pageSize)
	}
	incidentPageSize := document.Components.Parameters["IncidentPageSize"].Schema
	if incidentPageSize.Minimum != 1 || incidentPageSize.Maximum != 100 || string(incidentPageSize.Default) != "50" {
		t.Fatalf("IncidentPageSize schema = %#v", incidentPageSize)
	}
	alertPageSize := document.Components.Parameters["AlertPageSize"].Schema
	if alertPageSize.Minimum != 1 || alertPageSize.Maximum != 100 || string(alertPageSize.Default) != "50" {
		t.Fatalf("AlertPageSize schema = %#v", alertPageSize)
	}
	location := document.Components.Parameters["RunLocation"].Schema
	if location.MinLength != 1 || location.MaxBytes != 63 {
		t.Fatalf("RunLocation schema = %#v", location)
	}
	for _, name := range []string{
		"RunPageResponse", "RunResponse", "ConfirmationCauseResponse", "ObservationResponse",
		"MonitorHealthResponse", "HealthCountsResponse", "IncidentPageResponse", "IncidentResponse", "IncidentTimelineResponse",
		"AlertPageResponse", "AlertResponse", "AlertDeliveryPageResponse",
		"AlertDeliveryResponse", "DeliveryAttemptResponse",
		"WebhookIntegrationResponse", "CreateWebhookIntegrationResponse", "CreateWebhookIntegrationRequest",
		"PrepareWebhookSigningSecretResponse", "WebhookIntegrationVersionRequest", "WebhookIntegrationStateRequest",
		"ObservationPhasesResponse", "HTTPObservationResponse", "TLSObservationResponse",
	} {
		if _, found := document.Components.Schemas[name]; !found {
			t.Errorf("OpenAPI omits %s", name)
		}
	}

	requestSchemas := []string{
		"CreateFirstAdministratorRequest", "LoginRequest", "CreateOrganizationRequest",
		"CreateWebhookIntegrationRequest",
		"WebhookIntegrationVersionRequest", "WebhookIntegrationStateRequest",
		"CreateMonitorRequest", "RenameMonitorRequest", "ChangeMonitorStateRequest",
		"CreateMonitorRevisionRequest",
	}
	for _, name := range requestSchemas {
		if got := document.Components.Schemas[name].PropertyNameComparison; got != "ascii-case-insensitive" {
			t.Errorf("%s property-name comparison = %q", name, got)
		}
	}

	webhookRequest := document.Components.Schemas["CreateWebhookIntegrationRequest"]
	webhookName := webhookRequest.Properties["name"]
	webhookDestination := webhookRequest.Properties["destinationUrl"]
	if webhookName.LengthUnit != "utf16-code-units" ||
		webhookDestination.Pattern != "^https://[^?#]+$" || webhookDestination.MaxBytes != 2048 {
		t.Errorf("Webhook request schema = name %#v, destination %#v", webhookName, webhookDestination)
	}
	webhookResponse := document.Components.Schemas["WebhookIntegrationResponse"]
	if destination := webhookResponse.Properties["destinationUrl"]; destination.Pattern != "^https://[^?#]+$" || destination.MaxBytes != 2048 {
		t.Errorf("Webhook response destination schema = %#v", destination)
	}
	createdWebhook := document.Components.Schemas["CreateWebhookIntegrationResponse"]
	if signature := createdWebhook.Properties["signingSecret"]; signature.Pattern != "^phwh_[A-Za-z0-9_-]{43}$" {
		t.Errorf("Webhook one-time secret schema = %#v", signature)
	}

	rotationRequest := document.Components.Schemas["WebhookIntegrationVersionRequest"]
	rotationVersion := rotationRequest.Properties["version"]
	if rotationVersion.Format != "int64" || rotationVersion.Minimum != 1 {
		t.Errorf("Webhook rotation version schema = %#v", rotationVersion)
	}
	stateRequest := document.Components.Schemas["WebhookIntegrationStateRequest"]
	stateVersion := stateRequest.Properties["version"]
	if _, found := stateRequest.Properties["enabled"]; !found ||
		stateVersion.Format != "int64" || stateVersion.Minimum != 1 {
		t.Errorf("Webhook state request schema = %#v", stateRequest)
	}
	preparedWebhook := document.Components.Schemas["PrepareWebhookSigningSecretResponse"]
	if signature := preparedWebhook.Properties["signingSecret"]; signature.Pattern != "^phwh_[A-Za-z0-9_-]{43}$" {
		t.Errorf("Webhook prepared one-time secret schema = %#v", signature)
	}

	httpConfiguration := document.Components.Schemas["HTTPCheckConfigurationV1"]
	url := httpConfiguration.Properties["url"]
	if url.Pattern != "^[Hh][Tt][Tt][Pp][Ss]?://" || url.LengthUnit != "utf16-code-units" {
		t.Errorf("HTTP URL schema = %#v", url)
	}
	headers := httpConfiguration.Properties["headers"]
	if headers.UniqueNameComparison != "ascii-case-insensitive" {
		t.Errorf("HTTP headers uniqueness = %q", headers.UniqueNameComparison)
	}

	header := document.Components.Schemas["HTTPHeader"]
	name := header.Properties["name"]
	if name.Pattern != "^[!#$%&'*+.^_`|~0-9A-Za-z-]+$" {
		t.Errorf("HTTP header-name pattern = %q", name.Pattern)
	}
	wantForbidden := []string{
		"authorization", "proxy-authorization", "cookie", "host", "content-length", "transfer-encoding",
	}
	if len(name.ForbiddenNames) != len(wantForbidden) {
		t.Fatalf("forbidden header names = %#v", name.ForbiddenNames)
	}
	for index := range wantForbidden {
		if name.ForbiddenNames[index] != wantForbidden[index] {
			t.Fatalf("forbidden header names = %#v", name.ForbiddenNames)
		}
	}
	value := header.Properties["value"]
	if value.Pattern != `^[^\u0000-\u001f\u007f-\u009f]*$` || value.LengthUnit != "utf16-code-units" {
		t.Errorf("HTTP header-value schema = %#v", value)
	}
}

func TestOpenAPIIsHiddenOutsideDevelopment(t *testing.T) {
	environment := newTestEnvironment(t, false, 0)
	response := environment.request(t, environment.client, http.MethodGet, "/openapi/v1.json", "", "", "")
	assertProblem(t, response, http.StatusNotFound, "Not Found", "")
}
