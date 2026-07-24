package user

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fixedClock struct{ value time.Time }

func (clock fixedClock) Now() time.Time { return clock.value }

type fixedUUID struct {
	value string
	time  time.Time
	err   error
}

func (generator *fixedUUID) NewUUIDv7(at time.Time) (string, error) {
	generator.time = at
	return generator.value, generator.err
}

type fakePasswordHasher struct {
	mu           sync.Mutex
	hashCalls    []string
	verifyCalls  [][2]string
	verification PasswordVerification
	hashErr      error
	verifyErr    error
	hashResult   string
}

func (hasher *fakePasswordHasher) Hash(password string) (string, error) {
	hasher.mu.Lock()
	defer hasher.mu.Unlock()
	hasher.hashCalls = append(hasher.hashCalls, password)
	if hasher.hashErr != nil {
		return "", hasher.hashErr
	}
	return hasher.hashResult, nil
}
func (hasher *fakePasswordHasher) Verify(hash, password string) (PasswordVerification, error) {
	hasher.mu.Lock()
	defer hasher.mu.Unlock()
	hasher.verifyCalls = append(hasher.verifyCalls, [2]string{hash, password})
	return hasher.verification, hasher.verifyErr
}

type fakeStore struct {
	anyUsers     bool
	account      User
	found        bool
	createErr    error
	created      []User
	updatedID    ID
	updatedHash  string
	findCalls    int
	contextValue any
}

func (store *fakeStore) AnyUsersExist(ctx context.Context) (bool, error) {
	store.contextValue = ctx.Value(contextKey{})
	return store.anyUsers, nil
}
func (store *fakeStore) FindByID(ctx context.Context, _ ID) (User, bool, error) {
	store.contextValue = ctx.Value(contextKey{})
	return store.account, store.found, nil
}
func (store *fakeStore) FindByEmail(ctx context.Context, _ string) (User, bool, error) {
	store.contextValue = ctx.Value(contextKey{})
	store.findCalls++
	return store.account, store.found, nil
}
func (store *fakeStore) CreateFirstAdministrator(ctx context.Context, value User) error {
	store.contextValue = ctx.Value(contextKey{})
	store.created = append(store.created, value)
	return store.createErr
}
func (store *fakeStore) UpdatePasswordHash(ctx context.Context, id ID, hash string) error {
	store.contextValue = ctx.Value(contextKey{})
	store.updatedID, store.updatedHash = id, hash
	return nil
}

type contextKey struct{}

func TestCreateFirstAdministratorValidatesAllFieldsBeforeDependencies(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	hasher := &fakePasswordHasher{hashResult: "hash"}
	uuid := &fixedUUID{value: "user"}
	service := NewService(store, hasher, fixedClock{time.Now()}, uuid)
	result, err := service.CreateFirstAdministrator(context.Background(), CreateFirstAdministratorCommand{})
	if err != nil {
		t.Fatal(err)
	}
	want := []ValidationFailure{
		{Field: "email", Message: EmailValidationMessage},
		{Field: "displayName", Message: DisplayNameValidationMessage},
		{Field: "password", Message: PasswordValidationMessage},
	}
	if result.Kind != CreateFirstAdministratorInvalid || !reflect.DeepEqual(result.Failures, want) {
		t.Fatalf("result = %#v, want %#v", result, want)
	}
	if len(store.created) != 0 || len(hasher.hashCalls) != 0 || !uuid.time.IsZero() {
		t.Fatal("validation failure touched dependencies")
	}
}

