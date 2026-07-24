package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fixedClock struct{ value time.Time }

func (clock fixedClock) Now() time.Time { return clock.value }

type sequenceUUIDs struct {
	values []string
	times  []time.Time
}

func (generator *sequenceUUIDs) NewUUIDv7(at time.Time) (string, error) {
	generator.times = append(generator.times, at)
	value := generator.values[0]
	generator.values = generator.values[1:]
	return value, nil
}

type fakeChecks struct {
	supported     bool
	canonical     json.RawMessage
	failures      [][2]string
	validateCalls int
}

func (checks *fakeChecks) IsSupported(string) bool { return checks.supported }
func (checks *fakeChecks) Validate(_ string, _ int, _ json.RawMessage) (json.RawMessage, [][2]string) {
	checks.validateCalls++
	return checks.canonical, checks.failures
}

type fakeStore struct {
	projectExists    bool
	monitor          Monitor
	monitorFound     bool
	monitors         []Monitor
	revision         Revision
	revisionFound    bool
	revisions        []Revision
	updateErr        error
	appendErr        error
	created          []Monitor
	updated          []Monitor
	appended         []Revision
	expectedVersions []uint32
	projectCalls     int
	findCalls        int
	contextValue     any
}

func (store *fakeStore) ProjectExists(ctx context.Context, _, _ string) (bool, error) {
	store.contextValue = ctx.Value(contextKey{})
	store.projectCalls++
	return store.projectExists, nil
}
func (store *fakeStore) FindMonitor(ctx context.Context, _ Scope) (Monitor, bool, error) {
	store.contextValue = ctx.Value(contextKey{})
	store.findCalls++
	return store.monitor, store.monitorFound, nil
}
func (store *fakeStore) ListMonitors(context.Context, string, string) ([]Monitor, error) {
	return store.monitors, nil
}
func (store *fakeStore) CreateMonitor(ctx context.Context, value Monitor) error {
	store.contextValue = ctx.Value(contextKey{})
	store.created = append(store.created, value)
	return nil
}
func (store *fakeStore) UpdateMonitor(ctx context.Context, value Monitor, expectedVersion uint32) error {
	store.contextValue = ctx.Value(contextKey{})
	store.updated = append(store.updated, value)
	store.expectedVersions = append(store.expectedVersions, expectedVersion)
	return store.updateErr
}
func (store *fakeStore) AppendRevision(ctx context.Context, value Monitor, revision Revision, expectedVersion uint32) error {
	store.contextValue = ctx.Value(contextKey{})
	store.updated = append(store.updated, value)
	store.appended = append(store.appended, revision)
	store.expectedVersions = append(store.expectedVersions, expectedVersion)
	return store.appendErr
}
func (store *fakeStore) FindRevision(context.Context, string, ID, int) (Revision, bool, error) {
	return store.revision, store.revisionFound, nil
}
func (store *fakeStore) ListRevisions(context.Context, string, ID) ([]Revision, error) {
	return store.revisions, nil
}

type contextKey struct{}

func newService(store *fakeStore, checks *fakeChecks, now time.Time, ids ...string) *Service {
	return NewService(store, checks, fixedClock{now}, &sequenceUUIDs{values: ids})
}
func draftMonitor(revisions int) Monitor {
	value, _ := RestoreMonitor("monitor", "org", "project", "API", "http", StateDraft, revisions, testTime(), testTime(), 42)
	return value
}

func TestCreateValidatesAllFieldsBeforeProjectLookup(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	checks := &fakeChecks{supported: false}
	result, err := newService(store, checks, testTime(), "id").Create(context.Background(), CreateCommand{Name: " ", CheckType: "Bad-"})
	if err != nil {
		t.Fatal(err)
	}
	want := []ValidationFailure{{Field: "name", Message: NameValidationMessage}, {Field: "checkType", Message: CheckTypeValidationMessage}}
	if result.Kind != CreateInvalid || !reflect.DeepEqual(result.Failures, want) {
		t.Fatalf("result = %#v", result)
	}
	if store.projectCalls != 0 {
		t.Fatal("validation failure queried Project")
	}
}

func TestCreateRejectsWellFormedUnsupportedCheckType(t *testing.T) {
	t.Parallel()
	result, err := newService(&fakeStore{}, &fakeChecks{}, testTime(), "id").Create(context.Background(), CreateCommand{Name: "API", CheckType: "dns"})
	if err != nil {
		t.Fatal(err)
	}
	want := UnsupportedCheckTypeMessage("dns")
	if result.Kind != CreateInvalid || len(result.Failures) != 1 || result.Failures[0] != (ValidationFailure{Field: "checkType", Message: want}) {
		t.Fatalf("result = %#v", result)
	}
}

