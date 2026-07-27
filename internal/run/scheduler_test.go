package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeStore records what the scheduler asked it to do and can be told to contend for slots.
type fakeStore struct {
	mutex     sync.Mutex
	claimed   []Run
	completed []Run
	skipped   []Run
	released  []Run
	// heldSlots are slot instants that report ErrSlotHeld when claimed.
	heldSlots map[time.Time]bool
	// skipHeld reports every RecordSkipped as already held, which is the steady state where
	// the previous slot really did run.
	skipHeld bool
	// loseLease makes Complete report the lease was reclaimed.
	loseLease  bool
	claimError error
}

func newFakeStore() *fakeStore { return &fakeStore{heldSlots: map[time.Time]bool{}} }

func (store *fakeStore) ClaimSlot(_ context.Context, value Run, _ time.Time) (Run, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.claimError != nil {
		return Run{}, store.claimError
	}
	if store.heldSlots[value.Slot.ScheduledFor] {
		return Run{}, ErrSlotHeld
	}
	store.heldSlots[value.Slot.ScheduledFor] = true
	store.claimed = append(store.claimed, value)
	return value, nil
}

func (store *fakeStore) ReleaseSlot(_ context.Context, value Run) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.released = append(store.released, value)
	return nil
}

func (store *fakeStore) Complete(_ context.Context, value Run, holder string, observation Observation, _ []OutboxEntry) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.loseLease {
		return ErrLeaseLost
	}
	if observation.RunID != value.ID {
		return fmt.Errorf("observation %q does not belong to Run %q", observation.RunID, value.ID)
	}
	if observation.OrganizationID != value.Slot.OrganizationID || !observation.ScheduledFor.Equal(value.Slot.ScheduledFor) {
		return errors.New("observation identity was not stamped from the Run")
	}
	if holder == "" {
		return errors.New("completion did not carry the lease holder")
	}
	store.completed = append(store.completed, value)
	return nil
}

func (store *fakeStore) RecordSkipped(_ context.Context, value Run, _ time.Time) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.skipHeld {
		return ErrSlotHeld
	}
	store.skipped = append(store.skipped, value)
	return nil
}

type fakeSource struct {
	monitors []Schedulable
	err      error
}

func (source *fakeSource) ListSchedulable(context.Context) ([]Schedulable, error) {
	return source.monitors, source.err
}

type fakeExecutor struct {
	mutex     sync.Mutex
	calls     int
	outcome   Outcome
	statusFor func() *HTTPDetail
}

func (executor *fakeExecutor) Execute(_ context.Context, _ string, _ int, _ json.RawMessage) Execution {
	executor.mutex.Lock()
	executor.calls++
	executor.mutex.Unlock()

	outcome := executor.outcome
	if outcome == "" {
		outcome = OutcomePassed
	}
	instant := time.Date(2026, time.July, 27, 10, 0, 1, 0, time.UTC)
	execution := Execution{
		Outcome:    outcome,
		StartedAt:  instant,
		FinishedAt: instant.Add(120 * time.Millisecond),
		Observation: Observation{
			Duration: 120 * time.Millisecond,
			Phases:   Phases{Connect: 10 * time.Millisecond, FirstByte: 90 * time.Millisecond},
		},
	}
	if executor.statusFor != nil {
		execution.Observation.HTTP = executor.statusFor()
	}
	return execution
}

type fixedClock struct{ value time.Time }

func (clock fixedClock) Now() time.Time { return clock.value }

type sequenceIDs struct {
	mutex sync.Mutex
	next  int
}

func (generator *sequenceIDs) NewUUIDv7(time.Time) (string, error) {
	generator.mutex.Lock()
	defer generator.mutex.Unlock()
	generator.next++
	return fmt.Sprintf("00000000-0000-7000-8000-%012d", generator.next), nil
}

