package health

import (
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, kind, outcome, code string
		want                      Evidence
	}{
		{"passed", "scheduled", "passed", "", EvidencePassing},
		{"unexpected status", "scheduled", "failed", "probe.http.status.unexpected", EvidenceFailing},
		{"timeout", "scheduled", "timedout", "probe.execution.timeout", EvidenceFailing},
		{"redirect exhaustion", "scheduled", "errored", "probe.http.redirect.tooMany", EvidenceFailing},
		{"invalid certificate", "scheduled", "errored", "probe.tls.certificateInvalid", EvidenceFailing},
		{"transport", "scheduled", "errored", "probe.transport.failed", EvidenceFailing},
		{"resolution", "scheduled", "errored", "outbound.resolution.failed", EvidenceFailing},
		{"empty resolution", "scheduled", "errored", "outbound.resolution.empty", EvidenceFailing},
		{"connect", "scheduled", "errored", "outbound.connect.failed", EvidenceFailing},
		{"address mismatch", "scheduled", "errored", "outbound.address.mismatch", EvidenceLocationFault},
		{"policy denial", "scheduled", "errored", "outbound.address.denied", EvidenceIndeterminate},
		{"unknown code", "scheduled", "errored", "future.failure", EvidenceIndeterminate},
		{"invalid combination", "scheduled", "failed", "outbound.connect.failed", EvidenceIndeterminate},
		{"cancelled", "scheduled", "cancelled", "", EvidenceIndeterminate},
		{"skipped", "scheduled", "skipped", "", EvidenceIndeterminate},
		{"outcome null", "scheduled", "", "", EvidenceIndeterminate},
		{"manual pass", "manual", "passed", "", EvidenceIndeterminate},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := Classify(testCase.kind, testCase.outcome, testCase.code); got != testCase.want {
				t.Fatalf("Classify(%q, %q, %q) = %q, want %q",
					testCase.kind, testCase.outcome, testCase.code, got, testCase.want)
			}
		})
	}
}

func TestFailureAndRecoveryRequireMatchingConfirmations(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 7, 28, 1, 0, 0, 0, time.UTC)
	current := initialSnapshot(t, base)

	failure := evaluate(t, current, scheduledInput(
		"event-fail", "run-fail", 1, base.Add(time.Minute), base.Add(time.Minute+time.Second),
		"failed", "probe.http.status.unexpected", "candidate-failure"))
	if failure.Snapshot.State != StateDegraded || failure.Snapshot.StableState != StateUnknown ||
		failure.CandidateCreated == nil || failure.CandidateCreated.Direction != DirectionFailure {
		t.Fatalf("failure candidate = %#v", failure)
	}

	down := evaluate(t, failure.Snapshot, confirmationInput(
		"event-confirm-fail", "run-confirm-fail", 1, base.Add(2*time.Minute),
		base.Add(2*time.Minute+time.Second), "failed", "probe.http.status.unexpected",
		failure.CandidateCreated.ID))
	if down.Snapshot.State != StateDown || down.Snapshot.StableState != StateDown ||
		down.CandidateCompleted == nil || down.CandidateCompleted.State != CandidateConfirmed {
		t.Fatalf("confirmed failure = %#v", down)
	}

	recovery := evaluate(t, down.Snapshot, scheduledInput(
		"event-pass", "run-pass", 1, base.Add(3*time.Minute), base.Add(3*time.Minute+time.Second),
		"passed", "", "candidate-recovery"))
	if recovery.Snapshot.State != StateDegraded || recovery.Snapshot.StableState != StateDown ||
		recovery.CandidateCreated == nil || recovery.CandidateCreated.Direction != DirectionRecovery {
		t.Fatalf("recovery candidate = %#v", recovery)
	}

	healthy := evaluate(t, recovery.Snapshot, confirmationInput(
		"event-confirm-pass", "run-confirm-pass", 1, base.Add(4*time.Minute),
		base.Add(4*time.Minute+time.Second), "passed", "", recovery.CandidateCreated.ID))
	if healthy.Snapshot.State != StateHealthy || healthy.Snapshot.StableState != StateHealthy ||
		healthy.CandidateCompleted == nil || healthy.CandidateCompleted.State != CandidateConfirmed {
		t.Fatalf("confirmed recovery = %#v", healthy)
	}
	if healthy.Snapshot.Version != 4 {
		t.Fatalf("transition version = %d, want 4", healthy.Snapshot.Version)
	}
}

