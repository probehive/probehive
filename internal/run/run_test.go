package run

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func testSlot() Slot {
	return Slot{
		OrganizationID: "org",
		MonitorID:      "monitor",
		RevisionNumber: 3,
		Location:       "managed-eu-west",
		ScheduledFor:   time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC),
	}
}

func TestSlotValidateRejectsIncompleteIdentity(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*Slot){
		"no Organization":     func(slot *Slot) { slot.OrganizationID = "" },
		"no Monitor":          func(slot *Slot) { slot.MonitorID = "" },
		"revision below one":  func(slot *Slot) { slot.RevisionNumber = 0 },
		"no location":         func(slot *Slot) { slot.Location = "" },
		"oversized location":  func(slot *Slot) { slot.Location = strings.Repeat("a", MaxLocationLength+1) },
		"no scheduled slot":   func(slot *Slot) { slot.ScheduledFor = time.Time{} },
		"non-UTC scheduled":   func(slot *Slot) { slot.ScheduledFor = slot.ScheduledFor.In(time.FixedZone("CST", 8*3600)) },
		"unset zero instants": func(slot *Slot) { *slot = Slot{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			slot := testSlot()
			mutate(&slot)
			if err := slot.Validate(); err == nil {
				t.Fatalf("Slot.Validate() = nil, want an error")
			}
		})
	}

	if err := testSlot().Validate(); err != nil {
		t.Fatalf("Slot.Validate() error = %v, want nil", err)
	}
}

func TestClaimProducesAnInFlightRun(t *testing.T) {
	t.Parallel()
	expiry := time.Date(2026, time.July, 27, 10, 1, 0, 0, time.UTC)
	value, err := Claim("run-1", testSlot(), KindScheduled, "worker-a", expiry)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if !value.InFlight() {
		t.Fatalf("Claim().InFlight() = false, want true")
	}
	if value.Outcome != "" {
		t.Fatalf("Claim().Outcome = %q, want empty", value.Outcome)
	}
	if !value.StartedAt.IsZero() || !value.FinishedAt.IsZero() {
		t.Fatalf("Claim() recorded execution instants %v/%v, want both zero", value.StartedAt, value.FinishedAt)
	}
}

func TestClaimRejectsUnusableArguments(t *testing.T) {
	t.Parallel()
	expiry := time.Date(2026, time.July, 27, 10, 1, 0, 0, time.UTC)
	cases := map[string]struct {
		id     ID
		kind   Kind
		holder string
		expiry time.Time
	}{
		"no identifier":     {"", KindScheduled, "worker-a", expiry},
		"unknown kind":      {"run-1", Kind("periodic"), "worker-a", expiry},
		"no holder":         {"run-1", KindScheduled, "", expiry},
		"oversized holder":  {"run-1", KindScheduled, strings.Repeat("w", MaxLeaseHolderLength+1), expiry},
		"no lease expiry":   {"run-1", KindScheduled, "worker-a", time.Time{}},
		"non-UTC lease end": {"run-1", KindScheduled, "worker-a", expiry.In(time.FixedZone("CST", 8*3600))},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Claim(testCase.id, testSlot(), testCase.kind, testCase.holder, testCase.expiry); err == nil {
				t.Fatalf("Claim() = nil error, want a rejection")
			}
		})
	}
}

func TestCompleteReleasesTheLeaseAndRecordsInstants(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 27, 10, 0, 1, 0, time.UTC)
	finish := start.Add(400 * time.Millisecond)
	value, err := Claim("run-1", testSlot(), KindScheduled, "worker-a", start.Add(time.Minute))
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if err := value.Complete(OutcomePassed, start, finish); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if value.InFlight() {
		t.Fatalf("Complete() left the Run in flight")
	}
	if value.LeaseHolder != "" || !value.LeaseExpiresAt.IsZero() {
		t.Fatalf("Complete() kept lease %q/%v, want it released", value.LeaseHolder, value.LeaseExpiresAt)
	}
	if !value.StartedAt.Equal(start) || !value.FinishedAt.Equal(finish) {
		t.Fatalf("Complete() instants = %v/%v, want %v/%v", value.StartedAt, value.FinishedAt, start, finish)
	}

	if err := value.Complete(OutcomeFailed, start, finish); err == nil {
		t.Fatalf("second Complete() = nil error, want a rejection")
	}
}

