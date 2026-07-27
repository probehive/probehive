// Package run owns Run identity, slot identity, lease state, outcomes, and the retention
// arithmetic that decides which stored month is still worth keeping (ADR 0021, ADR 0025).
//
// A Run is one Monitor Revision executed once from one Probe Location. This package holds
// what a Run is and what transitions it allows; where it is stored is internal/postgres's
// concern, and what it measured is produced by internal/probe.
//
// It is a feature package and stays standard-library-only, so it restates the shape of an
// Observation rather than importing internal/probe. The mapping between the two belongs to
// the composition that owns both (ADR 0025).
package run

import (
	"errors"
	"fmt"
	"time"
)

// ID identifies a Run. It is a UUIDv7 so that identifiers order by creation, but identity
// for scheduling purposes is the Slot below, not this value (ADR 0021).
type ID string

// Kind separates the three reasons a Run exists. ADR 0021 requires a confirmation Run to be
// identifiable as one rather than inferred from timing.
type Kind string

const (
	// KindScheduled is an ordinary execution of a due slot.
	KindScheduled Kind = "scheduled"
	// KindConfirmation is a re-execution taken to confirm a suspected state change.
	KindConfirmation Kind = "confirmation"
	// KindManual is an execution a person asked for. It is exempt from slot uniqueness,
	// because asking twice is a request rather than a duplicate.
	KindManual Kind = "manual"
)

// Outcome is what one Run amounted to. The set matches probe.Outcome plus OutcomeSkipped,
// which only the scheduler produces.
type Outcome string

const (
	// OutcomePassed means the target answered and every assertion accepted the answer.
	OutcomePassed Outcome = "passed"
	// OutcomeFailed means the target answered and an assertion rejected the answer.
	OutcomeFailed Outcome = "failed"
	// OutcomeErrored means execution itself failed.
	OutcomeErrored Outcome = "errored"
	// OutcomeTimedOut means the effective execution deadline expired.
	OutcomeTimedOut Outcome = "timedout"
	// OutcomeCancelled means the caller's context was cancelled, which is what a graceful
	// shutdown or a lost lease looks like from the executor.
	OutcomeCancelled Outcome = "cancelled"
	// OutcomeSkipped means the scheduler deliberately did not execute this slot. It is the
	// only outcome with no execution behind it, so it has no start, finish, or Observation.
	OutcomeSkipped Outcome = "skipped"
)

// Sentinel errors a caller distinguishes rather than reports.
var (
	// ErrSlotHeld means another worker holds an unexpired lease on the slot, or the slot
	// already finished. Claiming again is not an error; it is a slot that is not available.
	ErrSlotHeld = errors.New("the Run slot is held by another lease")
	// ErrLeaseLost means the recorder no longer holds the lease it is recording against.
	// ADR 0021 requires the result to be discarded rather than written.
	ErrLeaseLost = errors.New("the Run lease was lost before the result was recorded")
)

// Slot is the idempotent identity of a scheduled Run (ADR 0021): one Monitor Revision, one
// Probe Location, one due instant. Two Runs with the same Slot are the same execution, which
// the storage layer makes unrepresentable rather than merely unlikely.
type Slot struct {
	// OrganizationID carries tenant identity explicitly, as every tenant-scoped record must
	// under ADR 0009.
	OrganizationID string
	// MonitorID is the long-lived Monitor identity.
	MonitorID string
	// RevisionNumber is the exact immutable revision that was executed, not the latest one.
	RevisionNumber int
	// Location names the Probe Location that executed the Run. Its registry is a later
	// decision, so this is a bounded identifier rather than a reference (ADR 0025).
	Location string
	// ScheduledFor is the instant the slot was due, in UTC. For a manual Run it is the
	// instant the Run was requested. It is the partition key (ADR 0025).
	ScheduledFor time.Time
}

// Validate reports whether a Slot is complete and well formed.
func (slot Slot) Validate() error {
	if slot.OrganizationID == "" || slot.MonitorID == "" {
		return errors.New("a Run slot requires Organization and Monitor identity")
	}
	if slot.RevisionNumber < 1 {
		return errors.New("revision numbers start at 1")
	}
	if slot.Location == "" || len(slot.Location) > MaxLocationLength {
		return fmt.Errorf("a Probe Location identifier is 1 to %d bytes", MaxLocationLength)
	}
	if !isUTC(slot.ScheduledFor) {
		return errors.New("a Run slot requires a UTC scheduled instant")
	}
	return nil
}

