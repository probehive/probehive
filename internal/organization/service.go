package organization

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Clock supplies domain-relevant time.
type Clock interface {
	Now() time.Time
}

// UUIDGenerator supplies time-ordered UUID version 7 identifiers.
type UUIDGenerator interface {
	NewUUIDv7(time.Time) (string, error)
}

// Store is the persistence port owned by the Organization feature.
type Store interface {
	FindByID(context.Context, ID) (Organization, bool, error)
	FindDefaultProject(context.Context, ID) (Project, bool, error)
	FindProject(context.Context, ID, ProjectID) (Project, bool, error)
	// ListForMember returns the Organizations a user belongs to, with their default
	// Projects, in creation order using the Organization id as a tie-breaker.
	ListForMember(context.Context, string) ([]Details, error)
	FindMembership(context.Context, ID, string) (Membership, bool, error)
	// UpdateDisplayName renames one Organization and reports whether it existed.
	UpdateDisplayName(context.Context, ID, string) (bool, error)
	// ListIDs returns every Organization id in creation order.
	ListIDs(context.Context) ([]ID, error)
	// ListUserIDs returns every local user id in creation order. An instance is one
	// team, so provisioning and user creation both rely on it to keep memberships
	// complete.
	ListUserIDs(context.Context) ([]string, error)
	// Create inserts the Organization, its default Project, and the initial
	// memberships in one transaction, so no path can produce an Organization
	// without a member.
	Create(context.Context, Organization, Project, []Membership) error
	// CreateMemberships inserts memberships that do not exist yet.
	CreateMemberships(context.Context, []Membership) error
}

// ProvisionKind identifies an Organization provisioning result.
type ProvisionKind uint8

const (
	ProvisionInvalid ProvisionKind = iota + 1
	ProvisionCreated
)

// ProvisionCommand requests an Organization and its default Project.
type ProvisionCommand struct {
	DisplayName string
	// CreatorUserID guarantees the creating Administrator is a member even if the
	// user listing races a concurrent user creation.
	CreatorUserID string
}

// ProvisionResult is the complete expected outcome of Organization provisioning.
type ProvisionResult struct {
	Kind     ProvisionKind
	Details  Details
	Failures []ValidationFailure
}

// Service owns Organization use cases.
type Service struct {
	store Store
	clock Clock
	uuids UUIDGenerator
}

// NewService constructs the Organization use cases.
func NewService(store Store, clock Clock, uuids UUIDGenerator) *Service {
	if store == nil || clock == nil || uuids == nil {
		panic("organization.Service requires a store, clock, and UUID generator")
	}
	return &Service{store: store, clock: clock, uuids: uuids}
}

// Provision validates and creates an Organization with its default Project. An
// instance is one team: every current user becomes an Administrator of the new
// Organization inside the creation transaction, so teammates share it immediately.
func (service *Service) Provision(ctx context.Context, command ProvisionCommand) (ProvisionResult, error) {
	displayName, validDisplayName := NormalizeDisplayName(command.DisplayName)
	if !validDisplayName {
		return ProvisionResult{Kind: ProvisionInvalid, Failures: []ValidationFailure{
			{Code: DisplayNameInvalidCode, Field: "displayName", Message: DisplayNameValidationMessage},
		}}, nil
	}
	if command.CreatorUserID == "" {
		return ProvisionResult{}, errors.New("provisioning requires the creating user")
	}

	now := service.clock.Now().UTC()
	organizationID, err := service.uuids.NewUUIDv7(now)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("generate Organization id: %w", err)
	}
	projectID, err := service.uuids.NewUUIDv7(now)
	if err != nil {
		return ProvisionResult{}, fmt.Errorf("generate default Project id: %w", err)
	}
	created, err := NewOrganization(ID(organizationID), displayName, now)
	if err != nil {
		return ProvisionResult{}, err
	}
	project, err := NewDefaultProject(ProjectID(projectID), created.ID, now)
	if err != nil {
		return ProvisionResult{}, err
	}
	memberships, err := service.membershipsForNewOrganization(ctx, created.ID, command.CreatorUserID, now)
	if err != nil {
		return ProvisionResult{}, err
	}
	if err = service.store.Create(ctx, created, project, memberships); err != nil {
		return ProvisionResult{}, err
	}
	return ProvisionResult{Kind: ProvisionCreated, Details: Details{Organization: created, DefaultProject: project}}, nil
}

