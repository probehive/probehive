package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDeliveryExecutorEmitsDeterministicSignedRequest(t *testing.T) {
	now := testInstant().Add(2 * time.Hour)
	claim, keyring, signingSecret := testDeliveryClaim(t, now, 2)
	var captured DeliveryRequest
	var body []byte
	client := httpDoerFunc(func(request DeliveryRequest) (*DeliveryResponse, error) {
		captured = request
		body = append([]byte(nil), request.Body...)
		return &DeliveryResponse{StatusCode: http.StatusNoContent}, nil
	})

	result := NewDeliveryExecutor(client, keyring, fixedClock{now}).Execute(
		t.Context(), claim,
	)
	if result.Outcome != OutcomeSucceeded || result.Retry ||
		result.HTTPStatus == nil || *result.HTTPStatus != http.StatusNoContent ||
		result.FailureCode != "" {
		t.Fatalf("Execute() = %#v", result)
	}
	wantBody := fmt.Sprintf(
		`{"schemaVersion":"v1","deliveryId":"00000000-0000-7000-8000-000000000010","alertId":"00000000-0000-7000-8000-000000000011","alertKind":"incident.opened","organizationId":"00000000-0000-7000-8000-000000000012","projectId":"00000000-0000-7000-8000-000000000013","monitorId":"00000000-0000-7000-8000-000000000014","incidentId":"00000000-0000-7000-8000-000000000015","incidentVersion":3,"occurredAt":"%s","attempt":2}`,
		claim.OccurredAt.Format(time.RFC3339Nano),
	)
	if string(body) != wantBody {
		t.Fatalf("request body = %s\nwant %s", body, wantBody)
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	canonical := strings.Join([]string{
		"v1", claim.DeliveryID, timestamp, "2", "4", wantBody,
	}, "\n")
	mac := hmac.New(sha256.New, []byte(signingSecret))
	_, _ = io.WriteString(mac, canonical)
	wantSignature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	for name, want := range map[string]string{
		"Content-Type":              "application/json",
		"User-Agent":                "ProbeHive-Webhook/1",
		"ProbeHive-Webhook-Version": "v1",
		"ProbeHive-Delivery-Id":     claim.DeliveryID,
		"ProbeHive-Timestamp":       timestamp,
		"ProbeHive-Attempt":         "2",
		"ProbeHive-Secret-Version":  "4",
		"ProbeHive-Signature":       wantSignature,
	} {
		if got := captured.Headers[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if captured.URL != claim.DestinationURL {
		t.Fatalf("request target = %s", captured.URL)
	}
}

func TestDeliveryExecutorClassifiesBoundedOutcomes(t *testing.T) {
	now := testInstant().Add(3 * time.Hour)
	claim, keyring, _ := testDeliveryClaim(t, now, 1)
	tests := []struct {
		name        string
		status      int
		outcome     string
		failureCode string
		retry       bool
	}{
		{"success", 200, OutcomeSucceeded, "", false},
		{"request timeout", 408, OutcomeFailed, FailureCodeHTTPRetryable, true},
		{"too early", 425, OutcomeFailed, FailureCodeHTTPRetryable, true},
		{"rate limited", 429, OutcomeFailed, FailureCodeHTTPRetryable, true},
		{"server failure", 503, OutcomeFailed, FailureCodeHTTPRetryable, true},
		{"redirect", 302, OutcomeFailed, FailureCodeHTTPRejected, false},
		{"client rejection", 422, OutcomeFailed, FailureCodeHTTPRejected, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := httpDoerFunc(func(DeliveryRequest) (*DeliveryResponse, error) {
				return &DeliveryResponse{StatusCode: test.status}, nil
			})
			result := NewDeliveryExecutor(client, keyring, fixedClock{now}).Execute(
				t.Context(), claim,
			)
			if result.Outcome != test.outcome || result.FailureCode != test.failureCode ||
				result.Retry != test.retry || result.HTTPStatus == nil ||
				*result.HTTPStatus != test.status {
				t.Fatalf("Execute() = %#v", result)
			}
		})
	}
}

func TestDeliveryExecutorUsesAllowlistedNetworkCode(t *testing.T) {
	now := testInstant().Add(4 * time.Hour)
	claim, keyring, _ := testDeliveryClaim(t, now, 1)
	for _, test := range []struct {
		name string
		code string
		want string
	}{
		{"allowlisted outbound code", "outbound.address.denied", "outbound.address.denied"},
		{"unlisted code", "provider.sensitive.failure", FailureCodeNetwork},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := httpDoerFunc(func(DeliveryRequest) (*DeliveryResponse, error) {
				return nil, codedDeliveryError{code: test.code}
			})
			result := NewDeliveryExecutor(client, keyring, fixedClock{now}).Execute(
				t.Context(), claim,
			)
			if result.Outcome != OutcomeFailed || !result.Retry ||
				result.FailureCode != test.want || result.HTTPStatus != nil {
				t.Fatalf("Execute() = %#v", result)
			}
		})
	}
}

