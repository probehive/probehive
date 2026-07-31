package httpapi

import (
	"context"
	"time"

	"github.com/probehive/probehive/internal/webhook"
)

func (store *memoryWebhookStore) SetEnabled(
	_ context.Context,
	organizationID, integrationID string,
	expectedVersion int64,
	enabled bool,
	now time.Time,
) (webhook.Integration, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	index, value, found := store.findIntegrationLocked(organizationID, integrationID)
	if !found {
		return webhook.Integration{}, webhook.ErrIntegrationNotFound
	}
	if value.Version != expectedVersion {
		return webhook.Integration{}, webhook.ErrConcurrentUpdate
	}
	if value.Enabled == enabled {
		return value, nil
	}
	if enabled {
		count := 0
		for _, existing := range store.byOrganization[organizationID] {
			if existing.Enabled {
				count++
			}
		}
		if count >= webhook.MaxEnabledIntegrations {
			return webhook.Integration{}, webhook.ErrEnabledLimit
		}
	}
	value.Enabled = enabled
	value.Version++
	value.UpdatedAt = now.UTC()
	store.byOrganization[organizationID][index] = value
	return value, nil
}
