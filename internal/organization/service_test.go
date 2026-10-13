package organization

import (
	"context"
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
	byID               Organization
	byIDFound          bool
	listed             []Details
	listErr            error
	renamedTo          []string
	renameFound        bool
	renameErr          error
	membership         Membership
	membershipFound    bool
	createdMemberships []Membership
	addedMemberships   []Membership
	createErr          error
	userIDs            []string
	userIDErr          error
	organizationIDs    []ID
	created            []Details
	lastContextValue   any
}

func (store *fakeStore) FindByID(ctx context.Context, _ ID) (Organization, bool, error) {
	store.lastContextValue = ctx.Value(contextKey{})
	return store.byID, store.byIDFound, nil
}
func (store *fakeStore) ListForMember(ctx context.Context, _ string) ([]Details, error) {
	store.lastContextValue = ctx.Value(contextKey{})
	return store.listed, store.listErr
}
func (store *fakeStore) UpdateDisplayName(ctx context.Context, _ ID, displayName string) (bool, error) {
	store.lastContextValue = ctx.Value(contextKey{})
	store.renamedTo = append(store.renamedTo, displayName)
	return store.renameFound, store.renameErr
}
func (store *fakeStore) FindMembership(ctx context.Context, _ ID, _ string) (Membership, bool, error) {
	store.lastContextValue = ctx.Value(contextKey{})
	return store.membership, store.membershipFound, nil
}
func (store *fakeStore) ListIDs(ctx context.Context) ([]ID, error) {
	store.lastContextValue = ctx.Value(contextKey{})
	return store.organizationIDs, nil
}
func (store *fakeStore) ListUserIDs(ctx context.Context) ([]string, error) {
	store.lastContextValue = ctx.Value(contextKey{})
	return store.userIDs, store.userIDErr
}
func (store *fakeStore) Create(ctx context.Context, organization Organization, memberships []Membership) error {
	store.lastContextValue = ctx.Value(contextKey{})
	store.created = append(store.created, Details{Organization: organization})
	store.createdMemberships = append(store.createdMemberships, memberships...)
	return store.createErr
}
func (store *fakeStore) CreateMemberships(ctx context.Context, memberships []Membership) error {
	store.lastContextValue = ctx.Value(contextKey{})
	store.addedMemberships = append(store.addedMemberships, memberships...)
	return nil
}

type contextKey struct{}

func TestProvisionRejectsInvalidFieldsAndMissingCreatorBeforeIO(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	generator := &sequenceUUIDs{values: []string{"org", "project"}}
	service := NewService(store, fixedClock{time.Now()}, generator)
	result, err := service.Provision(context.Background(), ProvisionCommand{DisplayName: "  "})
	if err != nil {
		t.Fatal(err)
	}
	want := []ValidationFailure{
		{Code: DisplayNameInvalidCode, Field: "displayName", Message: DisplayNameValidationMessage},
	}
	if result.Kind != ProvisionInvalid || !reflect.DeepEqual(result.Failures, want) {
		t.Fatalf("result = %#v, want failures %#v", result, want)
	}
	if len(store.created) != 0 || len(generator.times) != 0 {
		t.Fatal("validation failure touched dependencies")
	}

	_, err = service.Provision(context.Background(), ProvisionCommand{DisplayName: "Acme"})
	if err == nil {
		t.Fatal("provisioning without a creator should fail")
	}
}

