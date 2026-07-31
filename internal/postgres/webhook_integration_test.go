package postgres

import (
	"bytes"
	"testing"
	"time"

	"github.com/probehive/probehive/internal/webhook"
)

func TestWebhookIntegrationPersistsCiphertextAndRewrapsKeys(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, _ := seedTenant(t, database, 1500, "webhook-tenant")
	oldKey := webhook.WrappingKey{ID: "old", Key: bytes.Repeat([]byte{1}, 32)}
	oldRing, err := webhook.NewKeyring([]webhook.WrappingKey{oldKey})
	if err != nil {
		t.Fatal(err)
	}
	now := testTime()
	service := webhook.NewService(
		database.Webhooks(), fixedClock{value: now},
		&sequenceUUIDs{values: []string{testUUID(1501), testUUID(1502)}},
		bytes.NewReader(sequenceBytes(128)), oldRing,
	)
	result, err := service.Create(t.Context(), webhook.CreateCommand{
		OrganizationID: string(organizationValue.ID),
		Name:           "Primary receiver",
		DestinationURL: "https://hooks.example.test/events",
	})
	if err != nil || result.Kind != webhook.CreateCreated {
		t.Fatalf("Create() = %#v, %v", result, err)
	}

	var keyID string
	var nonce, ciphertext []byte
	if err := database.pool.QueryRow(t.Context(), `
SELECT wrapping_key_id, nonce, ciphertext
FROM webhook_signing_secrets
WHERE organization_id=$1 AND integration_id=$2 AND secret_version=1`,
		string(organizationValue.ID), result.Integration.ID,
	).Scan(&keyID, &nonce, &ciphertext); err != nil {
		t.Fatal(err)
	}
	if keyID != "old" || len(nonce) != 12 ||
		bytes.Contains(ciphertext, []byte(result.SigningSecret)) {
		t.Fatalf("stored secret key/nonce/ciphertext = %q/%d/%x", keyID, len(nonce), ciphertext)
	}

	listed, err := service.List(t.Context(), string(organizationValue.ID))
	if err != nil || len(listed) != 1 || listed[0] != result.Integration {
		t.Fatalf("List() = %#v, %v", listed, err)
	}
	conflict, err := service.Create(t.Context(), webhook.CreateCommand{
		OrganizationID: string(organizationValue.ID),
		Name:           "Primary receiver",
		DestinationURL: "https://other.example.test/events",
	})
	if err != nil || conflict.Kind != webhook.CreateConflict {
		t.Fatalf("conflicting Create() = %#v, %v", conflict, err)
	}

	newRing, err := webhook.NewKeyring([]webhook.WrappingKey{
		{ID: "new", Key: bytes.Repeat([]byte{2}, 32)},
		oldKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	rewrapper := webhook.NewService(
		database.Webhooks(), fixedClock{value: now.Add(time.Minute)},
		&sequenceUUIDs{}, bytes.NewReader(sequenceBytes(64)), newRing,
	)
	if err := rewrapper.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := database.pool.QueryRow(t.Context(), `
SELECT wrapping_key_id
FROM webhook_signing_secrets
WHERE organization_id=$1 AND integration_id=$2 AND secret_version=1`,
		string(organizationValue.ID), result.Integration.ID,
	).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if keyID != "new" {
		t.Fatalf("rewrapped key id = %q, want new", keyID)
	}
}

func sequenceBytes(length int) []byte {
	value := make([]byte, length)
	for index := range value {
		value[index] = byte(index)
	}
	return value
}