func testSchedulable() Schedulable {
	return Schedulable{
		OrganizationID: "00000000-0000-7000-8000-000000000001",
		MonitorID:      "00000000-0000-7000-8000-000000000002",
		RevisionNumber: 3,
		CheckType:      "http", CheckSchemaVersion: 1,
		CheckConfiguration: json.RawMessage(`{"url":"https://example.test"}`),
		Interval:           time.Minute,
	}
}

func newTestScheduler(t *testing.T, source MonitorSource, store Store, executor Executor, now time.Time) *Scheduler {
	t.Helper()
	scheduler, err := NewScheduler(SchedulerConfig{
		Source: source, Store: store, Executor: executor,
		Clock: fixedClock{value: now}, UUIDs: &sequenceIDs{},
		Location: "test-location", ExecutionCeiling: 30 * time.Second,
		MaxBackfillSlots: 3,
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	return scheduler
}

func TestTickClaimsExecutesAndRecordsTheDueSlot(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 27, 10, 30, 0, 0, time.UTC)
	schedulable := testSchedulable()
	store := newFakeStore()
	store.skipHeld = true
	executor := &fakeExecutor{}
	scheduler := newTestScheduler(t, &fakeSource{monitors: []Schedulable{schedulable}}, store, executor, now)

	result, err := scheduler.Tick(t.Context(), now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Considered != 1 || result.Claimed != 1 {
		t.Fatalf("Tick() = %#v, want one Monitor considered and claimed", result)
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
	if len(store.completed) != 1 {
		t.Fatalf("completed Runs = %d, want 1", len(store.completed))
	}

	completed := store.completed[0]
	if completed.Outcome != OutcomePassed || completed.InFlight() {
		t.Fatalf("completed Run = %q/in-flight %v, want a finished passed Run", completed.Outcome, completed.InFlight())
	}
	if completed.Slot.Location != "test-location" {
		t.Fatalf("Run location = %q, want the configured location", completed.Slot.Location)
	}
	if completed.Slot.RevisionNumber != schedulable.RevisionNumber {
		t.Fatalf("Run revision = %d, want the Monitor's latest %d",
			completed.Slot.RevisionNumber, schedulable.RevisionNumber)
	}

	expected, err := SlotFor(schedulable.MonitorID, schedulable.Interval, now)
	if err != nil {
		t.Fatalf("SlotFor() error = %v", err)
	}
	if !completed.Slot.ScheduledFor.Equal(expected) {
		t.Fatalf("Run slot = %v, want the derived %v", completed.Slot.ScheduledFor, expected)
	}
}

// The in-memory suppression is what keeps a five-second tick from re-attempting a one-minute
// slot twelve times. Losing it is harmless, but having it must not skip a genuinely new slot.
func TestTickAttemptsEachSlotOnceAndAdvancesWithTime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 27, 10, 30, 0, 0, time.UTC)
	schedulable := testSchedulable()
	store := newFakeStore()
	store.skipHeld = true
	executor := &fakeExecutor{}
	scheduler := newTestScheduler(t, &fakeSource{monitors: []Schedulable{schedulable}}, store, executor, now)

	for range 4 {
		if _, err := scheduler.Tick(t.Context(), now.Add(5*time.Second)); err != nil {
			t.Fatalf("Tick() error = %v", err)
		}
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls within one slot = %d, want 1", executor.calls)
	}

	// A tick two intervals later is a new slot and must run.
	if _, err := scheduler.Tick(t.Context(), now.Add(2*time.Minute)); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if executor.calls != 2 {
		t.Fatalf("executor calls after a new slot = %d, want 2", executor.calls)
	}
}

// A slot another worker owns is not an error and is never executed twice.
func TestTickLeavesAHeldSlotAlone(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 27, 10, 30, 0, 0, time.UTC)
	schedulable := testSchedulable()
	store := newFakeStore()
	store.skipHeld = true
	slot, err := SlotFor(schedulable.MonitorID, schedulable.Interval, now)
	if err != nil {
		t.Fatalf("SlotFor() error = %v", err)
	}
	store.heldSlots[slot] = true

	executor := &fakeExecutor{}
	scheduler := newTestScheduler(t, &fakeSource{monitors: []Schedulable{schedulable}}, store, executor, now)

	result, err := scheduler.Tick(t.Context(), now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Held != 1 || result.Claimed != 0 {
		t.Fatalf("Tick() = %#v, want the slot reported as held", result)
	}
	if executor.calls != 0 {
		t.Fatalf("executor ran %d times for a held slot, want 0", executor.calls)
	}
}

// The misfire policy of ADR 0021, bounded by ADR 0026: a restart records the missed slots it
// is allowed to and stops at the bound.
func TestTickRecordsMissedSlotsUpToTheBound(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 27, 10, 30, 0, 0, time.UTC)
	schedulable := testSchedulable()
	store := newFakeStore()
	executor := &fakeExecutor{}
	scheduler := newTestScheduler(t, &fakeSource{monitors: []Schedulable{schedulable}}, store, executor, now)

	result, err := scheduler.Tick(t.Context(), now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Skipped != 3 {
		t.Fatalf("Tick() recorded %d skipped Runs, want the configured bound of 3", result.Skipped)
	}

	current, err := SlotFor(schedulable.MonitorID, schedulable.Interval, now)
	if err != nil {
		t.Fatalf("SlotFor() error = %v", err)
	}
	for index, skipped := range store.skipped {
		if skipped.Outcome != OutcomeSkipped {
			t.Fatalf("recorded Run %d outcome = %q, want skipped", index, skipped.Outcome)
		}
		want := current.Add(-time.Duration(index+1) * schedulable.Interval)
		if !skipped.Slot.ScheduledFor.Equal(want) {
			t.Fatalf("skipped slot %d = %v, want %v", index, skipped.Slot.ScheduledFor, want)
		}
		if !skipped.StartedAt.IsZero() || skipped.LeaseHolder != "" {
			t.Fatalf("skipped Run %d carries execution or lease state: %#v", index, skipped)
		}
	}
}

// Walking backwards stops at the first slot that already exists: everything older than a
// recorded slot was recorded too, so continuing would be wasted round trips.
func TestTickStopsBackfillAtTheFirstExistingSlot(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 27, 10, 30, 0, 0, time.UTC)
	store := newFakeStore()
	store.skipHeld = true
	scheduler := newTestScheduler(t, &fakeSource{monitors: []Schedulable{testSchedulable()}}, store, &fakeExecutor{}, now)

	result, err := scheduler.Tick(t.Context(), now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Skipped != 0 || len(store.skipped) != 0 {
		t.Fatalf("Tick() recorded %d skipped Runs, want none behind an existing slot", result.Skipped)
	}
}

// ADR 0021 requires a worker that lost its lease to discard its result. The scheduler must
// treat that as an ordinary outcome rather than an error to retry.
func TestTickDiscardsAResultWhoseLeaseWasLost(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 27, 10, 30, 0, 0, time.UTC)
	store := newFakeStore()
	store.skipHeld = true
	store.loseLease = true
	scheduler := newTestScheduler(t, &fakeSource{monitors: []Schedulable{testSchedulable()}}, store, &fakeExecutor{}, now)

	result, err := scheduler.Tick(t.Context(), now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Claimed != 0 || result.Held != 1 || result.Failed != 0 {
		t.Fatalf("Tick() = %#v, want the lost lease counted as held rather than failed", result)
	}
	if len(store.completed) != 0 {
		t.Fatalf("completed Runs = %d, want the result discarded", len(store.completed))
	}
}

func TestTickAppliesTheOperatorIntervalFloor(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 27, 10, 30, 0, 0, time.UTC)
	schedulable := testSchedulable()
	schedulable.Interval = 30 * time.Second
	store := newFakeStore()
	store.skipHeld = true

	scheduler, err := NewScheduler(SchedulerConfig{
		Source: &fakeSource{monitors: []Schedulable{schedulable}}, Store: store, Executor: &fakeExecutor{},
		Clock: fixedClock{value: now}, UUIDs: &sequenceIDs{},
		ExecutionCeiling: 30 * time.Second,
		MinimumInterval:  5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	if _, err := scheduler.Tick(t.Context(), now); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(store.claimed) != 1 {
		t.Fatalf("claimed Runs = %d, want 1", len(store.claimed))
	}

	want, err := SlotFor(schedulable.MonitorID, 5*time.Minute, now)
	if err != nil {
		t.Fatalf("SlotFor() error = %v", err)
	}
	if !store.claimed[0].Slot.ScheduledFor.Equal(want) {
		t.Fatalf("slot = %v, want the floored-interval slot %v",
			store.claimed[0].Slot.ScheduledFor, want)
	}
}

// A Monitor the source could not describe is an installation problem, not a check result, so
// it is counted as failed and never becomes a Run.
func TestTickRejectsAnUnschedulableMonitorWithoutWritingARun(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 27, 10, 30, 0, 0, time.UTC)
	broken := testSchedulable()
	broken.CheckConfiguration = nil
	store := newFakeStore()
	scheduler := newTestScheduler(t, &fakeSource{monitors: []Schedulable{broken}}, store, &fakeExecutor{}, now)

	result, err := scheduler.Tick(t.Context(), now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Failed != 1 || result.Claimed != 0 {
		t.Fatalf("Tick() = %#v, want one failure and no claim", result)
	}
	if len(store.claimed) != 0 || len(store.skipped) != 0 {
		t.Fatalf("an unschedulable Monitor produced Runs: claimed %d, skipped %d",
			len(store.claimed), len(store.skipped))
	}
}

func TestTickReportsAFailedListing(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 27, 10, 30, 0, 0, time.UTC)
	source := &fakeSource{err: errors.New("database is unreachable")}
	scheduler := newTestScheduler(t, source, newFakeStore(), &fakeExecutor{}, now)

	if _, err := scheduler.Tick(t.Context(), now); err == nil {
		t.Fatalf("Tick() = nil error, want the listing failure reported")
	}
}

// A shutdown mid-execution frees the slot rather than leaving it claimed until the lease
// expires (ADR 0021).
func TestTickReleasesTheSlotWhenTheContextIsCancelled(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 27, 10, 30, 0, 0, time.UTC)
	store := newFakeStore()
	store.skipHeld = true
	scheduler := newTestScheduler(t, &fakeSource{monitors: []Schedulable{testSchedulable()}}, store, &fakeExecutor{}, now)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := scheduler.Tick(ctx, now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(store.released) != 1 {
		t.Fatalf("released slots = %d, want the claimed slot released on shutdown", len(store.released))
	}
	if len(store.completed) != 0 {
		t.Fatalf("completed Runs = %d, want none after a cancelled tick", len(store.completed))
	}
	if result.Claimed != 0 {
		t.Fatalf("Tick() = %#v, want no claim counted after cancellation", result)
	}
}

func TestNewSchedulerRejectsAnIncompleteComposition(t *testing.T) {
	t.Parallel()
	complete := SchedulerConfig{
		Source: &fakeSource{}, Store: newFakeStore(), Executor: &fakeExecutor{},
		Clock: fixedClock{}, UUIDs: &sequenceIDs{}, ExecutionCeiling: time.Second,
	}
	if _, err := NewScheduler(complete); err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	cases := map[string]func(*SchedulerConfig){
		"no source":            func(config *SchedulerConfig) { config.Source = nil },
		"no store":             func(config *SchedulerConfig) { config.Store = nil },
		"no executor":          func(config *SchedulerConfig) { config.Executor = nil },
		"no clock":             func(config *SchedulerConfig) { config.Clock = nil },
		"no identifiers":       func(config *SchedulerConfig) { config.UUIDs = nil },
		"no execution ceiling": func(config *SchedulerConfig) { config.ExecutionCeiling = 0 },
		"negative floor":       func(config *SchedulerConfig) { config.MinimumInterval = -time.Second },
		"oversized location": func(config *SchedulerConfig) {
			config.Location = string(make([]byte, MaxLocationLength+1))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := complete
			mutate(&config)
			if _, err := NewScheduler(config); err == nil {
				t.Fatalf("NewScheduler() = nil error, want a rejection")
			}
		})
	}
}

