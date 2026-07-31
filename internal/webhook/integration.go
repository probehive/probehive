// Package webhook owns signed Webhook Integration configuration and secret protection.
package webhook

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	MaxNameLength        = 100
	MaxDestinationBytes  = 2048
	signingSecretBytes   = 32
	initialSecretVersion = int64(1)
)

const (
	NameInvalidCode           = "webhook.name.invalid"
	DestinationInvalidCode    = "webhook.destinationUrl.invalid"
	NameConflictCode          = "webhook.name.conflict"
	KeyringUnavailableCode    = "webhook.keyring.unavailable"
	VersionInvalidCode        = "webhook.version.invalid"
	ConcurrentUpdateCode      = "webhook.version.conflict"
	RotationInProgressCode    = "webhook.rotation.inProgress"
	PendingSecretMissingCode  = "webhook.rotation.pendingMissing"
	RetiringSecretMissingCode = "webhook.rotation.retiringMissing"
)

const (
	NameValidationMessage        = "A Webhook Integration name is 1 to 100 characters after trimming."
	DestinationValidationMessage = "A Webhook destination must be an absolute HTTPS URL without user information, a query string, or a fragment."
	VersionValidationMessage     = "The Webhook Integration version must be a positive integer."
)

var (
	ErrNameConflict          = errors.New("Webhook Integration name already exists")
	ErrIntegrationNotFound   = errors.New("Webhook Integration was not found")
	ErrConcurrentUpdate      = errors.New("Webhook Integration changed concurrently")
	ErrRotationInProgress    = errors.New("Webhook signing-secret rotation is already in progress")
	ErrPendingSecretMissing  = errors.New("Webhook pending signing secret was not found")
	ErrRetiringSecretMissing = errors.New("Webhook retiring signing secret was not found")
	ErrSecretChanged         = errors.New("Webhook signing secret changed concurrently")
)

