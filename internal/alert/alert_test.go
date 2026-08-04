package alert

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type recordingStore struct {
	event IncidentTransitionedV1
	value Alert
	calls int
}

func (store *recordingStore) ProjectIncidentTransition(
	_ context.Context, event IncidentTransitionedV1, value Alert, _ IDGenerator,
) error {
	store.event, store.value = event, value
	store.calls++
	return nil
}

func (*recordingStore) ListAlerts(context.Context, Scope, ListQuery) ([]Alert, bool, bool, error) {
	return nil, false, false, nil
}

type testClock struct{ value time.Time }

func (clock testClock) Now() time.Time { return clock.value }

type testIDs struct{}

func (testIDs) NewUUIDv7(time.Time) (string, error) {
	return "00000000-0000-7000-8000-000000000099", nil
}

func validIncidentTransition(now time.Time) IncidentTransitionedV1 {
	return IncidentTransitionedV1{
		EventID:        "00000000-0000-7000-8000-000000000001",
		OrganizationID: "00000000-0000-7000-8000-000000000002",
		OccurredAt:     now, AggregateType: "incident",
		AggregateID: "00000000-0000-7000-8000-000000000003", AggregateVersion: 2,
		CausationID: "00000000-0000-7000-8000-000000000004",
		IncidentID:  "00000000-0000-7000-8000-000000000003",
		ProjectID:   "00000000-0000-7000-8000-000000000005",
		MonitorID:   "00000000-0000-7000-8000-000000000006",
		Transition:  "resolved",
	}
}

func TestHandleIncidentTransitionValidatesEnvelopeAndBuildsImmutableAlert(t *testing.T) {
	occurredAt := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	projectedAt := occurredAt.Add(time.Second)
	event := validIncidentTransition(occurredAt)
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	store := &recordingStore{}
	service := NewService(store, testClock{projectedAt}, testIDs{})
	if err := service.HandleIncidentTransition(
		t.Context(), event.EventID, event.OrganizationID, payload,
	); err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || store.event != event {
		t.Fatalf("projected event = %#v, calls %d", store.event, store.calls)
	}
	if store.value.ID == "" || store.value.Kind != KindIncidentResolved ||
		store.value.IncidentVersion != event.AggregateVersion ||
		store.value.OccurredAt != occurredAt || store.value.CreatedAt != projectedAt {
		t.Fatalf("Alert = %#v", store.value)
	}

	withAdditionalMember := append(payload[:len(payload)-1], []byte(`,"optionalMetadata":"ignored"}`)...)
	if err := service.HandleIncidentTransition(
		t.Context(), event.EventID, event.OrganizationID, withAdditionalMember,
	); err != nil {
		t.Fatalf("additional event member was rejected: %v", err)
	}
}

func TestHandleIncidentTransitionRejectsMismatchedAndInvalidFacts(t *testing.T) {
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)
	base := validIncidentTransition(now)
	cases := []struct {
		name           string
		eventID        string
		organizationID string
		mutate         func(*IncidentTransitionedV1)
		want           error
	}{
		{name: "row event mismatch", eventID: "wrong", organizationID: base.OrganizationID, want: ErrPayloadInvalid},
		{name: "row Organization mismatch", eventID: base.EventID, organizationID: "wrong", want: ErrOrganizationMismatch},
		{name: "acknowledgement is not an Alert fact", eventID: base.EventID, organizationID: base.OrganizationID,
			mutate: func(value *IncidentTransitionedV1) { value.Transition = "acknowledged" }, want: ErrPayloadInvalid},
		{name: "aggregate mismatch", eventID: base.EventID, organizationID: base.OrganizationID,
			mutate: func(value *IncidentTransitionedV1) { value.AggregateID = "wrong" }, want: ErrPayloadInvalid},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			event := base
			if testCase.mutate != nil {
				testCase.mutate(&event)
			}
			payload, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			err = NewService(&recordingStore{}, testClock{now}, testIDs{}).
				HandleIncidentTransition(t.Context(), testCase.eventID, testCase.organizationID, payload)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("HandleIncidentTransition() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestListRejectsUnscopedAndUnboundedQueries(t *testing.T) {
	service := NewService(&recordingStore{}, testClock{}, testIDs{})
	if _, _, err := service.List(t.Context(), Scope{}, ListQuery{PageSize: 50}); err == nil {
		t.Fatal("unscoped Alert query succeeded")
	}
	scope := Scope{OrganizationID: "org", ProjectID: "project", MonitorID: "monitor"}
	if _, _, err := service.List(t.Context(), scope, ListQuery{PageSize: 101}); err == nil {
		t.Fatal("oversized Alert query succeeded")
	}
	nonUTC := time.Date(2026, time.August, 1, 10, 0, 0, 0, time.FixedZone("UTC+08:00", 8*60*60))
	if _, _, err := service.List(t.Context(), scope, ListQuery{
		PageSize: 50, Cursor: &Cursor{OccurredAt: nonUTC, ID: "alert"},
	}); err == nil {
		t.Fatal("non-UTC Alert cursor succeeded")
	}
}