// MaxLocationLength bounds the Probe Location identifier until the entity that owns it
// exists and can impose its own rule (ADR 0025).
const MaxLocationLength = 63

// Run is one execution of one Monitor Revision from one Probe Location.
//
// Three states are representable and a fourth is not. A claimed Run has a lease and no
// outcome; a finished Run has an outcome and no lease; a skipped Run has OutcomeSkipped, no
// lease, and no execution instants. A Run that is both leased and finished is rejected here
// and by a check constraint in the database (ADR 0025).
type Run struct {
	ID   ID
	Slot Slot
	Kind Kind
	// Outcome is empty while the Run is in flight.
	Outcome Outcome
	// StartedAt and FinishedAt are the wall-clock record of execution, zero when the Run has
	// not started or was skipped. Latency is the Observation's monotonic Duration, never the
	// difference between these two.
	StartedAt  time.Time
	FinishedAt time.Time
	// LeaseHolder identifies the worker that claimed the slot, and is the token a recorder
	// must still match to write its result. It is empty once the Run is no longer in flight.
	LeaseHolder string
	// LeaseExpiresAt is when the claim lapses and any worker may reclaim the slot. It is
	// zero once the Run is no longer in flight.
	LeaseExpiresAt time.Time
}

// Claim builds an in-flight Run: the slot is taken, execution has not produced anything yet.
func Claim(id ID, slot Slot, kind Kind, holder string, leaseExpiresAt time.Time) (Run, error) {
	if id == "" {
		return Run{}, errors.New("a Run requires an identifier")
	}
	if err := slot.Validate(); err != nil {
		return Run{}, err
	}
	if !validKind(kind) {
		return Run{}, fmt.Errorf("unknown Run kind %q", kind)
	}
	if holder == "" || len(holder) > MaxLeaseHolderLength {
		return Run{}, fmt.Errorf("a lease holder token is 1 to %d bytes", MaxLeaseHolderLength)
	}
	if !isUTC(leaseExpiresAt) {
		return Run{}, errors.New("a lease requires a UTC expiry")
	}
	return Run{ID: id, Slot: slot, Kind: kind, LeaseHolder: holder, LeaseExpiresAt: leaseExpiresAt}, nil
}

// MaxLeaseHolderLength bounds the worker token so a lease column cannot become free storage.
const MaxLeaseHolderLength = 128

// Skip builds a Run that the scheduler deliberately did not execute. ADR 0021's misfire
// policy is skip and record: a gap is written down rather than filled with late executions.
func Skip(id ID, slot Slot, kind Kind) (Run, error) {
	if id == "" {
		return Run{}, errors.New("a Run requires an identifier")
	}
	if err := slot.Validate(); err != nil {
		return Run{}, err
	}
	if !validKind(kind) {
		return Run{}, fmt.Errorf("unknown Run kind %q", kind)
	}
	return Run{ID: id, Slot: slot, Kind: kind, Outcome: OutcomeSkipped}, nil
}

// InFlight reports whether the Run has been claimed and has not yet recorded a result.
func (value Run) InFlight() bool { return value.Outcome == "" }

// RenewLease extends a claim while the holder is still executing.
func (value *Run) RenewLease(expiresAt time.Time) error {
	if !value.InFlight() {
		return errors.New("a Run that has recorded its outcome holds no lease")
	}
	if !isUTC(expiresAt) {
		return errors.New("a lease requires a UTC expiry")
	}
	if !expiresAt.After(value.LeaseExpiresAt) {
		return errors.New("a lease renewal must extend the current expiry")
	}
	value.LeaseExpiresAt = expiresAt
	return nil
}

// Complete records an execution outcome and releases the lease. OutcomeSkipped is rejected:
// a Run that was claimed and executed did not skip, whatever it found.
func (value *Run) Complete(outcome Outcome, startedAt, finishedAt time.Time) error {
	if !value.InFlight() {
		return errors.New("a Run records its outcome once")
	}
	if !validExecutionOutcome(outcome) {
		return fmt.Errorf("unknown Run execution outcome %q", outcome)
	}
	if !isUTC(startedAt) || !isUTC(finishedAt) {
		return errors.New("persisted timestamps must be UTC")
	}
	if finishedAt.Before(startedAt) {
		return errors.New("a Run cannot finish before it started")
	}
	value.Outcome = outcome
	value.StartedAt = startedAt
	value.FinishedAt = finishedAt
	value.LeaseHolder = ""
	value.LeaseExpiresAt = time.Time{}
	return nil
}