func TestCreateChecksScopedProjectThenCreatesDraftAtUTCInstant(t *testing.T) {
	t.Parallel()
	local := time.Date(2026, 7, 24, 8, 0, 0, 0, time.FixedZone("CST", 8*3600))
	store := &fakeStore{projectExists: true}
	checks := &fakeChecks{supported: true}
	ctx := context.WithValue(context.Background(), contextKey{}, "kept")
	result, err := newService(store, checks, local, "monitor-id").Create(ctx, CreateCommand{OrganizationID: "org", ProjectID: "project", Name: " API ", CheckType: "http"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != CreateCreated || result.Monitor.ID != "monitor-id" || result.Monitor.Name != "API" || result.Monitor.State != StateDraft || result.Monitor.CreatedAt != local.UTC() {
		t.Fatalf("result = %#v", result)
	}
	if store.contextValue != "kept" || len(store.created) != 1 {
		t.Fatal("create did not propagate context or persist")
	}
}

func TestCreateReturnsProjectNotFoundWithoutClockOrUUID(t *testing.T) {
	t.Parallel()
	ids := &sequenceUUIDs{values: []string{"unused"}}
	service := NewService(&fakeStore{}, &fakeChecks{supported: true}, fixedClock{testTime()}, ids)
	result, err := service.Create(context.Background(), CreateCommand{OrganizationID: "org", ProjectID: "other", Name: "API", CheckType: "http"})
	if err != nil || result.Kind != CreateProjectNotFound || len(ids.times) != 0 {
		t.Fatalf("result = %#v, error %v", result, err)
	}
}

func TestRenameValidationNotFoundArchivedSuccessAndConcurrency(t *testing.T) {
	t.Parallel()
	now := testTime()
	scope := Scope{OrganizationID: "org", ProjectID: "project", MonitorID: "monitor"}
	tests := []struct {
		name       string
		store      *fakeStore
		requested  string
		wantKind   UpdateKind
		wantDetail string
	}{
		{"invalid", &fakeStore{}, " ", UpdateInvalid, ""},
		{"not found", &fakeStore{}, "Name", UpdateNotFound, ""},
		{"archived", &fakeStore{monitor: withState(draftMonitor(0), StateArchived), monitorFound: true}, "Name", UpdateConflict, ArchivedReadOnlyDetail},
		{"success", &fakeStore{monitor: draftMonitor(0), monitorFound: true}, " Name ", UpdateUpdated, ""},
		{"concurrency", &fakeStore{monitor: draftMonitor(0), monitorFound: true, updateErr: ErrConcurrentUpdate}, "Name", UpdateConflict, ConcurrentUpdateDetail},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := newService(test.store, &fakeChecks{supported: true}, now, "unused").Rename(context.Background(), scope, test.requested)
			if err != nil || result.Kind != test.wantKind || result.Detail != test.wantDetail {
				t.Fatalf("result = %#v, error %v", result, err)
			}
			if test.wantKind == UpdateUpdated && (result.Monitor.Name != "Name" || result.Monitor.State != StateDraft || test.store.expectedVersions[0] != 42) {
				t.Fatalf("unexpected rename: %#v, versions %#v", result, test.store.expectedVersions)
			}
		})
	}
}

func TestChangeStateValidationLifecycleAndConcurrency(t *testing.T) {
	t.Parallel()
	now := testTime()
	scope := Scope{OrganizationID: "org", ProjectID: "project", MonitorID: "monitor"}
	invalid, err := newService(&fakeStore{}, &fakeChecks{}, now, "unused").ChangeState(context.Background(), scope, "draft")
	if err != nil || invalid.Kind != UpdateInvalid || invalid.Failures[0].Message != TargetStateValidationMessage {
		t.Fatalf("invalid = %#v, %v", invalid, err)
	}
	withoutRevision := &fakeStore{monitor: draftMonitor(0), monitorFound: true}
	conflict, err := newService(withoutRevision, &fakeChecks{}, now, "unused").ChangeState(context.Background(), scope, "active")
	if err != nil || conflict.Kind != UpdateConflict || conflict.Detail != ActivationWithoutRevisionDetail {
		t.Fatalf("conflict = %#v, %v", conflict, err)
	}
	store := &fakeStore{monitor: draftMonitor(1), monitorFound: true}
	updated, err := newService(store, &fakeChecks{}, now.Add(time.Minute), "unused").ChangeState(context.Background(), scope, "active")
	if err != nil || updated.Kind != UpdateUpdated || updated.Monitor.State != StateActive || store.expectedVersions[0] != 42 {
		t.Fatalf("updated = %#v, %v", updated, err)
	}
	store = &fakeStore{monitor: draftMonitor(1), monitorFound: true, updateErr: ErrConcurrentUpdate}
	conflict, err = newService(store, &fakeChecks{}, now, "unused").ChangeState(context.Background(), scope, "active")
	if err != nil || conflict.Detail != ConcurrentUpdateDetail {
		t.Fatalf("concurrency = %#v, %v", conflict, err)
	}
}

