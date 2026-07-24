package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrConcurrentUpdate = errors.New("Monitor modified concurrently")

// Clock supplies domain-relevant time.
type Clock interface{ Now() time.Time }

// UUIDGenerator supplies time-ordered UUID version 7 identifiers.
type UUIDGenerator interface {
	NewUUIDv7(time.Time) (string, error)
}

// CheckValidator is the narrow port consumed by Monitor use cases. Each failure pair
// contains field path at index 0 and exact message at index 1, preserving encounter order.
type CheckValidator interface {
	IsSupported(string) bool
	Validate(string, int, json.RawMessage) (json.RawMessage, [][2]string)
}

// Scope carries explicit tenant and ownership identity for every Monitor lookup.
type Scope struct {
	OrganizationID string
	ProjectID      string
	MonitorID      ID
}

// Store is the persistence port owned by the Monitor feature.
type Store interface {
	ProjectExists(context.Context, string, string) (bool, error)
	FindMonitor(context.Context, Scope) (Monitor, bool, error)
	// ListMonitors returns creation order with UUID as tie-breaker.
	ListMonitors(context.Context, string, string) ([]Monitor, error)
	CreateMonitor(context.Context, Monitor) error
	// UpdateMonitor uses expectedVersion in its mutation predicate and returns ErrConcurrentUpdate on zero rows.
	UpdateMonitor(context.Context, Monitor, uint32) error
	// AppendRevision atomically inserts the revision and advances the Monitor using expectedVersion.
	AppendRevision(context.Context, Monitor, Revision, uint32) error
	FindRevision(context.Context, string, ID, int) (Revision, bool, error)
	// ListRevisions returns ascending revision number.
	ListRevisions(context.Context, string, ID) ([]Revision, error)
}

// CreateKind identifies a Monitor creation result.
type CreateKind uint8

const (
	CreateInvalid CreateKind = iota + 1
	CreateCreated
	CreateProjectNotFound
)

// CreateCommand requests a new draft Monitor.
type CreateCommand struct {
	OrganizationID string
	ProjectID      string
	Name           string
	CheckType      string
}

// CreateResult is an expected Monitor creation outcome.
type CreateResult struct {
	Kind     CreateKind
	Monitor  Monitor
	Failures []ValidationFailure
}

// UpdateKind identifies rename and lifecycle results.
type UpdateKind uint8

const (
	UpdateInvalid UpdateKind = iota + 1
	UpdateUpdated
	UpdateNotFound
	UpdateConflict
)

// UpdateResult is an expected rename or state-transition outcome.
type UpdateResult struct {
	Kind     UpdateKind
	Monitor  Monitor
	Failures []ValidationFailure
	Detail   string
}

// RevisionKind identifies revision append results.
type RevisionKind uint8

const (
	RevisionInvalid RevisionKind = iota + 1
	RevisionCreated
	RevisionMonitorNotFound
	RevisionConflict
)

// RevisionResult is an expected immutable-revision append outcome.
type RevisionResult struct {
	Kind     RevisionKind
	Revision Revision
	Monitor  Monitor
	Failures []ValidationFailure
	Detail   string
}

// Service owns Monitor use cases.
type Service struct {
	store  Store
	checks CheckValidator
	clock  Clock
	uuids  UUIDGenerator
}

// NewService constructs Monitor use cases.
func NewService(store Store, checks CheckValidator, clock Clock, uuids UUIDGenerator) *Service {
	if store == nil || checks == nil || clock == nil || uuids == nil {
		panic("monitor.Service requires a store, check validator, clock, and UUID generator")
	}
	return &Service{store: store, checks: checks, clock: clock, uuids: uuids}
}

