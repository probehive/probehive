package webhook

import (
	"context"
	"testing"
	"time"
)

func (store *memoryStore) SetEnabled(
	_ context.Context,
	organizationID, integrationID string,
	expectedVersion int64,
	enabled bool,
	now time.Time,
) (Integration, error) {
	for index, value := range store.integrations {
		if value.OrganizationID != organizationID || value.ID != integrationID {
			continue
		}
		if value.Version != expectedVersion {
			return Integration{}, ErrConcurrentUpdate
		}
		if value.Enabled == enabled {
			return value, nil
		}
		if enabled {
			count := 0
			for _, existing := range store.integrations {
				if existing.OrganizationID == organizationID && existing.Enabled {
					count++
				}
			}
			if count >= MaxEnabledIntegrations {
				return Integration{}, ErrEnabledLimit
			}
		}
		value.Enabled = enabled
		value.Version++
		value.UpdatedAt = now.UTC()
		store.integrations[index] = value
		return value, nil
	}
	return Integration{}, ErrIntegrationNotFound
}

func TestSetEnabledUsesOptimisticVersionAndIsDesiredStateIdempotent(t *testing.T) {
	now := testInstant()
	store := &memoryStore{integrations: []Integration{
		testStateIntegration("integration", false, 1, now),
	}}
	service := newTestService(t, store)
	enabled := true

	result, err := service.SetEnabled(t.Context(), StateCommand{
		OrganizationID: "organization", IntegrationID: "integration",
		ExpectedVersion: 1, Enabled: &enabled,
	})
	if err != nil || result.Kind != StateUpdated || !result.Integration.Enabled ||
		result.Integration.Version != 2 {
		t.Fatalf("SetEnabled(true) = %#v, %v", result, err)
	}
	result, err = service.SetEnabled(t.Context(), StateCommand{
		OrganizationID: "organization", IntegrationID: "integration",
		ExpectedVersion: 2, Enabled: &enabled,
	})
	if err != nil || result.Kind != StateUpdated || result.Integration.Version != 2 {
		t.Fatalf("idempotent SetEnabled(true) = %#v, %v", result, err)
	}
	result, err = service.SetEnabled(t.Context(), StateCommand{
		OrganizationID: "organization", IntegrationID: "integration",
		ExpectedVersion: 1, Enabled: &enabled,
	})
	if err != nil || result.Kind != StateConflict || result.Code != ConcurrentUpdateCode {
		t.Fatalf("stale SetEnabled(true) = %#v, %v", result, err)
	}

	result, err = service.SetEnabled(t.Context(), StateCommand{
		OrganizationID: "organization", IntegrationID: "integration",
		ExpectedVersion: 0,
	})
	if err != nil || result.Kind != StateInvalid || len(result.Failures) != 2 {
		t.Fatalf("invalid SetEnabled() = %#v, %v", result, err)
	}
}

func TestSetEnabledEnforcesOrganizationLimit(t *testing.T) {
	now := testInstant()
	values := make([]Integration, 0, MaxEnabledIntegrations+1)
	for index := 0; index < MaxEnabledIntegrations; index++ {
		values = append(values, testStateIntegration(
			string(rune('a'+index)), true, 1, now,
		))
	}
	values = append(values, testStateIntegration("overflow", false, 1, now))
	service := newTestService(t, &memoryStore{integrations: values})
	enabled := true
	result, err := service.SetEnabled(t.Context(), StateCommand{
		OrganizationID: "organization", IntegrationID: "overflow",
		ExpectedVersion: 1, Enabled: &enabled,
	})
	if err != nil || result.Kind != StateConflict || result.Code != EnabledLimitCode {
		t.Fatalf("overflow SetEnabled(true) = %#v, %v", result, err)
	}
}

func testStateIntegration(id string, enabled bool, version int64, now time.Time) Integration {
	value, err := NewIntegration(
		id, "organization", id, "https://hooks.example.test/events",
		enabled, version, 1, nil, nil, now, now,
	)
	if err != nil {
		panic(err)
	}
	return value
}
