package run

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"time"
)

// Schedulable is the projection of one active Monitor that the scheduler needs to put it on
// the clock and hand it to an executor (ADR 0026).
//
// It is deliberately not monitor.Monitor: a feature package imports no sibling feature
// implementation, and the scheduler needs a revision's configuration rather than a Monitor's
// lifecycle. The persistence adapter assembles it from both tables.
type Schedulable struct {
	OrganizationID string
	MonitorID      string
	// RevisionNumber is the latest revision, which is the definition a new Run executes.
	// The Run records it, so replacing configuration never rewrites what already ran.
	RevisionNumber     int
	CheckType          string
	CheckSchemaVersion int
	CheckConfiguration json.RawMessage
	// Interval is the Monitor's configured execution interval before the operator floor is
	// applied. The scheduler applies the floor; storage keeps what the tenant configured.
	Interval time.Duration
	// NotBefore is the instant the Monitor last changed, and is the floor on misfire
	// recording. Without it, a Monitor that has just been activated looks exactly like one
	// the installation was down for: the scheduler has no memory of either, and would record
	// missed slots from before the Monitor was ever eligible to run.
	//
	// A gap is only a gap if the Monitor was already schedulable across it.
	NotBefore time.Time
}

// Validate rejects a projection the scheduler could not act on.
func (value Schedulable) Validate() error {
	if value.OrganizationID == "" || value.MonitorID == "" {
		return errors.New("a schedulable Monitor requires Organization and Monitor identity")
	}
	if value.RevisionNumber < 1 {
		return errors.New("a schedulable Monitor requires a revision")
	}
	if value.CheckType == "" || value.CheckSchemaVersion < 1 {
		return errors.New("a schedulable Monitor requires a versioned check type")
	}
	if len(value.CheckConfiguration) == 0 {
		return errors.New("a schedulable Monitor requires check configuration")
	}
	if value.Interval < time.Second {
		return errors.New("a schedulable Monitor requires a positive whole-second interval")
	}
	return nil
}

// EffectiveInterval applies the operator floor. ADR 0026 makes the operator limit a minimum
// rather than a maximum, because a shorter interval is more load: an installation raises the
// floor and every Monitor configured below it slows down, without its stored configuration
// being rewritten.
func EffectiveInterval(configured, operatorMinimum time.Duration) time.Duration {
	if operatorMinimum > configured {
		return operatorMinimum
	}
	return configured
}

// SlotOffset returns a stable value in [0, interval) derived from the Monitor identifier.
//
// Its purpose is spreading load: without it every Monitor sharing an interval becomes due in
// the same second. It is derived from the identifier rather than from randomness so that it
// survives a restart and is identical in every worker, which is what lets two workers agree
// on a slot instant without talking to each other (ADR 0026).
func SlotOffset(monitorID string, interval time.Duration) time.Duration {
	seconds := int64(interval / time.Second)
	if seconds <= 0 {
		return 0
	}
	digest := fnv.New64a()
	// Hash writes never fail, and the interface's error is documented as always nil.
	_, _ = digest.Write([]byte(monitorID))
	return time.Duration(int64(digest.Sum64()%uint64(seconds))) * time.Second
}

// SlotFor returns the instant of the slot that is current at now: the most recent instant on
// the Monitor's derived series that has already arrived.
//
// The series is every offset + k*interval counted from the Unix epoch, so any worker computes
// the same instants from the same Monitor and interval. That is what makes ADR 0021's slot
// identity a value two workers agree on rather than one whoever inserted first decided.
func SlotFor(monitorID string, interval time.Duration, now time.Time) (time.Time, error) {
	seconds := int64(interval / time.Second)
	if seconds <= 0 || interval%time.Second != 0 {
		return time.Time{}, fmt.Errorf("a slot interval is a positive whole number of seconds, got %v", interval)
	}
	offset := int64(SlotOffset(monitorID, interval) / time.Second)
	index := floorDiv(now.UTC().Unix()-offset, seconds)
	return time.Unix(index*seconds+offset, 0).UTC(), nil
}

// MissedSlots returns the slots strictly between after and current, newest first, bounded by
// limit. It is how ADR 0021's skip-and-record policy learns what to record without walking a
// gap that may be weeks wide (ADR 0026).
//
// A zero after means the caller has no memory of this Monitor — a restart — so the full limit
// is returned and the slots that did run are rejected by the slot index rather than by a query.
func MissedSlots(monitorID string, interval time.Duration, current time.Time, after time.Time, limit int) ([]time.Time, error) {
	if limit < 1 {
		return nil, nil
	}
	seconds := int64(interval / time.Second)
	if seconds <= 0 || interval%time.Second != 0 {
		return nil, fmt.Errorf("a slot interval is a positive whole number of seconds, got %v", interval)
	}
	missed := make([]time.Time, 0, limit)
	for candidate := current.Add(-interval); len(missed) < limit; candidate = candidate.Add(-interval) {
		if !after.IsZero() && !candidate.After(after) {
			break
		}
		missed = append(missed, candidate)
	}
	return missed, nil
}

// floorDiv divides rounding towards negative infinity, so a slot series is continuous across
// the Unix epoch instead of folding at it.
func floorDiv(dividend, divisor int64) int64 {
	quotient := dividend / divisor
	if dividend%divisor != 0 && (dividend < 0) != (divisor < 0) {
		quotient--
	}
	return quotient
}
