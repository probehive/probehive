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
	FindBySlug(context.Context, string) (Organization, bool, error)
	FindDefaultProject(context.Context, ID) (Project, bool, error)
	FindProject(context.Context, ID, ProjectID) (Project, bool, error)
	// Create inserts both values in one transaction and returns ErrDuplicateSlug on a uniqueness race.
	Create(context.Context, Organization, Project) error
}

// ProvisionKind identifies an idempotent Organization provisioning result.
type ProvisionKind uint8

const (
	ProvisionInvalid ProvisionKind = iota + 1
	ProvisionCreated
	ProvisionReplayed
	ProvisionSlugConflict
)

// ProvisionCommand requests an Organization and its default Project.
type ProvisionCommand struct {
	Slug        string
	DisplayName string
}

// ProvisionResult is the complete expected outcome of Organization provisioning.
type ProvisionResult struct {
	Kind     ProvisionKind
	Details  Details
	Failures []ValidationFailure
	Detail   string
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

// Provision validates and idempotently creates an Organization with its default Project.
func (service *Service) Provision(ctx context.Context, command ProvisionCommand) (ProvisionResult, error) {
	var failures []ValidationFailure
	slug, validSlug := ValidateSlug(command.Slug)
	if !validSlug {
		failures = append(failures, ValidationFailure{Field: "slug", Message: SlugValidationMessage})
	}
	displayName, validDisplayName := NormalizeDisplayName(command.DisplayName)
	if !validDisplayName {
		failures = append(failures, ValidationFailure{Field: "displayName", Message: DisplayNameValidationMessage})
	}
	if len(failures) != 0 {
		return ProvisionResult{Kind: ProvisionInvalid, Failures: failures}, nil
	}

	existing, found, err := service.store.FindBySlug(ctx, slug)
	if err != nil {
		return ProvisionResult{}, err
	}
	if found {
		return service.replayOrConflict(ctx, existing, displayName)
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
	created, err := NewOrganization(ID(organizationID), slug, displayName, now)
	if err != nil {
		return ProvisionResult{}, err
	}
	project, err := NewDefaultProject(ProjectID(projectID), created.ID, now)
	if err != nil {
		return ProvisionResult{}, err
	}
	if err = service.store.Create(ctx, created, project); err != nil {
		if !errors.Is(err, ErrDuplicateSlug) {
			return ProvisionResult{}, err
		}
		winner, winnerFound, findErr := service.store.FindBySlug(ctx, slug)
		if findErr != nil {
			return ProvisionResult{}, findErr
		}
		if !winnerFound {
			return ProvisionResult{}, fmt.Errorf("slug %q reported a uniqueness violation but no winner is readable", slug)
		}
		return service.replayOrConflict(ctx, winner, displayName)
	}
	return ProvisionResult{Kind: ProvisionCreated, Details: Details{Organization: created, DefaultProject: project}}, nil
}

func (service *Service) replayOrConflict(ctx context.Context, existing Organization, displayName string) (ProvisionResult, error) {
	if existing.DisplayName != displayName {
		return ProvisionResult{Kind: ProvisionSlugConflict, Detail: SlugConflictDetail(existing.Slug)}, nil
	}
	project, found, err := service.store.FindDefaultProject(ctx, existing.ID)
	if err != nil {
		return ProvisionResult{}, err
	}
	if !found {
		return ProvisionResult{}, fmt.Errorf("%w: %s", ErrDefaultProjectMissing, existing.ID)
	}
	return ProvisionResult{Kind: ProvisionReplayed, Details: Details{Organization: existing, DefaultProject: project}}, nil
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