// Restore validates a Run loaded from persistence, rejecting the state combinations the
// schema forbids so a hand-edited row cannot become a domain object.
func Restore(
	id ID,
	slot Slot,
	kind Kind,
	outcome Outcome,
	startedAt, finishedAt time.Time,
	leaseHolder string,
	leaseExpiresAt time.Time,
) (Run, error) {
	if id == "" {
		return Run{}, errors.New("a Run requires an identifier")
	}
	if err := slot.Validate(); err != nil {
		return Run{}, err
	}
	if !validKind(kind) {
		return Run{}, fmt.Errorf("unknown Run kind %q", kind)
	}

	leased := leaseHolder != "" || !leaseExpiresAt.IsZero()
	if outcome == "" {
		if !leased {
			return Run{}, errors.New("a Run with no outcome must hold a lease")
		}
		if leaseHolder == "" || !isUTC(leaseExpiresAt) {
			return Run{}, errors.New("a held lease requires a holder and a UTC expiry")
		}
		if !startedAt.IsZero() || !finishedAt.IsZero() {
			return Run{}, errors.New("a Run in flight has no execution instants")
		}
		return Run{ID: id, Slot: slot, Kind: kind, LeaseHolder: leaseHolder, LeaseExpiresAt: leaseExpiresAt}, nil
	}

	if leased {
		return Run{}, errors.New("a Run that recorded its outcome holds no lease")
	}
	if outcome == OutcomeSkipped {
		if !startedAt.IsZero() || !finishedAt.IsZero() {
			return Run{}, errors.New("a skipped Run never executed and has no execution instants")
		}
		return Run{ID: id, Slot: slot, Kind: kind, Outcome: outcome}, nil
	}
	if !validExecutionOutcome(outcome) {
		return Run{}, fmt.Errorf("unknown Run outcome %q", outcome)
	}
	if !isUTC(startedAt) || !isUTC(finishedAt) {
		return Run{}, errors.New("an executed Run requires UTC execution instants")
	}
	if finishedAt.Before(startedAt) {
		return Run{}, errors.New("a Run cannot finish before it started")
	}
	return Run{
		ID: id, Slot: slot, Kind: kind, Outcome: outcome,
		StartedAt: startedAt, FinishedAt: finishedAt,
	}, nil
}

// Misfired reports whether a due slot is too stale to execute. ADR 0021 fixes the boundary
// at one interval: running a backlog of stale checks produces alerts about the past, and
// dropping the slots silently makes the gap invisible, so a stale slot is recorded as
// skipped instead.
func Misfired(scheduledFor, now time.Time, interval time.Duration) bool {
	if interval <= 0 {
		return false
	}
	return now.Sub(scheduledFor) > interval
}

// LeaseMargin is added to the effective execution ceiling to derive a lease duration
// (ADR 0021). It covers claiming, recording, and ordinary scheduling jitter, so a worker
// that is merely slow does not lose a lease it is still executing under.
const LeaseMargin = 30 * time.Second

// MaxLeaseDuration caps the lease so an absurd execution ceiling cannot make an abandoned
// slot unreclaimable for hours.
const MaxLeaseDuration = 15 * time.Minute

// LeaseDuration derives a bounded lease from the effective execution ceiling.
func LeaseDuration(executionCeiling time.Duration) time.Duration {
	if executionCeiling < 0 {
		executionCeiling = 0
	}
	duration := executionCeiling + LeaseMargin
	if duration > MaxLeaseDuration {
		return MaxLeaseDuration
	}
	return duration
}

func validKind(kind Kind) bool {
	return kind == KindScheduled || kind == KindConfirmation || kind == KindManual
}

func validExecutionOutcome(outcome Outcome) bool {
	switch outcome {
	case OutcomePassed, OutcomeFailed, OutcomeErrored, OutcomeTimedOut, OutcomeCancelled:
		return true
	default:
		return false
	}
}

func isUTC(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	_, offset := value.Zone()
	return offset == 0
}
