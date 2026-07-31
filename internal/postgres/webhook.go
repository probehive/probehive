package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/probehive/probehive/internal/webhook"
)

var _ webhook.Store = (*WebhookStore)(nil)

const webhookNameUniqueIndex = "ux_webhook_integrations_organization_name"

type WebhookStore struct{ pool *pgxpool.Pool }

func (database *DB) Webhooks() *WebhookStore { return &WebhookStore{pool: database.pool} }

func (store *WebhookStore) Create(
	ctx context.Context, value webhook.Integration, secret webhook.StoredSecret,
) error {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Webhook Integration creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	if _, err := transaction.Exec(ctx, `
INSERT INTO webhook_integrations (
    id, organization_id, name, destination_url, enabled, version,
    active_secret_version, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		value.ID, value.OrganizationID, value.Name, value.DestinationURL, value.Enabled,
		value.Version, value.ActiveSecretVersion, value.CreatedAt.UTC(), value.UpdatedAt.UTC(),
	); err != nil {
		if isConstraintViolation(err, uniqueViolation, webhookNameUniqueIndex) {
			return webhook.ErrNameConflict
		}
		return fmt.Errorf("insert Webhook Integration: %w", err)
	}

	if _, err := transaction.Exec(ctx, `
INSERT INTO webhook_signing_secrets (
    organization_id, integration_id, secret_version, state,
    wrapping_key_id, nonce, ciphertext, created_at, activated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)`,
		secret.OrganizationID, secret.IntegrationID, secret.Version, secret.State,
		secret.Envelope.KeyID, secret.Envelope.Nonce, secret.Envelope.Ciphertext,
		secret.CreatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("insert Webhook signing secret: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		if isConstraintViolation(err, uniqueViolation, webhookNameUniqueIndex) {
			return webhook.ErrNameConflict
		}
		return fmt.Errorf("commit Webhook Integration creation: %w", err)
	}
	return nil
}

func (store *WebhookStore) List(
	ctx context.Context, organizationID string,
) ([]webhook.Integration, error) {
	rows, err := store.pool.Query(ctx, `
SELECT id, organization_id, name, destination_url, enabled, version,
       active_secret_version, created_at, updated_at
FROM webhook_integrations
WHERE organization_id = $1
ORDER BY created_at, id`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list Webhook Integrations: %w", err)
	}
	defer rows.Close()

	values := make([]webhook.Integration, 0)
	for rows.Next() {
		value, err := scanWebhookIntegration(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan Webhook Integrations: %w", err)
	}
	return values, nil
}

func (store *WebhookStore) ListRetainedSecrets(ctx context.Context) ([]webhook.StoredSecret, error) {
	rows, err := store.pool.Query(ctx, `
SELECT organization_id, integration_id, secret_version, state,
       wrapping_key_id, nonce, ciphertext, created_at
FROM webhook_signing_secrets
WHERE state <> 'retired'
ORDER BY organization_id, integration_id, secret_version`)
	if err != nil {
		return nil, fmt.Errorf("list retained Webhook secrets: %w", err)
	}
	defer rows.Close()

	values := make([]webhook.StoredSecret, 0)
	for rows.Next() {
		var value webhook.StoredSecret
		if err := rows.Scan(
			&value.OrganizationID, &value.IntegrationID, &value.Version, &value.State,
			&value.Envelope.KeyID, &value.Envelope.Nonce, &value.Envelope.Ciphertext,
			&value.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan retained Webhook secret: %w", err)
		}
		value.CreatedAt = value.CreatedAt.UTC()
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan retained Webhook secrets: %w", err)
	}
	return values, nil
}

func (store *WebhookStore) ReplaceEnvelope(
	ctx context.Context, current webhook.StoredSecret, replacement webhook.Envelope,
) error {
	tag, err := store.pool.Exec(ctx, `
UPDATE webhook_signing_secrets
SET wrapping_key_id=$1, nonce=$2, ciphertext=$3
WHERE organization_id=$4 AND integration_id=$5 AND secret_version=$6
  AND wrapping_key_id=$7 AND nonce=$8 AND ciphertext=$9
  AND state <> 'retired'`,
		replacement.KeyID, replacement.Nonce, replacement.Ciphertext,
		current.OrganizationID, current.IntegrationID, current.Version,
		current.Envelope.KeyID, current.Envelope.Nonce, current.Envelope.Ciphertext,
	)
	if err != nil {
		return fmt.Errorf("rewrap Webhook signing secret: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return webhook.ErrSecretChanged
	}
	return nil
}

func scanWebhookIntegration(row pgx.Row) (webhook.Integration, error) {
	var (
		id, organizationID, name, destinationURL string
		enabled                                  bool
		version, activeSecretVersion             int64
		createdAt, updatedAt                     time.Time
	)
	if err := row.Scan(
		&id, &organizationID, &name, &destinationURL, &enabled, &version,
		&activeSecretVersion, &createdAt, &updatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return webhook.Integration{}, err
		}
		return webhook.Integration{}, fmt.Errorf("scan Webhook Integration: %w", err)
	}
	value, err := webhook.NewIntegration(
		id, organizationID, name, destinationURL, enabled, version,
		activeSecretVersion, createdAt.UTC(), updatedAt.UTC(),
	)
	if err != nil {
		return webhook.Integration{}, fmt.Errorf("restore Webhook Integration: %w", err)
	}
	return value, nil
}
