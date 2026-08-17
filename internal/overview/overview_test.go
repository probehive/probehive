package overview

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	value Summary
	found bool
	err   error
	limit int
}

func (store *fakeStore) GetOverview(_ context.Context, _ string, limit int) (Summary, bool, error) {
	store.limit = limit
	return store.value, store.found, store.err
}

func validSummary() Summary {
	return Summary{
		OrganizationID: "019f8f3d-5bb0-76fc-8fcf-02811ef6b2ee",
		Monitors:       MonitorCounts{Total: 5, Draft: 1, Active: 3, Paused: 1},
		Health:         HealthCounts{NotEvaluated: 1, Healthy: 1, Down: 1},
		Incidents:      IncidentCounts{Active: 2, Open: 1, Acknowledged: 1},
		ActiveIncidents: []ActiveIncident{
			{ID: "incident-1", ProjectID: "project-1", MonitorID: "monitor-1", MonitorName: "Checkout", State: "open", UpdatedAt: time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC)},
			{ID: "incident-2", ProjectID: "project-1", MonitorID: "monitor-2", MonitorName: "API", State: "acknowledged", UpdatedAt: time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC)},
		},
		Integrations: IntegrationCounts{Total: 2, Enabled: 1},
		StatusPage:   StatusPageState{Configured: true, Published: true},
	}
}

func TestServiceReturnsBoundedValidatedSummary(t *testing.T) {
	store := &fakeStore{value: validSummary(), found: true}
	service := NewService(store)
	value, found, err := service.Get(t.Context(), store.value.OrganizationID)
	if err != nil || !found {
		t.Fatalf("Get() = (%#v, %v, %v)", value, found, err)
	}
	if store.limit != ActiveIncidentPreviewLimit {
		t.Fatalf("Store limit = %d, want %d", store.limit, ActiveIncidentPreviewLimit)
	}
	if len(value.ActiveIncidents) != 2 || value.ActiveIncidentsTruncated {
		t.Fatalf("active Incident preview = %#v, truncated %v", value.ActiveIncidents, value.ActiveIncidentsTruncated)
	}
}

func TestServiceRejectsInconsistentSummary(t *testing.T) {
	value := validSummary()
	value.Health.NotEvaluated++
	service := NewService(&fakeStore{value: value, found: true})
	if _, _, err := service.Get(t.Context(), value.OrganizationID); err == nil {
		t.Fatal("Get() error = nil, want inconsistent health counts")
	}
}

func TestServicePreservesStoreOutcomes(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		value, found, err := NewService(&fakeStore{}).Get(t.Context(), "organization")
		if err != nil || found || value.OrganizationID != "" || len(value.ActiveIncidents) != 0 {
			t.Fatalf("Get() = (%#v, %v, %v)", value, found, err)
		}
	})
	t.Run("store error", func(t *testing.T) {
		want := errors.New("store failed")
		_, _, err := NewService(&fakeStore{err: want}).Get(t.Context(), "organization")
		if !errors.Is(err, want) {
			t.Fatalf("Get() error = %v, want %v", err, want)
		}
	})
}