func TestDeliveryDispatcherBoundsRetriesAtFiveCalls(t *testing.T) {
	now := testInstant().Add(5 * time.Hour)
	for _, test := range []struct {
		name         string
		sequence     int64
		wantRetried  int
		wantFailed   int
		wantTerminal bool
		wantNext     bool
	}{
		{"first failure schedules retry", 1, 1, 0, false, true},
		{"fifth failure is terminal", 5, 0, 1, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			claim, keyring, _ := testDeliveryClaim(t, now, test.sequence)
			store := &recordingDeliveryStore{claims: []DeliveryClaim{claim}}
			dispatcher, err := NewDeliveryDispatcher(DeliveryDispatcherConfig{
				Store: store, Clock: fixedClock{now}, UUIDs: fixedIDs{},
				Keyring: keyring,
				Client: httpDoerFunc(func(DeliveryRequest) (*DeliveryResponse, error) {
					return &DeliveryResponse{StatusCode: http.StatusServiceUnavailable}, nil
				}),
				RetryDelay: func(string, int) time.Duration { return 7 * time.Second },
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := dispatcher.Tick(t.Context(), now)
			if err != nil {
				t.Fatal(err)
			}
			if result.Claimed != 1 || result.Retried != test.wantRetried ||
				result.Failed != test.wantFailed || len(store.completions) != 1 {
				t.Fatalf("Tick() = %#v, completions %#v", result, store.completions)
			}
			completion := store.completions[0]
			if completion.terminal != test.wantTerminal ||
				(completion.next != nil) != test.wantNext {
				t.Fatalf("completion = %#v", completion)
			}
			if completion.next != nil && !completion.next.Equal(now.Add(7*time.Second)) {
				t.Fatalf("next retry = %v", completion.next)
			}
		})
	}
}

func testDeliveryClaim(
	t *testing.T, now time.Time, sequence int64,
) (DeliveryClaim, *Keyring, string) {
	t.Helper()
	keyring := mustKeyring(t, []WrappingKey{{
		ID: "delivery", Key: bytes.Repeat([]byte{7}, 32),
	}})
	signingSecret := "phwh_NGV0ZXJtaW5pc3RpYy1zaWduaW5nLXNlY3JldA"
	organizationID := "00000000-0000-7000-8000-000000000012"
	integrationID := "00000000-0000-7000-8000-000000000016"
	envelope, err := keyring.Seal(
		[]byte(signingSecret),
		secretAssociatedData(organizationID, integrationID, 4),
		bytes.NewReader(sequenceBytesForDelivery(12)),
	)
	if err != nil {
		t.Fatal(err)
	}
	return DeliveryClaim{
		DeliveryRoute: DeliveryRoute{
			DeliveryID:         "00000000-0000-7000-8000-000000000010",
			OrganizationID:     organizationID,
			AlertID:            "00000000-0000-7000-8000-000000000011",
			AlertKind:          "incident.opened",
			ProjectID:          "00000000-0000-7000-8000-000000000013",
			MonitorID:          "00000000-0000-7000-8000-000000000014",
			IncidentID:         "00000000-0000-7000-8000-000000000015",
			IncidentVersion:    3,
			IntegrationID:      integrationID,
			IntegrationVersion: 8,
			SecretVersion:      4,
			DestinationURL:     "https://hooks.example.test/events",
			OccurredAt:         now.Add(-2 * time.Minute).UTC(),
			RoutedAt:           now.Add(-time.Minute).UTC(),
			Envelope:           envelope,
		},
		Sequence:       sequence,
		StartedAt:      now.UTC(),
		LeaseExpiresAt: now.Add(DeliveryLeaseDuration).UTC(),
		LeaseHolder:    "00000000-0000-7000-8000-000000000017",
	}, keyring, signingSecret
}

type httpDoerFunc func(DeliveryRequest) (*DeliveryResponse, error)

func (function httpDoerFunc) Do(_ context.Context, request DeliveryRequest) (*DeliveryResponse, error) {
	return function(request)
}

type codedDeliveryError struct{ code string }

func (value codedDeliveryError) Error() string     { return "coded delivery failure" }
func (value codedDeliveryError) ErrorCode() string { return value.code }

type deliveryCompletion struct {
	claim    DeliveryClaim
	update   AttemptUpdate
	next     *time.Time
	terminal bool
}

type recordingDeliveryStore struct {
	claims      []DeliveryClaim
	completions []deliveryCompletion
}

func (store *recordingDeliveryStore) Claim(
	context.Context, string, time.Time, time.Time, int,
) ([]DeliveryClaim, error) {
	claims := store.claims
	store.claims = nil
	return claims, nil
}

func (store *recordingDeliveryStore) Complete(
	_ context.Context,
	claim DeliveryClaim,
	update AttemptUpdate,
	next *time.Time,
	terminal bool,
) error {
	store.completions = append(store.completions, deliveryCompletion{
		claim: claim, update: update, next: next, terminal: terminal,
	})
	return nil
}

func (*recordingDeliveryStore) ListAudit(
	context.Context, DeliveryScope,
) ([]DeliveryAudit, bool, error) {
	return nil, false, nil
}

func sequenceBytesForDelivery(length int) []byte {
	value := make([]byte, length)
	for index := range value {
		value[index] = byte(index + 1)
	}
	return value
}