// Concurrency is bounded so the embedded worker cannot consume the API process it shares
// (ADR 0020).
func TestTickBoundsConcurrentExecutions(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 27, 10, 30, 0, 0, time.UTC)
	monitors := make([]Schedulable, 0, 20)
	for index := range 20 {
		value := testSchedulable()
		value.MonitorID = fmt.Sprintf("00000000-0000-7000-8000-%012d", index)
		monitors = append(monitors, value)
	}
	store := newFakeStore()
	store.skipHeld = true
	executor := &countingExecutor{}

	scheduler, err := NewScheduler(SchedulerConfig{
		Source: &fakeSource{monitors: monitors}, Store: store, Executor: executor,
		Clock: fixedClock{value: now}, UUIDs: &sequenceIDs{},
		ExecutionCeiling: 30 * time.Second, Concurrency: 3,
	})
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	result, err := scheduler.Tick(t.Context(), now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Claimed != len(monitors) {
		t.Fatalf("Tick() claimed %d, want all %d", result.Claimed, len(monitors))
	}
	if peak := executor.peak(); peak > 3 {
		t.Fatalf("peak concurrent executions = %d, want at most 3", peak)
	}
}

type countingExecutor struct {
	mutex   sync.Mutex
	current int
	highest int
}

func (executor *countingExecutor) Execute(context.Context, string, int, json.RawMessage) Execution {
	executor.mutex.Lock()
	executor.current++
	if executor.current > executor.highest {
		executor.highest = executor.current
	}
	executor.mutex.Unlock()

	// Long enough for overlap to be observable if the bound were not enforced, short enough
	// not to slow the suite down.
	time.Sleep(2 * time.Millisecond)

	executor.mutex.Lock()
	executor.current--
	executor.mutex.Unlock()

	instant := time.Date(2026, time.July, 27, 10, 0, 1, 0, time.UTC)
	return Execution{
		Outcome: OutcomePassed, StartedAt: instant, FinishedAt: instant,
		Observation: Observation{Duration: time.Millisecond},
	}
}

