package incident

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type recordingStore struct {
	event HealthTransitionedV1
	ids   ProcessIDs
	calls int
}

func (store *recordingStore) ProcessHealthTransition(
	_ context.Context, event HealthTransitionedV1, ids ProcessIDs, _ time.Time,
) error {
	store.event, store.ids = event, ids
	store.calls++
	return nil
}
func (*recordingStore) ListIncidents(context.Context, Scope, ListQuery) ([]Incident, bool, bool, error) {
	return nil, false, false, nil
}

func (*recordingStore) ListInbox(context.Context, string, InboxQuery, time.Time) ([]InboxItem, bool, bool, error) {
	return nil, false, false, nil
}
func (*recordingStore) GetIncident(context.Context, Scope, string) (Incident, bool, error) {
	return Incident{}, false, nil
}
func (*recordingStore) AcknowledgeIncident(context.Context, Scope, string, string, string, time.Time) (Incident, bool, error) {
	return Incident{}, false, nil
}

type incidentTestClock struct{ now time.Time }

func (clock incidentTestClock) Now() time.Time { return clock.now }

type incidentTestIDs struct{ next int }

func (ids *incidentTestIDs) NewUUIDv7(time.Time) (string, error) {
	ids.next++
	if ids.next == 1 {
		return "00000000-0000-7000-8000-000000000101", nil
	}
	if ids.next == 2 {
		return "00000000-0000-7000-8000-000000000102", nil
	}
	return "00000000-0000-7000-8000-000000000103", nil
}

func validHealthTransitionEvent(now time.Time) HealthTransitionedV1 {
	return HealthTransitionedV1{
		EventID:        "00000000-0000-7000-8000-000000000001",
		OrganizationID: "00000000-0000-7000-8000-000000000002",
		OccurredAt:     now, AggregateType: "monitorHealth",
		AggregateID: "00000000-0000-7000-8000-000000000003", AggregateVersion: 2,
		CausationID:  "00000000-0000-7000-8000-000000000004",
		TransitionID: "00000000-0000-7000-8000-000000000005",
		MonitorID:    "00000000-0000-7000-8000-000000000003",
		ProjectID:    "00000000-0000-7000-8000-000000000006",
		OldState:     "degraded", NewState: "down", PolicyVersion: "phase1.v1",
	}
}

func TestHandleHealthTransitionValidatesEnvelopeAndPreservesEventIdentity(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	store := &recordingStore{}
	service := NewService(store, incidentTestClock{now: now}, &incidentTestIDs{})
	event := validHealthTransitionEvent(now)
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.HandleHealthTransition(
		t.Context(), event.EventID, event.OrganizationID, payload,
	); err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || store.event.EventID != event.EventID ||
		store.ids.IncidentID == "" || store.ids.TimelineID == "" ||
		store.ids.AlertEventID == "" || store.ids.IncidentID == store.ids.TimelineID ||
		store.ids.TimelineID == store.ids.AlertEventID {
		t.Fatalf("stored transition = %#v, ids %#v", store.event, store.ids)
	}

	withAdditionalMember := append(payload[:len(payload)-1], []byte(`,"optionalMetadata":"ignored"}`)...)
	if err := service.HandleHealthTransition(
		t.Context(), event.EventID, event.OrganizationID, withAdditionalMember,
	); err != nil {
		t.Fatalf("additional event member was rejected: %v", err)
	}
	if store.calls != 2 {
		t.Fatalf("process calls = %d, want 2", store.calls)
	}

	withoutCausation := event
	withoutCausation.CausationID = ""
	withoutCausationPayload, err := json.Marshal(withoutCausation)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.HandleHealthTransition(
		t.Context(), event.EventID, event.OrganizationID, withoutCausationPayload,
	); err != nil {
		t.Fatalf("optional causation was required: %v", err)
	}
	if store.calls != 3 {
		t.Fatalf("process calls = %d, want 3", store.calls)
	}
}

func TestHandleHealthTransitionRejectsMismatchedAndIncompleteEnvelope(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	event := validHealthTransitionEvent(now)
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name           string
		eventID        string
		organizationID string
		mutate         func(*HealthTransitionedV1)
		want           error
	}{
		{name: "row event id mismatch", eventID: "00000000-0000-7000-8000-000000000099", organizationID: event.OrganizationID, want: ErrPayloadInvalid},
		{name: "row Organization mismatch", eventID: event.EventID, organizationID: "00000000-0000-7000-8000-000000000099", want: ErrOrganizationMismatch},
		{name: "missing policy version", eventID: event.EventID, organizationID: event.OrganizationID, mutate: func(value *HealthTransitionedV1) { value.PolicyVersion = "" }, want: ErrPayloadInvalid},
		{name: "bad aggregate", eventID: event.EventID, organizationID: event.OrganizationID, mutate: func(value *HealthTransitionedV1) { value.AggregateType = "incident" }, want: ErrPayloadInvalid},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := event
			if testCase.mutate != nil {
				testCase.mutate(&candidate)
			}
			candidatePayload := payload
			if testCase.mutate != nil {
				candidatePayload, err = json.Marshal(candidate)
				if err != nil {
					t.Fatal(err)
				}
			}
			service := NewService(&recordingStore{}, incidentTestClock{now: now}, &incidentTestIDs{})
			if got := service.HandleHealthTransition(
				t.Context(), testCase.eventID, testCase.organizationID, candidatePayload,
			); !errors.Is(got, testCase.want) {
				t.Fatalf("HandleHealthTransition() error = %v, want %v", got, testCase.want)
			}
		})
	}
}