func TestCreateFirstAdministratorNormalizesAndUsesOneUTCInstant(t *testing.T) {
	t.Parallel()
	local := time.Date(2026, 7, 24, 8, 0, 0, 0, time.FixedZone("CST", 8*3600))
	store := &fakeStore{}
	hasher := &fakePasswordHasher{hashResult: "argon-hash"}
	uuid := &fixedUUID{value: "user-id"}
	service := NewService(store, hasher, fixedClock{local}, uuid)
	ctx := context.WithValue(context.Background(), contextKey{}, "kept")
	result, err := service.CreateFirstAdministrator(ctx, CreateFirstAdministratorCommand{
		Email: " ADMIN@Example.Test ", DisplayName: " Admin ", Password: "password-123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != CreateFirstAdministratorCreated || result.User.Email != "admin@example.test" ||
		result.User.DisplayName != "Admin" || result.User.Role != AdministratorRole || result.User.PasswordHash != "argon-hash" ||
		result.User.CreatedAt != local.UTC() || uuid.time != local.UTC() {
		t.Fatalf("unexpected result: %#v, UUID time %v", result, uuid.time)
	}
	if store.contextValue != "kept" {
		t.Fatal("context was not propagated")
	}
}

func TestCreateFirstAdministratorMapsConcurrentSetupLoser(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	store := &fakeStore{createErr: ErrSetupAlreadyCompleted}
	service := NewService(store, &fakePasswordHasher{hashResult: "hash"}, fixedClock{now}, &fixedUUID{value: "user"})
	result, err := service.CreateFirstAdministrator(context.Background(), CreateFirstAdministratorCommand{
		Email: "admin@example.test", DisplayName: "Admin", Password: "password-123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != CreateFirstAdministratorAlreadyCompleted || result.User != (User{}) {
		t.Fatalf("unexpected setup result: %#v", result)
	}
}

func TestAuthenticateMalformedCredentialsDoNotTouchStoreOrHasher(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	hasher := &fakePasswordHasher{}
	service := NewService(store, hasher, fixedClock{time.Now()}, &fixedUUID{})
	result, err := service.Authenticate(context.Background(), AuthenticateCommand{Email: "bad", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != AuthenticateInvalidCredentials || store.findCalls != 0 || len(hasher.verifyCalls) != 0 {
		t.Fatalf("unexpected result or dependency use: %#v, %#v", result, hasher)
	}
}

func TestAuthenticateUnknownEmailUsesCachedDummyHash(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	hasher := &fakePasswordHasher{hashResult: "dummy-hash", verification: PasswordFailed}
	service := NewService(store, hasher, fixedClock{time.Now()}, &fixedUUID{})
	for range 2 {
		result, err := service.Authenticate(context.Background(), AuthenticateCommand{Email: "missing@example.test", Password: "guess"})
		if err != nil || result.Kind != AuthenticateInvalidCredentials {
			t.Fatalf("Authenticate = %#v, %v", result, err)
		}
	}
	if len(hasher.hashCalls) != 1 || len(hasher.verifyCalls) != 2 || hasher.hashCalls[0] != "probehive-timing-equalization-dummy" {
		t.Fatalf("dummy calls = hashes %#v, verifies %#v", hasher.hashCalls, hasher.verifyCalls)
	}
}

func TestAuthenticateSuccessAndFailureAreGeneric(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	account, _ := NewUser("user", "admin@example.test", "Admin", AdministratorRole, "stored", now)
	for _, test := range []struct {
		name         string
		verification PasswordVerification
		want         AuthenticateKind
	}{
		{"success", PasswordVerified, AuthenticateSuccess},
		{"failure", PasswordFailed, AuthenticateInvalidCredentials},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeStore{account: account, found: true}
			hasher := &fakePasswordHasher{verification: test.verification}
			result, err := NewService(store, hasher, fixedClock{now}, &fixedUUID{}).Authenticate(context.Background(), AuthenticateCommand{Email: "ADMIN@example.test", Password: "guess"})
			if err != nil || result.Kind != test.want {
				t.Fatalf("result = %#v, error %v", result, err)
			}
		})
	}
}

func TestAuthenticateRehashesOnlyAfterVerifiedLogin(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	account, _ := NewUser("user", "admin@example.test", "Admin", AdministratorRole, "old", now)
	store := &fakeStore{account: account, found: true}
	hasher := &fakePasswordHasher{verification: PasswordVerifiedRehashNeeded, hashResult: "new"}
	result, err := NewService(store, hasher, fixedClock{now}, &fixedUUID{}).Authenticate(context.Background(), AuthenticateCommand{Email: account.Email, Password: "correct"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != AuthenticateSuccess || result.User.PasswordHash != "new" || store.updatedID != account.ID || store.updatedHash != "new" {
		t.Fatalf("unexpected rehash result: %#v, store %#v", result, store)
	}
}

func TestSetupStatusPropagatesContext(t *testing.T) {
	t.Parallel()
	store := &fakeStore{anyUsers: true}
	service := NewService(store, &fakePasswordHasher{}, fixedClock{time.Now()}, &fixedUUID{})
	ctx := context.WithValue(context.Background(), contextKey{}, "kept")
	complete, err := service.SetupStatus(ctx)
	if err != nil || !complete || store.contextValue != "kept" {
		t.Fatalf("SetupStatus = %v, %v; context %#v", complete, err, store.contextValue)
	}
}

func TestGetReloadsSessionPrincipalAndPropagatesContext(t *testing.T) {
	t.Parallel()
	account, _ := NewUser("user", "admin@example.test", "Admin", AdministratorRole, "hash", time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC))
	store := &fakeStore{account: account, found: true}
	service := NewService(store, &fakePasswordHasher{}, fixedClock{time.Now()}, &fixedUUID{})
	ctx := context.WithValue(context.Background(), contextKey{}, "kept")
	got, found, err := service.Get(ctx, account.ID)
	if err != nil || !found || got != account || store.contextValue != "kept" {
		t.Fatalf("Get = %#v, %v, %v; context %#v", got, found, err, store.contextValue)
	}
}

func TestDependencyErrorsPropagate(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("hash failed")
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	service := NewService(&fakeStore{}, &fakePasswordHasher{hashErr: sentinel}, fixedClock{now}, &fixedUUID{value: "user"})
	_, err := service.CreateFirstAdministrator(context.Background(), CreateFirstAdministratorCommand{
		Email: "admin@example.test", DisplayName: "Admin", Password: "password-123",
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want wrapped sentinel", err)
	}
}
