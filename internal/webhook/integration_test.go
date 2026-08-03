package webhook

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCreateEncryptsOneTimeSecretAndListsDisabledIntegration(t *testing.T) {
	store := &memoryStore{}
	keyring := mustKeyring(t, []WrappingKey{{ID: "active", Key: bytes.Repeat([]byte{7}, 32)}})
	random := bytes.NewReader(sequence(96))
	service := NewService(store, fixedClock{testInstant()}, fixedIDs{}, random, keyring)

	result, err := service.Create(t.Context(), CreateCommand{
		OrganizationID: "00000000-0000-7000-8000-000000000001",
		Name:           " Primary receiver ",
		DestinationURL: "https://hooks.example.test/events",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != CreateCreated || result.Integration.Enabled {
		t.Fatalf("Create() result = %#v", result)
	}
	if result.Integration.Name != "Primary receiver" ||
		result.Integration.ActiveSecretVersion != 1 || result.Integration.Version != 1 {
		t.Fatalf("created Integration = %#v", result.Integration)
	}
	if !strings.HasPrefix(result.SigningSecret, "phwh_") {
		t.Fatalf("signing secret = %q", result.SigningSecret)
	}
	if len(store.secrets) != 1 {
		t.Fatalf("stored secrets = %d, want 1", len(store.secrets))
	}
	stored := store.secrets[0]
	if stored.Envelope.KeyID != "active" ||
		bytes.Contains(stored.Envelope.Ciphertext, []byte(result.SigningSecret)) {
		t.Fatalf("stored secret envelope leaks plaintext or has wrong key: %#v", stored.Envelope)
	}
	plaintext, err := keyring.Open(
		stored.Envelope,
		secretAssociatedData(stored.OrganizationID, stored.IntegrationID, stored.Version),
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != result.SigningSecret {
		t.Fatalf("decrypted secret = %q, want returned secret", plaintext)
	}

	listed, err := service.List(t.Context(), result.Integration.OrganizationID)
	if err != nil || len(listed) != 1 || listed[0] != result.Integration {
		t.Fatalf("List() = %#v, %v", listed, err)
	}
}

func TestCreateValidatesDestinationConflictAndUnavailableKeyring(t *testing.T) {
	tests := []struct {
		name, destination, field, code string
	}{
		{"", "https://hooks.example.test/events", "name", NameInvalidCode},
		{"receiver", "http://hooks.example.test/events", "destinationUrl", DestinationInvalidCode},
		{"receiver", "https://user@hooks.example.test/events", "destinationUrl", DestinationInvalidCode},
		{"receiver", "https://hooks.example.test/events?token=secret", "destinationUrl", DestinationInvalidCode},
		{"receiver", "https://hooks.example.test/events#fragment", "destinationUrl", DestinationInvalidCode},
		{"receiver", "https://bad_host.example.test/events", "destinationUrl", DestinationInvalidCode},
	}
	for _, test := range tests {
		t.Run(test.field+"/"+test.destination, func(t *testing.T) {
			service := newTestService(t, &memoryStore{})
			result, err := service.Create(t.Context(), CreateCommand{
				OrganizationID: "organization", Name: test.name, DestinationURL: test.destination,
			})
			if err != nil || result.Kind != CreateInvalid || len(result.Failures) != 1 ||
				result.Failures[0].Field != test.field || result.Failures[0].Code != test.code {
				t.Fatalf("Create() = %#v, %v", result, err)
			}
		})
	}

	store := &memoryStore{}
	service := newTestService(t, store)
	command := CreateCommand{
		OrganizationID: "organization", Name: "Receiver",
		DestinationURL: "https://hooks.example.test/events",
	}
	if result, err := service.Create(t.Context(), command); err != nil || result.Kind != CreateCreated {
		t.Fatalf("first Create() = %#v, %v", result, err)
	}
	if result, err := service.Create(t.Context(), command); err != nil ||
		result.Kind != CreateConflict || result.Code != NameConflictCode {
		t.Fatalf("conflicting Create() = %#v, %v", result, err)
	}

	unavailable := NewService(&memoryStore{}, fixedClock{testInstant()}, fixedIDs{}, bytes.NewReader(sequence(64)), nil)
	result, err := unavailable.Create(t.Context(), CreateCommand{
		OrganizationID: "organization", Name: "Receiver",
		DestinationURL: "https://hooks.example.test/events",
	})
	if err != nil || result.Kind != CreateKeyringUnavailable ||
		result.Code != KeyringUnavailableCode {
		t.Fatalf("unavailable Create() = %#v, %v", result, err)
	}
}

func TestInitializeAuthenticatesAndRewrapsRetainedSecrets(t *testing.T) {
	oldKey := WrappingKey{ID: "old", Key: bytes.Repeat([]byte{1}, 32)}
	newKey := WrappingKey{ID: "new", Key: bytes.Repeat([]byte{2}, 32)}
	oldRing := mustKeyring(t, []WrappingKey{oldKey})
	secret := StoredSecret{
		OrganizationID: "organization", IntegrationID: "integration", Version: 1,
		State: "active", CreatedAt: testInstant(),
	}
	associatedData := secretAssociatedData(secret.OrganizationID, secret.IntegrationID, secret.Version)
	envelope, err := oldRing.Seal([]byte("phwh_retained"), associatedData, bytes.NewReader(sequence(32)))
	if err != nil {
		t.Fatal(err)
	}
	secret.Envelope = envelope
	store := &memoryStore{secrets: []StoredSecret{secret}}
	service := NewService(
		store, fixedClock{testInstant()}, fixedIDs{}, bytes.NewReader(sequence(32)),
		mustKeyring(t, []WrappingKey{newKey, oldKey}),
	)
	if err := service.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := store.secrets[0].Envelope.KeyID; got != "new" {
		t.Fatalf("rewrapped key id = %q, want new", got)
	}
	plaintext, err := service.keyring.Open(store.secrets[0].Envelope, associatedData)
	if err != nil || string(plaintext) != "phwh_retained" {
		t.Fatalf("rewrapped plaintext/error = %q/%v", plaintext, err)
	}

	missing := NewService(
		&memoryStore{secrets: []StoredSecret{secret}}, fixedClock{testInstant()},
		fixedIDs{}, bytes.NewReader(sequence(32)), nil,
	)
	if err := missing.Initialize(t.Context()); !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("Initialize() without keyring = %v", err)
	}
}

func TestKeyringParsingAndAuthentication(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	keyring, err := ParseKeyring("current:" + encoded)
	if err != nil || keyring.ActiveKeyID() != "current" {
		t.Fatalf("ParseKeyring() = %#v, %v", keyring, err)
	}
	envelope, err := keyring.Seal([]byte("secret"), []byte("scope"), bytes.NewReader(sequence(32)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Open(envelope, []byte("other-scope")); !errors.Is(err, ErrSecretAuthentication) {
		t.Fatalf("Open() with wrong AAD = %v", err)
	}
	for _, invalid := range []string{"bad", "UPPER:" + encoded, "key:short", "key:" + encoded + ",key:" + encoded} {
		if _, err := ParseKeyring(invalid); err == nil {
			t.Errorf("ParseKeyring(%q) succeeded", invalid)
		}
	}
	if keyring, err := ParseKeyring(""); err != nil || keyring != nil {
		t.Fatalf("empty ParseKeyring() = %#v, %v", keyring, err)
	}
}

type memoryStore struct {
	integrations []Integration
	secrets      []StoredSecret
	retireError  error
}

func (store *memoryStore) Create(_ context.Context, value Integration, secret StoredSecret) error {
	for _, existing := range store.integrations {
		if existing.OrganizationID == value.OrganizationID && existing.Name == value.Name {
			return ErrNameConflict
		}
	}
	store.integrations = append(store.integrations, value)
	store.secrets = append(store.secrets, secret)
	return nil
}

func (store *memoryStore) List(_ context.Context, organizationID string) ([]Integration, error) {
	var values []Integration
	for _, value := range store.integrations {
		if value.OrganizationID == organizationID {
			values = append(values, value)
		}
	}
	return values, nil
}

func (store *memoryStore) ListRetainedSecrets(context.Context) ([]StoredSecret, error) {
	return append([]StoredSecret(nil), store.secrets...), nil
}

func (store *memoryStore) ReplaceEnvelope(
	_ context.Context, current StoredSecret, replacement Envelope,
) error {
	for index, secret := range store.secrets {
		if secret.OrganizationID == current.OrganizationID &&
			secret.IntegrationID == current.IntegrationID && secret.Version == current.Version &&
			secret.Envelope.KeyID == current.Envelope.KeyID {
			store.secrets[index].Envelope = replacement
			return nil
		}
	}
	return ErrSecretChanged
}

type fixedClock struct{ value time.Time }

func (clock fixedClock) Now() time.Time { return clock.value }

type fixedIDs struct{}

func (fixedIDs) NewUUIDv7(time.Time) (string, error) {
	return "00000000-0000-7000-8000-000000000001", nil
}

func newTestService(t *testing.T, store Store) *Service {
	t.Helper()
	return NewService(
		store, fixedClock{testInstant()}, fixedIDs{}, bytes.NewReader(sequence(256)),
		mustKeyring(t, []WrappingKey{{ID: "test", Key: bytes.Repeat([]byte{9}, 32)}}),
	)
}

func mustKeyring(t *testing.T, keys []WrappingKey) *Keyring {
	t.Helper()
	keyring, err := NewKeyring(keys)
	if err != nil {
		t.Fatal(err)
	}
	return keyring
}

func sequence(length int) []byte {
	value := make([]byte, length)
	for index := range value {
		value[index] = byte(index)
	}
	return value
}

func testInstant() time.Time {
	return time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
}