func (executor *countingExecutor) peak() int {
	executor.mutex.Lock()
	defer executor.mutex.Unlock()
	return executor.highest
}

// A Monitor that has just been activated looks exactly like one the installation was down
// for: the scheduler has no memory of either. Without the NotBefore floor, activating a
// Monitor immediately wrote a run of skipped Runs for slots it was never eligible for.
func TestTickDoesNotBackfillBeforeAMonitorWasSchedulable(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 27, 10, 30, 0, 0, time.UTC)
	schedulable := testSchedulable()
	// Activated ten seconds ago, so no slot before that was ever missed.
	schedulable.NotBefore = now.Add(-10 * time.Second)

	store := newFakeStore()
	scheduler := newTestScheduler(t, &fakeSource{monitors: []Schedulable{schedulable}}, store, &fakeExecutor{}, now)

	result, err := scheduler.Tick(t.Context(), now)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Skipped != 0 || len(store.skipped) != 0 {
		t.Fatalf("Tick() recorded %d skipped Runs for a just-activated Monitor, want none", result.Skipped)
	}
	if result.Claimed != 1 {
		t.Fatalf("Tick() = %#v, want the current slot still claimed", result)
	}

	// A Monitor that has been active for a long while keeps the misfire recording that
	// ADR 0021 asks for.
	longActive := testSchedulable()
	longActive.MonitorID = "00000000-0000-7000-8000-000000000099"
	longActive.NotBefore = now.Add(-24 * time.Hour)
	store = newFakeStore()
	scheduler = newTestScheduler(t, &fakeSource{monitors: []Schedulable{longActive}}, store, &fakeExecutor{}, now)
	if result, err = scheduler.Tick(t.Context(), now); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if result.Skipped != 3 {
		t.Fatalf("Tick() recorded %d skipped Runs for a long-active Monitor, want the bound of 3", result.Skipped)
	}
}
