package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/probehive/probehive/internal/webhook"
)

var _ webhook.DeliveryStore = (*WebhookStore)(nil)

func (store *WebhookStore) Claim(
	ctx context.Context,
	holder string,
	now time.Time,
	leaseExpiresAt time.Time,
	limit int,
) ([]webhook.DeliveryClaim, error) {
	if holder == "" || limit < 1 || limit > webhook.DeliveryBatchSize ||
		!leaseExpiresAt.After(now) {
		return nil, errors.New("invalid Webhook delivery claim")
	}
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin Webhook delivery claim: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	if _, err := transaction.Exec(ctx, `
UPDATE webhook_delivery_attempts AS attempt
SET outcome='failed', finished_at=$1, failure_code=$2
FROM webhook_deliveries AS delivery
WHERE attempt.delivery_id=delivery.id
  AND attempt.organization_id=delivery.organization_id
  AND attempt.sequence=delivery.attempt_count
  AND attempt.outcome='inProgress'
  AND delivery.completed_at IS NULL
  AND delivery.lease_expires_at <= $1`,
		now.UTC(), webhook.FailureCodeOutcomeUncertain,
	); err != nil {
		return nil, fmt.Errorf("finalize expired Webhook delivery attempt: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
UPDATE webhook_deliveries AS delivery
SET completed_at=$1, lease_holder=NULL, lease_expires_at=NULL
WHERE delivery.completed_at IS NULL
  AND delivery.attempt_count >= $2
  AND delivery.lease_expires_at <= $1
  AND NOT EXISTS (
      SELECT 1
      FROM webhook_delivery_attempts AS attempt
      WHERE attempt.delivery_id=delivery.id
        AND attempt.organization_id=delivery.organization_id
        AND attempt.outcome='inProgress'
  )`,
		now.UTC(), webhook.MaxDeliveryAttempts,
	); err != nil {
		return nil, fmt.Errorf("complete exhausted Webhook delivery: %w", err)
	}

	rows, err := transaction.Query(ctx, `
SELECT delivery.id, delivery.organization_id, delivery.alert_id,
       alert.kind, alert.project_id, alert.monitor_id,
       alert.incident_id, alert.incident_version,
       delivery.integration_id, delivery.integration_version,
       delivery.secret_version, integration.destination_url,
       alert.occurred_at, delivery.routed_at,
       secret.wrapping_key_id, secret.nonce, secret.ciphertext,
       delivery.attempt_count
FROM webhook_deliveries AS delivery
JOIN alerts AS alert
  ON alert.id=delivery.alert_id
 AND alert.organization_id=delivery.organization_id
JOIN webhook_integrations AS integration
  ON integration.id=delivery.integration_id
 AND integration.organization_id=delivery.organization_id
JOIN webhook_signing_secrets AS secret
  ON secret.organization_id=delivery.organization_id
 AND secret.integration_id=delivery.integration_id
 AND secret.secret_version=delivery.secret_version
WHERE delivery.completed_at IS NULL
  AND delivery.available_at <= $1
  AND delivery.attempt_count < $2
  AND (
      delivery.lease_expires_at IS NULL
      OR delivery.lease_expires_at <= $1
  )
ORDER BY delivery.available_at, delivery.id
LIMIT $3
FOR UPDATE OF delivery SKIP LOCKED`,
		now.UTC(), webhook.MaxDeliveryAttempts, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select Webhook delivery claims: %w", err)
	}

	type candidate struct {
		route        webhook.DeliveryRoute
		attemptCount int64
	}
	candidates := make([]candidate, 0, limit)
	for rows.Next() {
		var value candidate
		var wrappingKeyID *string
		if err := rows.Scan(
			&value.route.DeliveryID,
			&value.route.OrganizationID,
			&value.route.AlertID,
			&value.route.AlertKind,
			&value.route.ProjectID,
			&value.route.MonitorID,
			&value.route.IncidentID,
			&value.route.IncidentVersion,
			&value.route.IntegrationID,
			&value.route.IntegrationVersion,
			&value.route.SecretVersion,
			&value.route.DestinationURL,
			&value.route.OccurredAt,
			&value.route.RoutedAt,
			&wrappingKeyID,
			&value.route.Envelope.Nonce,
			&value.route.Envelope.Ciphertext,
			&value.attemptCount,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan Webhook delivery claim: %w", err)
		}
		if wrappingKeyID != nil {
			value.route.Envelope.KeyID = *wrappingKeyID
		}
		value.route.OccurredAt = value.route.OccurredAt.UTC()
		value.route.RoutedAt = value.route.RoutedAt.UTC()
		candidates = append(candidates, value)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan Webhook delivery claims: %w", err)
	}

	claims := make([]webhook.DeliveryClaim, 0, len(candidates))
	for _, candidate := range candidates {
		sequence := candidate.attemptCount + 1
		tag, err := transaction.Exec(ctx, `
UPDATE webhook_deliveries
SET attempt_count=$1, lease_holder=$2, lease_expires_at=$3
WHERE id=$4 AND organization_id=$5
  AND completed_at IS NULL
  AND attempt_count=$6`,
			sequence, holder, leaseExpiresAt.UTC(),
			candidate.route.DeliveryID, candidate.route.OrganizationID,
			candidate.attemptCount,
		)
		if err != nil {
			return nil, fmt.Errorf("lease Webhook delivery: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return nil, errors.New("Webhook delivery changed while locked")
		}
		if _, err := transaction.Exec(ctx, `
INSERT INTO webhook_delivery_attempts (
    organization_id, delivery_id, alert_id, integration_id,
    integration_version, secret_version, sequence,
    started_at, outcome
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'inProgress')`,
			candidate.route.OrganizationID,
			candidate.route.DeliveryID,
			candidate.route.AlertID,
			candidate.route.IntegrationID,
			candidate.route.IntegrationVersion,
			candidate.route.SecretVersion,
			sequence,
			now.UTC(),
		); err != nil {
			return nil, fmt.Errorf("insert Webhook delivery attempt: %w", err)
		}
		claims = append(claims, webhook.DeliveryClaim{
			DeliveryRoute:  candidate.route,
			Sequence:       sequence,
			StartedAt:      now.UTC(),
			LeaseExpiresAt: leaseExpiresAt.UTC(),
			LeaseHolder:    holder,
		})
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit Webhook delivery claims: %w", err)
	}
	return claims, nil
}

func (store *WebhookStore) Complete(
	ctx context.Context,
	claim webhook.DeliveryClaim,
	update webhook.AttemptUpdate,
	nextAvailableAt *time.Time,
	terminal bool,
) error {
	if claim.DeliveryID == "" || claim.OrganizationID == "" ||
		claim.LeaseHolder == "" || claim.Sequence < 1 ||
		update.FinishedAt.Before(claim.StartedAt) ||
		(!terminal && nextAvailableAt == nil) ||
		(terminal && nextAvailableAt != nil) {
		return errors.New("invalid Webhook delivery completion")
	}
	transaction, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Webhook delivery completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	tag, err := transaction.Exec(ctx, `
UPDATE webhook_delivery_attempts AS attempt
SET finished_at=$1, outcome=$2, http_status=$3, failure_code=$4
FROM webhook_deliveries AS delivery
WHERE attempt.delivery_id=$5
  AND attempt.organization_id=$6
  AND attempt.sequence=$7
  AND attempt.outcome='inProgress'
  AND delivery.id=attempt.delivery_id
  AND delivery.organization_id=attempt.organization_id
  AND delivery.lease_holder=$8
  AND delivery.lease_expires_at > $1
  AND delivery.attempt_count=$7`,
		update.FinishedAt.UTC(),
		update.Outcome,
		update.HTTPStatus,
		nullableString(update.FailureCode),
		claim.DeliveryID,
		claim.OrganizationID,
		claim.Sequence,
		claim.LeaseHolder,
	)
	if err != nil {
		return fmt.Errorf("complete Webhook delivery attempt: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return webhook.ErrDeliveryLeaseLost
	}

	tag, err = transaction.Exec(ctx, `
UPDATE webhook_deliveries
SET available_at=COALESCE($1::timestamptz, available_at),
    completed_at=CASE WHEN $2 THEN $3::timestamptz ELSE NULL END,
    lease_holder=NULL,
    lease_expires_at=NULL
WHERE id=$4 AND organization_id=$5
  AND lease_holder=$6
  AND attempt_count=$7`,
		nextAvailableAt,
		terminal,
		update.FinishedAt.UTC(),
		claim.DeliveryID,
		claim.OrganizationID,
		claim.LeaseHolder,
		claim.Sequence,
	)
	if err != nil {
		return fmt.Errorf("release Webhook delivery lease: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return webhook.ErrDeliveryLeaseLost
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit Webhook delivery completion: %w", err)
	}
	return nil
}

func (store *WebhookStore) ListAudit(
	ctx context.Context, scope webhook.DeliveryScope,
) ([]webhook.DeliveryAudit, bool, error) {
	var found bool
	if err := store.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM alerts
    WHERE id=$1 AND organization_id=$2
      AND project_id=$3 AND monitor_id=$4
)`,
		scope.AlertID,
		scope.OrganizationID,
		scope.ProjectID,
		scope.MonitorID,
	).Scan(&found); err != nil {
		return nil, false, fmt.Errorf("check Webhook delivery Alert scope: %w", err)
	}
	if !found {
		return nil, false, nil
	}

	rows, err := store.pool.Query(ctx, `
SELECT delivery.id, delivery.integration_id,
       delivery.integration_version, delivery.secret_version,
       delivery.routed_at,
       attempt.sequence, attempt.started_at, attempt.finished_at,
       attempt.outcome, attempt.http_status, attempt.failure_code
FROM webhook_deliveries AS delivery
LEFT JOIN webhook_delivery_attempts AS attempt
  ON attempt.delivery_id=delivery.id
 AND attempt.organization_id=delivery.organization_id
WHERE delivery.organization_id=$1
  AND delivery.alert_id=$2
ORDER BY delivery.routed_at, delivery.id, attempt.sequence`,
		scope.OrganizationID, scope.AlertID,
	)
	if err != nil {
		return nil, false, fmt.Errorf("list Webhook delivery audit: %w", err)
	}
	defer rows.Close()

	values := make([]webhook.DeliveryAudit, 0, webhook.MaxEnabledIntegrations)
	for rows.Next() {
		var (
			deliveryID, integrationID string
			integrationVersion        int64
			secretVersion             int64
			routedAt                  time.Time
			sequence                  *int64
			startedAt, finishedAt     *time.Time
			outcome, failureCode      *string
			httpStatus                *int
		)
		if err := rows.Scan(
			&deliveryID,
			&integrationID,
			&integrationVersion,
			&secretVersion,
			&routedAt,
			&sequence,
			&startedAt,
			&finishedAt,
			&outcome,
			&httpStatus,
			&failureCode,
		); err != nil {
			return nil, false, fmt.Errorf("scan Webhook delivery audit: %w", err)
		}
		if len(values) == 0 || values[len(values)-1].DeliveryID != deliveryID {
			values = append(values, webhook.DeliveryAudit{
				DeliveryID:         deliveryID,
				IntegrationID:      integrationID,
				IntegrationVersion: integrationVersion,
				SecretVersion:      secretVersion,
				RoutedAt:           routedAt.UTC(),
				Attempts:           []webhook.DeliveryAttempt{},
			})
		}
		if sequence == nil {
			continue
		}
		attempt := webhook.DeliveryAttempt{
			Sequence:    *sequence,
			Outcome:     valueOrEmptyString(outcome),
			HTTPStatus:  httpStatus,
			FailureCode: valueOrEmptyString(failureCode),
		}
		if startedAt != nil {
			attempt.StartedAt = startedAt.UTC()
		}
		if finishedAt != nil {
			value := finishedAt.UTC()
			attempt.FinishedAt = &value
		}
		last := len(values) - 1
		values[last].Attempts = append(values[last].Attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("scan Webhook delivery audit: %w", err)
	}
	return values, true, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func valueOrEmptyString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
