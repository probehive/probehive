package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/probehive/probehive/internal/webhook"
)

func (store *WebhookStore) SetEnabled(
	ctx context.Context,
	organizationID, integrationID string,
	expectedVersion int64,
	enabled bool,
	now time.Time,
) (webhook.Integration, error) {
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return webhook.Integration{}, fmt.Errorf("begin Webhook Integration state change: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var lockedOrganizationID string
	if err := transaction.QueryRow(ctx, `
SELECT id
FROM organizations
WHERE id=$1
FOR UPDATE`, organizationID).Scan(&lockedOrganizationID); errors.Is(err, pgx.ErrNoRows) {
		return webhook.Integration{}, webhook.ErrIntegrationNotFound
	} else if err != nil {
		return webhook.Integration{}, fmt.Errorf("lock Webhook Integration Organization: %w", err)
	}

	current, err := findWebhookIntegrationForUpdate(
		ctx, transaction, organizationID, integrationID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return webhook.Integration{}, webhook.ErrIntegrationNotFound
	}
	if err != nil {
		return webhook.Integration{}, err
	}
	if current.Version != expectedVersion {
		return webhook.Integration{}, webhook.ErrConcurrentUpdate
	}
	if current.Enabled == enabled {
		return current, nil
	}

	if enabled {
		var count int
		if err := transaction.QueryRow(ctx, `
SELECT count(*)
FROM webhook_integrations
WHERE organization_id=$1 AND enabled`, organizationID).Scan(&count); err != nil {
			return webhook.Integration{}, fmt.Errorf("count enabled Webhook Integrations: %w", err)
		}
		if count >= webhook.MaxEnabledIntegrations {
			return webhook.Integration{}, webhook.ErrEnabledLimit
		}
	}

	updated := current
	updated.Enabled = enabled
	updated.Version++
	updated.UpdatedAt = now.UTC()
	tag, err := transaction.Exec(ctx, `
UPDATE webhook_integrations
SET enabled=$1, version=$2, updated_at=$3
WHERE organization_id=$4 AND id=$5`,
		updated.Enabled, updated.Version, updated.UpdatedAt,
		updated.OrganizationID, updated.ID)
	if err != nil {
		return webhook.Integration{}, fmt.Errorf("change Webhook Integration state: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return webhook.Integration{}, webhook.ErrIntegrationNotFound
	}
	if err := transaction.Commit(ctx); err != nil {
		return webhook.Integration{}, fmt.Errorf("commit Webhook Integration state change: %w", err)
	}
	return updated, nil
}
