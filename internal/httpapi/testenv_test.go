package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/probehive/probehive/internal/check"
	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/user"
)

type testEnvironment struct {
	server        *httptest.Server
	client        *http.Client
	clock         *testClock
	users         *memoryUserStore
	sessions      *memorySessionStore
	antiforgery   *memoryAntiforgeryStore
	organizations *memoryOrganizationStore
	monitors      *memoryMonitorStore
}

func newTestEnvironment(t *testing.T, development bool, credentialLimit int, configure ...func(*Config)) *testEnvironment {
	t.Helper()
	clock := &testClock{value: time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)}
	users := newMemoryUserStore()
	sessions := newMemorySessionStore()
	antiforgery := newMemoryAntiforgeryStore()
	organizations := newMemoryOrganizationStore()
	monitors := newMemoryMonitorStore()
	ids := &testUUIDGenerator{}

	organizationService := organization.NewService(organizations, clock, ids)
	userService := user.NewService(users, testPasswordHasher{}, clock, ids)
	monitorService := monitor.NewService(monitors, check.NewCatalog(), clock, ids)
	config := Config{
		Organizations:               organizationService,
		Users:                       userService,
		Monitors:                    monitorService,
		Sessions:                    sessions,
		Antiforgery:                 antiforgery,
		Clock:                       clock,
		Ready:                       func(context.Context) error { return nil },
		Random:                      &testRandomReader{},
		Logger:                      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Development:                 development,
		CredentialAttemptsPerMinute: credentialLimit,
	}
	for _, configureEnvironment := range configure {
		configureEnvironment(&config)
	}
	application, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(application)
	jar, err := cookiejar.New(nil)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	environment := &testEnvironment{
		server: server, client: &http.Client{Jar: jar}, clock: clock,
		users: users, sessions: sessions, antiforgery: antiforgery,
		organizations: organizations, monitors: monitors,
	}
	t.Cleanup(server.Close)
	return environment
}

type recordedResponse struct {
	StatusCode int
	Header     http.Header
	Cookies    []*http.Cookie
	Body       []byte
}

func (environment *testEnvironment) request(
	t *testing.T,
	client *http.Client,
	method, path, body, origin, antiforgeryToken string,
	cookies ...*http.Cookie,
) recordedResponse {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, environment.server.URL+path, bodyReader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if antiforgeryToken != "" {
		request.Header.Set(antiforgeryHeaderName, antiforgeryToken)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return recordedResponse{
		StatusCode: response.StatusCode, Header: response.Header.Clone(),
		Cookies: response.Cookies(), Body: responseBody,
	}
}

func (environment *testEnvironment) getAntiforgery(t *testing.T) (api.AntiforgeryTokenResponse, recordedResponse) {
	t.Helper()
	response := environment.request(t, environment.client, http.MethodGet, "/api/v1/auth/antiforgery", "", "", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("antiforgery status = %d, body %s", response.StatusCode, response.Body)
	}
	var token api.AntiforgeryTokenResponse
	if err := json.Unmarshal(response.Body, &token); err != nil {
		t.Fatal(err)
	}
	return token, response
}

func (environment *testEnvironment) bootstrapAdministrator(t *testing.T) string {
	t.Helper()
	token, _ := environment.getAntiforgery(t)
	response := environment.request(
		t, environment.client, http.MethodPost, "/api/v1/setup/admin",
		`{"email":"admin@example.test","displayName":"Admin","password":"password-123"}`,
		environment.server.URL, token.RequestToken,
	)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, body %s", response.StatusCode, response.Body)
	}
	refreshed, _ := environment.getAntiforgery(t)
	return refreshed.RequestToken
}

func decodeProblem(t *testing.T, response recordedResponse) api.ProblemDetails {
	t.Helper()
	if contentType := response.Header.Get("Content-Type"); contentType != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json; body %s", contentType, response.Body)
	}
	var problem api.ProblemDetails
	if err := json.Unmarshal(response.Body, &problem); err != nil {
		t.Fatal(err)
	}
	return problem
}

func findCookie(t *testing.T, response recordedResponse, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("response has no %s cookie: %#v", name, response.Cookies)
	return nil
}

type testClock struct {
	mu    sync.Mutex
	value time.Time
}

func (clock *testClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.value
}

func (clock *testClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.value = clock.value.Add(duration)
}

type testUUIDGenerator struct {
	mu      sync.Mutex
	counter uint64
}

func (generator *testUUIDGenerator) NewUUIDv7(time.Time) (string, error) {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	generator.counter++
	return fmt.Sprintf("00000000-0000-7000-8000-%012x", generator.counter), nil
}

type testRandomReader struct {
	mu   sync.Mutex
	next byte
}

func (reader *testRandomReader) Read(destination []byte) (int, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	for index := range destination {
		destination[index] = reader.next
		reader.next++
	}
	return len(destination), nil
}

type testPasswordHasher struct{}

