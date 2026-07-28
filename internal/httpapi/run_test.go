package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/run"
)

type memoryRunStore struct {
	mu           sync.Mutex
	monitors     *memoryMonitorStore
	values       []run.Run
	observations map[string]run.Observation
}

func newMemoryRunStore(monitors *memoryMonitorStore) *memoryRunStore {
	return &memoryRunStore{
		monitors: monitors, observations: make(map[string]run.Observation),
	}
}

func (store *memoryRunStore) MonitorExists(ctx context.Context, scope run.Scope) (bool, error) {
	_, found, err := store.monitors.FindMonitor(ctx, monitor.Scope{
		OrganizationID: scope.OrganizationID,
		ProjectID:      scope.ProjectID,
		MonitorID:      monitor.ID(scope.MonitorID),
	})
	return found, err
}

func (store *memoryRunStore) ListRuns(
	_ context.Context, scope run.Scope, query run.ListQuery,
) ([]run.Run, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	values := make([]run.Run, 0)
	for _, value := range store.values {
		if value.Slot.OrganizationID != scope.OrganizationID ||
			value.Slot.MonitorID != scope.MonitorID ||
			value.Slot.ScheduledFor.Before(query.NotBefore) {
			continue
		}
		if query.Cursor != nil {
			afterCursor := value.Slot.ScheduledFor.Before(query.Cursor.ScheduledFor) ||
				(value.Slot.ScheduledFor.Equal(query.Cursor.ScheduledFor) && value.ID < query.Cursor.ID)
			if !afterCursor {
				continue
			}
		}
		if query.Outcome != "" && value.Outcome != query.Outcome {
			continue
		}
		if query.Kind != "" && value.Kind != query.Kind {
			continue
		}
		if query.Location != "" && value.Slot.Location != query.Location {
			continue
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Slot.ScheduledFor.Equal(values[j].Slot.ScheduledFor) {
			return values[i].ID > values[j].ID
		}
		return values[i].Slot.ScheduledFor.After(values[j].Slot.ScheduledFor)
	})
	more := len(values) > query.PageSize
	if more {
		values = values[:query.PageSize]
	}
	return values, more, nil
}

func (store *memoryRunStore) FindScopedRun(
	ctx context.Context, scope run.Scope, id run.ID,
) (run.Run, bool, error) {
	exists, err := store.MonitorExists(ctx, scope)
	if err != nil || !exists {
		return run.Run{}, false, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, value := range store.values {
		if value.ID == id &&
			value.Slot.OrganizationID == scope.OrganizationID &&
			value.Slot.MonitorID == scope.MonitorID {
			return value, true, nil
		}
	}
	return run.Run{}, false, nil
}

func (store *memoryRunStore) FindObservation(
	_ context.Context, organizationID string, id run.ID, scheduledFor time.Time,
) (run.Observation, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, found := store.observations[memoryObservationKey(organizationID, id, scheduledFor)]
	return value, found, nil
}

func (store *memoryRunStore) seed(value run.Run, observation *run.Observation) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.values = append(store.values, value)
	if observation != nil {
		store.observations[memoryObservationKey(
			observation.OrganizationID, observation.RunID, observation.ScheduledFor,
		)] = *observation
	}
}

func memoryObservationKey(organizationID string, id run.ID, scheduledFor time.Time) string {
	return organizationID + "/" + string(id) + "/" + scheduledFor.UTC().Format(time.RFC3339Nano)
}