// A claimed Run executed, whatever it found. Skipped is reserved for the scheduler,
// so allowing it here would let an execution disguise itself as a slot that never ran.
func TestCompleteRejectsSkipped(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 27, 10, 0, 1, 0, time.UTC)
	value, err := Claim("run-1", testSlot(), KindScheduled, "worker-a", start.Add(time.Minute))
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if err := value.Complete(OutcomeSkipped, start, start); err == nil {
		t.Fatalf("Complete(OutcomeSkipped) = nil error, want a rejection")
	}
}

func TestCompleteRejectsFinishingBeforeStarting(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.July, 27, 10, 0, 1, 0, time.UTC)
	value, err := Claim("run-1", testSlot(), KindScheduled, "worker-a", start.Add(time.Minute))
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if err := value.Complete(OutcomePassed, start, start.Add(-time.Second)); err == nil {
		t.Fatalf("Complete() = nil error, want a rejection")
	}
}

func TestSkipProducesAFinishedRunWithNoExecution(t *testing.T) {
	t.Parallel()
	value, err := Skip("run-1", testSlot(), KindScheduled)
	if err != nil {
		t.Fatalf("Skip() error = %v", err)
	}
	if value.InFlight() {
		t.Fatalf("Skip().InFlight() = true, want false")
	}
	if value.Outcome != OutcomeSkipped {
		t.Fatalf("Skip().Outcome = %q, want %q", value.Outcome, OutcomeSkipped)
	}
	if !value.StartedAt.IsZero() || !value.FinishedAt.IsZero() || value.LeaseHolder != "" {
		t.Fatalf("Skip() produced execution or lease state, want none")
	}
}

func TestRenewLeaseOnlyExtends(t *testing.T) {
	t.Parallel()
	expiry := time.Date(2026, time.July, 27, 10, 1, 0, 0, time.UTC)
	value, err := Claim("run-1", testSlot(), KindScheduled, "worker-a", expiry)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if err := value.RenewLease(expiry.Add(-time.Second)); err == nil {
		t.Fatalf("RenewLease() backwards = nil error, want a rejection")
	}
	if err := value.RenewLease(expiry.Add(time.Minute)); err != nil {
		t.Fatalf("RenewLease() error = %v", err)
	}
	if !value.LeaseExpiresAt.Equal(expiry.Add(time.Minute)) {
		t.Fatalf("RenewLease() expiry = %v, want %v", value.LeaseExpiresAt, expiry.Add(time.Minute))
	}

	finished := value
	if err := finished.Complete(OutcomePassed, expiry, expiry); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err := finished.RenewLease(expiry.Add(time.Hour)); err == nil {
		t.Fatalf("RenewLease() after Complete() = nil error, want a rejection")
	}
}

// Restore is the boundary a hand-edited row has to cross, so it rejects every combination
// the schema check constraints forbid rather than trusting the database to be intact.
func TestRestoreRejectsStatesTheSchemaForbids(t *testing.T) {
	t.Parallel()
	instant := time.Date(2026, time.July, 27, 10, 0, 1, 0, time.UTC)
	cases := map[string]struct {
		outcome     Outcome
		startedAt   time.Time
		finishedAt  time.Time
		leaseHolder string
		leaseEnd    time.Time
	}{
		"no outcome and no lease":     {"", time.Time{}, time.Time{}, "", time.Time{}},
		"in flight with instants":     {"", instant, instant, "worker-a", instant},
		"in flight without holder":    {"", time.Time{}, time.Time{}, "", instant},
		"finished but still leased":   {OutcomePassed, instant, instant, "worker-a", instant},
		"skipped with instants":       {OutcomeSkipped, instant, instant, "", time.Time{}},
		"executed without instants":   {OutcomePassed, time.Time{}, time.Time{}, "", time.Time{}},
		"finished before it started":  {OutcomePassed, instant, instant.Add(-time.Second), "", time.Time{}},
		"unknown outcome":             {Outcome("degraded"), instant, instant, "", time.Time{}},
		"non-UTC execution instants":  {OutcomePassed, instant.In(time.FixedZone("CST", 8*3600)), instant, "", time.Time{}},
		"lease expiry without holder": {"", time.Time{}, time.Time{}, "worker-a", time.Time{}},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := Restore("run-1", testSlot(), KindScheduled, testCase.outcome,
				testCase.startedAt, testCase.finishedAt, testCase.leaseHolder, testCase.leaseEnd)
			if err == nil {
				t.Fatalf("Restore() = nil error, want a rejection")
			}
		})
	}
}

