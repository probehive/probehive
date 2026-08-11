package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	DeliveryChannel        = "webhook"
	MaxDeliveryAttempts    = 5
	MaxDeliveryBodyBytes   = 16 * 1024
	MaxResponseHeaderBytes = 16 * 1024
	DeliveryTimeout        = 10 * time.Second
)

const (
	OutcomeInProgress = "inProgress"
	OutcomeSucceeded  = "succeeded"
	OutcomeFailed     = "failed"
	OutcomeCancelled  = "cancelled"
)

const (
	SuppressionReasonMaintenance = "maintenance"
)

const (
	FailureCodeCancelled         = "webhook.delivery.cancelled"
	FailureCodeDestination       = "webhook.delivery.destination.invalid"
	FailureCodeHTTPRejected      = "webhook.delivery.http.rejected"
	FailureCodeHTTPRetryable     = "webhook.delivery.http.retryable"
	FailureCodeNetwork           = "webhook.delivery.network"
	FailureCodePayload           = "webhook.delivery.payload.invalid"
	FailureCodeSecretUnavailable = "webhook.delivery.secret.unavailable"
	FailureCodeTimeout           = "webhook.delivery.timeout"
	FailureCodeOutcomeUncertain  = "webhook.delivery.outcome.uncertain"
)

var (
	ErrDeliveryLeaseLost = errors.New("the Webhook delivery lease was lost")
	ErrDeliveryInvalid   = errors.New("invalid Webhook delivery")
)

type DeliveryRoute struct {
	DeliveryID         string
	OrganizationID     string
	AlertID            string
	AlertKind          string
	ProjectID          string
	MonitorID          string
	IncidentID         string
	IncidentVersion    int64
	IntegrationID      string
	IntegrationVersion int64
	SecretVersion      int64
	DestinationURL     string
	OccurredAt         time.Time
	RoutedAt           time.Time
	Envelope           Envelope
}

type DeliveryClaim struct {
	DeliveryRoute
	Sequence       int64
	StartedAt      time.Time
	LeaseExpiresAt time.Time
	LeaseHolder    string
}

type AttemptUpdate struct {
	Outcome     string
	FinishedAt  time.Time
	HTTPStatus  *int
	FailureCode string
}

type DeliveryAttempt struct {
	Sequence    int64
	StartedAt   time.Time
	FinishedAt  *time.Time
	Outcome     string
	HTTPStatus  *int
	FailureCode string
}

type DeliveryAudit struct {
	DeliveryID          string
	IntegrationID       string
	IntegrationVersion  int64
	SecretVersion       int64
	RoutedAt            time.Time
	SuppressionReason   string
	MaintenanceWindowID string
	Attempts            []DeliveryAttempt
}

type DeliveryScope struct {
	OrganizationID string
	ProjectID      string
	MonitorID      string
	AlertID        string
}

func (scope DeliveryScope) Validate() error {
	if scope.OrganizationID == "" || scope.ProjectID == "" ||
		scope.MonitorID == "" || scope.AlertID == "" {
		return errors.New("Webhook delivery audit requires complete scope")
	}
	return nil
}

type DeliveryStore interface {
	Claim(context.Context, string, time.Time, time.Time, int) ([]DeliveryClaim, error)
	Complete(context.Context, DeliveryClaim, AttemptUpdate, *time.Time, bool) error
	ListAudit(context.Context, DeliveryScope) ([]DeliveryAudit, bool, error)
}

type DeliveryRequest struct {
	URL     string
	Body    []byte
	Headers map[string]string
}

type DeliveryResponse struct {
	StatusCode int
}

type HTTPDoer interface {
	Do(context.Context, DeliveryRequest) (*DeliveryResponse, error)
}

type DeliveryExecution struct {
	AttemptUpdate
	Retry bool
}

type DeliveryExecutor struct {
	client  HTTPDoer
	keyring *Keyring
	clock   Clock
}

func NewDeliveryExecutor(client HTTPDoer, keyring *Keyring, clock Clock) *DeliveryExecutor {
	if client == nil || clock == nil {
		panic("Webhook DeliveryExecutor requires an HTTP client and clock")
	}
	return &DeliveryExecutor{client: client, keyring: keyring, clock: clock}
}

