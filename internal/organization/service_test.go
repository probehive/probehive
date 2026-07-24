package organization

import (
	"context"
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
	err    error
}

func (generator *sequenceUUIDs) NewUUIDv7(at time.Time) (string, error) {
	generator.times = append(generator.times, at)
	if generator.err != nil {
		return "", generator.err
	}
	value := generator.values[0]
	generator.values = generator.values[1:]
	return value, nil
}

type fakeStore struct {
	byID              Organization
	byIDFound         bool
	bySlug            Organization
	bySlugFound       bool
	defaultProject    Project
	defaultFound      bool
	createErr         error
	winnerAfterCreate bool
	findBySlugErr     error
	created           []Details
	findBySlugCalls   int
	defaultCalls      int
	lastContextValue  any
}

func (store *fakeStore) FindByID(ctx context.Context, _ ID) (Organization, bool, error) {
	store.lastContextValue = ctx.Value(contextKey{})
	return store.byID, store.byIDFound, nil
}
func (store *fakeStore) FindBySlug(ctx context.Context, _ string) (Organization, bool, error) {
	store.lastContextValue = ctx.Value(contextKey{})
	store.findBySlugCalls++
	if store.findBySlugErr != nil {
		return Organization{}, false, store.findBySlugErr
	}
	return store.bySlug, store.bySlugFound, nil
}
func (store *fakeStore) FindDefaultProject(ctx context.Context, _ ID) (Project, bool, error) {
	store.lastContextValue = ctx.Value(contextKey{})
	store.defaultCalls++
	return store.defaultProject, store.defaultFound, nil
}
func (*fakeStore) FindProject(context.Context, ID, ProjectID) (Project, bool, error) {
	return Project{}, false, nil
}
func (store *fakeStore) Create(ctx context.Context, organization Organization, project Project) error {
	store.lastContextValue = ctx.Value(contextKey{})
	store.created = append(store.created, Details{Organization: organization, DefaultProject: project})
	if store.winnerAfterCreate {
		store.bySlugFound = true
	}
	return store.createErr
}

type contextKey struct{}

func TestProvisionRejectsAllFieldsBeforeIO(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	generator := &sequenceUUIDs{values: []string{"org", "project"}}
	service := NewService(store, fixedClock{time.Now()}, generator)
	result, err := service.Provision(context.Background(), ProvisionCommand{Slug: "A-", DisplayName: "  "})
	if err != nil {
		t.Fatal(err)
	}
	want := []ValidationFailure{
		{Field: "slug", Message: SlugValidationMessage},
		{Field: "displayName", Message: DisplayNameValidationMessage},
	}
	if result.Kind != ProvisionInvalid || !reflect.DeepEqual(result.Failures, want) {
		t.Fatalf("result = %#v, want failures %#v", result, want)
	}
	if store.findBySlugCalls != 0 || len(store.created) != 0 || len(generator.times) != 0 {
		t.Fatal("validation failure touched dependencies")
	}
}

func TestProvisionCreatesOrganizationAndDefaultProjectAtOneUTCInstant(t *testing.T) {
	t.Parallel()
	local := time.Date(2026, 7, 24, 9, 0, 0, 0, time.FixedZone("CST", 8*3600))
	store := &fakeStore{}
	generator := &sequenceUUIDs{values: []string{"org-id", "project-id"}}
	service := NewService(store, fixedClock{local}, generator)
	ctx := context.WithValue(context.Background(), contextKey{}, "kept")
	result, err := service.Provision(ctx, ProvisionCommand{Slug: "acme", DisplayName: "  Acme  "})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != ProvisionCreated || len(store.created) != 1 {
		t.Fatalf("result = %#v, creates = %d", result, len(store.created))
	}
	wantTime := local.UTC()
	if result.Details.Organization.ID != "org-id" || result.Details.DefaultProject.ID != "project-id" ||
		result.Details.Organization.DisplayName != "Acme" || result.Details.DefaultProject.OrganizationID != "org-id" ||
		result.Details.Organization.CreatedAt != wantTime || result.Details.DefaultProject.CreatedAt != wantTime {
		t.Fatalf("unexpected details: %#v", result.Details)
	}
	if !reflect.DeepEqual(generator.times, []time.Time{wantTime, wantTime}) {
		t.Fatalf("UUID timestamps = %#v", generator.times)
	}
	if store.lastContextValue != "kept" {
		t.Fatal("context value was not propagated")
	}
}

