package postgres

import (
	"bytes"
	"testing"
	"time"

	"github.com/probehive/probehive/internal/webhook"
)

func TestWebhookSigningSecretRotationPersistsAtomicStateTransitions(t *testing.T) {
	database := newIntegrationDatabase(t, true)
	organizationValue, _ := seedTenant(t, database, 1510, "webhook-rotation-tenant")
	keyring, err := webhook.NewKeyring([]webhook.WrappingKey{{
		ID: "active", Key: bytes.Repeat([]byte{4}, 32),
	}})
	if err != nil {
		t.Fatal(err)
	}
	now := testTime()
	service := webhook.NewService(
		database.Webhooks(), fixedClock{value: now},
		&sequenceUUIDs{values: []string{testUUID(1511)}},
		bytes.NewReader(sequenceBytes(512)), keyring,
	)
	created, err := service.Create(t.Context(), webhook.CreateCommand{
		OrganizationID: string(organizationValue.ID),
		Name:           "Rotating receiver",
		DestinationURL: "https://hooks.example.test/events",
	})
	if err != nil || created.Kind != webhook.CreateCreated {
		t.Fatalf("Create() = %#v, %v", created, err)
	}

	prepared, err := service.PrepareRotation(t.Context(), webhook.RotationCommand{
		OrganizationID:  string(organizationValue.ID),
		IntegrationID:   created.Integration.ID,
		ExpectedVersion: 1,
	})
	if err != nil || prepared.Kind != webhook.RotationPrepared ||
		prepared.Integration.Version != 2 || prepared.SecretVersion != 2 {
		t.Fatalf("PrepareRotation() = %#v, %v", prepared, err)
	}
	assertWebhookSecretRow(
		t, database, string(organizationValue.ID), created.Integration.ID,
		2, "pending", true, time.Time{},
	)

	inProgress, err := service.PrepareRotation(t.Context(), webhook.RotationCommand{
		OrganizationID:  string(organizationValue.ID),
		IntegrationID:   created.Integration.ID,
		ExpectedVersion: 2,
	})
	if err != nil || inProgress.Code != webhook.RotationInProgressCode {
		t.Fatalf("second PrepareRotation() = %#v, %v", inProgress, err)
	}
	stale, err := service.ActivateRotation(t.Context(), webhook.RotationCommand{
		OrganizationID:  string(organizationValue.ID),
		IntegrationID:   created.Integration.ID,
		ExpectedVersion: 1,
	})
	if err != nil || stale.Code != webhook.ConcurrentUpdateCode {
		t.Fatalf("stale ActivateRotation() = %#v, %v", stale, err)
	}

	activated, err := service.ActivateRotation(t.Context(), webhook.RotationCommand{
		OrganizationID:  string(organizationValue.ID),
		IntegrationID:   created.Integration.ID,
		ExpectedVersion: 2,
	})
	if err != nil || activated.Kind != webhook.RotationUpdated ||
		activated.Integration.Version != 3 ||
		activated.Integration.ActiveSecretVersion != 2 {
		t.Fatalf("ActivateRotation() = %#v, %v", activated, err)
	}
	assertWebhookSecretRow(
		t, database, string(organizationValue.ID), created.Integration.ID,
		1, "retiring", true, time.Time{},
	)
	assertWebhookSecretRow(
		t, database, string(organizationValue.ID), created.Integration.ID,
		2, "active", true, now,
	)

	inProgress, err = service.PrepareRotation(t.Context(), webhook.RotationCommand{
		OrganizationID:  string(organizationValue.ID),
		IntegrationID:   created.Integration.ID,
		ExpectedVersion: 3,
	})
	if err != nil || inProgress.Code != webhook.RotationInProgressCode {
		t.Fatalf("PrepareRotation() with retiring secret = %#v, %v", inProgress, err)
	}
	retired, err := service.RetireRotation(t.Context(), webhook.RotationCommand{
		OrganizationID:  string(organizationValue.ID),
		IntegrationID:   created.Integration.ID,
		ExpectedVersion: 3,
	})
	if err != nil || retired.Kind != webhook.RotationUpdated ||
		retired.Integration.Version != 4 ||
		retired.Integration.ActiveSecretVersion != 2 {
		t.Fatalf("RetireRotation() = %#v, %v", retired, err)
	}
	assertWebhookSecretRow(
		t, database, string(organizationValue.ID), created.Integration.ID,
		1, "retired", false, now,
	)
}

func assertWebhookSecretRow(
	t *testing.T,
	database *DB,
	organizationID, integrationID string,
	version int64,
	wantState string,
	wantMaterial bool,
	wantTransitionAt time.Time,
) {
	t.Helper()
	var (
		state                  string
		keyID                  *string
		nonce, ciphertext      []byte
		activatedAt, retiredAt *time.Time
	)
	if err := database.pool.QueryRow(t.Context(), `
SELECT state, wrapping_key_id, nonce, ciphertext, activated_at, retired_at
FROM webhook_signing_secrets
WHERE organization_id=$1 AND integration_id=$2 AND secret_version=$3`,
		organizationID, integrationID, version,
	).Scan(&state, &keyID, &nonce, &ciphertext, &activatedAt, &retiredAt); err != nil {
		t.Fatal(err)
	}
	if state != wantState {
		t.Fatalf("secret %d state = %q, want %q", version, state, wantState)
	}
	hasMaterial := keyID != nil && len(nonce) == 12 && len(ciphertext) >= 16
	if hasMaterial != wantMaterial {
		t.Fatalf(
			"secret %d material = key %v, nonce %d, ciphertext %d",
			version, keyID, len(nonce), len(ciphertext),
		)
	}
	var transitionAt *time.Time
	switch wantState {
	case "active":
		transitionAt = activatedAt
	case "retired":
		transitionAt = retiredAt
	}
	if !wantTransitionAt.IsZero() &&
		(transitionAt == nil || !transitionAt.UTC().Equal(wantTransitionAt.UTC())) {
		t.Fatalf("secret %d transition time = %v, want %v", version, transitionAt, wantTransitionAt)
	}
}
