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

func TestWebhookEnabledLimitSerializesConcurrentChanges(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, _ := seedTenant(t, database, 1510, "webhook-limit-tenant")
	now := testTime()
	keyring, err := webhook.NewKeyring([]webhook.WrappingKey{{
		ID: "test", Key: bytes.Repeat([]byte{9}, 32),
	}})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 6)
	for index := range ids {
		ids[index] = testUUID(1511 + index)
	}
	service := webhook.NewService(
		database.Webhooks(), fixedClock{value: now},
		&sequenceUUIDs{values: ids}, bytes.NewReader(sequenceBytes(512)), keyring,
	)
	names := []string{"Receiver A", "Receiver B", "Receiver C", "Receiver D", "Receiver E", "Receiver F"}
	integrations := make([]webhook.Integration, 0, len(names))
	for _, name := range names {
		result, err := service.Create(t.Context(), webhook.CreateCommand{
			OrganizationID: string(organizationValue.ID),
			Name:           name, DestinationURL: "https://hooks.example.test/events",
		})
		if err != nil || result.Kind != webhook.CreateCreated {
			t.Fatalf("Create(%q) = %#v, %v", name, result, err)
		}
		integrations = append(integrations, result.Integration)
	}
	enabled := true
	for index := 0; index < 4; index++ {
		result, err := service.SetEnabled(t.Context(), webhook.StateCommand{
			OrganizationID:  string(organizationValue.ID),
			IntegrationID:   integrations[index].ID,
			ExpectedVersion: 1, Enabled: &enabled,
		})
		if err != nil || result.Kind != webhook.StateUpdated {
			t.Fatalf("SetEnabled(%d) = %#v, %v", index, result, err)
		}
	}

	start := make(chan struct{})
	results := make(chan webhook.StateResult, 2)
	errorsByChange := make(chan error, 2)
	for index := 4; index < 6; index++ {
		integrationID := integrations[index].ID
		go func() {
			<-start
			result, err := service.SetEnabled(t.Context(), webhook.StateCommand{
				OrganizationID:  string(organizationValue.ID),
				IntegrationID:   integrationID,
				ExpectedVersion: 1, Enabled: &enabled,
			})
			results <- result
			errorsByChange <- err
		}()
	}
	close(start)

	updated, limited := 0, 0
	for range 2 {
		result, err := <-results, <-errorsByChange
		if err != nil {
			t.Fatalf("concurrent SetEnabled() error = %v", err)
		}
		switch {
		case result.Kind == webhook.StateUpdated:
			updated++
		case result.Kind == webhook.StateConflict && result.Code == webhook.EnabledLimitCode:
			limited++
		default:
			t.Fatalf("concurrent SetEnabled() = %#v", result)
		}
	}
	if updated != 1 || limited != 1 {
		t.Fatalf("concurrent results updated/limited = %d/%d", updated, limited)
	}
	var enabledCount int
	if err := database.pool.QueryRow(t.Context(), `
SELECT count(*) FROM webhook_integrations
WHERE organization_id=$1 AND enabled`, string(organizationValue.ID)).Scan(&enabledCount); err != nil {
		t.Fatal(err)
	}
	if enabledCount != webhook.MaxEnabledIntegrations {
		t.Fatalf("enabled Integration count = %d", enabledCount)
	}
}

func sequenceBytes(length int) []byte {
	value := make([]byte, length)
	for index := range value {
		value[index] = byte(index)
	}
	return value
}
