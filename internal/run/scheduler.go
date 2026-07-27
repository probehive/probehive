package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// MonitorSource supplies the Monitors that are eligible to run. The scheduler asks for all
// of them and decides which are due itself, because a slot is derived arithmetic rather than
// stored state (ADR 0026).
type MonitorSource interface {
	ListSchedulable(ctx context.Context) ([]Schedulable, error)
}

// Store is the Run persistence port the scheduler consumes.
type Store interface {
	ClaimSlot(ctx context.Context, value Run, now time.Time) (Run, error)
	ReleaseSlot(ctx context.Context, value Run) error
	Complete(ctx context.Context, value Run, holder string, observation Observation, entries []OutboxEntry) error
	RecordSkipped(ctx context.Context, value Run, now time.Time) error
}

// Execution is one executor's report of one Run. The Observation it carries has no identity:
// the scheduler owns the Run the measurement belongs to and stamps it before storing.
type Execution struct {
	Outcome     Outcome
	StartedAt   time.Time
	FinishedAt  time.Time
	Observation Observation
}

// Executor turns a stored check configuration into a measurement. The composition root
// implements it over internal/probe, which is the one place the two Observation shapes that
// ADR 0025 keeps separate are allowed to meet.
type Executor interface {
	Execute(ctx context.Context, checkType string, schemaVersion int, configuration json.RawMessage) Execution
}

// IDGenerator supplies time-ordered UUID version 7 identifiers.
type IDGenerator interface {
	NewUUIDv7(time.Time) (string, error)
}

// Clock supplies domain-relevant time.
type Clock interface{ Now() time.Time }

// Scheduler bounds and defaults.
const (
	// DefaultTickInterval is how often the scheduler looks for due slots. It is well below
	// MinIntervalSeconds so a slot is noticed near the instant it becomes due.
	DefaultTickInterval = 5 * time.Second
	// DefaultMaxBackfillSlots bounds how many missed slots are recorded per Monitor per tick
	// (ADR 0026). Beyond it, the absence of Runs is the record.
	DefaultMaxBackfillSlots = 10
	// DefaultConcurrency bounds in-flight executions for the embedded worker (ADR 0020).
	DefaultConcurrency = 8
	// DefaultLocation is the Probe Location identifier a self-hosted installation reports
	// until the Probe Location registry exists (ADR 0026).
	DefaultLocation = "local"
	// releaseTimeout bounds the out-of-band release of a slot during shutdown, which cannot
	// use the cancelled context that caused the shutdown.
	releaseTimeout = 5 * time.Second
)

// SchedulerConfig composes a Scheduler. Every collaborator is a port, so the whole tick is
// exercisable without a database, an HTTP server, or a clock that moves on its own.
type SchedulerConfig struct {
	Source   MonitorSource
	Store    Store
	Executor Executor
	Clock    Clock
	UUIDs    IDGenerator
	Logger   *slog.Logger

	// Location is the Probe Location identifier every claimed slot carries.
	Location string
	// MinimumInterval is the operator floor applied to every Monitor's configured interval.
	MinimumInterval time.Duration
	// ExecutionCeiling is the operator's total execution limit. It derives the lease
	// duration, so a lease always outlives the execution it protects.
	ExecutionCeiling time.Duration
	// TickInterval is how often due slots are looked for. Zero takes DefaultTickInterval.
	TickInterval time.Duration
	// MaxBackfillSlots bounds recorded misfires per Monitor per tick. Zero takes the default.
	MaxBackfillSlots int
	// Concurrency bounds in-flight executions. Zero takes DefaultConcurrency.
	Concurrency int
}

// Scheduler puts active Monitors on their derived slot series, claims due slots, executes
// them, and records what happened (ADR 0026).
//
// It does not renew leases while executing. A lease is the execution ceiling plus a margin
// (LeaseDuration), and every execution is bounded by that same ceiling, so a lease outlives
// the run it protects by construction. Store.RenewLease exists for a future executor whose
// duration is not bounded in advance — a browser journey, a remote Agent — and calling it
// here would be a renewal that can never arrive in time to matter.
type Scheduler struct {
	config SchedulerConfig
	// attempted remembers the last slot attempted per Monitor so a five-second tick does not
	// re-attempt a five-minute slot sixty times. It is an optimization with no correctness
	// role: losing it costs one redundant insert that the slot index rejects harmlessly.
	attempted map[string]time.Time
}

