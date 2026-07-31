package webhook

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestSigningSecretRotationRequiresPrepareActivateRetireOrder(t *testing.T) {
	store := &memoryStore{}
	service := newTestService(t, store)
	created, err := service.Create(t.Context(), CreateCommand{
		OrganizationID: "organization",
		Name:           "Receiver",
		DestinationURL: "https://hooks.example.test/events",
	})
	if err != nil || created.Kind != CreateCreated {
		t.Fatalf("Create() = %#v, %v", created, err)
	}

	prepared, err := service.PrepareRotation(t.Context(), RotationCommand{
		OrganizationID: "organization", IntegrationID: created.Integration.ID, ExpectedVersion: 1,
	})
	if err != nil || prepared.Kind != RotationPrepared ||
		prepared.Integration.Version != 2 ||
		prepared.Integration.ActiveSecretVersion != 1 ||
		prepared.SecretVersion != 2 ||
		prepared.SigningSecret == created.SigningSecret {
		t.Fatalf("PrepareRotation() = %#v, %v", prepared, err)
	}
	if len(store.secrets) != 2 || store.secrets[1].State != "pending" {
		t.Fatalf("prepared secrets = %#v", store.secrets)
	}
	plaintext, err := service.keyring.Open(
		store.secrets[1].Envelope,
		secretAssociatedData("organization", created.Integration.ID, 2),
	)
	if err != nil || string(plaintext) != prepared.SigningSecret {
		t.Fatalf("prepared secret plaintext/error = %q/%v", plaintext, err)
	}

	inProgress, err := service.PrepareRotation(t.Context(), RotationCommand{
		OrganizationID: "organization", IntegrationID: created.Integration.ID, ExpectedVersion: 2,
	})
	if err != nil || inProgress.Kind != RotationConflict ||
		inProgress.Code != RotationInProgressCode || inProgress.SigningSecret != "" {
		t.Fatalf("second PrepareRotation() = %#v, %v", inProgress, err)
	}

	activated, err := service.ActivateRotation(t.Context(), RotationCommand{
		OrganizationID: "organization", IntegrationID: created.Integration.ID, ExpectedVersion: 2,
	})
	if err != nil || activated.Kind != RotationUpdated ||
		activated.Integration.Version != 3 ||
		activated.Integration.ActiveSecretVersion != 2 ||
		store.secrets[0].State != "retiring" || store.secrets[1].State != "active" {
		t.Fatalf("ActivateRotation() = %#v, %v, secrets %#v", activated, err, store.secrets)
	}

	inProgress, err = service.PrepareRotation(t.Context(), RotationCommand{
		OrganizationID: "organization", IntegrationID: created.Integration.ID, ExpectedVersion: 3,
	})
	if err != nil || inProgress.Code != RotationInProgressCode {
		t.Fatalf("PrepareRotation() before retirement = %#v, %v", inProgress, err)
	}

	retired, err := service.RetireRotation(t.Context(), RotationCommand{
		OrganizationID: "organization", IntegrationID: created.Integration.ID, ExpectedVersion: 3,
	})
	if err != nil || retired.Kind != RotationUpdated ||
		retired.Integration.Version != 4 ||
		retired.Integration.ActiveSecretVersion != 2 ||
		store.secrets[0].State != "retired" ||
		len(store.secrets[0].Envelope.Nonce) != 0 ||
		len(store.secrets[0].Envelope.Ciphertext) != 0 {
		t.Fatalf("RetireRotation() = %#v, %v, secrets %#v", retired, err, store.secrets)
	}

	next, err := service.PrepareRotation(t.Context(), RotationCommand{
		OrganizationID: "organization", IntegrationID: created.Integration.ID, ExpectedVersion: 4,
	})
	if err != nil || next.Kind != RotationPrepared || next.SecretVersion != 3 ||
		next.Integration.Version != 5 || next.Integration.ActiveSecretVersion != 2 {
		t.Fatalf("next PrepareRotation() = %#v, %v", next, err)
	}
}

