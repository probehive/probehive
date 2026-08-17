package httpapi

import (
	"context"
	"time"

	"github.com/probehive/probehive/internal/webhook"
)

func (store *memoryWebhookStore) Find(
	_ context.Context, organizationID, integrationID string,
) (webhook.Integration, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, value := range store.byOrganization[organizationID] {
		if value.ID == integrationID {
			return value, true, nil
		}
	}
	return webhook.Integration{}, false, nil
}

func (store *memoryWebhookStore) PrepareSecret(
	_ context.Context,
	updated webhook.Integration,
	secret webhook.StoredSecret,
	expectedVersion int64,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	values := store.byOrganization[updated.OrganizationID]
	for index, value := range values {
		if value.ID != updated.ID {
			continue
		}
		if value.Version != expectedVersion {
			return webhook.ErrConcurrentUpdate
		}
		for _, existing := range store.secrets {
			if existing.OrganizationID == updated.OrganizationID &&
				existing.IntegrationID == updated.ID &&
				(existing.State == "pending" || existing.State == "retiring") {
				return webhook.ErrRotationInProgress
			}
		}
		values[index] = updated
		store.byOrganization[updated.OrganizationID] = values
		store.secrets = append(store.secrets, cloneStoredSecret(secret))
		return nil
	}
	return webhook.ErrIntegrationNotFound
}

func (store *memoryWebhookStore) ActivateSecret(
	_ context.Context,
	organizationID, integrationID string,
	expectedVersion int64,
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
	pendingIndex, activeIndex := -1, -1
	for secretIndex, secret := range store.secrets {
		if secret.OrganizationID != organizationID || secret.IntegrationID != integrationID {
			continue
		}
		switch secret.State {
		case "pending":
			pendingIndex = secretIndex
		case "active":
			activeIndex = secretIndex
		}
	}
	if pendingIndex < 0 {
		return webhook.Integration{}, webhook.ErrPendingSecretMissing
	}
	if activeIndex < 0 {
		return webhook.Integration{}, webhook.ErrSecretChanged
	}
	store.secrets[activeIndex].State = "retiring"
	store.secrets[pendingIndex].State = "active"
	retiringVersion := value.ActiveSecretVersion
	value.ActiveSecretVersion = store.secrets[pendingIndex].Version
	value.PendingSecretVersion = nil
	value.RetiringSecretVersion = &retiringVersion
	value.Version++
	value.UpdatedAt = now
	store.byOrganization[organizationID][index] = value
	return value, nil
}

func (store *memoryWebhookStore) RetireSecret(
	_ context.Context,
	organizationID, integrationID string,
	expectedVersion int64,
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
	retiringIndex := -1
	for secretIndex, secret := range store.secrets {
		if secret.OrganizationID == organizationID &&
			secret.IntegrationID == integrationID && secret.State == "retiring" {
			retiringIndex = secretIndex
			break
		}
	}
	if retiringIndex < 0 {
		return webhook.Integration{}, webhook.ErrRetiringSecretMissing
	}
	store.secrets[retiringIndex].State = "retired"
	store.secrets[retiringIndex].Envelope = webhook.Envelope{}
	value.RetiringSecretVersion = nil
	value.Version++
	value.UpdatedAt = now
	store.byOrganization[organizationID][index] = value
	return value, nil
}

func (store *memoryWebhookStore) findIntegrationLocked(
	organizationID, integrationID string,
) (int, webhook.Integration, bool) {
	for index, value := range store.byOrganization[organizationID] {
		if value.ID == integrationID {
			return index, value, true
		}
	}
	return 0, webhook.Integration{}, false
}
