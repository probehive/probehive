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
		"/healthz":                               {"get", "head"},
		"/readyz":                                {"get", "head"},
		"/openapi/v1.json":                       {"get", "head"},
		"/api/v1/setup/status":                   {"get"},
		"/api/v1/setup/admin":                    {"post"},
		"/api/v1/auth/antiforgery":               {"get"},
		"/api/v1/auth/login":                     {"post"},
		"/api/v1/auth/logout":                    {"post"},
		"/api/v1/auth/session":                   {"get"},
		"/api/v1/organizations":                  {"post"},
		"/api/v1/organizations/{organizationId}": {"get"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors":                                        {"get", "post"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}":                            {"get"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/name":                       {"put"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/state":                      {"put"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions":                  {"get", "post"},
		"/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions/{revisionNumber}": {"get"},
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
		Format               string   `json:"format"`
		Pattern              string   `json:"pattern"`
		Maximum              int      `json:"maximum"`
		LengthUnit           string   `json:"x-probehive-length-unit"`
		UniqueNameComparison string   `json:"x-probehive-unique-name-comparison"`
		ForbiddenNames       []string `json:"x-probehive-forbidden-names"`
	}
	type schema struct {
		PropertyNameComparison string              `json:"x-probehive-property-name-comparison"`
		Properties             map[string]property `json:"properties"`
	}
	var document struct {
		Components struct {
			Parameters map[string]struct {
				Schema property `json:"schema"`
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

	requestSchemas := []string{
		"CreateFirstAdministratorRequest", "LoginRequest", "CreateOrganizationRequest",
		"CreateMonitorRequest", "RenameMonitorRequest", "ChangeMonitorStateRequest",
		"CreateMonitorRevisionRequest",
	}
	for _, name := range requestSchemas {
		if got := document.Components.Schemas[name].PropertyNameComparison; got != "ascii-case-insensitive" {
			t.Errorf("%s property-name comparison = %q", name, got)
		}
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
