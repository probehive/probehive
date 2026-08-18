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
)

func TestMonitorInventoryReturnsBoundedScopedPage(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	environment.bootstrapAdministrator(t)
	organizationID := "00000000-0000-7000-8000-000000000101"
	projectID := "00000000-0000-7000-8000-000000000102"
	createdAt := environment.clock.Now()
	value, err := monitor.RestoreMonitor(
		"00000000-0000-7000-8000-000000000103", organizationID, projectID,
		"API Gateway", "http", monitor.StateDraft, 60, 0, createdAt, createdAt, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	environment.monitors.seed(value)
	environment.grantMembership(t, organizationID, organization.RoleAdministrator)
	path := "/api/v1/organizations/" + organizationID + "/projects/" + projectID +
		"/monitor-inventory?search=api&state=draft&health=notEvaluated&runOutcome=notRun&maintenance=none&sort=updatedAt&direction=desc&page=1&pageSize=10"
	response := environment.request(t, environment.client, http.MethodGet, path, "", "", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET inventory = %d, body %s", response.StatusCode, response.Body)
	}
	var page api.MonitorInventoryPageResponse
	if err := json.Unmarshal(response.Body, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Page != 1 || page.PageSize != 10 || len(page.Items) != 1 {
		t.Fatalf("inventory page = %#v", page)
	}
	item := page.Items[0]
	if item.Monitor.ID != string(value.ID) || item.Health != nil || item.LastRun != nil || item.Maintenance.State != "none" || item.Maintenance.WindowID != nil {
		t.Fatalf("inventory item = %#v", item)
	}
}

func TestMonitorInventoryRejectsEveryInvalidQueryDimension(t *testing.T) {
	tests := []struct{ query, field, code string }{
		{"search=%20", "search", monitorInventorySearchInvalidCode},
		{"state=running", "state", monitorInventoryStateInvalidCode},
		{"health=stale", "health", monitorInventoryHealthInvalidCode},
		{"runOutcome=unknown", "runOutcome", monitorInventoryRunInvalidCode},
		{"maintenance=ended", "maintenance", monitorInventoryMaintenanceInvalidCode},
		{"sort=health", "sort", monitorInventorySortInvalidCode},
		{"direction=forward", "direction", monitorInventoryDirectionInvalidCode},
		{"page=0", "page", monitorInventoryPageInvalidCode},
		{"pageSize=101", "pageSize", monitorInventoryPageSizeInvalidCode},
	}
	for _, test := range tests {
		t.Run(test.field, func(t *testing.T) {
			_, failures := parseMonitorInventoryQuery(mustQuery(t, test.query))
			if len(failures) != 1 || failures[0][0] != test.code || failures[0][1] != test.field {
				t.Fatalf("failures = %#v", failures)
			}
		})
	}
	_, failures := parseMonitorInventoryQuery(mustQuery(t, "page=1&page=2"))
	if len(failures) != 1 || failures[0][0] != monitorInventoryPageInvalidCode {
		t.Fatalf("duplicate page failures = %#v", failures)
	}
}

func TestMonitorInventoryWireKeepsOperationalDimensionsSeparate(t *testing.T) {
	now := time.Date(2026, 8, 18, 1, 0, 0, 0, time.UTC)
	value, err := monitor.RestoreMonitor("monitor", "org", "project", "API", "http", monitor.StatePaused, 60, 1, now, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	response := toMonitorInventoryItemResponse(monitor.InventoryItem{
		Monitor:     value,
		Health:      &monitor.InventoryHealth{State: "down", UpdatedAt: now},
		LastRun:     &monitor.InventoryRun{ID: "run", ScheduledFor: now},
		Maintenance: monitor.InventoryMaintenance{State: monitor.InventoryMaintenanceActive, WindowID: "window", StartsAt: now, EndsAt: now.Add(time.Hour)},
	})
	if response.Monitor.State != "paused" || response.Health == nil || response.Health.State != "down" || response.LastRun == nil || response.LastRun.Outcome != "inProgress" || response.Maintenance.State != "active" || response.Maintenance.WindowID == nil {
		t.Fatalf("response = %#v", response)
	}
}

func mustQuery(t *testing.T, value string) url.Values {
	t.Helper()
	parsed, err := url.ParseQuery(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