func (testPasswordHasher) Hash(password string) (string, error) { return "test:" + password, nil }
func (testPasswordHasher) Verify(hash, password string) (user.PasswordVerification, error) {
	if hash == "test:"+password {
		return user.PasswordVerified, nil
	}
	return user.PasswordFailed, nil
}

type memoryUserStore struct {
	mu      sync.Mutex
	byID    map[user.ID]user.User
	byEmail map[string]user.ID
}

func newMemoryUserStore() *memoryUserStore {
	return &memoryUserStore{byID: make(map[user.ID]user.User), byEmail: make(map[string]user.ID)}
}
func (store *memoryUserStore) AnyUsersExist(context.Context) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return len(store.byID) != 0, nil
}
func (store *memoryUserStore) FindByID(_ context.Context, id user.ID) (user.User, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, found := store.byID[id]
	return value, found, nil
}
func (store *memoryUserStore) FindByEmail(_ context.Context, email string) (user.User, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	id, found := store.byEmail[email]
	if !found {
		return user.User{}, false, nil
	}
	return store.byID[id], true, nil
}
func (store *memoryUserStore) CreateFirstAdministrator(_ context.Context, value user.User) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.byID) != 0 {
		return user.ErrSetupAlreadyCompleted
	}
	store.byID[value.ID], store.byEmail[value.Email] = value, value.ID
	return nil
}
func (store *memoryUserStore) UpdatePasswordHash(_ context.Context, id user.ID, hash string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, found := store.byID[id]
	if !found {
		return errors.New("user not found")
	}
	value.PasswordHash = hash
	store.byID[id] = value
	return nil
}

type memorySessionStore struct {
	mu      sync.Mutex
	entries map[user.TokenHash]user.Session
}

func newMemorySessionStore() *memorySessionStore {
	return &memorySessionStore{entries: make(map[user.TokenHash]user.Session)}
}
func (store *memorySessionStore) Create(_ context.Context, value user.Session) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.entries[value.TokenHash] = value
	return nil
}
func (store *memorySessionStore) FindByTokenHash(_ context.Context, hash user.TokenHash) (user.Session, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, found := store.entries[hash]
	return value, found, nil
}
func (store *memorySessionStore) DeleteByTokenHash(_ context.Context, hash user.TokenHash) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.entries, hash)
	return nil
}
func (store *memorySessionStore) only(t *testing.T) user.Session {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.entries) != 1 {
		t.Fatalf("session count = %d, want 1", len(store.entries))
	}
	for _, value := range store.entries {
		return value
	}
	return user.Session{}
}

type memoryAntiforgeryStore struct {
	mu                    sync.Mutex
	anonymousKey          user.AnonymousAntiforgeryKey
	anonymousKeySet       bool
	anonymousKeyCreations int
	entries               map[user.TokenHash]user.SessionAntiforgeryRecord
}

func newMemoryAntiforgeryStore() *memoryAntiforgeryStore {
	return &memoryAntiforgeryStore{entries: make(map[user.TokenHash]user.SessionAntiforgeryRecord)}
}
func (store *memoryAntiforgeryStore) GetOrCreateAnonymousAntiforgeryKey(_ context.Context, candidate user.AnonymousAntiforgeryKey) (user.AnonymousAntiforgeryKey, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.anonymousKeySet {
		store.anonymousKey = candidate
		store.anonymousKeySet = true
		store.anonymousKeyCreations++
	}
	return store.anonymousKey, nil
}
func (store *memoryAntiforgeryStore) FindAnonymousAntiforgeryKey(_ context.Context) (user.AnonymousAntiforgeryKey, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.anonymousKey, store.anonymousKeySet, nil
}
func (store *memoryAntiforgeryStore) CreateSessionAntiforgery(_ context.Context, value user.SessionAntiforgeryRecord) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for selector, existing := range store.entries {
		if existing.SessionTokenHash == value.SessionTokenHash {
			delete(store.entries, selector)
		}
	}
	store.entries[value.SelectorHash] = value
	return nil
}
func (store *memoryAntiforgeryStore) FindSessionAntiforgeryBySelectorHash(_ context.Context, hash user.TokenHash) (user.SessionAntiforgeryRecord, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, found := store.entries[hash]
	return value, found, nil
}
func (store *memoryAntiforgeryStore) DeleteSessionAntiforgeryBySelectorHash(_ context.Context, hash user.TokenHash) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.entries, hash)
	return nil
}

type memoryOrganizationStore struct {
	mu       sync.Mutex
	byID     map[organization.ID]organization.Organization
	bySlug   map[string]organization.ID
	projects map[organization.ProjectID]organization.Project
}

