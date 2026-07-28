package run

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

var (
	// ErrManualTargetNotFound means the Monitor is outside the complete supplied scope.
	ErrManualTargetNotFound = errors.New("the manual Run target was not found")
	// ErrManualTargetUnconfigured means the Monitor exists but has no revision to execute.
	ErrManualTargetUnconfigured = errors.New("the manual Run target has no revision")
	// ErrManualCapacity means the shared execution concurrency is already fully occupied.
	ErrManualCapacity = errors.New("manual Run execution capacity is exhausted")
)

// ManualStore is the persistence port for explicitly requested executions.
type ManualStore interface {
	Store
	LoadManualTarget(context.Context, Scope) (Schedulable, bool, error)
	MonitorExists(context.Context, Scope) (bool, error)
}

// ManualConfig composes the manual Run use case.
type ManualConfig struct {
	Store            ManualStore
	Executor         Executor
	Clock            Clock
	UUIDs            IDGenerator
	Logger           *slog.Logger
	ExecutionSlots   *ExecutionSlots
	Location         string
	ExecutionCeiling time.Duration
}

// ManualRunner executes one Monitor revision because a person explicitly requested it.
type ManualRunner struct {
	config ManualConfig
}

// NewManualRunner validates a manual Run composition.
func NewManualRunner(config ManualConfig) (*ManualRunner, error) {
	if config.Store == nil || config.Executor == nil || config.Clock == nil || config.UUIDs == nil {
		return nil, errors.New("manual runner requires a store, executor, clock, and UUID generator")
	}
	if config.Location == "" {
		config.Location = DefaultLocation
	}
	if len(config.Location) > MaxLocationLength {
		return nil, fmt.Errorf("a Probe Location identifier is at most %d bytes", MaxLocationLength)
	}
	if config.ExecutionCeiling <= 0 {
		return nil, errors.New("manual runner requires a positive execution ceiling")
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
	return &ManualRunner{config: config}, nil
}

// Trigger executes the latest revision under the complete Monitor scope. Scheduling state
// does not apply: a manual request may deliberately exercise a draft or paused Monitor.
func (runner *ManualRunner) Trigger(ctx context.Context, scope Scope) (Run, error) {
	if err := scope.validate(); err != nil {
		return Run{}, err
	}
	target, found, err := runner.config.Store.LoadManualTarget(ctx, scope)
	if err != nil {
		return Run{}, fmt.Errorf("load manual Run target: %w", err)
	}
	if !found {
		exists, existsErr := runner.config.Store.MonitorExists(ctx, scope)
		if existsErr != nil {
			return Run{}, fmt.Errorf("check manual Run target: %w", existsErr)
		}
		if !exists {
			return Run{}, ErrManualTargetNotFound
		}
		return Run{}, ErrManualTargetUnconfigured
	}
	if err := target.Validate(); err != nil {
		return Run{}, fmt.Errorf("validate manual Run target: %w", err)
	}
	if !runner.config.ExecutionSlots.TryAcquire() {
		return Run{}, ErrManualCapacity
	}
	defer runner.config.ExecutionSlots.Release()

	now := runner.config.Clock.Now().UTC()
	holder, err := runner.config.UUIDs.NewUUIDv7(now)
	if err != nil {
		return Run{}, fmt.Errorf("generate manual Run lease holder: %w", err)
	}
	runID, err := runner.config.UUIDs.NewUUIDv7(now)
	if err != nil {
		return Run{}, fmt.Errorf("generate manual Run id: %w", err)
	}
	proposed, err := Claim(
		ID(runID),
		Slot{
			OrganizationID: target.OrganizationID,
			MonitorID:      target.MonitorID,
			RevisionNumber: target.RevisionNumber,
			Location:       runner.config.Location,
			ScheduledFor:   now,
		},
		KindManual,
		holder,
		now.Add(LeaseDuration(runner.config.ExecutionCeiling)),
	)
	if err != nil {
		return Run{}, fmt.Errorf("build manual Run: %w", err)
	}
	claimed, err := runner.config.Store.ClaimSlot(ctx, proposed, now)
	if err != nil {
		return Run{}, fmt.Errorf("claim manual Run: %w", err)
	}

	recorded := false
	defer func() {
		if recorded {
			return
		}
		releaseContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
		defer cancel()
		if releaseErr := runner.config.Store.ReleaseSlot(releaseContext, claimed); releaseErr != nil &&
			!errors.Is(releaseErr, ErrLeaseLost) {
			runner.config.Logger.Error("cannot release failed manual Run", "error", releaseErr)
		}
	}()

	executionContext, cancel := context.WithTimeout(ctx, runner.config.ExecutionCeiling)
	defer cancel()
	execution := runner.config.Executor.Execute(
		executionContext, target.CheckType, target.CheckSchemaVersion, target.CheckConfiguration)
	if ctx.Err() != nil {
		return Run{}, ctx.Err()
	}
	completed := claimed
	if err := completed.Complete(
		execution.Outcome, execution.StartedAt.UTC(), execution.FinishedAt.UTC()); err != nil {
		return Run{}, fmt.Errorf("complete manual Run domain state: %w", err)
	}
	observation := execution.Observation
	observation.RunID = completed.ID
	observation.ScheduledFor = completed.Slot.ScheduledFor
	observation.OrganizationID = completed.Slot.OrganizationID
	eventID, err := runner.config.UUIDs.NewUUIDv7(completed.FinishedAt)
	if err != nil {
		return Run{}, fmt.Errorf("generate manual Run event: %w", err)
	}
	entry, err := NewRunRecordedEntry(ID(eventID), completed, completed.FinishedAt)
	if err != nil {
		return Run{}, fmt.Errorf("build manual Run event: %w", err)
	}
	if err := runner.config.Store.Complete(
		ctx, completed, claimed.LeaseHolder, observation, []OutboxEntry{entry}); err != nil {
		return Run{}, fmt.Errorf("record manual Run: %w", err)
	}
	recorded = true
	return completed, nil
}