// NewScheduler validates a composition and returns a Scheduler.
func NewScheduler(config SchedulerConfig) (*Scheduler, error) {
	if config.Source == nil || config.Store == nil || config.Executor == nil {
		return nil, errors.New("run.Scheduler requires a Monitor source, a Run store, and an executor")
	}
	if config.Clock == nil || config.UUIDs == nil {
		return nil, errors.New("run.Scheduler requires a clock and a UUID generator")
	}
	if config.Location == "" {
		config.Location = DefaultLocation
	}
	if len(config.Location) > MaxLocationLength {
		return nil, fmt.Errorf("a Probe Location identifier is at most %d bytes", MaxLocationLength)
	}
	if config.ExecutionCeiling <= 0 {
		return nil, errors.New("run.Scheduler requires a positive execution ceiling")
	}
	if config.MinimumInterval < 0 {
		return nil, errors.New("an operator interval floor cannot be negative")
	}
	if config.TickInterval <= 0 {
		config.TickInterval = DefaultTickInterval
	}
	if config.MaxBackfillSlots <= 0 {
		config.MaxBackfillSlots = DefaultMaxBackfillSlots
	}
	if config.Concurrency <= 0 {
		config.Concurrency = DefaultConcurrency
	}
	if config.Logger == nil {
		config.Logger = slog.New(slog.DiscardHandler)
	}
	return &Scheduler{config: config, attempted: make(map[string]time.Time)}, nil
}

// TickResult counts what one pass over the active Monitors did.
type TickResult struct {
	// Considered is how many active Monitors the tick looked at.
	Considered int
	// Claimed is how many slots this scheduler took and executed.
	Claimed int
	// Held is how many due slots another worker owned or had already finished.
	Held int
	// Skipped is how many missed slots were recorded under the misfire policy.
	Skipped int
	// Failed is how many Monitors could not be scheduled at all. A failure here is an
	// installation problem, not a check result, so it never becomes a Run.
	Failed int
}

// Serve ticks until the context is done. It is the embedded worker's loop (ADR 0020).
func (scheduler *Scheduler) Serve(ctx context.Context) error {
	ticker := time.NewTicker(scheduler.config.TickInterval)
	defer ticker.Stop()
	scheduler.config.Logger.Info("ProbeHive scheduler started",
		"location", scheduler.config.Location,
		"tickInterval", scheduler.config.TickInterval,
		"concurrency", scheduler.config.Concurrency)

	for {
		select {
		case <-ctx.Done():
			scheduler.config.Logger.Info("ProbeHive scheduler stopped")
			return nil
		case <-ticker.C:
			result, err := scheduler.Tick(ctx, scheduler.config.Clock.Now().UTC())
			if err != nil {
				if ctx.Err() != nil {
					continue
				}
				// A tick that cannot list Monitors is logged and retried on the next tick.
				// Stopping the scheduler because the database blinked would turn a transient
				// fault into an outage that needs a restart to clear.
				scheduler.config.Logger.Error("scheduler tick failed", "error", err)
				continue
			}
			if result.Claimed != 0 || result.Skipped != 0 || result.Failed != 0 {
				scheduler.config.Logger.Info("scheduler tick",
					"considered", result.Considered, "claimed", result.Claimed,
					"held", result.Held, "skipped", result.Skipped, "failed", result.Failed)
			}
		}
	}
}

// Tick performs exactly one pass: list active Monitors, record the slots that were missed,
// and claim and execute the slot that is currently due.
func (scheduler *Scheduler) Tick(ctx context.Context, now time.Time) (TickResult, error) {
	monitors, err := scheduler.config.Source.ListSchedulable(ctx)
	if err != nil {
		return TickResult{}, fmt.Errorf("list schedulable Monitors: %w", err)
	}

	var (
		mutex  sync.Mutex
		result = TickResult{Considered: len(monitors)}
		group  sync.WaitGroup
		slots  = make(chan struct{}, scheduler.config.Concurrency)
	)
	for _, schedulable := range monitors {
		due, previous, ok := scheduler.plan(schedulable, now)
		if !ok {
			result.Failed++
			continue
		}
		if due.IsZero() {
			continue
		}
		scheduler.attempted[schedulable.MonitorID] = due

		group.Add(1)
		slots <- struct{}{}
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			// Backfill runs inside the pool rather than in the loop above: on the first tick
			// after a restart every Monitor has missed slots to record, and doing that
			// serially would make startup cost one round trip per Monitor per missed slot.
			skipped := scheduler.recordMissed(ctx, schedulable, due, previous, now)
			outcome := scheduler.runSlot(ctx, schedulable, due, now)
			mutex.Lock()
			defer mutex.Unlock()
			result.Skipped += skipped
			switch outcome {
			case slotClaimed:
				result.Claimed++
			case slotHeld:
				result.Held++
			case slotFailed:
				result.Failed++
			}
		}()
	}
	group.Wait()
	return result, nil
}