// Create validates all fields before checking Project existence.
func (service *Service) Create(ctx context.Context, command CreateCommand) (CreateResult, error) {
	var failures []ValidationFailure
	name, validName := NormalizeName(command.Name)
	if !validName {
		failures = append(failures, ValidationFailure{Field: "name", Message: NameValidationMessage})
	}
	checkType, validCheckType := ValidateCheckType(command.CheckType)
	if !validCheckType {
		failures = append(failures, ValidationFailure{Field: "checkType", Message: CheckTypeValidationMessage})
	} else if !service.checks.IsSupported(checkType) {
		failures = append(failures, ValidationFailure{Field: "checkType", Message: UnsupportedCheckTypeMessage(checkType)})
	}
	if len(failures) != 0 {
		return CreateResult{Kind: CreateInvalid, Failures: failures}, nil
	}

	exists, err := service.store.ProjectExists(ctx, command.OrganizationID, command.ProjectID)
	if err != nil {
		return CreateResult{}, err
	}
	if !exists {
		return CreateResult{Kind: CreateProjectNotFound}, nil
	}

	now := service.clock.Now().UTC()
	id, err := service.uuids.NewUUIDv7(now)
	if err != nil {
		return CreateResult{}, fmt.Errorf("generate Monitor id: %w", err)
	}
	created, err := NewMonitor(ID(id), command.OrganizationID, command.ProjectID, name, checkType, now)
	if err != nil {
		return CreateResult{}, err
	}
	if err = service.store.CreateMonitor(ctx, created); err != nil {
		return CreateResult{}, err
	}
	return CreateResult{Kind: CreateCreated, Monitor: created}, nil
}

// Get returns a Monitor only within its complete Organization and Project scope.
func (service *Service) Get(ctx context.Context, scope Scope) (Monitor, bool, error) {
	return service.store.FindMonitor(ctx, scope)
}

// List returns Monitors in one Project, or found=false when that Project is outside the Organization.
func (service *Service) List(ctx context.Context, organizationID, projectID string) ([]Monitor, bool, error) {
	exists, err := service.store.ProjectExists(ctx, organizationID, projectID)
	if err != nil || !exists {
		return nil, exists, err
	}
	values, err := service.store.ListMonitors(ctx, organizationID, projectID)
	if err != nil {
		return nil, false, err
	}
	if values == nil {
		values = []Monitor{}
	}
	return values, true, nil
}

// Rename validates before lookup, then applies an optimistic mutation.
func (service *Service) Rename(ctx context.Context, scope Scope, requestedName string) (UpdateResult, error) {
	name, valid := NormalizeName(requestedName)
	if !valid {
		return UpdateResult{Kind: UpdateInvalid, Failures: []ValidationFailure{{Field: "name", Message: NameValidationMessage}}}, nil
	}
	value, found, err := service.store.FindMonitor(ctx, scope)
	if err != nil {
		return UpdateResult{}, err
	}
	if !found {
		return UpdateResult{Kind: UpdateNotFound}, nil
	}
	if value.State == StateArchived {
		return UpdateResult{Kind: UpdateConflict, Detail: ArchivedReadOnlyDetail}, nil
	}
	expectedVersion := value.Version
	if err = value.Rename(name, service.clock.Now().UTC()); err != nil {
		return UpdateResult{}, err
	}
	if err = service.store.UpdateMonitor(ctx, value, expectedVersion); err != nil {
		if errors.Is(err, ErrConcurrentUpdate) {
			return UpdateResult{Kind: UpdateConflict, Detail: ConcurrentUpdateDetail}, nil
		}
		return UpdateResult{}, err
	}
	return UpdateResult{Kind: UpdateUpdated, Monitor: value}, nil
}

// ChangeState validates a wire target and applies the lifecycle state machine optimistically.
func (service *Service) ChangeState(ctx context.Context, scope Scope, requestedState string) (UpdateResult, error) {
	target, valid := targetState(requestedState)
	if !valid {
		return UpdateResult{Kind: UpdateInvalid, Failures: []ValidationFailure{{Field: "state", Message: TargetStateValidationMessage}}}, nil
	}
	value, found, err := service.store.FindMonitor(ctx, scope)
	if err != nil {
		return UpdateResult{}, err
	}
	if !found {
		return UpdateResult{Kind: UpdateNotFound}, nil
	}
	expectedVersion := value.Version
	if transitionErr := value.TransitionTo(target, service.clock.Now().UTC()); transitionErr != nil {
		return UpdateResult{Kind: UpdateConflict, Detail: transitionErr.Error()}, nil
	}
	if err = service.store.UpdateMonitor(ctx, value, expectedVersion); err != nil {
		if errors.Is(err, ErrConcurrentUpdate) {
			return UpdateResult{Kind: UpdateConflict, Detail: ConcurrentUpdateDetail}, nil
		}
		return UpdateResult{}, err
	}
	return UpdateResult{Kind: UpdateUpdated, Monitor: value}, nil
}

