package run

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewRunRecordedEntryCanonicalizesScheduledFor(t *testing.T) {
	t.Parallel()
	scheduledFor := time.Date(2026, time.August, 10, 9, 30, 15, 123456789, time.UTC)
	value, err := Claim(
		"run-1", Slot{
			OrganizationID: "org", MonitorID: "monitor", RevisionNumber: 1,
			Location: "embedded", ScheduledFor: scheduledFor,
		}, KindManual, "worker-a", scheduledFor.Add(time.Minute))
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if err := value.Complete(OutcomePassed, scheduledFor, scheduledFor.Add(time.Second)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	entry, err := NewRunRecordedEntry("event-1", value, scheduledFor.Add(time.Second))
	if err != nil {
		t.Fatalf("NewRunRecordedEntry() error = %v", err)
	}
	var event RunRecordedV1
	if err := json.Unmarshal(entry.Payload, &event); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	want := scheduledFor.Truncate(time.Microsecond)
	if !event.ScheduledFor.Equal(want) {
		t.Fatalf("event scheduledFor = %s, want %s", event.ScheduledFor, want)
	}
}