func TestSigningSecretRotationReportsValidationScopeAndStateConflicts(t *testing.T) {
	store := &memoryStore{}
	service := newTestService(t, store)
	created, err := service.Create(t.Context(), CreateCommand{
		OrganizationID: "organization",
		Name:           "Receiver",
		DestinationURL: "https://hooks.example.test/events",
	})
	if err != nil {
		t.Fatal(err)
	}
	base := RotationCommand{
		OrganizationID: "organization", IntegrationID: created.Integration.ID, ExpectedVersion: 1,
	}

	invalid := base
	invalid.ExpectedVersion = 0
	if result, err := service.PrepareRotation(t.Context(), invalid); err != nil ||
		result.Kind != RotationInvalid || result.Failures[0].Code != VersionInvalidCode {
		t.Fatalf("invalid PrepareRotation() = %#v, %v", result, err)
	}
	stale := base
	stale.ExpectedVersion = 2
	if result, err := service.PrepareRotation(t.Context(), stale); err != nil ||
		result.Code != ConcurrentUpdateCode {
		t.Fatalf("stale PrepareRotation() = %#v, %v", result, err)
	}
	missing := base
	missing.IntegrationID = "missing"
	if result, err := service.PrepareRotation(t.Context(), missing); err != nil ||
		result.Kind != RotationNotFound {
		t.Fatalf("missing PrepareRotation() = %#v, %v", result, err)
	}
	if result, err := service.ActivateRotation(t.Context(), base); err != nil ||
		result.Code != PendingSecretMissingCode {
		t.Fatalf("unprepared ActivateRotation() = %#v, %v", result, err)
	}
	if result, err := service.RetireRotation(t.Context(), base); err != nil ||
		result.Code != RetiringSecretMissingCode {
		t.Fatalf("unactivated RetireRotation() = %#v, %v", result, err)
	}
}

func (store *memoryStore) Find(
	_ context.Context, organizationID, integrationID string,
) (Integration, bool, error) {
	for _, value := range store.integrations {
		if value.OrganizationID == organizationID && value.ID == integrationID {
			return value, true, nil
		}
	}
	return Integration{}, false, nil
}

func (store *memoryStore) PrepareSecret(
	_ context.Context, updated Integration, secret StoredSecret, expectedVersion int64,
) error {
	for index, value := range store.integrations {
		if value.OrganizationID != updated.OrganizationID || value.ID != updated.ID {
			continue
		}
		if value.Version != expectedVersion {
			return ErrConcurrentUpdate
		}
		for _, existing := range store.secrets {
			if existing.OrganizationID == updated.OrganizationID &&
				existing.IntegrationID == updated.ID &&
				(existing.State == "pending" || existing.State == "retiring") {
				return ErrRotationInProgress
			}
		}
		store.integrations[index] = updated
		store.secrets = append(store.secrets, secret)
		return nil
	}
	return ErrIntegrationNotFound
}

func (store *memoryStore) ActivateSecret(
	_ context.Context,
	organizationID, integrationID string,
	expectedVersion int64,
	now time.Time,
) (Integration, error) {
	index, value, found := store.findIntegration(organizationID, integrationID)
	if !found {
		return Integration{}, ErrIntegrationNotFound
	}
	if value.Version != expectedVersion {
		return Integration{}, ErrConcurrentUpdate
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
		return Integration{}, ErrPendingSecretMissing
	}
	if activeIndex < 0 {
		return Integration{}, ErrSecretChanged
	}
	store.secrets[activeIndex].State = "retiring"
	store.secrets[pendingIndex].State = "active"
	value.ActiveSecretVersion = store.secrets[pendingIndex].Version
	value.Version++
	value.UpdatedAt = now
	store.integrations[index] = value
	return value, nil
}

func (store *memoryStore) RetireSecret(
	_ context.Context,
	organizationID, integrationID string,
	expectedVersion int64,
	now time.Time,
) (Integration, error) {
	index, value, found := store.findIntegration(organizationID, integrationID)
	if !found {
		return Integration{}, ErrIntegrationNotFound
	}
	if value.Version != expectedVersion {
		return Integration{}, ErrConcurrentUpdate
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
		return Integration{}, ErrRetiringSecretMissing
	}
	store.secrets[retiringIndex].State = "retired"
	store.secrets[retiringIndex].Envelope = Envelope{}
	value.Version++
	value.UpdatedAt = now
	store.integrations[index] = value
	return value, nil
}

func (store *memoryStore) findIntegration(
	organizationID, integrationID string,
) (int, Integration, bool) {
	for index, value := range store.integrations {
		if value.OrganizationID == organizationID && value.ID == integrationID {
			return index, value, true
		}
	}
	return 0, Integration{}, false
}

func TestPreparedSecretDoesNotExposePlaintextInMemoryStore(t *testing.T) {
	store := &memoryStore{}
	service := newTestService(t, store)
	created, err := service.Create(t.Context(), CreateCommand{
		OrganizationID: "organization",
		Name:           "Receiver",
		DestinationURL: "https://hooks.example.test/events",
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := service.PrepareRotation(t.Context(), RotationCommand{
		OrganizationID:  "organization",
		IntegrationID:   created.Integration.ID,
		ExpectedVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(store.secrets[1].Envelope.Ciphertext, []byte(prepared.SigningSecret)) {
		t.Fatal("prepared ciphertext contains the one-time signing secret")
	}
}
