package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var ErrSetupAlreadyCompleted = errors.New("setup already completed")

// ErrDuplicateEmail is returned by Store.Create when the email uniqueness race is lost.
var ErrDuplicateEmail = errors.New("user email already exists")

// Clock supplies domain-relevant time.
type Clock interface {
	Now() time.Time
}

// UUIDGenerator supplies time-ordered UUID version 7 identifiers.
type UUIDGenerator interface {
	NewUUIDv7(time.Time) (string, error)
}

// Store is the persistence port for instance-scoped local users.
type Store interface {
	AnyUsersExist(context.Context) (bool, error)
	FindByID(context.Context, ID) (User, bool, error)
	FindByEmail(context.Context, string) (User, bool, error)
	// List returns every local user in creation order. PasswordHash travels with
	// the aggregate but never leaves the process through serialization.
	List(context.Context) ([]User, error)
	// CreateFirstAdministrator serializes setup and returns ErrSetupAlreadyCompleted to concurrent losers.
	CreateFirstAdministrator(context.Context, User) error
	// Create inserts an additional user, returning ErrDuplicateEmail on a lost
	// email uniqueness race.
	Create(context.Context, User) error
	UpdatePasswordHash(context.Context, ID, string) error
}

// PasswordVerification is the outcome of verifying a password hash.
type PasswordVerification uint8

const (
	PasswordFailed PasswordVerification = iota
	PasswordVerified
	PasswordVerifiedRehashNeeded
)

// PasswordHasher is implemented by the composition layer's reviewed Argon2id adapter.
type PasswordHasher interface {
	Hash(string) (string, error)
	Verify(hash, password string) (PasswordVerification, error)
}

// CreateFirstAdministratorKind identifies a setup use-case result.
type CreateFirstAdministratorKind uint8

const (
	CreateFirstAdministratorInvalid CreateFirstAdministratorKind = iota + 1
	CreateFirstAdministratorCreated
	CreateFirstAdministratorAlreadyCompleted
)

// CreateFirstAdministratorCommand carries first-setup fields.
type CreateFirstAdministratorCommand struct {
	Email       string
	DisplayName string
	Password    string
}

// CreateFirstAdministratorResult is an expected setup outcome.
type CreateFirstAdministratorResult struct {
	Kind     CreateFirstAdministratorKind
	User     User
	Failures []ValidationFailure
}

// CreateUserKind identifies a user-creation use-case result.
type CreateUserKind uint8

const (
	CreateUserInvalid CreateUserKind = iota + 1
	CreateUserCreated
	CreateUserDuplicateEmail
)

// CreateUserCommand carries the fields of an additional local user.
type CreateUserCommand struct {
	Email       string
	DisplayName string
	Password    string
}

// CreateUserResult is an expected user-creation outcome.
type CreateUserResult struct {
	Kind     CreateUserKind
	User     User
	Failures []ValidationFailure
}

// AuthenticateKind identifies a login result without revealing account existence.
type AuthenticateKind uint8

const (
	AuthenticateInvalidCredentials AuthenticateKind = iota + 1
	AuthenticateSuccess
)

// AuthenticateCommand carries local credentials.
type AuthenticateCommand struct {
	Email    string
	Password string
}

// AuthenticateResult is a generic-failure login outcome.
type AuthenticateResult struct {
	Kind AuthenticateKind
	User User
}

// Service owns local-user setup and authentication use cases.
type Service struct {
	store  Store
	hasher PasswordHasher
	clock  Clock
	uuids  UUIDGenerator

	dummyMu   sync.Mutex
	dummyHash string
}

// NewService constructs local-user use cases.
func NewService(store Store, hasher PasswordHasher, clock Clock, uuids UUIDGenerator) *Service {
	if store == nil || hasher == nil || clock == nil || uuids == nil {
		panic("user.Service requires a store, password hasher, clock, and UUID generator")
	}
	return &Service{store: store, hasher: hasher, clock: clock, uuids: uuids}
}

// SetupStatus reports true once any local user exists.
func (service *Service) SetupStatus(ctx context.Context) (bool, error) {
	return service.store.AnyUsersExist(ctx)
}

// Get returns a local user by id so a persisted Session can restore its principal.
func (service *Service) Get(ctx context.Context, id ID) (User, bool, error) {
	return service.store.FindByID(ctx, id)
}

// List returns every local user in creation order.
func (service *Service) List(ctx context.Context) ([]User, error) {
	values, err := service.store.List(ctx)
	if err != nil {
		return nil, err
	}
	if values == nil {
		values = []User{}
	}
	return values, nil
}

// validateAccountInput applies the shared local-account field rules: a normalized
// email, an optional display name that defaults to it, and a password.
func validateAccountInput(email, displayName, password string) (string, string, []ValidationFailure) {
	var failures []ValidationFailure
	normalizedEmail, validEmail := NormalizeEmail(email)
	if !validEmail {
		failures = append(failures, ValidationFailure{Code: EmailInvalidCode, Field: "email", Message: EmailValidationMessage})
	}
	// An omitted display name defaults to the normalized login email, so first-run
	// setup needs only an email and a password. A valid email can still exceed the
	// 100-unit display-name ceiling, in which case the operator supplies one.
	normalizedDisplayName := strings.TrimSpace(displayName)
	if normalizedDisplayName == "" && validEmail {
		normalizedDisplayName = normalizedEmail
	}
	if _, validDisplayName := NormalizeDisplayName(normalizedDisplayName); !validDisplayName {
		failures = append(failures, ValidationFailure{Code: DisplayNameInvalidCode, Field: "displayName", Message: DisplayNameValidationMessage})
	}
	if !ValidPassword(password) {
		failures = append(failures, ValidationFailure{Code: PasswordLengthCode, Field: "password", Message: PasswordValidationMessage})
	}
	return normalizedEmail, normalizedDisplayName, failures
}

