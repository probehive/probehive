package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/probehive/probehive/internal/alert"
	"github.com/probehive/probehive/internal/webhook"
)

type webhookRouteTarget struct {
	integrationID      string
	integrationVersion int64
	secretVersion      int64
}

func routeWebhookDeliveries(
	ctx context.Context,
	transaction pgx.Tx,
	value alert.Alert,
	routeIDs alert.IDGenerator,
) error {
	rows, err := transaction.Query(ctx, `
SELECT id, version, active_secret_version
FROM webhook_integrations
WHERE organization_id=$1 AND enabled
ORDER BY id
LIMIT $2
FOR SHARE`, value.OrganizationID, webhook.MaxEnabledIntegrations+1)
	if err != nil {
		return fmt.Errorf("list enabled Webhook Integrations for routing: %w", err)
	}

	targets := make([]webhookRouteTarget, 0, webhook.MaxEnabledIntegrations)
	for rows.Next() {
		var target webhookRouteTarget
		if err := rows.Scan(
			&target.integrationID, &target.integrationVersion, &target.secretVersion,
		); err != nil {
			rows.Close()
			return fmt.Errorf("scan enabled Webhook Integration route: %w", err)
		}
		targets = append(targets, target)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan enabled Webhook Integration routes: %w", err)
	}
	if len(targets) > webhook.MaxEnabledIntegrations {
		return errors.New("enabled Webhook Integration limit invariant was violated")
	}

	for _, target := range targets {
		deliveryID, err := routeIDs.NewUUIDv7(value.CreatedAt)
		if err != nil {
			return fmt.Errorf("generate Webhook delivery id: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
INSERT INTO webhook_deliveries (
    id, organization_id, alert_id, integration_id,
    integration_version, secret_version, routed_at, available_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$7)`,
			deliveryID, value.OrganizationID, value.ID, target.integrationID,
			target.integrationVersion, target.secretVersion, value.CreatedAt.UTC(),
		); err != nil {
			return fmt.Errorf("insert Webhook delivery route: %w", err)
		}
	}
	return nil
}