func TestContradictionReturnsToPriorStableState(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 7, 28, 2, 0, 0, 0, time.UTC)
	current := initialSnapshot(t, base)
	healthy := evaluate(t, current, scheduledInput(
		"event-pass", "run-pass", 1, base.Add(time.Minute), base.Add(time.Minute+time.Second),
		"passed", "", "unused"))
	failure := evaluate(t, healthy.Snapshot, scheduledInput(
		"event-fail", "run-fail", 1, base.Add(2*time.Minute), base.Add(2*time.Minute+time.Second),
		"failed", "probe.http.status.unexpected", "candidate"))
	contradiction := evaluate(t, failure.Snapshot, confirmationInput(
		"event-confirm", "run-confirm", 1, base.Add(3*time.Minute),
		base.Add(3*time.Minute+time.Second), "passed", "", "candidate"))
	if contradiction.Snapshot.State != StateHealthy ||
		contradiction.CandidateCompleted == nil ||
		contradiction.CandidateCompleted.State != CandidateContradicted {
		t.Fatalf("contradiction = %#v", contradiction)
	}
}

func TestIndeterminateAndSupersededConfirmationsDoNotRewriteHealth(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 7, 28, 3, 0, 0, 0, time.UTC)
	failure := evaluate(t, initialSnapshot(t, base), scheduledInput(
		"event-fail", "run-fail", 1, base.Add(time.Minute), base.Add(time.Minute+time.Second),
		"failed", "probe.http.status.unexpected", "candidate"))

	indeterminate := evaluate(t, failure.Snapshot, confirmationInput(
		"event-confirm", "run-confirm", 1, base.Add(2*time.Minute),
		base.Add(2*time.Minute+time.Second), "errored", "outbound.address.denied", "candidate"))
	if indeterminate.Snapshot.State != StateDegraded || indeterminate.Snapshot.Candidate == nil ||
		indeterminate.Snapshot.Candidate.ID != "candidate" {
		t.Fatalf("indeterminate confirmation = %#v", indeterminate)
	}

	newer := evaluate(t, indeterminate.Snapshot, scheduledInput(
		"event-new", "run-new", 1, base.Add(3*time.Minute), base.Add(3*time.Minute+time.Second),
		"passed", "", "unused"))
	before := newer.Snapshot
	ignored := evaluate(t, before, confirmationInput(
		"event-late-confirm", "run-late-confirm", 1, base.Add(4*time.Minute),
		base.Add(4*time.Minute+time.Second), "failed", "probe.http.status.unexpected", "candidate"))
	if !ignored.IgnoredConfirmation || ignored.Snapshot.State != before.State ||
		ignored.Snapshot.LastRunID != before.LastRunID ||
		!ignored.Snapshot.LastDeterminateFinishedAt.Equal(before.LastDeterminateFinishedAt) {
		t.Fatalf("superseded confirmation = %#v, before %#v", ignored, before)
	}
}