type slotOutcome uint8

const (
	slotClaimed slotOutcome = iota + 1
	slotHeld
	slotFailed
)

// plan computes the slot that is currently due, and the last slot this scheduler attempted.
// A due slot already attempted this run returns zero, which is the in-memory suppression that
// keeps a fast tick from re-attempting a slow Monitor's slot on every pass.
func (scheduler *Scheduler) plan(schedulable Schedulable, now time.Time) (due, previous time.Time, ok bool) {
	if err := schedulable.Validate(); err != nil {
		scheduler.config.Logger.Error("skipping unschedulable Monitor",
			"monitorId", schedulable.MonitorID, "error", err)
		return time.Time{}, time.Time{}, false
	}
	interval := EffectiveInterval(schedulable.Interval, scheduler.config.MinimumInterval)
	current, err := SlotFor(schedulable.MonitorID, interval, now)
	if err != nil {
		scheduler.config.Logger.Error("cannot derive a slot",
			"monitorId", schedulable.MonitorID, "error", err)
		return time.Time{}, time.Time{}, false
	}
	last := scheduler.attempted[schedulable.MonitorID]
	if !last.IsZero() && !current.After(last) {
		return time.Time{}, last, true
	}
	return current, last, true
}

// recordMissed writes the misfire Runs for slots between the last one this scheduler attempted
// and the one now due (ADR 0021, bounded by ADR 0026). A slot another worker already ran is
// rejected by the slot index, so no query is needed to find out which ones were really missed.
func (scheduler *Scheduler) recordMissed(ctx context.Context, schedulable Schedulable, due, previous, now time.Time) int {
	interval := EffectiveInterval(schedulable.Interval, scheduler.config.MinimumInterval)
	// The walk stops at whichever is later: the last slot this scheduler attempted, or the
	// instant the Monitor last changed. The second bound is what stops a Monitor that was
	// just activated from being credited with missing slots it was never eligible for.
	after := previous
	if schedulable.NotBefore.After(after) {
		after = schedulable.NotBefore.UTC()
	}
	missed, err := MissedSlots(schedulable.MonitorID, interval, due, after, scheduler.config.MaxBackfillSlots)
	if err != nil || len(missed) == 0 {
		return 0
	}

	recorded := 0
	for _, instant := range missed {
		if ctx.Err() != nil {
			return recorded
		}
		value, buildErr := scheduler.newRun(schedulable, instant, "", now)
		if buildErr != nil {
			scheduler.config.Logger.Error("cannot build a skipped Run",
				"monitorId", schedulable.MonitorID, "error", buildErr)
			return recorded
		}
		skipped, skipErr := Skip(value.ID, value.Slot, KindScheduled)
		if skipErr != nil {
			scheduler.config.Logger.Error("cannot build a skipped Run",
				"monitorId", schedulable.MonitorID, "error", skipErr)
			return recorded
		}
		switch err := scheduler.config.Store.RecordSkipped(ctx, skipped, now); {
		case err == nil:
			recorded++
		case errors.Is(err, ErrSlotHeld):
			// The slot ran, or another worker recorded it. Everything older than a slot that
			// exists also exists, so there is nothing left to walk back to.
			return recorded
		default:
			scheduler.config.Logger.Error("cannot record a skipped Run",
				"monitorId", schedulable.MonitorID, "error", err)
			return recorded
		}
	}
	return recorded
}

