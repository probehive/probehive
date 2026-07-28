package run

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type confirmationTestStore struct {
	target        Schedulable
	eligible      bool
	claimErr      error
	existing      Run
	existingFound bool
	loaded        []ConfirmationRequest
	claimed       []Run
	completed     []Run
	entries       [][]OutboxEntry
}

func (store *confirmationTestStore) LoadConfirmationTarget(
	_ context.Context, request ConfirmationRequest,
) (Schedulable, bool, error) {
	store.loaded = append(store.loaded, request)
	return store.target, store.eligible, nil
}
func (store *confirmationTestStore) FindConfirmation(
	context.Context, string, string,
) (Run, bool, error) {
	return store.existing, store.existingFound, nil
}
func (store *confirmationTestStore) ClaimSlot(_ context.Context, value Run, _ time.Time) (Run, error) {
	if store.claimErr != nil {
		return Run{}, store.claimErr
	}
	store.claimed = append(store.claimed, value)
	return value, nil
}
func (*confirmationTestStore) ReleaseSlot(context.Context, Run) error { return nil }
func (store *confirmationTestStore) Complete(
	_ context.Context, value Run, _ string, observation Observation, entries []OutboxEntry,
) error {
	if observation.RunID != value.ID || observation.OrganizationID != value.Slot.OrganizationID ||
		!observation.ScheduledFor.Equal(value.Slot.ScheduledFor) {
		return errors.New("confirmation Observation was not stamped from the Run")
	}
	store.completed = append(store.completed, value)
	store.entries = append(store.entries, entries)
	return nil
}
func (*confirmationTestStore) RecordSkipped(context.Context, Run, []OutboxEntry, time.Time) error {
	return errors.New("confirmation runner must not record a skipped Run")
}

func confirmationRequest(now time.Time) ConfirmationRequest {
	return ConfirmationRequest{
		EventID:        "00000000-0000-7000-8000-000000000010",
		OrganizationID: "00000000-0000-7000-8000-000000000001",
		CandidateID:    "00000000-0000-7000-8000-000000000011",
		MonitorID:      "00000000-0000-7000-8000-000000000002",
		RevisionNumber: 3, Location: "test-location",
		TriggeringRunID:        "00000000-0000-7000-8000-000000000012",
		TriggeringScheduledFor: now.Add(-time.Minute), RequestedFor: now,
		ExpectedEvidence: "failing", PolicyVersion: "phase1.v1",
	}
}

func newConfirmationTestRunner(
	t *testing.T, store ConfirmationStore, executor Executor, now time.Time,
) *ConfirmationRunner {
	t.Helper()
	runner, err := NewConfirmationRunner(ConfirmationConfig{
		Store: store, Executor: executor, Clock: fixedClock{value: now},
		UUIDs: &sequenceIDs{}, ExecutionCeiling: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func TestConfirmationRunnerRecordsExplicitCauseAndResultEvent(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	request := confirmationRequest(now)
	target := testSchedulable()
	store := &confirmationTestStore{target: target, eligible: true}
	executor := &fakeExecutor{outcome: OutcomeErrored}
	runner := newConfirmationTestRunner(t, store, executor, now)
	if err := runner.Execute(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if len(store.loaded) != 1 || len(store.claimed) != 1 || len(store.completed) != 1 || executor.calls != 1 {
		t.Fatalf("loaded %d, claimed %d, completed %d, executed %d", len(store.loaded), len(store.claimed), len(store.completed), executor.calls)
	}
	completed := store.completed[0]
	if completed.Kind != KindConfirmation || completed.Confirmation == nil ||
		completed.Confirmation.CandidateID != request.CandidateID ||
		completed.Confirmation.TriggeringRunID != ID(request.TriggeringRunID) ||
		!completed.Confirmation.TriggeringScheduledFor.Equal(request.TriggeringScheduledFor) ||
		completed.Confirmation.CausationEventID != request.EventID ||
		completed.Confirmation.PolicyVersion != request.PolicyVersion {
		t.Fatalf("confirmation cause = %#v", completed.Confirmation)
	}
	if len(store.entries) != 1 || len(store.entries[0]) != 1 {
		t.Fatalf("outbox entries = %#v", store.entries)
	}
	entry := store.entries[0][0]
	var event RunRecordedV1
	if err := json.Unmarshal(entry.Payload, &event); err != nil {
		t.Fatal(err)
	}
	if entry.Topic != TopicRunRecordedV1 || event.EventID != string(entry.ID) ||
		event.RunID != string(completed.ID) || event.Kind != string(KindConfirmation) ||
		event.OrganizationID != request.OrganizationID {
		t.Fatalf("confirmation result event = %#v, entry %#v", event, entry)
	}
}

func TestConfirmationRunnerTreatsCompletedCandidateAsIdempotent(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	request := confirmationRequest(now)
	existing, err := ClaimConfirmation(
		"00000000-0000-7000-8000-000000000020",
		Slot{OrganizationID: request.OrganizationID, MonitorID: request.MonitorID,
			RevisionNumber: request.RevisionNumber, Location: request.Location, ScheduledFor: request.RequestedFor},
		ConfirmationCause{CandidateID: request.CandidateID, TriggeringRunID: ID(request.TriggeringRunID),
			TriggeringScheduledFor: request.TriggeringScheduledFor, CausationEventID: request.EventID,
			PolicyVersion: request.PolicyVersion},
		"holder", now.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := existing.Complete(OutcomePassed, now, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	store := &confirmationTestStore{
		target: testSchedulable(), eligible: true, claimErr: ErrSlotHeld,
		existing: existing, existingFound: true,
	}
	executor := &fakeExecutor{}
	runner := newConfirmationTestRunner(t, store, executor, now)
	if err := runner.Execute(t.Context(), request); err != nil {
		t.Fatalf("idempotent Execute() error = %v", err)
	}
	if executor.calls != 0 || len(store.completed) != 0 {
		t.Fatalf("idempotent execution ran %d times and completed %d Runs", executor.calls, len(store.completed))
	}

	inFlight := existing
	inFlight.Outcome, inFlight.StartedAt, inFlight.FinishedAt = "", time.Time{}, time.Time{}
	inFlight.LeaseHolder, inFlight.LeaseExpiresAt = "holder", now.Add(time.Minute)
	store.existing = inFlight
	if err := runner.Execute(t.Context(), request); !errors.Is(err, ErrConfirmationHeld) {
		t.Fatalf("held in-flight confirmation error = %v", err)
	}
}