func TestProvisionCreatesOrganizationAndTeamMembershipsAtOneUTCInstant(t *testing.T) {
	t.Parallel()
	local := time.Date(2026, 7, 24, 9, 0, 0, 0, time.FixedZone("CST", 8*3600))
	store := &fakeStore{userIDs: []string{"creator", "teammate"}}
	generator := &sequenceUUIDs{values: []string{"org-id", "project-id"}}
	service := NewService(store, fixedClock{local}, generator)
	ctx := context.WithValue(context.Background(), contextKey{}, "kept")
	result, err := service.Provision(ctx, ProvisionCommand{DisplayName: "  Acme  ", CreatorUserID: "creator"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != ProvisionCreated || len(store.created) != 1 {
		t.Fatalf("result = %#v, creates = %d", result, len(store.created))
	}
	wantTime := local.UTC()
	if result.Details.Organization.ID != "org-id" ||
		result.Details.Organization.DisplayName != "Acme" ||
		result.Details.Organization.CreatedAt != wantTime {
		t.Fatalf("unexpected details: %#v", result.Details)
	}
	if !reflect.DeepEqual(generator.times, []time.Time{wantTime}) {
		t.Fatalf("UUID timestamps = %#v", generator.times)
	}
	if store.lastContextValue != "kept" {
		t.Fatal("context value was not propagated")
	}
	if len(store.createdMemberships) != 2 {
		t.Fatalf("memberships = %#v, want one per user", store.createdMemberships)
	}
	for _, membership := range store.createdMemberships {
		if membership.OrganizationID != "org-id" || membership.Role != RoleAdministrator || membership.CreatedAt != wantTime {
			t.Fatalf("membership = %#v, want an Administrator of the new Organization", membership)
		}
	}
}

func TestAddMemberToAllOrganizationsGrantsAdministratorEverywhere(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	store := &fakeStore{organizationIDs: []ID{"org-a", "org-b"}}
	err := NewService(store, fixedClock{now}, &sequenceUUIDs{}).AddMemberToAllOrganizations(context.Background(), "new-user")
	if err != nil {
		t.Fatal(err)
	}
	if len(store.addedMemberships) != 2 {
		t.Fatalf("memberships = %#v, want one per Organization", store.addedMemberships)
	}
	for _, membership := range store.addedMemberships {
		if membership.UserID != "new-user" || membership.Role != RoleAdministrator || membership.CreatedAt != now {
			t.Fatalf("membership = %#v, want the user as Administrator everywhere", membership)
		}
	}
}

func TestGetDistinguishesNotFoundAndBrokenInvariant(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	service := NewService(&fakeStore{}, fixedClock{now}, &sequenceUUIDs{})
	if _, found, err := service.Get(context.Background(), "missing"); err != nil || found {
		t.Fatalf("missing Get = found %v, error %v", found, err)
	}
	existing, _ := NewOrganization("org", "Acme", now)
	service = NewService(&fakeStore{byID: existing, byIDFound: true}, fixedClock{now}, &sequenceUUIDs{})
	if _, found, err := service.Get(context.Background(), existing.ID); err != nil || !found {
		t.Fatalf("Get = found %v, error %v", found, err)
	}
}

func TestRenameValidatesBeforeTouchingTheStore(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	service := NewService(store, fixedClock{time.Now()}, &sequenceUUIDs{})

	invalid, err := service.Rename(context.Background(), "org", "   ")
	if err != nil || invalid.Kind != RenameInvalid {
		t.Fatalf("invalid rename = %#v, error %v", invalid, err)
	}
	want := []ValidationFailure{
		{Code: DisplayNameInvalidCode, Field: "displayName", Message: DisplayNameValidationMessage},
	}
	if !reflect.DeepEqual(invalid.Failures, want) {
		t.Fatalf("failures = %#v, want %#v", invalid.Failures, want)
	}
	if len(store.renamedTo) != 0 {
		t.Fatal("an invalid rename reached the store")
	}

	missing, err := service.Rename(context.Background(), "org", "Acme")
	if err != nil || missing.Kind != RenameNotFound {
		t.Fatalf("missing rename = %#v, error %v", missing, err)
	}

	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	existing, _ := NewOrganization("org", "Renamed", now)
	store = &fakeStore{
		renameFound: true, byID: existing, byIDFound: true,
	}
	renamed, err := NewService(store, fixedClock{now}, &sequenceUUIDs{}).
		Rename(context.Background(), "org", "  Renamed  ")
	if err != nil || renamed.Kind != RenameRenamed {
		t.Fatalf("rename = %#v, error %v", renamed, err)
	}
	if !reflect.DeepEqual(store.renamedTo, []string{"Renamed"}) {
		t.Fatalf("store received %#v, want the trimmed display name only", store.renamedTo)
	}
	if renamed.Details.Organization.ID != "org" {
		t.Fatalf("rename changed the id: %#v", renamed.Details.Organization)
	}
}