// CreateRevision validates configuration and atomically appends the next immutable revision.
func (service *Service) CreateRevision(
	ctx context.Context,
	scope Scope,
	checkSchemaVersion int,
	checkConfiguration json.RawMessage,
) (RevisionResult, error) {
	value, found, err := service.store.FindMonitor(ctx, scope)
	if err != nil {
		return RevisionResult{}, err
	}
	if !found {
		return RevisionResult{Kind: RevisionMonitorNotFound}, nil
	}
	if value.State == StateArchived {
		return RevisionResult{Kind: RevisionConflict, Detail: ArchivedReadOnlyDetail}, nil
	}
	if !service.checks.IsSupported(value.CheckType) {
		return RevisionResult{Kind: RevisionInvalid, Failures: []ValidationFailure{{Field: "checkType", Message: UnsupportedCheckTypeMessage(value.CheckType)}}}, nil
	}
	canonical, checkFailures := service.checks.Validate(value.CheckType, checkSchemaVersion, checkConfiguration)
	if len(checkFailures) != 0 {
		failures := make([]ValidationFailure, len(checkFailures))
		for index, failure := range checkFailures {
			failures[index] = ValidationFailure{Field: failure[0], Message: failure[1]}
		}
		return RevisionResult{Kind: RevisionInvalid, Failures: failures}, nil
	}

	now := service.clock.Now().UTC()
	id, err := service.uuids.NewUUIDv7(now)
	if err != nil {
		return RevisionResult{}, fmt.Errorf("generate Monitor revision id: %w", err)
	}
	revision, err := NewRevision(
		RevisionID(id), value.ID, value.OrganizationID, value.LatestRevisionNumber+1,
		value.CheckType, checkSchemaVersion, canonical, now,
	)
	if err != nil {
		return RevisionResult{}, err
	}
	expectedVersion := value.Version
	if err = value.RecordRevision(revision.RevisionNumber, now); err != nil {
		return RevisionResult{}, err
	}
	if err = service.store.AppendRevision(ctx, value, revision, expectedVersion); err != nil {
		if errors.Is(err, ErrConcurrentUpdate) {
			return RevisionResult{Kind: RevisionConflict, Detail: ConcurrentUpdateDetail}, nil
		}
		return RevisionResult{}, err
	}
	return RevisionResult{Kind: RevisionCreated, Revision: revision, Monitor: value}, nil
}

// GetRevision scopes a revision through its parent Monitor.
func (service *Service) GetRevision(ctx context.Context, scope Scope, revisionNumber int) (Revision, bool, error) {
	_, found, err := service.store.FindMonitor(ctx, scope)
	if err != nil || !found {
		return Revision{}, found, err
	}
	return service.store.FindRevision(ctx, scope.OrganizationID, scope.MonitorID, revisionNumber)
}

// ListRevisions returns ascending immutable revisions after verifying full Monitor scope.
func (service *Service) ListRevisions(ctx context.Context, scope Scope) ([]Revision, bool, error) {
	_, found, err := service.store.FindMonitor(ctx, scope)
	if err != nil || !found {
		return nil, found, err
	}
	values, err := service.store.ListRevisions(ctx, scope.OrganizationID, scope.MonitorID)
	if err != nil {
		return nil, false, err
	}
	if values == nil {
		values = []Revision{}
	}
	return values, true, nil
}

func targetState(value string) (State, bool) {
	switch value {
	case "active":
		return StateActive, true
	case "paused":
		return StatePaused, true
	case "archived":
		return StateArchived, true
	default:
		return "", false
	}
}
