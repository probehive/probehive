package webhook

import (
	"context"
	"errors"
)

type StateCommand struct {
	OrganizationID  string
	IntegrationID   string
	ExpectedVersion int64
	Enabled         *bool
}

type StateKind uint8

const (
	StateUpdated StateKind = iota + 1
	StateInvalid
	StateNotFound
	StateConflict
)

type StateResult struct {
	Kind        StateKind
	Integration Integration
	Failures    []ValidationFailure
	Code        string
	Detail      string
}

func (service *Service) SetEnabled(
	ctx context.Context, command StateCommand,
) (StateResult, error) {
	if command.OrganizationID == "" || command.IntegrationID == "" {
		return StateResult{}, errors.New("Webhook state change requires Organization and Integration identity")
	}
	var failures []ValidationFailure
	if command.Enabled == nil {
		failures = append(failures, ValidationFailure{
			Code: EnabledInvalidCode, Field: "enabled", Message: EnabledValidationMessage,
		})
	}
	if command.ExpectedVersion < 1 {
		failures = append(failures, ValidationFailure{
			Code: VersionInvalidCode, Field: "version", Message: VersionValidationMessage,
		})
	}
	if len(failures) != 0 {
		return StateResult{Kind: StateInvalid, Failures: failures}, nil
	}

	value, err := service.store.SetEnabled(
		ctx, command.OrganizationID, command.IntegrationID,
		command.ExpectedVersion, *command.Enabled, service.clock.Now().UTC(),
	)
	if err != nil {
		switch {
		case errors.Is(err, ErrIntegrationNotFound):
			return StateResult{Kind: StateNotFound}, nil
		case errors.Is(err, ErrConcurrentUpdate):
			return StateResult{
				Kind: StateConflict, Code: ConcurrentUpdateCode,
				Detail: "The Webhook Integration changed; reload it before retrying.",
			}, nil
		case errors.Is(err, ErrEnabledLimit):
			return StateResult{
				Kind: StateConflict, Code: EnabledLimitCode,
				Detail: "An Organization may have at most five enabled Webhook Integrations.",
			}, nil
		default:
			return StateResult{}, err
		}
	}
	return StateResult{Kind: StateUpdated, Integration: value}, nil
}