// runSlot claims one due slot, executes it, and records the result under the lease it took.
func (scheduler *Scheduler) runSlot(ctx context.Context, schedulable Schedulable, due, now time.Time) slotOutcome {
	holder, err := scheduler.config.UUIDs.NewUUIDv7(now)
	if err != nil {
		scheduler.config.Logger.Error("cannot generate a lease holder token", "error", err)
		return slotFailed
	}
	proposed, err := scheduler.newRun(schedulable, due, holder, now)
	if err != nil {
		scheduler.config.Logger.Error("cannot build a Run", "monitorId", schedulable.MonitorID, "error", err)
		return slotFailed
	}

	claimed, err := scheduler.config.Store.ClaimSlot(ctx, proposed, now)
	switch {
	case errors.Is(err, ErrSlotHeld):
		return slotHeld
	case err != nil:
		if ctx.Err() != nil {
			return slotHeld
		}
		scheduler.config.Logger.Error("cannot claim a slot", "monitorId", schedulable.MonitorID, "error", err)
		return slotFailed
	}

	executionContext, cancel := context.WithTimeout(ctx, scheduler.config.ExecutionCeiling)
	defer cancel()
	execution := scheduler.config.Executor.Execute(
		executionContext, schedulable.CheckType, schedulable.CheckSchemaVersion, schedulable.CheckConfiguration)

	// A shutdown mid-execution releases the slot instead of leaving it claimed until the
	// lease expires (ADR 0021). The release cannot use the context that was just cancelled.
	if ctx.Err() != nil {
		releaseContext, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
		defer releaseCancel()
		if err := scheduler.config.Store.ReleaseSlot(releaseContext, claimed); err != nil && !errors.Is(err, ErrLeaseLost) {
			scheduler.config.Logger.Error("cannot release a slot during shutdown",
				"monitorId", schedulable.MonitorID, "error", err)
		}
		return slotHeld
	}

	completed := claimed
	if err := completed.Complete(execution.Outcome, execution.StartedAt.UTC(), execution.FinishedAt.UTC()); err != nil {
		scheduler.config.Logger.Error("executor reported an unusable result",
			"monitorId", schedulable.MonitorID, "error", err)
		return slotFailed
	}
	observation := execution.Observation
	observation.RunID = completed.ID
	observation.ScheduledFor = completed.Slot.ScheduledFor
	observation.OrganizationID = completed.Slot.OrganizationID

	switch err := scheduler.config.Store.Complete(ctx, completed, claimed.LeaseHolder, observation, nil); {
	case err == nil:
		return slotClaimed
	case errors.Is(err, ErrLeaseLost):
		// The lease was reclaimed while this execution ran, so the result is discarded rather
		// than written. That is ADR 0021's rule, and the store enforces it transactionally.
		scheduler.config.Logger.Warn("discarding a result whose lease was lost",
			"monitorId", schedulable.MonitorID, "scheduledFor", completed.Slot.ScheduledFor)
		return slotHeld
	default:
		scheduler.config.Logger.Error("cannot record a Run", "monitorId", schedulable.MonitorID, "error", err)
		return slotFailed
	}
}

// newRun builds the Run for one slot. An empty holder produces the identity and slot only,
// which is what a skipped Run needs.
//
// The lease is measured from now rather than from the slot instant. A tick that runs late —
// after a restart, or behind a slow database — would otherwise mint a lease that expired
// before it was written, and the claim would be reclaimable the moment it was taken.
func (scheduler *Scheduler) newRun(schedulable Schedulable, due time.Time, holder string, now time.Time) (Run, error) {
	id, err := scheduler.config.UUIDs.NewUUIDv7(due)
	if err != nil {
		return Run{}, fmt.Errorf("generate Run id: %w", err)
	}
	slot := Slot{
		OrganizationID: schedulable.OrganizationID,
		MonitorID:      schedulable.MonitorID,
		RevisionNumber: schedulable.RevisionNumber,
		Location:       scheduler.config.Location,
		ScheduledFor:   due.UTC(),
	}
	if holder == "" {
		if err := slot.Validate(); err != nil {
			return Run{}, err
		}
		return Run{ID: ID(id), Slot: slot, Kind: KindScheduled}, nil
	}
	return Claim(ID(id), slot, KindScheduled, holder,
		now.UTC().Add(LeaseDuration(scheduler.config.ExecutionCeiling)))
}
