package user

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrSetupAlreadyCompleted = errors.New("setup already completed")

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
	// CreateFirstAdministrator serializes setup and returns ErrSetupAlreadyCompleted to concurrent losers.
	CreateFirstAdministrator(context.Context, User) error
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

// CreateFirstAdministrator validates and atomically bootstraps the instance administrator.
func (service *Service) CreateFirstAdministrator(ctx context.Context, command CreateFirstAdministratorCommand) (CreateFirstAdministratorResult, error) {
	var failures []ValidationFailure
	email, validEmail := NormalizeEmail(command.Email)
	if !validEmail {
		failures = append(failures, ValidationFailure{Field: "email", Message: EmailValidationMessage})
	}
	displayName, validDisplayName := NormalizeDisplayName(command.DisplayName)
	if !validDisplayName {
		failures = append(failures, ValidationFailure{Field: "displayName", Message: DisplayNameValidationMessage})
	}
	if !ValidPassword(command.Password) {
		failures = append(failures, ValidationFailure{Field: "password", Message: PasswordValidationMessage})
	}
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