type Integration struct {
	ID                  string
	OrganizationID      string
	Name                string
	DestinationURL      string
	Enabled             bool
	Version             int64
	ActiveSecretVersion int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type StoredSecret struct {
	OrganizationID string
	IntegrationID  string
	Version        int64
	State          string
	Envelope       Envelope
	CreatedAt      time.Time
}

type Store interface {
	Create(context.Context, Integration, StoredSecret) error
	List(context.Context, string) ([]Integration, error)
	Find(context.Context, string, string) (Integration, bool, error)
	PrepareSecret(context.Context, Integration, StoredSecret, int64) error
	ActivateSecret(context.Context, string, string, int64, time.Time) (Integration, error)
	RetireSecret(context.Context, string, string, int64, time.Time) (Integration, error)
	ListRetainedSecrets(context.Context) ([]StoredSecret, error)
	ReplaceEnvelope(context.Context, StoredSecret, Envelope) error
}

type Clock interface{ Now() time.Time }

type IDGenerator interface {
	NewUUIDv7(time.Time) (string, error)
}

type ValidationFailure struct {
	Code    string
	Field   string
	Message string
}

type CreateCommand struct {
	OrganizationID string
	Name           string
	DestinationURL string
}

type CreateKind uint8

const (
	CreateCreated CreateKind = iota + 1
	CreateInvalid
	CreateConflict
	CreateKeyringUnavailable
)

type CreateResult struct {
	Kind          CreateKind
	Integration   Integration
	SigningSecret string
	Failures      []ValidationFailure
	Code          string
	Detail        string
}

type Service struct {
	store   Store
	clock   Clock
	uuids   IDGenerator
	random  io.Reader
	keyring *Keyring
}

func NewService(store Store, clock Clock, uuids IDGenerator, random io.Reader, keyring *Keyring) *Service {
	if store == nil || clock == nil || uuids == nil || random == nil {
		panic("webhook.Service requires a store, clock, UUID generator, and random source")
	}
	return &Service{store: store, clock: clock, uuids: uuids, random: random, keyring: keyring}
}

// Initialize validates every retained signing secret and rewraps old-key ciphertext before
// the configuration API becomes available.
func (service *Service) Initialize(ctx context.Context) error {
	secrets, err := service.store.ListRetainedSecrets(ctx)
	if err != nil {
		return err
	}
	if len(secrets) != 0 && service.keyring == nil {
		return ErrKeyringUnavailable
	}
	for _, secret := range secrets {
		associatedData := secretAssociatedData(secret.OrganizationID, secret.IntegrationID, secret.Version)
		plaintext, err := service.keyring.Open(secret.Envelope, associatedData)
		if err != nil {
			return fmt.Errorf("open Webhook secret %s/%d: %w", secret.IntegrationID, secret.Version, err)
		}
		if secret.Envelope.KeyID == service.keyring.ActiveKeyID() {
			clear(plaintext)
			continue
		}
		replacement, err := service.keyring.Seal(plaintext, associatedData, service.random)
		clear(plaintext)
		if err != nil {
			return err
		}
		if err := service.store.ReplaceEnvelope(ctx, secret, replacement); err != nil &&
			!errors.Is(err, ErrSecretChanged) {
			return err
		}
	}
	return nil
}

func (service *Service) Create(ctx context.Context, command CreateCommand) (CreateResult, error) {
	var failures []ValidationFailure
	name, validName := NormalizeName(command.Name)
	if !validName {
		failures = append(failures, ValidationFailure{
			Code: NameInvalidCode, Field: "name", Message: NameValidationMessage,
		})
	}
	destination, validDestination := NormalizeDestinationURL(command.DestinationURL)
	if !validDestination {
		failures = append(failures, ValidationFailure{
			Code: DestinationInvalidCode, Field: "destinationUrl", Message: DestinationValidationMessage,
		})
	}
	if len(failures) != 0 {
		return CreateResult{Kind: CreateInvalid, Failures: failures}, nil
	}
	if service.keyring == nil {
		return CreateResult{
			Kind: CreateKeyringUnavailable, Code: KeyringUnavailableCode,
			Detail: "The operator has not configured Webhook signing-secret encryption.",
		}, nil
	}

	now := service.clock.Now().UTC()
	id, err := service.uuids.NewUUIDv7(now)
	if err != nil {
		return CreateResult{}, fmt.Errorf("generate Webhook Integration id: %w", err)
	}
	value, err := NewIntegration(
		id, command.OrganizationID, name, destination, false, 1, initialSecretVersion, now, now,
	)
	if err != nil {
		return CreateResult{}, err
	}

	stored, signingSecret, err := service.newStoredSecret(value, initialSecretVersion, "active", now)
	if err != nil {
		return CreateResult{}, err
	}
	if err := service.store.Create(ctx, value, stored); err != nil {
		if errors.Is(err, ErrNameConflict) {
			return CreateResult{
				Kind: CreateConflict, Code: NameConflictCode,
				Detail: "A Webhook Integration with that name already exists in this Organization.",
			}, nil
		}
		return CreateResult{}, err
	}
	return CreateResult{Kind: CreateCreated, Integration: value, SigningSecret: signingSecret}, nil
}

func (service *Service) List(ctx context.Context, organizationID string) ([]Integration, error) {
	values, err := service.store.List(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if values == nil {
		values = []Integration{}
	}
	return values, nil
}

func NewIntegration(
	id, organizationID, name, destinationURL string,
	enabled bool, version, activeSecretVersion int64,
	createdAt, updatedAt time.Time,
) (Integration, error) {
	if id == "" || organizationID == "" {
		return Integration{}, errors.New("a Webhook Integration requires identity")
	}
	if normalized, ok := NormalizeName(name); !ok || normalized != name {
		return Integration{}, errors.New("invalid Webhook Integration name")
	}
	if normalized, ok := NormalizeDestinationURL(destinationURL); !ok || normalized != destinationURL {
		return Integration{}, errors.New("invalid Webhook destination URL")
	}
	if version < 1 || activeSecretVersion < 1 {
		return Integration{}, errors.New("invalid Webhook Integration version")
	}
	if !isUTC(createdAt) || !isUTC(updatedAt) || updatedAt.Before(createdAt) {
		return Integration{}, errors.New("invalid Webhook Integration timestamps")
	}
	return Integration{
		ID: id, OrganizationID: organizationID, Name: name, DestinationURL: destinationURL,
		Enabled: enabled, Version: version, ActiveSecretVersion: activeSecretVersion,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

func NormalizeName(candidate string) (string, bool) {
	normalized := strings.TrimSpace(candidate)
	length := 0
	for _, character := range normalized {
		length += utf16.RuneLen(character)
	}
	return normalized, length >= 1 && length <= MaxNameLength
}

func NormalizeDestinationURL(candidate string) (string, bool) {
	normalized := strings.TrimSpace(candidate)
	if len(normalized) < 1 || len(normalized) > MaxDestinationBytes {
		return "", false
	}
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", false
	}
	host := parsed.Hostname()
	if host == "" || strings.Contains(host, "%") || !validDestinationHost(host) {
		return "", false
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil || value == 0 {
			return "", false
		}
	}
	return normalized, true
}

func validDestinationHost(host string) bool {
	trimmed := strings.TrimSuffix(host, ".")
	if address, err := netip.ParseAddr(trimmed); err == nil {
		return address.IsValid() && address.Zone() == ""
	}
	if len(trimmed) < 1 || len(trimmed) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.ToLower(trimmed), ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for index := range label {
			character := label[index]
			if (character >= 'a' && character <= 'z') ||
				(character >= '0' && character <= '9') || character == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func secretAssociatedData(organizationID, integrationID string, version int64) []byte {
	return []byte(organizationID + "\x00" + integrationID + "\x00" + strconv.FormatInt(version, 10))
}

func isUTC(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	_, offset := value.Zone()
	return offset == 0
}