func seedRunQueryMonitor(t *testing.T, environment *testEnvironment) (string, string, string) {
	t.Helper()
	const organizationID = "00000000-0000-7000-8000-000000000002"
	const projectID = "00000000-0000-7000-8000-000000000003"
	const monitorID = "00000000-0000-7000-8000-000000000010"
	now := environment.clock.Now()
	value, err := monitor.RestoreMonitor(
		monitor.ID(monitorID), organizationID, projectID, "Checkout", "http",
		monitor.StateActive, 60, 1, now, now, 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	environment.monitors.seed(value)
	return organizationID, projectID, monitorID
}

func completedHTTPRun(
	t *testing.T, id, organizationID, monitorID string, scheduledFor time.Time, outcome run.Outcome,
) run.Run {
	t.Helper()
	value, err := run.Claim(
		run.ID(id),
		run.Slot{
			OrganizationID: organizationID,
			MonitorID:      monitorID,
			RevisionNumber: 1,
			Location:       "local",
			ScheduledFor:   scheduledFor,
		},
		run.KindScheduled,
		"worker-secret",
		scheduledFor.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Complete(outcome, scheduledFor, scheduledFor.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestRunQueryAPIListsPagesFiltersAndReturnsObservation(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	environment.bootstrapAdministrator(t)
	organizationID, projectID, monitorID := seedRunQueryMonitor(t, environment)
	now := environment.clock.Now()

	first := completedHTTPRun(
		t, "00000000-0000-7000-8000-000000000020",
		organizationID, monitorID, now, run.OutcomePassed,
	)
	second := completedHTTPRun(
		t, "00000000-0000-7000-8000-000000000019",
		organizationID, monitorID, now.Add(-time.Minute), run.OutcomeFailed,
	)
	skipped, err := run.Skip(
		"00000000-0000-7000-8000-000000000018",
		run.Slot{
			OrganizationID: organizationID, MonitorID: monitorID, RevisionNumber: 1,
			Location: "local", ScheduledFor: now.Add(-2 * time.Minute),
		},
		run.KindScheduled,
	)
	if err != nil {
		t.Fatal(err)
	}
	observation := run.Observation{
		RunID: first.ID, ScheduledFor: first.Slot.ScheduledFor, OrganizationID: organizationID,
		Duration: 1200 * time.Microsecond,
		Phases: run.Phases{
			Connect: 100 * time.Microsecond, TLS: 200 * time.Microsecond,
			FirstByte: 700 * time.Microsecond,
		},
		HTTP: &run.HTTPDetail{
			StatusCode: 200, Protocol: "HTTP/2.0", BodyBytes: 42,
			TLS: &run.TLSDetail{
				Version: "TLS 1.3", CipherSuite: "TLS_AES_128_GCM_SHA256",
				CertificateExpiresAt: now.Add(30 * 24 * time.Hour),
			},
		},
	}
	environment.runs.seed(first, &observation)
	environment.runs.seed(second, nil)
	environment.runs.seed(skipped, nil)

	base := "/api/v1/organizations/" + organizationID + "/projects/" + projectID +
		"/monitors/" + monitorID + "/runs"
	notBefore := url.QueryEscape(now.Add(-time.Hour).Format(time.RFC3339))
	response := environment.request(
		t, environment.client, http.MethodGet,
		base+"?notBefore="+notBefore+"&pageSize=2", "", "", "",
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("first Run page = %d, body %s", response.StatusCode, response.Body)
	}
	if strings.Contains(string(response.Body), "worker-secret") ||
		strings.Contains(string(response.Body), "leaseHolder") {
		t.Fatalf("Run response disclosed a lease holder: %s", response.Body)
	}
	var firstPage api.RunPageResponse
	if err := json.Unmarshal(response.Body, &firstPage); err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Items) != 2 || firstPage.Items[0].ID != string(first.ID) ||
		firstPage.Items[1].ID != string(second.ID) || firstPage.NextCursor == nil {
		t.Fatalf("first Run page = %#v", firstPage)
	}

	response = environment.request(
		t, environment.client, http.MethodGet,
		base+"?notBefore="+notBefore+"&pageSize=2&cursor="+url.QueryEscape(*firstPage.NextCursor),
		"", "", "",
	)
	var secondPage api.RunPageResponse
	if response.StatusCode != http.StatusOK {
		t.Fatalf("second Run page = %d, body %s", response.StatusCode, response.Body)
	}
	if err := json.Unmarshal(response.Body, &secondPage); err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0].ID != string(skipped.ID) ||
		secondPage.NextCursor != nil {
		t.Fatalf("second Run page = %#v", secondPage)
	}

	filtered := environment.request(
		t, environment.client, http.MethodGet,
		base+"?notBefore="+notBefore+"&outcome=passed&kind=scheduled&location=local",
		"", "", "",
	)
	var filteredPage api.RunPageResponse
	if err := json.Unmarshal(filtered.Body, &filteredPage); err != nil {
		t.Fatal(err)
	}
	if filtered.StatusCode != http.StatusOK || len(filteredPage.Items) != 1 ||
		filteredPage.Items[0].ID != string(first.ID) {
		t.Fatalf("filtered Run page = %d, %#v", filtered.StatusCode, filteredPage)
	}

	item := environment.request(
		t, environment.client, http.MethodGet, base+"/"+string(first.ID), "", "", "",
	)
	var runResponse api.RunResponse
	if err := json.Unmarshal(item.Body, &runResponse); err != nil {
		t.Fatal(err)
	}
	if item.StatusCode != http.StatusOK || runResponse.Outcome == nil ||
		*runResponse.Outcome != "passed" || runResponse.LeaseExpiresAt != nil {
		t.Fatalf("Run item = %d, %#v", item.StatusCode, runResponse)
	}

	detail := environment.request(
		t, environment.client, http.MethodGet,
		base+"/"+string(first.ID)+"/observation", "", "", "",
	)
	var observationResponse api.ObservationResponse
	if err := json.Unmarshal(detail.Body, &observationResponse); err != nil {
		t.Fatal(err)
	}
	if detail.StatusCode != http.StatusOK ||
		observationResponse.DurationMicroseconds != 1200 ||
		observationResponse.HTTP == nil ||
		observationResponse.HTTP.TLS == nil ||
		observationResponse.HTTP.StatusCode != 200 {
		t.Fatalf("Observation = %d, %#v", detail.StatusCode, observationResponse)
	}

	noDetail := environment.request(
		t, environment.client, http.MethodGet,
		base+"/"+string(skipped.ID)+"/observation", "", "", "",
	)
	assertProblem(t, noDetail, http.StatusNotFound, "Not Found", "")
}

func TestRunQueryAPIValidatesParametersAndFullScope(t *testing.T) {
	environment := newTestEnvironment(t, true, 0)
	environment.bootstrapAdministrator(t)
	organizationID, projectID, monitorID := seedRunQueryMonitor(t, environment)
	environment.grantMembership(t, organizationID, organization.RoleViewer)
	now := environment.clock.Now()
	value := completedHTTPRun(
		t, "00000000-0000-7000-8000-000000000020",
		organizationID, monitorID, now, run.OutcomePassed,
	)
	environment.runs.seed(value, &run.Observation{
		RunID: value.ID, ScheduledFor: now, OrganizationID: organizationID,
	})

	base := "/api/v1/organizations/" + organizationID + "/projects/" + projectID +
		"/monitors/" + monitorID + "/runs"
	validTime := url.QueryEscape(now.Add(-time.Hour).Format(time.RFC3339))
	cases := []struct {
		query string
		field string
		code  string
	}{
		{"", "notBefore", runNotBeforeInvalidCode},
		{"?notBefore=tomorrow", "notBefore", runNotBeforeInvalidCode},
		{"?notBefore=" + validTime + "&pageSize=0", "pageSize", runPageSizeInvalidCode},
		{"?notBefore=" + validTime + "&pageSize=1&pageSize=2", "pageSize", runPageSizeInvalidCode},
		{"?notBefore=" + validTime + "&cursor=not-a-cursor", "cursor", runCursorInvalidCode},
		{"?notBefore=" + validTime + "&outcome=unknown", "outcome", runOutcomeInvalidCode},
		{"?notBefore=" + validTime + "&kind=unknown", "kind", runKindInvalidCode},
		{"?notBefore=" + validTime + "&location=" + strings.Repeat("x", 64), "location", runLocationInvalidCode},
	}
	for _, testCase := range cases {
		response := environment.request(
			t, environment.client, http.MethodGet, base+testCase.query, "", "", "",
		)
		problem := decodeProblem(t, response)
		if response.StatusCode != http.StatusBadRequest ||
			len(problem.Errors[testCase.field]) != 1 ||
			problem.Errors[testCase.field][0].Code != testCase.code {
			t.Errorf("%s = %d, %#v", testCase.query, response.StatusCode, problem)
		}
	}

	viewerRead := environment.request(
		t, environment.client, http.MethodGet,
		base+"?notBefore="+validTime, "", "", "",
	)
	if viewerRead.StatusCode != http.StatusOK {
		t.Fatalf("Viewer Run read = %d, body %s", viewerRead.StatusCode, viewerRead.Body)
	}

	wrongProject := environment.request(
		t, environment.client, http.MethodGet,
		"/api/v1/organizations/"+organizationID+
			"/projects/00000000-0000-7000-8000-000000000099/monitors/"+
			monitorID+"/runs/"+string(value.ID),
		"", "", "",
	)
	assertProblem(t, wrongProject, http.StatusNotFound, "Not Found", "")

	wrongMonitor := environment.request(
		t, environment.client, http.MethodGet,
		"/api/v1/organizations/"+organizationID+"/projects/"+projectID+
			"/monitors/00000000-0000-7000-8000-000000000099/runs/"+string(value.ID),
		"", "", "",
	)
	assertProblem(t, wrongMonitor, http.StatusNotFound, "Not Found", "")
}