func (executor *DeliveryExecutor) Execute(ctx context.Context, claim DeliveryClaim) DeliveryExecution {
	update := AttemptUpdate{Outcome: OutcomeFailed, FinishedAt: executor.clock.Now().UTC()}
	if ctx.Err() != nil {
		update.Outcome = OutcomeCancelled
		update.FailureCode = FailureCodeCancelled
		return DeliveryExecution{AttemptUpdate: update, Retry: true}
	}
	destination, ok := NormalizeDestinationURL(claim.DestinationURL)
	if !ok || destination != claim.DestinationURL {
		update.FailureCode = FailureCodeDestination
		return DeliveryExecution{AttemptUpdate: update}
	}
	secret, err := executor.openSecret(claim)
	if err != nil {
		update.FailureCode = FailureCodeSecretUnavailable
		return DeliveryExecution{AttemptUpdate: update}
	}
	body, signature, timestamp, err := signedPayload(claim, executor.clock.Now().UTC(), secret)
	clear(secret)
	if err != nil {
		update.FailureCode = FailureCodePayload
		return DeliveryExecution{AttemptUpdate: update}
	}

	request := DeliveryRequest{
		URL:  destination,
		Body: body,
		Headers: map[string]string{
			"Content-Type":              "application/json",
			"User-Agent":                "ProbeHive-Webhook/1",
			"ProbeHive-Webhook-Version": "v1",
			"ProbeHive-Delivery-Id":     claim.DeliveryID,
			"ProbeHive-Timestamp":       strconv.FormatInt(timestamp, 10),
			"ProbeHive-Attempt":         strconv.FormatInt(claim.Sequence, 10),
			"ProbeHive-Secret-Version":  strconv.FormatInt(claim.SecretVersion, 10),
			"ProbeHive-Signature":       signature,
		},
	}

	callContext, cancel := context.WithTimeout(ctx, DeliveryTimeout)
	defer cancel()
	response, err := executor.client.Do(callContext, request)
	if err != nil {
		update.FinishedAt = executor.clock.Now().UTC()
		switch {
		case errors.Is(err, context.Canceled), errors.Is(ctx.Err(), context.Canceled):
			update.Outcome = OutcomeCancelled
			update.FailureCode = FailureCodeCancelled
		case errors.Is(err, context.DeadlineExceeded),
			errors.Is(callContext.Err(), context.DeadlineExceeded):
			update.FailureCode = FailureCodeTimeout
		default:
			update.FailureCode = stableHTTPErrorCode(err)
		}
		return DeliveryExecution{AttemptUpdate: update, Retry: true}
	}
	if response == nil {
		update.FinishedAt = executor.clock.Now().UTC()
		update.FailureCode = FailureCodeNetwork
		return DeliveryExecution{AttemptUpdate: update, Retry: true}
	}
	status := response.StatusCode
	update.HTTPStatus = &status
	update.FinishedAt = executor.clock.Now().UTC()
	switch {
	case status >= 200 && status < 300:
		update.Outcome = OutcomeSucceeded
		return DeliveryExecution{AttemptUpdate: update}
	case status == 408 || status == 425 || status == 429 || status >= 500:
		update.FailureCode = FailureCodeHTTPRetryable
		return DeliveryExecution{AttemptUpdate: update, Retry: true}
	default:
		update.FailureCode = FailureCodeHTTPRejected
		return DeliveryExecution{AttemptUpdate: update}
	}
}

func (executor *DeliveryExecutor) openSecret(claim DeliveryClaim) ([]byte, error) {
	if executor.keyring == nil {
		return nil, ErrKeyringUnavailable
	}
	return executor.keyring.Open(
		claim.Envelope,
		secretAssociatedData(claim.OrganizationID, claim.IntegrationID, claim.SecretVersion),
	)
}

type webhookPayload struct {
	SchemaVersion   string    `json:"schemaVersion"`
	DeliveryID      string    `json:"deliveryId"`
	AlertID         string    `json:"alertId"`
	AlertKind       string    `json:"alertKind"`
	OrganizationID  string    `json:"organizationId"`
	ProjectID       string    `json:"projectId"`
	MonitorID       string    `json:"monitorId"`
	IncidentID      string    `json:"incidentId"`
	IncidentVersion int64     `json:"incidentVersion"`
	OccurredAt      time.Time `json:"occurredAt"`
	Attempt         int64     `json:"attempt"`
}

func signedPayload(
	claim DeliveryClaim, now time.Time, secret []byte,
) ([]byte, string, int64, error) {
	if claim.DeliveryID == "" || claim.OrganizationID == "" || claim.AlertID == "" ||
		claim.ProjectID == "" || claim.MonitorID == "" || claim.IncidentID == "" ||
		claim.IntegrationID == "" || claim.Sequence < 1 || claim.SecretVersion < 1 ||
		claim.IntegrationVersion < 1 || claim.IncidentVersion < 1 ||
		!isUTC(claim.OccurredAt) || !isUTC(claim.RoutedAt) ||
		!isUTC(now) || !isUTC(claim.StartedAt) {
		return nil, "", 0, ErrDeliveryInvalid
	}
	payload, err := json.Marshal(webhookPayload{
		SchemaVersion: "v1", DeliveryID: claim.DeliveryID, AlertID: claim.AlertID,
		AlertKind: claim.AlertKind, OrganizationID: claim.OrganizationID,
		ProjectID: claim.ProjectID, MonitorID: claim.MonitorID,
		IncidentID: claim.IncidentID, IncidentVersion: claim.IncidentVersion,
		OccurredAt: claim.OccurredAt.UTC(), Attempt: claim.Sequence,
	})
	if err != nil {
		return nil, "", 0, err
	}
	if len(payload) > MaxDeliveryBodyBytes {
		return nil, "", 0, ErrDeliveryInvalid
	}
	timestamp := now.Unix()
	canonical := strings.Join([]string{
		"v1", claim.DeliveryID, strconv.FormatInt(timestamp, 10),
		strconv.FormatInt(claim.Sequence, 10), strconv.FormatInt(claim.SecretVersion, 10),
		string(payload),
	}, "\n")
	mac := hmac.New(sha256.New, secret)
	_, _ = io.WriteString(mac, canonical)
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload, signature, timestamp, nil
}

func stableHTTPErrorCode(err error) string {
	var coder interface{ ErrorCode() string }
	if errors.As(err, &coder) && coder.ErrorCode() != "" {
		code := coder.ErrorCode()
		switch code {
		case
			"outbound.policy.unconfigured",
			"outbound.url.tooLong",
			"outbound.url.invalid",
			"outbound.url.notAbsolute",
			"outbound.url.scheme",
			"outbound.url.userInfo",
			"outbound.host.missing",
			"outbound.host.invalid",
			"outbound.port.invalid",
			"outbound.port.denied",
			"outbound.network.unsupported",
			"outbound.resolution.failed",
			"outbound.resolution.empty",
			"outbound.address.denied",
			"outbound.address.mismatch",
			"outbound.connect.failed":
			return code
		}
	}
	return FailureCodeNetwork
}
