package statuspage

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
	FindDraft(context.Context, string) (Draft, bool, error)
	ReplaceDraft(context.Context, Draft, int64) error
}

type ComponentInput struct {
	MonitorID string
	Label     string
}

type ReplaceCommand struct {
	OrganizationID string
	Title          string
	Version        int64
	Components     []ComponentInput
}

type ValidationFailure struct {
	Code    string
	Field   string
	Message string
}

type ReplaceKind uint8

const (
	ReplaceInvalid ReplaceKind = iota + 1
	ReplaceUpdated
	ReplaceConflict
)

type ReplaceResult struct {
	Kind     ReplaceKind
	Draft    Draft
	Failures []ValidationFailure
	Code     string
	Detail   string
}

type Service struct {
	store Store
	clock Clock
	uuids UUIDGenerator
}

func NewService(store Store, clock Clock, uuids UUIDGenerator) *Service {
	if store == nil || clock == nil || uuids == nil {
		panic("statuspage.Service requires a store, clock, and UUID generator")
	}
	return &Service{store: store, clock: clock, uuids: uuids}
}

func (service *Service) Get(ctx context.Context, organizationID string) (Draft, bool, error) {
	return service.store.FindDraft(ctx, organizationID)
}

func (service *Service) Replace(ctx context.Context, command ReplaceCommand) (ReplaceResult, error) {
	title, titleValid := NormalizeLabel(command.Title)
	var failures []ValidationFailure
	if !titleValid {
		failures = append(failures, ValidationFailure{
			Code: TitleInvalidCode, Field: "title", Message: TitleValidationMessage,
		})
	}
	if len(command.Components) < 1 || len(command.Components) > MaxComponents {
		failures = append(failures, ValidationFailure{
			Code: ComponentsInvalidCode, Field: "components", Message: ComponentsValidationMessage,
		})
	}

	normalized := make([]ComponentInput, len(command.Components))
	seenMonitors := make(map[string]struct{}, len(command.Components))
	for index, component := range command.Components {
		field := fmt.Sprintf("components[%d]", index)
		label, validLabel := NormalizeLabel(component.Label)
		if !validLabel {
			failures = append(failures, ValidationFailure{
				Code: ComponentLabelInvalidCode, Field: field + ".label",
				Message: ComponentLabelValidationMessage,
			})
		}
		if component.MonitorID == "" {
			failures = append(failures, ValidationFailure{
				Code: ComponentMonitorInvalidCode, Field: field + ".monitorId",
				Message: ComponentMonitorValidationMessage,
			})
		} else if _, duplicate := seenMonitors[component.MonitorID]; duplicate {
			failures = append(failures, ValidationFailure{
				Code: ComponentMonitorDuplicateCode, Field: field + ".monitorId",
				Message: ComponentMonitorDuplicateMessage,
			})
		}
		seenMonitors[component.MonitorID] = struct{}{}
		normalized[index] = ComponentInput{MonitorID: component.MonitorID, Label: label}
	}
	if command.Version < 0 {
		failures = append(failures, ValidationFailure{
			Code: ConcurrentUpdateCode, Field: "version", Message: ConcurrentUpdateDetail,
		})
	}
	if len(failures) != 0 {
		return ReplaceResult{Kind: ReplaceInvalid, Failures: failures}, nil
	}

	current, found, err := service.store.FindDraft(ctx, command.OrganizationID)
	if err != nil {
		return ReplaceResult{}, err
	}
	if (!found && command.Version != 0) || (found && command.Version != current.Version) {
		return ReplaceResult{
			Kind: ReplaceConflict, Code: ConcurrentUpdateCode, Detail: ConcurrentUpdateDetail,
		}, nil
	}

	now := service.clock.Now().UTC()
	pageID := ID("")
	createdAt := now
	version := int64(1)
	existingIDs := make(map[string]ComponentID)
	if found {
		pageID, createdAt, version = current.ID, current.CreatedAt, current.Version+1
		for _, component := range current.Components {
			existingIDs[component.MonitorID] = component.ID
		}
	} else {
		generated, generateErr := service.uuids.NewUUIDv7(now)
		if generateErr != nil {
			return ReplaceResult{}, fmt.Errorf("generate status page id: %w", generateErr)
		}
		pageID = ID(generated)
	}

	components := make([]Component, len(normalized))
	for index, input := range normalized {
		componentID := existingIDs[input.MonitorID]
		if componentID == "" {
			generated, generateErr := service.uuids.NewUUIDv7(now)
			if generateErr != nil {
				return ReplaceResult{}, fmt.Errorf("generate status component id: %w", generateErr)
			}
			componentID = ComponentID(generated)
		}
		components[index] = Component{
			ID: componentID, MonitorID: input.MonitorID, Label: input.Label, Position: index,
		}
	}
	draft, err := RestoreDraft(pageID, command.OrganizationID, title, version, createdAt, now, components)
	if err != nil {
		return ReplaceResult{}, err
	}
	if err = service.store.ReplaceDraft(ctx, draft, command.Version); err != nil {
		switch {
		case errors.Is(err, ErrMonitorUnavailable):
			return ReplaceResult{Kind: ReplaceInvalid, Failures: []ValidationFailure{{
				Code: MonitorUnavailableCode, Field: "components", Message: MonitorUnavailableDetail,
			}}}, nil
		case errors.Is(err, ErrConcurrentUpdate):
			return ReplaceResult{
				Kind: ReplaceConflict, Code: ConcurrentUpdateCode, Detail: ConcurrentUpdateDetail,
			}, nil
		default:
			return ReplaceResult{}, err
		}
	}
	return ReplaceResult{Kind: ReplaceUpdated, Draft: draft}, nil
}
