package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

var ErrConfirmationHeld = errors.New("the confirmation Run is still held")

type ConfirmationRequest struct {
	EventID                string
	OrganizationID         string
	CandidateID            string
	MonitorID              string
	RevisionNumber         int
	Location               string
	TriggeringRunID        string
	TriggeringScheduledFor time.Time
	RequestedFor           time.Time
	ExpectedEvidence       string
	PolicyVersion          string
}

func (request ConfirmationRequest) Validate() error {
	if request.EventID == "" || request.OrganizationID == "" || request.CandidateID == "" ||
		request.MonitorID == "" || request.TriggeringRunID == "" || request.PolicyVersion == "" {
		return errors.New("a confirmation request requires event, tenant, candidate, Monitor, triggering Run, and policy identity")
	}
	if request.RevisionNumber < 1 || request.Location == "" || len(request.Location) > MaxLocationLength {
		return errors.New("a confirmation request requires a revision and bounded location")
	}
	if !isUTC(request.TriggeringScheduledFor) || !isUTC(request.RequestedFor) {
		return errors.New("a confirmation request requires UTC instants")
	}
	if request.ExpectedEvidence != "passing" && request.ExpectedEvidence != "failing" {
		return errors.New("a confirmation request expects passing or failing evidence")
	}
	return nil
}

type ConfirmationStore interface {
	Store
	LoadConfirmationTarget(context.Context, ConfirmationRequest) (Schedulable, bool, error)
	FindConfirmation(context.Context, string, string) (Run, bool, error)
}

type ConfirmationConfig struct {
	Store            ConfirmationStore
	Executor         Executor
	Clock            Clock
	UUIDs            IDGenerator
	Logger           *slog.Logger
	ExecutionCeiling time.Duration
	ExecutionSlots   *ExecutionSlots
}

type ConfirmationRunner struct {
	config ConfirmationConfig
}

func NewConfirmationRunner(config ConfirmationConfig) (*ConfirmationRunner, error) {
	if config.Store == nil || config.Executor == nil || config.Clock == nil || config.UUIDs == nil {
		return nil, errors.New("confirmation runner requires a store, executor, clock, and UUID generator")
	}
	if config.ExecutionCeiling <= 0 {
		return nil, errors.New("confirmation runner requires a positive execution ceiling")
	}
	if config.ExecutionSlots == nil {
		var err error
		config.ExecutionSlots, err = NewExecutionSlots(DefaultConcurrency)
		if err != nil {
			return nil, err
		}
	}
	if config.Logger == nil {
		config.Logger = slog.New(slog.DiscardHandler)
	}
	return &ConfirmationRunner{config: config}, nil
}

func (runner *ConfirmationRunner) Execute(ctx context.Context, request ConfirmationRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	target, eligible, err := runner.config.Store.LoadConfirmationTarget(ctx, request)
	if err != nil {
		return fmt.Errorf("load confirmation target: %w", err)
	}
	if !eligible {
		return nil
	}
	if err := runner.config.ExecutionSlots.Acquire(ctx); err != nil {
		return err
	}
	defer runner.config.ExecutionSlots.Release()
	now := runner.config.Clock.Now().UTC()
	holder, err := runner.config.UUIDs.NewUUIDv7(now)
	if err != nil {
		return fmt.Errorf("generate confirmation lease holder: %w", err)
	}
	runID, err := runner.config.UUIDs.NewUUIDv7(request.RequestedFor)
	if err != nil {
		return fmt.Errorf("generate confirmation Run id: %w", err)
	}
	proposed, err := ClaimConfirmation(
		ID(runID),
		Slot{
			OrganizationID: request.OrganizationID,
			MonitorID:      request.MonitorID,
			RevisionNumber: request.RevisionNumber,
			Location:       request.Location,
			ScheduledFor:   request.RequestedFor,
		},
		ConfirmationCause{
			CandidateID:            request.CandidateID,
			TriggeringRunID:        ID(request.TriggeringRunID),
			TriggeringScheduledFor: request.TriggeringScheduledFor,
			CausationEventID:       request.EventID,
			PolicyVersion:          request.PolicyVersion,
		},
		holder,
		now.Add(LeaseDuration(runner.config.ExecutionCeiling)),
	)
	if err != nil {
		return fmt.Errorf("build confirmation Run: %w", err)
	}
	claimed, err := runner.config.Store.ClaimSlot(ctx, proposed, now)
	if errors.Is(err, ErrSlotHeld) {
		existing, found, findErr := runner.config.Store.FindConfirmation(
			ctx, request.OrganizationID, request.CandidateID)
		if findErr != nil {
			return fmt.Errorf("find existing confirmation Run: %w", findErr)
		}
		if found && !existing.InFlight() {
			return nil
		}
		return ErrConfirmationHeld
	}
	if err != nil {
		return fmt.Errorf("claim confirmation Run: %w", err)
	}

	executionContext, cancel := context.WithTimeout(ctx, runner.config.ExecutionCeiling)
	defer cancel()
	execution := runner.config.Executor.Execute(
		executionContext, target.CheckType, target.CheckSchemaVersion, json.RawMessage(target.CheckConfiguration))
	if ctx.Err() != nil {
		releaseContext, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
		defer releaseCancel()
		if releaseErr := runner.config.Store.ReleaseSlot(releaseContext, claimed); releaseErr != nil &&
			!errors.Is(releaseErr, ErrLeaseLost) {
			runner.config.Logger.Error("cannot release confirmation Run", "error", releaseErr)
		}
		return ctx.Err()
	}
	completed := claimed
	if err := completed.Complete(execution.Outcome, execution.StartedAt.UTC(), execution.FinishedAt.UTC()); err != nil {
		return fmt.Errorf("complete confirmation Run domain state: %w", err)
	}
	observation := execution.Observation
	observation.RunID = completed.ID
	observation.ScheduledFor = completed.Slot.ScheduledFor
	observation.OrganizationID = completed.Slot.OrganizationID
	eventID, err := runner.config.UUIDs.NewUUIDv7(completed.FinishedAt)
	if err != nil {
		return fmt.Errorf("generate confirmation result event: %w", err)
	}
	entry, err := NewRunRecordedEntry(ID(eventID), completed, completed.FinishedAt)
	if err != nil {
		return fmt.Errorf("build confirmation result event: %w", err)
	}
	if err := runner.config.Store.Complete(
		ctx, completed, claimed.LeaseHolder, observation, []OutboxEntry{entry}); err != nil {
		return fmt.Errorf("record confirmation Run: %w", err)
	}
	return nil
}