func TestProvisionReplaysIdenticalExistingOrganizationWithoutWrites(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	existing, _ := NewOrganization("org", "acme", "Acme", now)
	project, _ := NewDefaultProject("project", existing.ID, now)
	store := &fakeStore{bySlug: existing, bySlugFound: true, defaultProject: project, defaultFound: true}
	generator := &sequenceUUIDs{values: []string{"unused"}}
	result, err := NewService(store, fixedClock{now}, generator).Provision(context.Background(), ProvisionCommand{Slug: "acme", DisplayName: " Acme "})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != ProvisionReplayed || result.Details != (Details{Organization: existing, DefaultProject: project}) {
		t.Fatalf("unexpected replay: %#v", result)
	}
	if len(store.created) != 0 || len(generator.times) != 0 {
		t.Fatal("replay created state")
	}
}

func TestProvisionReportsDifferentDisplayNameConflictWithoutDefaultProjectLookup(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	existing, _ := NewOrganization("org", "acme", "First", now)
	store := &fakeStore{bySlug: existing, bySlugFound: true}
	result, err := NewService(store, fixedClock{now}, &sequenceUUIDs{}).Provision(context.Background(), ProvisionCommand{Slug: "acme", DisplayName: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != ProvisionSlugConflict || result.Detail != SlugConflictDetail("acme") {
		t.Fatalf("unexpected conflict: %#v", result)
	}
	if store.defaultCalls != 0 {
		t.Fatal("conflict read the default Project")
	}
}

func TestProvisionRereadsUniquenessRaceWinner(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	winner, _ := NewOrganization("winner", "acme", "Acme", now)
	project, _ := NewDefaultProject("project", winner.ID, now)
	store := &fakeStore{bySlug: winner, defaultProject: project, defaultFound: true, createErr: ErrDuplicateSlug, winnerAfterCreate: true}
	generator := &sequenceUUIDs{values: []string{"loser", "loser-project"}}
	result, err := NewService(store, fixedClock{now}, generator).Provision(context.Background(), ProvisionCommand{Slug: "acme", DisplayName: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != ProvisionReplayed || result.Details.Organization.ID != "winner" || store.findBySlugCalls != 2 {
		t.Fatalf("unexpected race replay: %#v, calls %d", result, store.findBySlugCalls)
	}
}

func TestProvisionRaceWithoutReadableWinnerFails(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	store := &fakeStore{createErr: ErrDuplicateSlug}
	generator := &sequenceUUIDs{values: []string{"loser", "loser-project"}}
	_, err := NewService(store, fixedClock{now}, generator).Provision(context.Background(), ProvisionCommand{Slug: "acme", DisplayName: "Acme"})
	if err == nil {
		t.Fatal("expected an invariant error")
	}
}

func TestProvisionMissingDefaultProjectFailsInvariant(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	existing, _ := NewOrganization("org", "acme", "Acme", now)
	store := &fakeStore{bySlug: existing, bySlugFound: true}
	_, err := NewService(store, fixedClock{now}, &sequenceUUIDs{}).Provision(context.Background(), ProvisionCommand{Slug: "acme", DisplayName: "Acme"})
	if !errors.Is(err, ErrDefaultProjectMissing) {
		t.Fatalf("error = %v, want ErrDefaultProjectMissing", err)
	}
}

func TestGetDistinguishesNotFoundAndBrokenInvariant(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	service := NewService(&fakeStore{}, fixedClock{now}, &sequenceUUIDs{})
	if _, found, err := service.Get(context.Background(), "missing"); err != nil || found {
		t.Fatalf("missing Get = found %v, error %v", found, err)
	}
	existing, _ := NewOrganization("org", "acme", "Acme", now)
	service = NewService(&fakeStore{byID: existing, byIDFound: true}, fixedClock{now}, &sequenceUUIDs{})
	if _, _, err := service.Get(context.Background(), existing.ID); !errors.Is(err, ErrDefaultProjectMissing) {
		t.Fatalf("error = %v, want ErrDefaultProjectMissing", err)
	}
}
