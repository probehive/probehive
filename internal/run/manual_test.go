package run

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type manualTestStore struct {
	target        Schedulable
	targetFound   bool
	monitorExists bool
	claimed       []Run
	completed     []Run
	released      []Run
	observation   Observation
	entries       []OutboxEntry
	completeErr   error
}

func (store *manualTestStore) LoadManualTarget(
	context.Context, Scope,
) (Schedulable, bool, error) {
	return store.target, store.targetFound, nil
}

func (store *manualTestStore) MonitorExists(context.Context, Scope) (bool, error) {
	return store.monitorExists, nil
}

func (store *manualTestStore) ClaimSlot(_ context.Context, value Run, _ time.Time) (Run, error) {
	store.claimed = append(store.claimed, value)
	return value, nil
}

func (store *manualTestStore) ReleaseSlot(_ context.Context, value Run) error {
	store.released = append(store.released, value)
	return nil
}

func (store *manualTestStore) Complete(
	_ context.Context,
	value Run,
	holder string,
	observation Observation,
	entries []OutboxEntry,
) error {
	if store.completeErr != nil {
		return store.completeErr
	}
	if holder == "" {
		return errors.New("manual completion requires the claiming holder")
	}
	store.completed = append(store.completed, value)
	store.observation = observation
	store.entries = append(store.entries, entries...)
	return nil
}

func (*manualTestStore) RecordSkipped(context.Context, Run, []OutboxEntry, time.Time) error {
	return errors.New("a manual runner must not record skipped Runs")
}

func newManualTestRunner(
	t *testing.T,
	store *manualTestStore,
	executor Executor,
	now time.Time,
	slots *ExecutionSlots,
) *ManualRunner {
	t.Helper()
	runner, err := NewManualRunner(ManualConfig{
		Store: store, Executor: executor, Clock: fixedClock{value: now}, UUIDs: &sequenceIDs{},
		ExecutionSlots: slots, Location: "manual-location", ExecutionCeiling: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewManualRunner() error = %v", err)
	}
	return runner
}

func manualTestScope() Scope {
	return Scope{
		OrganizationID: "00000000-0000-7000-8000-000000000001",
		ProjectID:      "00000000-0000-7000-8000-000000000003",
		MonitorID:      "00000000-0000-7000-8000-000000000002",
	}
}

func TestManualRunnerExecutesEveryRequestAndRecordsTheResultEvent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
	store := &manualTestStore{
		target: testSchedulable(), targetFound: true, monitorExists: true,
	}
	runner := newManualTestRunner(t, store, &fakeExecutor{}, now, nil)

	first, err := runner.Trigger(t.Context(), manualTestScope())
	if err != nil {
		t.Fatalf("first Trigger() error = %v", err)
	}
	second, err := runner.Trigger(t.Context(), manualTestScope())
	if err != nil {
		t.Fatalf("second Trigger() error = %v", err)
	}

	if first.ID == second.ID || len(store.claimed) != 2 || len(store.completed) != 2 {
		t.Fatalf("manual requests produced ids %q/%q, %d claims, and %d completions",
			first.ID, second.ID, len(store.claimed), len(store.completed))
	}
	if first.Kind != KindManual || first.Outcome != OutcomePassed ||
		first.Slot.RevisionNumber != store.target.RevisionNumber ||
		first.Slot.Location != "manual-location" || !first.Slot.ScheduledFor.Equal(now) {
		t.Fatalf("manual Run = %#v", first)
	}
	if store.observation.RunID != second.ID ||
		store.observation.OrganizationID != second.Slot.OrganizationID ||
		!store.observation.ScheduledFor.Equal(second.Slot.ScheduledFor) {
		t.Fatalf("stored Observation identity = %#v", store.observation)
	}
	if len(store.entries) != 2 {
		t.Fatalf("outbox entries = %d, want one per manual Run", len(store.entries))
	}
	var event RunRecordedV1
	if err := json.Unmarshal(store.entries[1].Payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.RunID != string(second.ID) || event.Kind != string(KindManual) ||
		event.Outcome != string(OutcomePassed) {
		t.Fatalf("manual Run event = %#v", event)
	}
	if len(store.released) != 0 {
		t.Fatalf("successful manual Runs released %d claims", len(store.released))
	}
}

func TestManualRunnerDistinguishesMissingAndUnconfiguredTargets(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
	for _, testCase := range []struct {
		name          string
		monitorExists bool
		want          error
	}{
		{name: "missing", want: ErrManualTargetNotFound},
		{name: "unconfigured", monitorExists: true, want: ErrManualTargetUnconfigured},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			store := &manualTestStore{monitorExists: testCase.monitorExists}
			runner := newManualTestRunner(t, store, &fakeExecutor{}, now, nil)
			if _, err := runner.Trigger(t.Context(), manualTestScope()); !errors.Is(err, testCase.want) {
				t.Fatalf("Trigger() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestManualRunnerRejectsExhaustedCapacityWithoutQueueing(t *testing.T) {
	t.Parallel()
	slots, err := NewExecutionSlots(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := slots.Acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer slots.Release()

	store := &manualTestStore{
		target: testSchedulable(), targetFound: true, monitorExists: true,
	}
	runner := newManualTestRunner(t, store, &fakeExecutor{}, time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC), slots)
	if _, err := runner.Trigger(t.Context(), manualTestScope()); !errors.Is(err, ErrManualCapacity) {
		t.Fatalf("Trigger() error = %v, want ErrManualCapacity", err)
	}
	if len(store.claimed) != 0 {
		t.Fatalf("capacity rejection claimed %d Runs", len(store.claimed))
	}
}

func TestManualRunnerReleasesAClaimWhenPersistenceFails(t *testing.T) {
	t.Parallel()
	store := &manualTestStore{
		target: testSchedulable(), targetFound: true, monitorExists: true,
		completeErr: errors.New("database unavailable"),
	}
	runner := newManualTestRunner(
		t, store, &fakeExecutor{}, time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC), nil)
	if _, err := runner.Trigger(t.Context(), manualTestScope()); err == nil {
		t.Fatal("Trigger() error = nil, want persistence failure")
	}
	if len(store.released) != 1 || len(store.completed) != 0 {
		t.Fatalf("failed manual Run released %d and completed %d claims",
			len(store.released), len(store.completed))
	}
}

func TestExecutionSlotsCancelOnlyWhileWaiting(t *testing.T) {
	t.Parallel()
	slots, err := NewExecutionSlots(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := slots.Acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := slots.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire() error = %v, want context cancellation", err)
	}
	slots.Release()

	if err := slots.Acquire(ctx); err != nil {
		t.Fatalf("immediately available Acquire() error = %v", err)
	}
	slots.Release()
}
