package run

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEffectiveIntervalTakesTheOperatorFloor(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		configured time.Duration
		minimum    time.Duration
		want       time.Duration
	}{
		"above the floor": {5 * time.Minute, time.Minute, 5 * time.Minute},
		"below the floor": {30 * time.Second, time.Minute, time.Minute},
		"at the floor":    {time.Minute, time.Minute, time.Minute},
		"no floor":        {30 * time.Second, 0, 30 * time.Second},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := EffectiveInterval(testCase.configured, testCase.minimum); got != testCase.want {
				t.Fatalf("EffectiveInterval() = %v, want %v", got, testCase.want)
			}
		})
	}
}

// The offset is what stops every Monitor sharing an interval from becoming due in the same
// second, so it has to actually vary across identifiers and stay inside the interval.
func TestSlotOffsetIsStableBoundedAndSpread(t *testing.T) {
	t.Parallel()
	const interval = 60 * time.Second

	first := SlotOffset("00000000-0000-7000-8000-000000000001", interval)
	if first != SlotOffset("00000000-0000-7000-8000-000000000001", interval) {
		t.Fatalf("SlotOffset() is not stable for one Monitor")
	}

	distinct := make(map[time.Duration]struct{})
	for index := range 200 {
		id := string(rune('a'+index%26)) + string(rune('a'+index/26))
		offset := SlotOffset(id, interval)
		if offset < 0 || offset >= interval {
			t.Fatalf("SlotOffset(%q) = %v, want [0, %v)", id, offset, interval)
		}
		distinct[offset] = struct{}{}
	}
	// A hash that produced one or two buckets would satisfy the bounds above and defeat the
	// whole purpose, so the spread itself is asserted.
	if len(distinct) < 20 {
		t.Fatalf("SlotOffset() produced %d distinct offsets over 200 Monitors, want a spread", len(distinct))
	}
}

func TestSlotForIsAlignedReproducibleAndNotInTheFuture(t *testing.T) {
	t.Parallel()
	const (
		monitorID = "00000000-0000-7000-8000-000000000001"
		interval  = 60 * time.Second
	)
	offset := SlotOffset(monitorID, interval)
	now := time.Date(2026, time.July, 27, 10, 30, 17, 500_000_000, time.UTC)

	slot, err := SlotFor(monitorID, interval, now)
	if err != nil {
		t.Fatalf("SlotFor() error = %v", err)
	}
	if slot.After(now) {
		t.Fatalf("SlotFor() = %v, which is after now %v", slot, now)
	}
	if now.Sub(slot) >= interval {
		t.Fatalf("SlotFor() = %v, which is more than one interval before now %v", slot, now)
	}
	if (slot.Unix()-int64(offset/time.Second))%int64(interval/time.Second) != 0 {
		t.Fatalf("SlotFor() = %v is not on the Monitor's series", slot)
	}
	if slot.Location() != time.UTC {
		t.Fatalf("SlotFor() location = %v, want UTC", slot.Location())
	}

	// Reproducibility is the whole point: a second worker computing the same slot from the
	// same inputs is what lets both agree without coordinating.
	elsewhere, err := SlotFor(monitorID, interval, now.In(time.FixedZone("CST", 8*3600)))
	if err != nil || !elsewhere.Equal(slot) {
		t.Fatalf("SlotFor() in another zone = %v (err %v), want %v", elsewhere, err, slot)
	}
}

func TestSlotForAdvancesExactlyOnePerInterval(t *testing.T) {
	t.Parallel()
	const (
		monitorID = "monitor-a"
		interval  = 30 * time.Second
	)
	now := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	first, err := SlotFor(monitorID, interval, now)
	if err != nil {
		t.Fatalf("SlotFor() error = %v", err)
	}
	// Boundaries sit on the Monitor's own series rather than on the caller's instant, so the
	// window that must hold one slot is measured from the slot, not from now.
	same, err := SlotFor(monitorID, interval, first.Add(interval-time.Nanosecond))
	if err != nil || !same.Equal(first) {
		t.Fatalf("slot moved within its own interval: %v then %v (err %v)", first, same, err)
	}
	next, err := SlotFor(monitorID, interval, first.Add(interval))
	if err != nil {
		t.Fatalf("SlotFor() error = %v", err)
	}
	if next.Sub(first) != interval {
		t.Fatalf("slot advanced by %v over one interval, want %v", next.Sub(first), interval)
	}
}