func TestCreateRevisionOrderingValidationCanonicalizationAndAtomicAdvance(t *testing.T) {
	t.Parallel()
	now := testTime()
	scope := Scope{OrganizationID: "org", ProjectID: "project", MonitorID: "monitor"}
	configuration := json.RawMessage(` { "url": "https://example.test" } `)
	canonical := json.RawMessage(`{"url":"https://example.test"}`)

	checks := &fakeChecks{supported: true, canonical: canonical, failures: [][2]string{{"checkSchemaVersion", "bad version"}, {"checkConfiguration.url", "bad URL"}}}
	store := &fakeStore{monitor: draftMonitor(0), monitorFound: true}
	invalid, err := newService(store, checks, now, "revision").CreateRevision(context.Background(), scope, 2, configuration)
	if err != nil || invalid.Kind != RevisionInvalid || !reflect.DeepEqual(invalid.Failures, []ValidationFailure{{"checkSchemaVersion", "bad version"}, {"checkConfiguration.url", "bad URL"}}) || len(store.appended) != 0 {
		t.Fatalf("invalid = %#v, error %v", invalid, err)
	}

	checks = &fakeChecks{supported: true, canonical: canonical}
	store = &fakeStore{monitor: draftMonitor(0), monitorFound: true}
	ctx := context.WithValue(context.Background(), contextKey{}, "kept")
	created, err := newService(store, checks, now, "revision-id").CreateRevision(ctx, scope, 1, configuration)
	if err != nil || created.Kind != RevisionCreated {
		t.Fatalf("created = %#v, error %v", created, err)
	}
	if created.Revision.ID != "revision-id" || created.Revision.RevisionNumber != 1 || string(created.Revision.CheckConfiguration) != string(canonical) ||
		created.Monitor.LatestRevisionNumber != 1 || created.Monitor.State != StateDraft || len(store.appended) != 1 || store.expectedVersions[0] != 42 || store.contextValue != "kept" {
		t.Fatalf("unexpected revision result: %#v, store %#v", created, store)
	}
}

func TestCreateRevisionNotFoundArchivedUnsupportedAndConcurrency(t *testing.T) {
	t.Parallel()
	scope := Scope{OrganizationID: "org", ProjectID: "project", MonitorID: "monitor"}
	now := testTime()
	tests := []struct {
		name       string
		store      *fakeStore
		checks     *fakeChecks
		wantKind   RevisionKind
		wantDetail string
		wantField  string
	}{
		{"not found", &fakeStore{}, &fakeChecks{supported: true}, RevisionMonitorNotFound, "", ""},
		{"archived", &fakeStore{monitor: withState(draftMonitor(0), StateArchived), monitorFound: true}, &fakeChecks{supported: true}, RevisionConflict, ArchivedReadOnlyDetail, ""},
		{"unsupported", &fakeStore{monitor: draftMonitor(0), monitorFound: true}, &fakeChecks{}, RevisionInvalid, "", "checkType"},
		{"concurrency", &fakeStore{monitor: draftMonitor(0), monitorFound: true, appendErr: ErrConcurrentUpdate}, &fakeChecks{supported: true, canonical: json.RawMessage(`{}`)}, RevisionConflict, ConcurrentUpdateDetail, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := newService(test.store, test.checks, now, "revision").CreateRevision(context.Background(), scope, 1, json.RawMessage(`{}`))
			if err != nil || result.Kind != test.wantKind || result.Detail != test.wantDetail {
				t.Fatalf("result = %#v, error %v", result, err)
			}
			if test.wantField != "" && (len(result.Failures) != 1 || result.Failures[0].Field != test.wantField) {
				t.Fatalf("failures = %#v", result.Failures)
			}
			if (test.name == "not found" || test.name == "archived" || test.name == "unsupported") && test.checks.validateCalls != 0 {
				t.Fatal("validator called before required short-circuit")
			}
		})
	}
}

func TestListAndRevisionQueriesPreserveScopeAndEmptyArrays(t *testing.T) {
	t.Parallel()
	service := newService(&fakeStore{}, &fakeChecks{}, testTime(), "unused")
	if values, found, err := service.List(context.Background(), "org", "project"); err != nil || found || values != nil {
		t.Fatalf("missing list = %#v, %v, %v", values, found, err)
	}
	store := &fakeStore{projectExists: true, monitor: draftMonitor(0), monitorFound: true}
	service = newService(store, &fakeChecks{}, testTime(), "unused")
	if values, found, err := service.List(context.Background(), "org", "project"); err != nil || !found || values == nil || len(values) != 0 {
		t.Fatalf("empty list = %#v, %v, %v", values, found, err)
	}
	scope := Scope{OrganizationID: "org", ProjectID: "project", MonitorID: "monitor"}
	if values, found, err := service.ListRevisions(context.Background(), scope); err != nil || !found || values == nil || len(values) != 0 {
		t.Fatalf("empty revisions = %#v, %v, %v", values, found, err)
	}
}

func withState(value Monitor, state State) Monitor { value.State = state; return value }

func TestConcurrentErrorUsesErrorsIs(t *testing.T) {
	t.Parallel()
	wrapped := errors.New("unrelated")
	if errors.Is(wrapped, ErrConcurrentUpdate) {
		t.Fatal("unrelated error matched concurrency sentinel")
	}
}