func TestRestoreAcceptsEachRepresentableState(t *testing.T) {
	t.Parallel()
	instant := time.Date(2026, time.July, 27, 10, 0, 1, 0, time.UTC)
	cases := map[string]struct {
		outcome     Outcome
		startedAt   time.Time
		finishedAt  time.Time
		leaseHolder string
		leaseEnd    time.Time
		wantFlight  bool
	}{
		"in flight": {"", time.Time{}, time.Time{}, "worker-a", instant, true},
		"finished":  {OutcomeFailed, instant, instant.Add(time.Second), "", time.Time{}, false},
		"skipped":   {OutcomeSkipped, time.Time{}, time.Time{}, "", time.Time{}, false},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value, err := Restore("run-1", testSlot(), KindScheduled, testCase.outcome,
				testCase.startedAt, testCase.finishedAt, testCase.leaseHolder, testCase.leaseEnd)
			if err != nil {
				t.Fatalf("Restore() error = %v", err)
			}
			if value.InFlight() != testCase.wantFlight {
				t.Fatalf("Restore().InFlight() = %v, want %v", value.InFlight(), testCase.wantFlight)
			}
		})
	}
}

func TestMisfiredBoundary(t *testing.T) {
	t.Parallel()
	due := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	interval := time.Minute
	cases := map[string]struct {
		now  time.Time
		want bool
	}{
		"on time":              {due, false},
		"within the interval":  {due.Add(59 * time.Second), false},
		"exactly one interval": {due.Add(interval), false},
		"past one interval":    {due.Add(interval + time.Nanosecond), true},
		"long after downtime":  {due.Add(2 * time.Hour), true},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := Misfired(due, testCase.now, interval); got != testCase.want {
				t.Fatalf("Misfired() = %v, want %v", got, testCase.want)
			}
		})
	}

	if Misfired(due, due.Add(time.Hour), 0) {
		t.Fatalf("Misfired() with no interval = true, want false")
	}
}

func TestLeaseDurationAddsMarginAndStaysBounded(t *testing.T) {
	t.Parallel()
	if got := LeaseDuration(30 * time.Second); got != 30*time.Second+LeaseMargin {
		t.Fatalf("LeaseDuration(30s) = %v, want %v", got, 30*time.Second+LeaseMargin)
	}
	if got := LeaseDuration(0); got != LeaseMargin {
		t.Fatalf("LeaseDuration(0) = %v, want %v", got, LeaseMargin)
	}
	if got := LeaseDuration(-time.Hour); got != LeaseMargin {
		t.Fatalf("LeaseDuration(negative) = %v, want %v", got, LeaseMargin)
	}
	if got := LeaseDuration(24 * time.Hour); got != MaxLeaseDuration {
		t.Fatalf("LeaseDuration(24h) = %v, want the %v cap", got, MaxLeaseDuration)
	}
}

func TestSentinelErrorsAreDistinguishable(t *testing.T) {
	t.Parallel()
	if errors.Is(ErrSlotHeld, ErrLeaseLost) {
		t.Fatalf("ErrSlotHeld and ErrLeaseLost must stay distinguishable")
	}
}