// CreateFirstAdministrator validates and atomically bootstraps the instance administrator.
func (service *Service) CreateFirstAdministrator(ctx context.Context, command CreateFirstAdministratorCommand) (CreateFirstAdministratorResult, error) {
	email, displayName, failures := validateAccountInput(command.Email, command.DisplayName, command.Password)
	if len(failures) != 0 {
		return CreateFirstAdministratorResult{Kind: CreateFirstAdministratorInvalid, Failures: failures}, nil
	}

	now := service.clock.Now().UTC()
	id, err := service.uuids.NewUUIDv7(now)
	if err != nil {
		return CreateFirstAdministratorResult{}, fmt.Errorf("generate user id: %w", err)
	}
	passwordHash, err := service.hasher.Hash(command.Password)
	if err != nil {
		return CreateFirstAdministratorResult{}, fmt.Errorf("hash password: %w", err)
	}
	created, err := NewUser(ID(id), email, displayName, AdministratorRole, passwordHash, now)
	if err != nil {
		return CreateFirstAdministratorResult{}, err
	}
	if err = service.store.CreateFirstAdministrator(ctx, created); err != nil {
		if errors.Is(err, ErrSetupAlreadyCompleted) {
			return CreateFirstAdministratorResult{Kind: CreateFirstAdministratorAlreadyCompleted}, nil
		}
		return CreateFirstAdministratorResult{}, err
	}
	return CreateFirstAdministratorResult{Kind: CreateFirstAdministratorCreated, User: created}, nil
}

// CreateUser validates and creates an additional local user. The instance role is
// Administrator because an instance is one team; Organization membership is granted
// separately by the caller.
func (service *Service) CreateUser(ctx context.Context, command CreateUserCommand) (CreateUserResult, error) {
	email, displayName, failures := validateAccountInput(command.Email, command.DisplayName, command.Password)
	if len(failures) != 0 {
		return CreateUserResult{Kind: CreateUserInvalid, Failures: failures}, nil
	}

	now := service.clock.Now().UTC()
	id, err := service.uuids.NewUUIDv7(now)
	if err != nil {
		return CreateUserResult{}, fmt.Errorf("generate user id: %w", err)
	}
	passwordHash, err := service.hasher.Hash(command.Password)
	if err != nil {
		return CreateUserResult{}, fmt.Errorf("hash password: %w", err)
	}
	created, err := NewUser(ID(id), email, displayName, AdministratorRole, passwordHash, now)
	if err != nil {
		return CreateUserResult{}, err
	}
	if err = service.store.Create(ctx, created); err != nil {
		if errors.Is(err, ErrDuplicateEmail) {
			return CreateUserResult{Kind: CreateUserDuplicateEmail}, nil
		}
		return CreateUserResult{}, err
	}
	return CreateUserResult{Kind: CreateUserCreated, User: created}, nil
}

// Authenticate verifies local credentials and upgrades an outdated hash after success.
func (service *Service) Authenticate(ctx context.Context, command AuthenticateCommand) (AuthenticateResult, error) {
	email, validEmail := NormalizeEmail(command.Email)
	if !validEmail || command.Password == "" {
		return AuthenticateResult{Kind: AuthenticateInvalidCredentials}, nil
	}
	account, found, err := service.store.FindByEmail(ctx, email)
	if err != nil {
		return AuthenticateResult{}, err
	}
	if !found {
		dummyHash, hashErr := service.timingDummyHash()
		if hashErr != nil {
			return AuthenticateResult{}, hashErr
		}
		if _, verifyErr := service.hasher.Verify(dummyHash, command.Password); verifyErr != nil {
			return AuthenticateResult{}, verifyErr
		}
		return AuthenticateResult{Kind: AuthenticateInvalidCredentials}, nil
	}

	verification, err := service.hasher.Verify(account.PasswordHash, command.Password)
	if err != nil {
		return AuthenticateResult{}, err
	}
	switch verification {
	case PasswordVerified:
		return AuthenticateResult{Kind: AuthenticateSuccess, User: account}, nil
	case PasswordVerifiedRehashNeeded:
		replacement, hashErr := service.hasher.Hash(command.Password)
		if hashErr != nil {
			return AuthenticateResult{}, hashErr
		}
		if err = account.ReplacePasswordHash(replacement); err != nil {
			return AuthenticateResult{}, err
		}
		if err = service.store.UpdatePasswordHash(ctx, account.ID, replacement); err != nil {
			return AuthenticateResult{}, err
		}
		return AuthenticateResult{Kind: AuthenticateSuccess, User: account}, nil
	default:
		return AuthenticateResult{Kind: AuthenticateInvalidCredentials}, nil
	}
}

func (service *Service) timingDummyHash() (string, error) {
	service.dummyMu.Lock()
	defer service.dummyMu.Unlock()
	if service.dummyHash == "" {
		value, err := service.hasher.Hash("probehive-timing-equalization-dummy")
		if err != nil {
			return "", fmt.Errorf("create timing-equalization hash: %w", err)
		}
		service.dummyHash = value
	}
	return service.dummyHash, nil
}
