package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"
)

func TestDispatcherLogsDeadLetterMetadataWithoutPayload(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC)
	entry := Entry{
		ID: "event-1", OrganizationID: "organization", Topic: "known", Attempts: 1,
		Payload: []byte("{\"secret\":\"sensitive-value\"}"),
	}
	store := &dispatcherStore{entries: []Entry{entry}}
	var output bytes.Buffer
	dispatcher, err := New(Config{
		Store: store,
		Handlers: map[string]Handler{"known": HandlerFunc(func(context.Context, Entry) error {
			return Permanent(CodePayloadInvalid)
		})},
		Clock: testClock{now: now}, UUIDs: testIDs{},
		Logger: slog.New(slog.NewJSONHandler(&output, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.Tick(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	var record struct {
		Message     string `json:"msg"`
		EventID     string `json:"eventId"`
		Topic       string `json:"topic"`
		FailureCode string `json:"failureCode"`
		Attempt     int    `json:"attempt"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("unmarshal log record: %v; log = %s", err, output.String())
	}
	if record.Message != "outbox entry dead-lettered" || record.EventID != entry.ID ||
		record.Topic != entry.Topic || record.FailureCode != CodePayloadInvalid || record.Attempt != entry.Attempts {
		t.Fatalf("dead-letter log = %#v", record)
	}
	if bytes.Contains(output.Bytes(), []byte("sensitive-value")) {
		t.Fatalf("dead-letter log exposed the event payload: %s", output.String())
	}
}