func (service *Service) membershipsForNewOrganization(
	ctx context.Context, organizationID ID, creatorUserID string, now time.Time,
) ([]Membership, error) {
	userIDs, err := service.store.ListUserIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users for provisioning: %w", err)
	}
	seen := make(map[string]struct{}, len(userIDs)+1)
	candidates := make([]string, 0, len(userIDs)+1)
	for _, userID := range append(userIDs, creatorUserID) {
		if userID == "" {
			continue
		}
		if _, duplicate := seen[userID]; duplicate {
			continue
		}
		seen[userID] = struct{}{}
		candidates = append(candidates, userID)
	}
	memberships := make([]Membership, 0, len(candidates))
	for _, userID := range candidates {
		membership, err := NewMembership(organizationID, userID, RoleAdministrator, now)
		if err != nil {
			return nil, err
		}
		memberships = append(memberships, membership)
	}
	return memberships, nil
}

// AddMemberToAllOrganizations makes one user an Administrator of every existing
// Organization. User creation calls it so a new teammate immediately shares the
// instance; inserts that already exist are ignored, so the operation is safely
// repeatable.
func (service *Service) AddMemberToAllOrganizations(ctx context.Context, userID string) error {
	if userID == "" {
		return errors.New("adding a member requires the user")
	}
	now := service.clock.Now().UTC()
	organizationIDs, err := service.store.ListIDs(ctx)
	if err != nil {
		return fmt.Errorf("list Organizations for membership: %w", err)
	}
	memberships := make([]Membership, 0, len(organizationIDs))
	for _, organizationID := range organizationIDs {
		membership, err := NewMembership(organizationID, userID, RoleAdministrator, now)
		if err != nil {
			return err
		}
		memberships = append(memberships, membership)
	}
	return service.store.CreateMemberships(ctx, memberships)
}

// ProvisionBootstrap creates the Organization that first-administrator setup gives a new
// installation. It is the same use case as every other creation path with the reserved
// bootstrap display name, so setup adds no second creation path.
func (service *Service) ProvisionBootstrap(ctx context.Context, creatorUserID string) (ProvisionResult, error) {
	return service.Provision(ctx, ProvisionCommand{
		DisplayName: BootstrapDisplayName, CreatorUserID: creatorUserID,
	})
}

// List returns the Organizations the user belongs to. An instance Administrator has
// no implicit visibility into an Organization it is not a member of.
func (service *Service) List(ctx context.Context, userID string) ([]Details, error) {
	values, err := service.store.ListForMember(ctx, userID)
	if err != nil {
		return nil, err
	}
	if values == nil {
		values = []Details{}
	}
	return values, nil
}

// Get returns an Organization and its default Project.
func (service *Service) Get(ctx context.Context, id ID) (Details, bool, error) {
	value, found, err := service.store.FindByID(ctx, id)
	if err != nil || !found {
		return Details{}, found, err
	}
	project, found, err := service.store.FindDefaultProject(ctx, value.ID)
	if err != nil {
		return Details{}, false, err
	}
	if !found {
		return Details{}, false, fmt.Errorf("%w: %s", ErrDefaultProjectMissing, value.ID)
	}
	return Details{Organization: value, DefaultProject: project}, true, nil
}

// Membership resolves a user's membership of one Organization. Absence is the caller's
// signal to answer 404 rather than 403, so an Organization the user does not belong to
// is indistinguishable from one that does not exist.
func (service *Service) Membership(ctx context.Context, id ID, userID string) (Membership, bool, error) {
	if userID == "" {
		return Membership{}, false, nil
	}
	return service.store.FindMembership(ctx, id, userID)
}

// RenameKind identifies an Organization rename result.
type RenameKind uint8

const (
	RenameInvalid RenameKind = iota + 1
	RenameRenamed
	RenameNotFound
)

// RenameResult is an expected Organization rename outcome.
type RenameResult struct {
	Kind     RenameKind
	Details  Details
	Failures []ValidationFailure
}

// Rename changes only an Organization's display name; the id is the only stable
// identifier and never changes. The rename is last-write-wins because a display
// name has no dependent state that a lost update could corrupt.
func (service *Service) Rename(ctx context.Context, id ID, requestedName string) (RenameResult, error) {
	displayName, valid := NormalizeDisplayName(requestedName)
	if !valid {
		return RenameResult{Kind: RenameInvalid, Failures: []ValidationFailure{
			{Code: DisplayNameInvalidCode, Field: "displayName", Message: DisplayNameValidationMessage},
		}}, nil
	}
	found, err := service.store.UpdateDisplayName(ctx, id, displayName)
	if err != nil {
		return RenameResult{}, err
	}
	if !found {
		return RenameResult{Kind: RenameNotFound}, nil
	}
	details, found, err := service.Get(ctx, id)
	if err != nil {
		return RenameResult{}, err
	}
	if !found {
		return RenameResult{Kind: RenameNotFound}, nil
	}
	return RenameResult{Kind: RenameRenamed, Details: details}, nil
}
