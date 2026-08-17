package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/probehive/probehive/internal/webhook"
)

func (store *WebhookStore) Find(
	ctx context.Context, organizationID, integrationID string,
) (webhook.Integration, bool, error) {
	value, err := scanWebhookIntegration(store.pool.QueryRow(ctx, webhookIntegrationSelect+`
WHERE integration.organization_id=$1 AND integration.id=$2`, organizationID, integrationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return webhook.Integration{}, false, nil
	}
	if err != nil {
		return webhook.Integration{}, false, err
	}
	return value, true, nil
}

func (store *WebhookStore) PrepareSecret(
	ctx context.Context,
	updated webhook.Integration,
	secret webhook.StoredSecret,
	expectedVersion int64,
) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Webhook signing-secret preparation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	current, err := findWebhookIntegrationForUpdate(
		ctx, transaction, updated.OrganizationID, updated.ID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return webhook.ErrIntegrationNotFound
	}
	if err != nil {
		return err
	}
	if current.Version != expectedVersion {
		return webhook.ErrConcurrentUpdate
	}
	if updated.Version != current.Version+1 ||
		updated.ActiveSecretVersion != current.ActiveSecretVersion ||
		secret.OrganizationID != current.OrganizationID ||
		secret.IntegrationID != current.ID ||
		secret.Version != current.ActiveSecretVersion+1 ||
		secret.State != "pending" {
		return errors.New("invalid prepared Webhook signing-secret snapshot")
	}

	var rotationInProgress bool
	if err := transaction.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM webhook_signing_secrets
    WHERE organization_id=$1 AND integration_id=$2
      AND state IN ('pending', 'retiring')
)`, current.OrganizationID, current.ID).Scan(&rotationInProgress); err != nil {
		return fmt.Errorf("inspect Webhook signing-secret rotation: %w", err)
	}
	if rotationInProgress {
		return webhook.ErrRotationInProgress
	}

	if _, err := transaction.Exec(ctx, `
INSERT INTO webhook_signing_secrets (
    organization_id, integration_id, secret_version, state,
    wrapping_key_id, nonce, ciphertext, created_at
) VALUES ($1,$2,$3,'pending',$4,$5,$6,$7)`,
		secret.OrganizationID, secret.IntegrationID, secret.Version,
		secret.Envelope.KeyID, secret.Envelope.Nonce, secret.Envelope.Ciphertext,
		secret.CreatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("insert pending Webhook signing secret: %w", err)
	}
	if err := updateWebhookIntegrationVersion(ctx, transaction, updated); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Webhook signing-secret preparation: %w", err)
	}
	return nil
}

func (store *WebhookStore) ActivateSecret(
	ctx context.Context,
	organizationID, integrationID string,
	expectedVersion int64,
	now time.Time,
) (webhook.Integration, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return webhook.Integration{}, fmt.Errorf("begin Webhook signing-secret activation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	current, err := findWebhookIntegrationForUpdate(ctx, transaction, organizationID, integrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return webhook.Integration{}, webhook.ErrIntegrationNotFound
	}
	if err != nil {
		return webhook.Integration{}, err
	}
	if current.Version != expectedVersion {
		return webhook.Integration{}, webhook.ErrConcurrentUpdate
	}

	var pendingVersion int64
	if err := transaction.QueryRow(ctx, `
SELECT secret_version
FROM webhook_signing_secrets
WHERE organization_id=$1 AND integration_id=$2 AND state='pending'
FOR UPDATE`, organizationID, integrationID).Scan(&pendingVersion); errors.Is(err, pgx.ErrNoRows) {
		return webhook.Integration{}, webhook.ErrPendingSecretMissing
	} else if err != nil {
		return webhook.Integration{}, fmt.Errorf("lock pending Webhook signing secret: %w", err)
	}

	tag, err := transaction.Exec(ctx, `
UPDATE webhook_signing_secrets
SET state='retiring'
WHERE organization_id=$1 AND integration_id=$2
  AND secret_version=$3 AND state='active'`,
		organizationID, integrationID, current.ActiveSecretVersion)
	if err != nil {
		return webhook.Integration{}, fmt.Errorf("mark active Webhook signing secret retiring: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return webhook.Integration{}, errors.New("active Webhook signing secret is missing")
	}
	tag, err = transaction.Exec(ctx, `
