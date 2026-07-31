package httpapi

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/probehive/probehive/internal/webhook"
)

type memoryWebhookStore struct {
	mu             sync.Mutex
	byOrganization map[string][]webhook.Integration
	secrets        []webhook.StoredSecret
}

func newMemoryWebhookStore() *memoryWebhookStore {
	return &memoryWebhookStore{byOrganization: make(map[string][]webhook.Integration)}
}

func (store *memoryWebhookStore) Create(
	_ context.Context, value webhook.Integration, secret webhook.StoredSecret,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, existing := range store.byOrganization[value.OrganizationID] {
		if existing.Name == value.Name {
			return webhook.ErrNameConflict
		}
	}
	store.byOrganization[value.OrganizationID] = append(
		store.byOrganization[value.OrganizationID], value,
	)
	store.secrets = append(store.secrets, cloneStoredSecret(secret))
	return nil
}

func (store *memoryWebhookStore) List(
	_ context.Context, organizationID string,
) ([]webhook.Integration, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]webhook.Integration(nil), store.byOrganization[organizationID]...), nil
}

func (store *memoryWebhookStore) ListRetainedSecrets(context.Context) ([]webhook.StoredSecret, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	values := make([]webhook.StoredSecret, len(store.secrets))
	for index, secret := range store.secrets {
		values[index] = cloneStoredSecret(secret)
	}
	return values, nil
}

func (store *memoryWebhookStore) ReplaceEnvelope(
	_ context.Context, current webhook.StoredSecret, replacement webhook.Envelope,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for index, secret := range store.secrets {
		if secret.OrganizationID == current.OrganizationID &&
			secret.IntegrationID == current.IntegrationID &&
			secret.Version == current.Version &&
			secret.Envelope.KeyID == current.Envelope.KeyID &&
			bytes.Equal(secret.Envelope.Nonce, current.Envelope.Nonce) &&
			bytes.Equal(secret.Envelope.Ciphertext, current.Envelope.Ciphertext) {
			store.secrets[index].Envelope = cloneEnvelope(replacement)
			return nil
		}
	}
	return webhook.ErrSecretChanged
}

func (store *memoryWebhookStore) onlySecret(t *testing.T) webhook.StoredSecret {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.secrets) != 1 {
		t.Fatalf("Webhook secret count = %d, want 1", len(store.secrets))
	}
	return cloneStoredSecret(store.secrets[0])
}

func cloneStoredSecret(value webhook.StoredSecret) webhook.StoredSecret {
	value.Envelope = cloneEnvelope(value.Envelope)
	return value
}

func cloneEnvelope(value webhook.Envelope) webhook.Envelope {
	value.Nonce = append([]byte(nil), value.Nonce...)
	value.Ciphertext = append([]byte(nil), value.Ciphertext...)
	return value
}
