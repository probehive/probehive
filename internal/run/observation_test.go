package run

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func testObservation() Observation {
	return Observation{
		RunID:          "run-1",
		ScheduledFor:   time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC),
		OrganizationID: "org",
		Duration:       420 * time.Millisecond,
		Phases:         Phases{Connect: 30 * time.Millisecond, FirstByte: 200 * time.Millisecond},
	}
}

func TestObservationValidateAcceptsAMeasurement(t *testing.T) {
	t.Parallel()
	value := testObservation()
	value.HTTP = &HTTPDetail{
		StatusCode: 200, Protocol: "HTTP/2.0", RedirectCount: 1, BodyBytes: 4096,
		TLS: &TLSDetail{
			Version: "TLS 1.3", CipherSuite: "TLS_AES_128_GCM_SHA256",
			CertificateExpiresAt: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	if err := value.Validate(); err != nil {
		t.Fatalf("Observation.Validate() error = %v", err)
	}
}

func TestObservationValidateRejectsUnusableRecords(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*Observation){
		"no Run identity":      func(value *Observation) { value.RunID = "" },
		"no Organization":      func(value *Observation) { value.OrganizationID = "" },
		"no scheduled instant": func(value *Observation) { value.ScheduledFor = time.Time{} },
		"non-UTC scheduled":    func(value *Observation) { value.ScheduledFor = value.ScheduledFor.In(time.FixedZone("CST", 8*3600)) },
		"negative duration":    func(value *Observation) { value.Duration = -time.Second },
		"negative phase":       func(value *Observation) { value.Phases.TLS = -time.Second },
		"oversized code":       func(value *Observation) { value.FailureCode = strings.Repeat("c", MaxCodeLength+1) },
		"class without a code": func(value *Observation) { value.FailureClass = "private" },
		"status below range":   func(value *Observation) { value.HTTP = &HTTPDetail{StatusCode: 99} },
		"status above range":   func(value *Observation) { value.HTTP = &HTTPDetail{StatusCode: 600} },
		"negative redirects":   func(value *Observation) { value.HTTP = &HTTPDetail{StatusCode: 200, RedirectCount: -1} },
		"negative body":        func(value *Observation) { value.HTTP = &HTTPDetail{StatusCode: 200, BodyBytes: -1} },
		"oversized protocol": func(value *Observation) {
			value.HTTP = &HTTPDetail{StatusCode: 200, Protocol: strings.Repeat("p", MaxProtocolLength+1)}
		},
		"oversized TLS version": func(value *Observation) {
			value.HTTP = &HTTPDetail{StatusCode: 200, TLS: &TLSDetail{Version: strings.Repeat("v", MaxTLSFieldLength+1)}}
		},
		"non-UTC certificate expiry": func(value *Observation) {
			value.HTTP = &HTTPDetail{StatusCode: 200, TLS: &TLSDetail{
				CertificateExpiresAt: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.FixedZone("CST", 8*3600)),
			}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value := testObservation()
			mutate(&value)
			if err := value.Validate(); err == nil {
				t.Fatalf("Observation.Validate() = nil error, want a rejection")
			}
		})
	}
}

// A denial class is meaningless without the outbound reason that carried it, but a bare
// failure code is ordinary: most probe failures name no address class at all.
func TestObservationAcceptsACodeWithoutAClass(t *testing.T) {
	t.Parallel()
	value := testObservation()
	value.FailureCode = "probe.transport.failed"
	if err := value.Validate(); err != nil {
		t.Fatalf("Observation.Validate() error = %v", err)
	}
}

func TestOutboxEntryValidate(t *testing.T) {
	t.Parallel()
	valid := OutboxEntry{
		ID: "entry-1", OrganizationID: "org", Topic: "example.topic",
		Payload:   json.RawMessage(`{"runId":"run-1"}`),
		CreatedAt: time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("OutboxEntry.Validate() error = %v", err)
	}

	cases := map[string]func(*OutboxEntry){
		"no identifier":   func(entry *OutboxEntry) { entry.ID = "" },
		"no Organization": func(entry *OutboxEntry) { entry.OrganizationID = "" },
		"no topic":        func(entry *OutboxEntry) { entry.Topic = "" },
		"oversized topic": func(entry *OutboxEntry) { entry.Topic = strings.Repeat("t", MaxTopicLength+1) },
		"no payload":      func(entry *OutboxEntry) { entry.Payload = nil },
		"invalid JSON":    func(entry *OutboxEntry) { entry.Payload = json.RawMessage(`{"runId":`) },
		"oversized payload": func(entry *OutboxEntry) {
			entry.Payload = json.RawMessage(`"` + strings.Repeat("x", MaxPayloadBytes) + `"`)
		},
		"no creation instant": func(entry *OutboxEntry) { entry.CreatedAt = time.Time{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			entry := valid
			mutate(&entry)
			if err := entry.Validate(); err == nil {
				t.Fatalf("OutboxEntry.Validate() = nil error, want a rejection")
			}
		})
	}
}