UPDATE webhook_signing_secrets
SET state='active', activated_at=$1
WHERE organization_id=$2 AND integration_id=$3
  AND secret_version=$4 AND state='pending'`,
		now.UTC(), organizationID, integrationID, pendingVersion)
	if err != nil {
		return webhook.Integration{}, fmt.Errorf("activate pending Webhook signing secret: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return webhook.Integration{}, webhook.ErrPendingSecretMissing
	}

	updated := current
	retiringVersion := current.ActiveSecretVersion
	updated.ActiveSecretVersion = pendingVersion
	updated.PendingSecretVersion = nil
	updated.RetiringSecretVersion = &retiringVersion
	updated.Version++
	updated.UpdatedAt = now.UTC()
	if err := updateWebhookIntegrationVersion(ctx, transaction, updated); err != nil {
		return webhook.Integration{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return webhook.Integration{}, fmt.Errorf("commit Webhook signing-secret activation: %w", err)
	}
	return updated, nil
}

func (store *WebhookStore) RetireSecret(
	ctx context.Context,
	organizationID, integrationID string,
	expectedVersion int64,
	now time.Time,
) (webhook.Integration, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return webhook.Integration{}, fmt.Errorf("begin Webhook signing-secret retirement: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	current, err := findWebhookIntegrationForUpdate(ctx, transaction, organizationID, integrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return webhook.Integration{}, webhook.ErrIntegrationNotFound
	}
	if err != nil {
		return webhook.Integration{}, err
	}
	if current.Version != expectedVersion {
		return webhook.Integration{}, webhook.ErrConcurrentUpdate
	}

	var retiringVersion int64
	if err := transaction.QueryRow(ctx, `
SELECT secret_version
FROM webhook_signing_secrets
WHERE organization_id=$1 AND integration_id=$2 AND state='retiring'
FOR UPDATE`,
		organizationID, integrationID,
	).Scan(&retiringVersion); errors.Is(err, pgx.ErrNoRows) {
		return webhook.Integration{}, webhook.ErrRetiringSecretMissing
	} else if err != nil {
		return webhook.Integration{}, fmt.Errorf("find retiring Webhook signing secret: %w", err)
	}
	var inUse bool
	if err := transaction.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM webhook_deliveries
    WHERE organization_id=$1
      AND integration_id=$2
      AND secret_version=$3
      AND completed_at IS NULL
)`,
		organizationID, integrationID, retiringVersion,
	).Scan(&inUse); err != nil {
		return webhook.Integration{}, fmt.Errorf("check retiring Webhook signing-secret use: %w", err)
	}
	if inUse {
		return webhook.Integration{}, webhook.ErrRetiringSecretInUse
	}

	tag, err := transaction.Exec(ctx, `
UPDATE webhook_signing_secrets
SET state='retired', wrapping_key_id=NULL, nonce=NULL, ciphertext=NULL, retired_at=$1
WHERE organization_id=$2 AND integration_id=$3 AND secret_version=$4 AND state='retiring'`,
		now.UTC(), organizationID, integrationID, retiringVersion)
	if err != nil {
		return webhook.Integration{}, fmt.Errorf("retire Webhook signing secret: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return webhook.Integration{}, webhook.ErrRetiringSecretMissing
	}

	updated := current
	updated.Version++
	updated.RetiringSecretVersion = nil
	updated.UpdatedAt = now.UTC()
	if err := updateWebhookIntegrationVersion(ctx, transaction, updated); err != nil {
		return webhook.Integration{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return webhook.Integration{}, fmt.Errorf("commit Webhook signing-secret retirement: %w", err)
	}
	return updated, nil
}

func findWebhookIntegrationForUpdate(
	ctx context.Context,
	transaction pgx.Tx,
	organizationID, integrationID string,
) (webhook.Integration, error) {
	return scanWebhookIntegration(transaction.QueryRow(ctx, webhookIntegrationSelect+`
WHERE integration.organization_id=$1 AND integration.id=$2
FOR UPDATE OF integration`, organizationID, integrationID))
}

func updateWebhookIntegrationVersion(
	ctx context.Context,
	transaction pgx.Tx,
	value webhook.Integration,
) error {
	tag, err := transaction.Exec(ctx, `
UPDATE webhook_integrations
SET version=$1, active_secret_version=$2, updated_at=$3
WHERE organization_id=$4 AND id=$5`,
		value.Version, value.ActiveSecretVersion, value.UpdatedAt.UTC(),
		value.OrganizationID, value.ID)
	if err != nil {
		return fmt.Errorf("advance Webhook Integration version: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return webhook.ErrIntegrationNotFound
	}
	return nil
}