func newMemoryOrganizationStore() *memoryOrganizationStore {
	return &memoryOrganizationStore{
		byID: make(map[organization.ID]organization.Organization), bySlug: make(map[string]organization.ID),
		projects: make(map[organization.ProjectID]organization.Project),
	}
}
func (store *memoryOrganizationStore) FindByID(_ context.Context, id organization.ID) (organization.Organization, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, found := store.byID[id]
	return value, found, nil
}
func (store *memoryOrganizationStore) FindBySlug(_ context.Context, slug string) (organization.Organization, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	id, found := store.bySlug[slug]
	if !found {
		return organization.Organization{}, false, nil
	}
	return store.byID[id], true, nil
}
func (store *memoryOrganizationStore) FindDefaultProject(_ context.Context, id organization.ID) (organization.Project, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, project := range store.projects {
		if project.OrganizationID == id && project.IsDefault {
			return project, true, nil
		}
	}
	return organization.Project{}, false, nil
}
func (store *memoryOrganizationStore) FindProject(_ context.Context, organizationID organization.ID, projectID organization.ProjectID) (organization.Project, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	project, found := store.projects[projectID]
	if !found || project.OrganizationID != organizationID {
		return organization.Project{}, false, nil
	}
	return project, true, nil
}
func (store *memoryOrganizationStore) Create(_ context.Context, value organization.Organization, project organization.Project) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, found := store.bySlug[value.Slug]; found {
		return organization.ErrDuplicateSlug
	}
	store.byID[value.ID], store.bySlug[value.Slug], store.projects[project.ID] = value, value.ID, project
	return nil
}

type memoryMonitorStore struct {
	mu          sync.Mutex
	projects    map[string]bool
	monitors    map[string]monitor.Monitor
	revisions   map[string]monitor.Revision
	updateError error
}

func newMemoryMonitorStore() *memoryMonitorStore {
	return &memoryMonitorStore{projects: make(map[string]bool), monitors: make(map[string]monitor.Monitor), revisions: make(map[string]monitor.Revision)}
}
func projectKey(organizationID, projectID string) string { return organizationID + "/" + projectID }
func monitorKey(scope monitor.Scope) string {
	return scope.OrganizationID + "/" + scope.ProjectID + "/" + string(scope.MonitorID)
}
func revisionKey(organizationID string, monitorID monitor.ID, number int) string {
	return fmt.Sprintf("%s/%s/%d", organizationID, monitorID, number)
}
func (store *memoryMonitorStore) ProjectExists(_ context.Context, organizationID, projectID string) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.projects[projectKey(organizationID, projectID)], nil
}
func (store *memoryMonitorStore) FindMonitor(_ context.Context, scope monitor.Scope) (monitor.Monitor, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, found := store.monitors[monitorKey(scope)]
	return value, found, nil
}
func (store *memoryMonitorStore) ListMonitors(_ context.Context, organizationID, projectID string) ([]monitor.Monitor, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	values := []monitor.Monitor{}
	for _, value := range store.monitors {
		if value.OrganizationID == organizationID && value.ProjectID == projectID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values, nil
}
func (store *memoryMonitorStore) CreateMonitor(_ context.Context, value monitor.Monitor) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.monitors[monitorKey(monitor.Scope{OrganizationID: value.OrganizationID, ProjectID: value.ProjectID, MonitorID: value.ID})] = value
	return nil
}
func (store *memoryMonitorStore) UpdateMonitor(_ context.Context, value monitor.Monitor, expected uint32) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.updateError != nil {
		return store.updateError
	}
	key := monitorKey(monitor.Scope{OrganizationID: value.OrganizationID, ProjectID: value.ProjectID, MonitorID: value.ID})
	current, found := store.monitors[key]
	if !found || current.Version != expected {
		return monitor.ErrConcurrentUpdate
	}
	value.Version = expected + 1
	store.monitors[key] = value
	return nil
}
func (store *memoryMonitorStore) AppendRevision(_ context.Context, value monitor.Monitor, revision monitor.Revision, expected uint32) error {
	if err := store.UpdateMonitor(context.Background(), value, expected); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.revisions[revisionKey(revision.OrganizationID, revision.MonitorID, revision.RevisionNumber)] = revision
	return nil
}
func (store *memoryMonitorStore) FindRevision(_ context.Context, organizationID string, monitorID monitor.ID, number int) (monitor.Revision, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, found := store.revisions[revisionKey(organizationID, monitorID, number)]
	return value, found, nil
}
func (store *memoryMonitorStore) ListRevisions(_ context.Context, organizationID string, monitorID monitor.ID) ([]monitor.Revision, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	values := []monitor.Revision{}
	for _, value := range store.revisions {
		if value.OrganizationID == organizationID && value.MonitorID == monitorID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].RevisionNumber < values[j].RevisionNumber })
	return values, nil
}
func (store *memoryMonitorStore) seed(value monitor.Monitor) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.projects[projectKey(value.OrganizationID, value.ProjectID)] = true
	store.monitors[monitorKey(monitor.Scope{OrganizationID: value.OrganizationID, ProjectID: value.ProjectID, MonitorID: value.ID})] = value
}
