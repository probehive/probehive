package webhook

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

type RotationCommand struct {
	OrganizationID  string
	IntegrationID   string
	ExpectedVersion int64
}

type RotationKind uint8

const (
	RotationPrepared RotationKind = iota + 1
	RotationUpdated
	RotationInvalid
	RotationNotFound
	RotationConflict
	RotationKeyringUnavailable
)

type RotationResult struct {
	Kind          RotationKind
	Integration   Integration
	SecretVersion int64
	SigningSecret string
	Failures      []ValidationFailure
	Code          string
	Detail        string
}

func (service *Service) PrepareRotation(
	ctx context.Context, command RotationCommand,
) (RotationResult, error) {
	if err := validateRotationIdentity(command); err != nil {
		return RotationResult{}, err
	}
	if result, ok := validateRotationVersion(command.ExpectedVersion); !ok {
		return result, nil
	}
	value, found, err := service.store.Find(ctx, command.OrganizationID, command.IntegrationID)
	if err != nil {
		return RotationResult{}, err
	}
	if !found {
		return RotationResult{Kind: RotationNotFound}, nil
	}
	if value.Version != command.ExpectedVersion {
		return rotationConflict(ErrConcurrentUpdate), nil
	}
	if service.keyring == nil {
		return RotationResult{
			Kind: RotationKeyringUnavailable, Code: KeyringUnavailableCode,
			Detail: "The operator has not configured Webhook signing-secret encryption.",
		}, nil
	}
	if value.ActiveSecretVersion == math.MaxInt64 {
		return RotationResult{}, errors.New("Webhook signing-secret version is exhausted")
	}

	now := service.clock.Now().UTC()
	secretVersion := value.ActiveSecretVersion + 1
	stored, signingSecret, err := service.newStoredSecret(value, secretVersion, "pending", now)
	if err != nil {
		return RotationResult{}, err
	}
	updated := value
	updated.Version++
	updated.UpdatedAt = now
	if err := service.store.PrepareSecret(ctx, updated, stored, command.ExpectedVersion); err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return RotationResult{Kind: RotationNotFound}, nil
		}
		if errors.Is(err, ErrConcurrentUpdate) || errors.Is(err, ErrRotationInProgress) {
			return rotationConflict(err), nil
		}
		return RotationResult{}, err
	}
	return RotationResult{
		Kind: RotationPrepared, Integration: updated,
		SecretVersion: secretVersion, SigningSecret: signingSecret,
	}, nil
}

func (service *Service) ActivateRotation(
	ctx context.Context, command RotationCommand,
) (RotationResult, error) {
	if err := validateRotationIdentity(command); err != nil {
		return RotationResult{}, err
	}
	if result, ok := validateRotationVersion(command.ExpectedVersion); !ok {
		return result, nil
	}
	value, err := service.store.ActivateSecret(
		ctx, command.OrganizationID, command.IntegrationID,
		command.ExpectedVersion, service.clock.Now().UTC(),
	)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return RotationResult{Kind: RotationNotFound}, nil
		}
		if errors.Is(err, ErrConcurrentUpdate) || errors.Is(err, ErrPendingSecretMissing) {
			return rotationConflict(err), nil
		}
		return RotationResult{}, err
	}
	return RotationResult{Kind: RotationUpdated, Integration: value}, nil
}

func (service *Service) RetireRotation(
	ctx context.Context, command RotationCommand,
) (RotationResult, error) {
	if err := validateRotationIdentity(command); err != nil {
		return RotationResult{}, err
	}
	if result, ok := validateRotationVersion(command.ExpectedVersion); !ok {
		return result, nil
	}
	value, err := service.store.RetireSecret(
		ctx, command.OrganizationID, command.IntegrationID,
		command.ExpectedVersion, service.clock.Now().UTC(),
	)
	if err != nil {
		if errors.Is(err, ErrIntegrationNotFound) {
			return RotationResult{Kind: RotationNotFound}, nil
		}
		if errors.Is(err, ErrConcurrentUpdate) || errors.Is(err, ErrRetiringSecretMissing) {
			return rotationConflict(err), nil
		}
		return RotationResult{}, err
	}
	return RotationResult{Kind: RotationUpdated, Integration: value}, nil
}

func validateRotationIdentity(command RotationCommand) error {
	if command.OrganizationID == "" || command.IntegrationID == "" {
		return errors.New("Webhook signing-secret rotation requires Organization and Integration identity")
	}
	return nil
}

func validateRotationVersion(version int64) (RotationResult, bool) {
	if version < 1 {
		return RotationResult{Kind: RotationInvalid, Failures: []ValidationFailure{{
			Code: VersionInvalidCode, Field: "version", Message: VersionValidationMessage,
		}}}, false
	}
	return RotationResult{}, true
}

func rotationConflict(err error) RotationResult {
	switch {
	case errors.Is(err, ErrConcurrentUpdate):
		return RotationResult{
			Kind: RotationConflict, Code: ConcurrentUpdateCode,
			Detail: "The Webhook Integration changed; reload it before retrying.",
		}
	case errors.Is(err, ErrRotationInProgress):
		return RotationResult{
			Kind: RotationConflict, Code: RotationInProgressCode,
			Detail: "Finish the current signing-secret rotation before preparing another secret.",
		}
	case errors.Is(err, ErrPendingSecretMissing):
		return RotationResult{
			Kind: RotationConflict, Code: PendingSecretMissingCode,
			Detail: "Prepare a new signing secret before activating it.",
		}
	case errors.Is(err, ErrRetiringSecretMissing):
		return RotationResult{
			Kind: RotationConflict, Code: RetiringSecretMissingCode,
			Detail: "No retiring signing secret is available to retire.",
		}
	default:
		panic("unexpected Webhook rotation conflict")
	}
}

func (service *Service) newStoredSecret(
	value Integration, version int64, state string, now time.Time,
) (StoredSecret, string, error) {
	randomSecret := make([]byte, signingSecretBytes)
	if _, err := io.ReadFull(service.random, randomSecret); err != nil {
		return StoredSecret{}, "", fmt.Errorf("generate Webhook signing secret: %w", err)
	}
	signingSecret := "phwh_" + base64.RawURLEncoding.EncodeToString(randomSecret)
	clear(randomSecret)
	associatedData := secretAssociatedData(value.OrganizationID, value.ID, version)
	envelope, err := service.keyring.Seal([]byte(signingSecret), associatedData, service.random)
	if err != nil {
		return StoredSecret{}, "", err
	}
	return StoredSecret{
		OrganizationID: value.OrganizationID, IntegrationID: value.ID,
		Version: version, State: state, Envelope: envelope, CreatedAt: now,
	}, signingSecret, nil
}
