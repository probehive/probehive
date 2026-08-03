package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/probehive/probehive/internal/alert"
	"github.com/probehive/probehive/internal/health"
	"github.com/probehive/probehive/internal/incident"
	"github.com/probehive/probehive/internal/outbox"
	"github.com/probehive/probehive/internal/postgres"
	"github.com/probehive/probehive/internal/run"
	"github.com/probehive/probehive/internal/webhook"
)

func newOutboxDispatcher(
	database *postgres.DB,
	healthService *health.Service,
	incidentService *incident.Service,
	alertService *alert.Service,
	confirmations *run.ConfirmationRunner,
	systemClock outbox.Clock,
	identifiers outbox.IDGenerator,
	logger *slog.Logger,
) (*outbox.Dispatcher, error) {
	return outbox.New(outbox.Config{
		Store: database.Outbox(), Clock: systemClock, UUIDs: identifiers, Logger: logger,
		Handlers: map[string]outbox.Handler{
			health.TopicRunRecordedV1: outbox.HandlerFunc(func(ctx context.Context, entry outbox.Entry) error {
				err := healthService.HandleRunRecorded(ctx, entry.ID, entry.OrganizationID, entry.Payload)
				switch {
				case errors.Is(err, health.ErrPayloadInvalid):
					return outbox.Permanent(outbox.CodePayloadInvalid)
				case errors.Is(err, health.ErrOrganizationMismatch):
					return outbox.Permanent(outbox.CodeOrganizationMismatch)
				default:
					return err
				}
			}),
			health.TopicConfirmationRequestedV1: outbox.HandlerFunc(func(ctx context.Context, entry outbox.Entry) error {
				request, err := decodeConfirmationRequest(entry)
				if err != nil {
					return err
				}
				return confirmations.Execute(ctx, request)
			}),
			health.TopicHealthTransitionedV1: outbox.HandlerFunc(func(ctx context.Context, entry outbox.Entry) error {
				err := incidentService.HandleHealthTransition(ctx, entry.ID, entry.OrganizationID, entry.Payload)
				switch {
				case errors.Is(err, incident.ErrPayloadInvalid):
					return outbox.Permanent(outbox.CodePayloadInvalid)
				case errors.Is(err, incident.ErrOrganizationMismatch):
					return outbox.Permanent(outbox.CodeOrganizationMismatch)
				case errors.Is(err, incident.ErrVersionGap):
					return outbox.Transient(outbox.CodeAggregateVersionGap)
				default:
					return err
				}
			}),
			incident.TopicIncidentTransitionedV1: outbox.HandlerFunc(func(ctx context.Context, entry outbox.Entry) error {
				err := alertService.HandleIncidentTransition(ctx, entry.ID, entry.OrganizationID, entry.Payload)
				switch {
				case errors.Is(err, alert.ErrPayloadInvalid):
					return outbox.Permanent(outbox.CodePayloadInvalid)
				case errors.Is(err, alert.ErrOrganizationMismatch):
					return outbox.Permanent(outbox.CodeOrganizationMismatch)
				default:
					return err
				}
			}),
		},
	})
}

func newWebhookDeliveryDispatcher(
	database *postgres.DB,
	keyring *webhook.Keyring,
	client webhook.HTTPDoer,
	systemClock webhook.Clock,
	identifiers webhook.IDGenerator,
	logger *slog.Logger,
) (*webhook.DeliveryDispatcher, error) {
	return webhook.NewDeliveryDispatcher(webhook.DeliveryDispatcherConfig{
		Store: database.Webhooks(), Keyring: keyring, Client: client,
		Clock: systemClock, UUIDs: identifiers, Logger: logger,
		RetryDelay: outbox.RetryDelay,
	})
}

func decodeConfirmationRequest(entry outbox.Entry) (run.ConfirmationRequest, error) {
	var event health.ConfirmationRequestedV1
	if err := json.Unmarshal(entry.Payload, &event); err != nil {
		return run.ConfirmationRequest{}, outbox.Permanent(outbox.CodePayloadInvalid)
	}
	if event.OrganizationID != entry.OrganizationID {
		return run.ConfirmationRequest{}, outbox.Permanent(outbox.CodeOrganizationMismatch)
	}
	if event.EventID != entry.ID || event.AggregateType != "healthCandidate" ||
		event.AggregateID != event.CandidateID || event.AggregateVersion != 1 ||
		event.CausationID == "" {
		return run.ConfirmationRequest{}, outbox.Permanent(outbox.CodePayloadInvalid)
	}
	request := run.ConfirmationRequest{
		EventID: event.EventID, OrganizationID: event.OrganizationID,
		CandidateID: event.CandidateID, MonitorID: event.MonitorID,
		RevisionNumber: event.RevisionNumber, Location: event.Location,
		TriggeringRunID:        event.TriggeringRunID,
		TriggeringScheduledFor: event.TriggeringScheduledFor.UTC(),
		RequestedFor:           event.RequestedFor.UTC(),
		ExpectedEvidence:       event.ExpectedEvidence, PolicyVersion: event.PolicyVersion,
	}
	if err := request.Validate(); err != nil {
		return run.ConfirmationRequest{}, outbox.Permanent(outbox.CodePayloadInvalid)
	}
	return request, nil
}
