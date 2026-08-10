package maintenance

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Clock interface{ Now() time.Time }

type UUIDGenerator interface {
	NewUUIDv7(time.Time) (string, error)
}

type Store interface {
	CreateWindow(context.Context, Window) error
	ListWindows(context.Context, Scope, time.Time) ([]Window, bool, error)
	FindWindow(context.Context, Scope, ID) (Window, bool, error)
	CancelWindow(context.Context, Window, uint32) error
}

type ValidationFailure struct {
	Code    string
	Field   string
	Message string
}

type CreateCommand struct {
	Scope    Scope
	StartsAt time.Time
	EndsAt   time.Time
}

type CreateKind uint8

const (
	CreateInvalid CreateKind = iota + 1
	CreateCreated
	CreateMonitorNotFound
	CreateConflict
)

type CreateResult struct {
	Kind     CreateKind
	Window   Window
	Failures []ValidationFailure
	Code     string
	Detail   string
}

type CancelKind uint8

const (
	CancelCancelled CancelKind = iota + 1
	CancelNotFound
	CancelConflict
)

type CancelResult struct {
	Kind   CancelKind
	Window Window
	Code   string
	Detail string
}

type Service struct {
	store Store
	clock Clock
	uuids UUIDGenerator
}

func NewService(store Store, clock Clock, uuids UUIDGenerator) *Service {
	if store == nil || clock == nil || uuids == nil {
		panic("maintenance.Service requires a store, clock, and UUID generator")
	}
	return &Service{store: store, clock: clock, uuids: uuids}
}

func (service *Service) Create(ctx context.Context, command CreateCommand) (CreateResult, error) {
	now := service.clock.Now().UTC()
	startsAtValid := explicitUTC(command.StartsAt) && !command.StartsAt.Before(now)
	endsAtValid := explicitUTC(command.EndsAt) && command.EndsAt.After(command.StartsAt)

	var failures []ValidationFailure
	if !startsAtValid {
		failures = append(failures, ValidationFailure{
			Code: StartsAtInvalidCode, Field: "startsAt", Message: StartsAtValidationMessage,
		})
	}
	if !endsAtValid {
		failures = append(failures, ValidationFailure{
			Code: EndsAtInvalidCode, Field: "endsAt", Message: EndsAtValidationMessage,
		})
	}
	if startsAtValid && endsAtValid && command.EndsAt.Sub(command.StartsAt) > MaxDuration {
		failures = append(failures, ValidationFailure{
			Code: DurationInvalidCode, Field: "endsAt", Message: DurationValidationMessage,
		})
	}
	if len(failures) != 0 {
		return CreateResult{Kind: CreateInvalid, Failures: failures}, nil
	}

	id, err := service.uuids.NewUUIDv7(now)
	if err != nil {
		return CreateResult{}, fmt.Errorf("generate maintenance window id: %w", err)
	}
	created, err := NewWindow(ID(id), command.Scope, command.StartsAt.UTC(), command.EndsAt.UTC(), now)
	if err != nil {
		return CreateResult{}, err
	}
	if err = service.store.CreateWindow(ctx, created); err != nil {
		switch {
		case errors.Is(err, ErrMonitorNotFound):
			return CreateResult{Kind: CreateMonitorNotFound}, nil
		case errors.Is(err, ErrOverlap):
			return CreateResult{Kind: CreateConflict, Code: OverlapCode, Detail: OverlapDetail}, nil
		default:
			return CreateResult{}, err
		}
	}
	return CreateResult{Kind: CreateCreated, Window: created}, nil
}

// List returns current, upcoming, and cancelled-but-not-yet-ended windows in start order.
func (service *Service) List(ctx context.Context, scope Scope) ([]Window, bool, error) {
	values, found, err := service.store.ListWindows(ctx, scope, service.clock.Now().UTC())
	if err != nil || !found {
		return nil, found, err
	}
	if values == nil {
		values = []Window{}
	}
	return values, true, nil
}

func (service *Service) Get(ctx context.Context, scope Scope, id ID) (Window, bool, error) {
	return service.store.FindWindow(ctx, scope, id)
}

func (service *Service) Cancel(ctx context.Context, scope Scope, id ID) (CancelResult, error) {
	value, found, err := service.store.FindWindow(ctx, scope, id)
	if err != nil {
		return CancelResult{}, err
	}
	if !found {
		return CancelResult{Kind: CancelNotFound}, nil
	}
	if value.CancelledAt != nil {
		return CancelResult{Kind: CancelCancelled, Window: value}, nil
	}

	expectedVersion := value.Version
	if err = value.Cancel(service.clock.Now().UTC()); err != nil {
		if errors.Is(err, ErrWindowEnded) {
			return CancelResult{Kind: CancelConflict, Code: WindowEndedCode, Detail: WindowEndedDetail}, nil
		}
		return CancelResult{}, err
	}
	if err = service.store.CancelWindow(ctx, value, expectedVersion); err != nil {
		if !errors.Is(err, ErrConcurrentUpdate) {
			return CancelResult{}, err
		}
		current, currentFound, findErr := service.store.FindWindow(ctx, scope, id)
		if findErr != nil {
			return CancelResult{}, findErr
		}
		if currentFound && current.CancelledAt != nil {
			return CancelResult{Kind: CancelCancelled, Window: current}, nil
		}
		return CancelResult{
			Kind: CancelConflict, Code: ConcurrentUpdateCode, Detail: ConcurrentUpdateDetail,
		}, nil
	}
	return CancelResult{Kind: CancelCancelled, Window: value}, nil
}