func TestLateSupersededAndManualRunsDoNotRewriteCurrentEvidence(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 7, 28, 4, 0, 0, 0, time.UTC)
	healthy := evaluate(t, initialSnapshot(t, base), scheduledInput(
		"event-pass", "run-pass", 2, base.Add(2*time.Minute), base.Add(2*time.Minute+time.Second),
		"passed", "", "unused"))

	lateInput := scheduledInput(
		"event-late", "run-late", 2, base.Add(time.Minute), base.Add(3*time.Minute),
		"failed", "probe.http.status.unexpected", "candidate")
	late := evaluate(t, healthy.Snapshot, lateInput)
	if !late.Late || late.Snapshot.State != StateHealthy || late.Snapshot.LastRunID != "run-pass" {
		t.Fatalf("late evidence = %#v", late)
	}

	supersededInput := scheduledInput(
		"event-old-revision", "run-old-revision", 1, base.Add(3*time.Minute), base.Add(3*time.Minute+time.Second),
		"failed", "probe.http.status.unexpected", "candidate-old")
	supersededInput.LatestRevisionNumber = 2
	superseded := evaluate(t, healthy.Snapshot, supersededInput)
	if !superseded.SupersededRevision || superseded.Snapshot.LastRunID != "run-pass" {
		t.Fatalf("superseded revision = %#v", superseded)
	}

	manualInput := scheduledInput(
		"event-manual", "run-manual", 2, base.Add(4*time.Minute), base.Add(4*time.Minute+time.Second),
		"passed", "", "unused")
	manualInput.Kind = "manual"
	manual := evaluate(t, healthy.Snapshot, manualInput)
	if manual.Evidence != EvidenceIndeterminate || manual.Snapshot.LastRunID != "run-pass" ||
		manual.Snapshot.State != StateHealthy {
		t.Fatalf("manual evidence = %#v", manual)
	}
}

func TestStaleness(t *testing.T) {
	t.Parallel()
	if got, err := StaleAfter(time.Minute, 10*time.Minute); err != nil || got != 20*time.Minute {
		t.Fatalf("execution-bound StaleAfter = %v, %v", got, err)
	}
	if got, err := StaleAfter(10*time.Minute, time.Minute); err != nil || got != 30*time.Minute {
		t.Fatalf("interval-bound StaleAfter = %v, %v", got, err)
	}
	if _, err := StaleAfter(0, time.Minute); err == nil {
		t.Fatal("StaleAfter accepted a zero interval")
	}

	base := time.Date(2026, 7, 28, 5, 0, 0, 0, time.UTC)
	failure := evaluate(t, initialSnapshot(t, base), scheduledInput(
		"event-fail", "run-fail", 1, base.Add(time.Minute), base.Add(time.Minute+time.Second),
		"failed", "probe.http.status.unexpected", "candidate"))
	stale, err := MarkStale(failure.Snapshot, base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if stale.Snapshot.State != StateUnknown || stale.Snapshot.Candidate != nil ||
		stale.CandidateCompleted == nil || stale.CandidateCompleted.State != CandidateStale ||
		stale.Snapshot.Counts.Missing != 1 {
		t.Fatalf("stale decision = %#v", stale)
	}
}

func initialSnapshot(t *testing.T, now time.Time) Snapshot {
	t.Helper()
	value, err := Initial("organization", "project", "monitor", now)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func evaluate(t *testing.T, current Snapshot, input Input) Decision {
	t.Helper()
	decision, err := Evaluate(current, input)
	if err != nil {
		t.Fatal(err)
	}
	return decision
}

func scheduledInput(
	eventID, runID string,
	revision int,
	scheduledFor, finishedAt time.Time,
	outcome, failureCode, candidateID string,
) Input {
	return Input{
		EventID: eventID, RunID: runID, Kind: "scheduled", Outcome: outcome,
		FailureCode: failureCode, RevisionNumber: revision, LatestRevisionNumber: revision,
		ScheduledFor: scheduledFor, FinishedAt: finishedAt, Now: finishedAt,
		NewCandidateID: candidateID,
	}
}

func confirmationInput(
	eventID, runID string,
	revision int,
	scheduledFor, finishedAt time.Time,
	outcome, failureCode, candidateID string,
) Input {
	value := scheduledInput(
		eventID, runID, revision, scheduledFor, finishedAt, outcome, failureCode, "")
	value.Kind = "confirmation"
	value.CandidateID = candidateID
	return value
}