func TestSlotForRejectsSubSecondIntervals(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	for _, interval := range []time.Duration{0, -time.Minute, 1500 * time.Millisecond, time.Millisecond} {
		if _, err := SlotFor("monitor-a", interval, now); err == nil {
			t.Fatalf("SlotFor(%v) = nil error, want a rejection", interval)
		}
	}
}

func TestMissedSlotsWalksBackwardsAndStopsAtTheLastAttempt(t *testing.T) {
	t.Parallel()
	const interval = time.Minute
	current := time.Date(2026, time.July, 27, 10, 10, 0, 0, time.UTC)

	// Three intervals of gap leaves exactly the two slots between them.
	previous := current.Add(-3 * interval)
	missed, err := MissedSlots("monitor-a", interval, current, previous, 10)
	if err != nil {
		t.Fatalf("MissedSlots() error = %v", err)
	}
	want := []time.Time{current.Add(-interval), current.Add(-2 * interval)}
	if len(missed) != len(want) {
		t.Fatalf("MissedSlots() = %v, want %v", missed, want)
	}
	for index, instant := range want {
		if !missed[index].Equal(instant) {
			t.Fatalf("MissedSlots()[%d] = %v, want %v", index, missed[index], instant)
		}
	}

	// No gap at all leaves nothing to record.
	if adjacent, err := MissedSlots("monitor-a", interval, current, current.Add(-interval), 10); err != nil || len(adjacent) != 0 {
		t.Fatalf("MissedSlots() with no gap = %v (err %v), want none", adjacent, err)
	}
}

// ADR 0026 bounds the walk so a week of downtime does not write a row per missed slot.
func TestMissedSlotsIsBounded(t *testing.T) {
	t.Parallel()
	const interval = 30 * time.Second
	current := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)

	missed, err := MissedSlots("monitor-a", interval, current, current.AddDate(0, 0, -7), 10)
	if err != nil {
		t.Fatalf("MissedSlots() error = %v", err)
	}
	if len(missed) != 10 {
		t.Fatalf("MissedSlots() over a week = %d slots, want the bound of 10", len(missed))
	}
	if !missed[0].Equal(current.Add(-interval)) {
		t.Fatalf("MissedSlots()[0] = %v, want the slot immediately before current", missed[0])
	}

	// A restart has no memory of this Monitor, so the full bound is walked.
	restarted, err := MissedSlots("monitor-a", interval, current, time.Time{}, 10)
	if err != nil || len(restarted) != 10 {
		t.Fatalf("MissedSlots() after a restart = %d slots (err %v), want 10", len(restarted), err)
	}
	if none, err := MissedSlots("monitor-a", interval, current, time.Time{}, 0); err != nil || len(none) != 0 {
		t.Fatalf("MissedSlots() with no bound = %v (err %v), want none", none, err)
	}
}

func TestSchedulableValidate(t *testing.T) {
	t.Parallel()
	valid := Schedulable{
		OrganizationID: "org", MonitorID: "monitor", RevisionNumber: 2,
		CheckType: "http", CheckSchemaVersion: 1,
		CheckConfiguration: json.RawMessage(`{"url":"https://example.test"}`),
		Interval:           time.Minute,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Schedulable.Validate() error = %v", err)
	}

	cases := map[string]func(*Schedulable){
		"no Organization":     func(value *Schedulable) { value.OrganizationID = "" },
		"no Monitor":          func(value *Schedulable) { value.MonitorID = "" },
		"no revision":         func(value *Schedulable) { value.RevisionNumber = 0 },
		"no check type":       func(value *Schedulable) { value.CheckType = "" },
		"no schema version":   func(value *Schedulable) { value.CheckSchemaVersion = 0 },
		"no configuration":    func(value *Schedulable) { value.CheckConfiguration = nil },
		"sub-second interval": func(value *Schedulable) { value.Interval = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value := valid
			mutate(&value)
			if err := value.Validate(); err == nil {
				t.Fatalf("Schedulable.Validate() = nil error, want a rejection")
			}
		})
	}
}
