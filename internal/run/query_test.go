package run

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeQueryStore struct {
	exists           bool
	values           []Run
	more             bool
	value            Run
	runFound         bool
	observation      Observation
	observationFound bool
	listCalls        int
	observationCalls int
	scope            Scope
	query            ListQuery
}

func (store *fakeQueryStore) MonitorExists(_ context.Context, scope Scope) (bool, error) {
	store.scope = scope
	return store.exists, nil
}

func (store *fakeQueryStore) ListRuns(
	_ context.Context, scope Scope, query ListQuery,
) ([]Run, bool, error) {
	store.listCalls++
	store.scope, store.query = scope, query
	return store.values, store.more, nil
}

func (store *fakeQueryStore) FindScopedRun(
	_ context.Context, scope Scope, _ ID,
) (Run, bool, error) {
	store.scope = scope
	return store.value, store.runFound, nil
}

func (store *fakeQueryStore) FindObservation(
	_ context.Context, organizationID string, _ ID, _ time.Time,
) (Observation, bool, error) {
	store.observationCalls++
	if organizationID != store.scope.OrganizationID {
		return Observation{}, false, errors.New("Observation query lost Organization scope")
	}
	return store.observation, store.observationFound, nil
}

func queryTestScope() Scope {
	return Scope{OrganizationID: "organization", ProjectID: "project", MonitorID: "monitor"}
}

func queryTestRun(t *testing.T, id string, scheduledFor time.Time, outcome Outcome) Run {
	t.Helper()
	value, err := Claim(
		ID(id),
		Slot{
			OrganizationID: "organization",
			MonitorID:      "monitor",
			RevisionNumber: 1,
			Location:       "local",
			ScheduledFor:   scheduledFor,
		},
		KindScheduled,
		"worker",
		scheduledFor.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != "" {
		if err := value.Complete(outcome, scheduledFor, scheduledFor.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	return value
}

func TestQueryServiceListPreservesScopeAndBuildsCursor(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	first := queryTestRun(t, "run-2", now, OutcomePassed)
	second := queryTestRun(t, "run-1", now.Add(-time.Minute), OutcomeFailed)
	store := &fakeQueryStore{
		exists: true,
		values: []Run{first, second},
		more:   true,
	}
	service := NewQueryService(store)
	scope := queryTestScope()
	query := ListQuery{
		NotBefore: now.Add(-time.Hour),
		PageSize:  2,
		Outcome:   OutcomeFailed,
		Kind:      KindScheduled,
		Location:  "local",
	}

	page, found, err := service.List(t.Context(), scope, query)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if !found || len(page.Runs) != 2 {
		t.Fatalf("List() = found %v, %d Runs", found, len(page.Runs))
	}
	if store.scope != scope || store.query != query || store.listCalls != 1 {
		t.Fatalf("List() store input = %#v, %#v, calls %d", store.scope, store.query, store.listCalls)
	}
	if page.NextCursor == nil ||
		page.NextCursor.ID != second.ID ||
		!page.NextCursor.ScheduledFor.Equal(second.Slot.ScheduledFor) {
		t.Fatalf("List() next cursor = %#v", page.NextCursor)
	}
}

func TestQueryServiceListHidesUnknownMonitorAndReturnsEmptyArray(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	store := &fakeQueryStore{}
	service := NewQueryService(store)
	query := ListQuery{NotBefore: now, PageSize: DefaultPageSize}

	page, found, err := service.List(t.Context(), queryTestScope(), query)
	if err != nil || found || store.listCalls != 0 {
		t.Fatalf("unknown List() = %#v, found %v, err %v, calls %d", page, found, err, store.listCalls)
	}

	store.exists = true
	page, found, err = service.List(t.Context(), queryTestScope(), query)
	if err != nil || !found || page.Runs == nil || len(page.Runs) != 0 {
		t.Fatalf("empty List() = %#v, found %v, err %v", page, found, err)
	}
}

func TestQueryServiceRejectsUnboundedAndUnknownFilters(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	service := NewQueryService(&fakeQueryStore{exists: true})
	cases := map[string]ListQuery{
		"zero lower bound": {PageSize: 1},
		"zero page":        {NotBefore: now},
		"oversized page":   {NotBefore: now, PageSize: MaxPageSize + 1},
		"unknown outcome":  {NotBefore: now, PageSize: 1, Outcome: "unknown"},
		"unknown kind":     {NotBefore: now, PageSize: 1, Kind: "unknown"},
	}
	for name, query := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := service.List(t.Context(), queryTestScope(), query); err == nil {
				t.Fatal("List() error = nil, want validation failure")
			}
		})
	}
}

func TestQueryServiceObservationStatesAndInvariant(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	completed := queryTestRun(t, "completed", now, OutcomePassed)
	observation := Observation{
		RunID: completed.ID, ScheduledFor: now, OrganizationID: "organization",
		Duration: time.Second,
	}

	t.Run("completed", func(t *testing.T) {
		store := &fakeQueryStore{
			value: completed, runFound: true,
			observation: observation, observationFound: true,
		}
		got, found, err := NewQueryService(store).GetObservation(
			t.Context(), queryTestScope(), completed.ID,
		)
		if err != nil || !found || got.RunID != completed.ID || store.observationCalls != 1 {
			t.Fatalf("GetObservation() = %#v, found %v, err %v, calls %d", got, found, err, store.observationCalls)
		}
	})

	t.Run("in flight", func(t *testing.T) {
		store := &fakeQueryStore{
			value: queryTestRun(t, "in-flight", now, ""), runFound: true,
		}
		if _, found, err := NewQueryService(store).GetObservation(
			t.Context(), queryTestScope(), store.value.ID,
		); err != nil || found || store.observationCalls != 0 {
			t.Fatalf("in-flight GetObservation() = found %v, err %v, calls %d", found, err, store.observationCalls)
		}
	})

	t.Run("skipped", func(t *testing.T) {
		skipped, err := Skip(
			"skipped",
			Slot{
				OrganizationID: "organization", MonitorID: "monitor", RevisionNumber: 1,
				Location: "local", ScheduledFor: now,
			},
			KindScheduled,
		)
		if err != nil {
			t.Fatal(err)
		}
		store := &fakeQueryStore{value: skipped, runFound: true}
		if _, found, err := NewQueryService(store).GetObservation(
			t.Context(), queryTestScope(), skipped.ID,
		); err != nil || found || store.observationCalls != 0 {
			t.Fatalf("skipped GetObservation() = found %v, err %v, calls %d", found, err, store.observationCalls)
		}
	})

	t.Run("missing completed detail", func(t *testing.T) {
		store := &fakeQueryStore{value: completed, runFound: true}
		if _, _, err := NewQueryService(store).GetObservation(
			t.Context(), queryTestScope(), completed.ID,
		); err == nil {
			t.Fatal("GetObservation() error = nil, want invariant failure")
		}
	})
}
