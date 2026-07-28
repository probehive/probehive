package health

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	TopicRunRecordedV1           = "run.recorded.v1"
	TopicConfirmationRequestedV1 = "run.confirmation.requested.v1"
	TopicHealthTransitionedV1    = "health.transitioned.v1"
)

var (
	ErrPayloadInvalid       = errors.New("invalid health event payload")
	ErrOrganizationMismatch = errors.New("health event Organization does not match its outbox row")
)

type RunRecordedV1 struct {
	EventID          string    `json:"eventId"`
	OrganizationID   string    `json:"organizationId"`
	OccurredAt       time.Time `json:"occurredAt"`
	AggregateType    string    `json:"aggregateType"`
	AggregateID      string    `json:"aggregateId"`
	AggregateVersion int       `json:"aggregateVersion"`
	RunID            string    `json:"runId"`
	MonitorID        string    `json:"monitorId"`
	RevisionNumber   int       `json:"revisionNumber"`
	Location         string    `json:"location"`
	ScheduledFor     time.Time `json:"scheduledFor"`
	Kind             string    `json:"kind"`
	Outcome          string    `json:"outcome"`
}

type ConfirmationRequestedV1 struct {
	EventID                string    `json:"eventId"`
	OrganizationID         string    `json:"organizationId"`
	OccurredAt             time.Time `json:"occurredAt"`
	AggregateType          string    `json:"aggregateType"`
	AggregateID            string    `json:"aggregateId"`
	AggregateVersion       int       `json:"aggregateVersion"`
	CausationID            string    `json:"causationId"`
	CandidateID            string    `json:"candidateId"`
	MonitorID              string    `json:"monitorId"`
	RevisionNumber         int       `json:"revisionNumber"`
	Location               string    `json:"location"`
	TriggeringRunID        string    `json:"triggeringRunId"`
	TriggeringScheduledFor time.Time `json:"triggeringScheduledFor"`
	RequestedFor           time.Time `json:"requestedFor"`
	ExpectedEvidence       string    `json:"expectedEvidence"`
	PolicyVersion          string    `json:"policyVersion"`
}

type HealthTransitionedV1 struct {
	EventID          string    `json:"eventId"`
	OrganizationID   string    `json:"organizationId"`
	OccurredAt       time.Time `json:"occurredAt"`
	AggregateType    string    `json:"aggregateType"`
	AggregateID      string    `json:"aggregateId"`
	AggregateVersion int64     `json:"aggregateVersion"`
	CausationID      string    `json:"causationId,omitempty"`
	TransitionID     string    `json:"transitionId"`
	MonitorID        string    `json:"monitorId"`
	ProjectID        string    `json:"projectId"`
	OldState         string    `json:"oldState"`
	NewState         string    `json:"newState"`
	PolicyVersion    string    `json:"policyVersion"`
}

type ProcessIDs struct {
	CandidateID         string
	ConfirmationEventID string
	TransitionID        string
	TransitionEventID   string
}

type Scope struct {
	OrganizationID string
	ProjectID      string
	MonitorID      string
}

type StaleTarget struct {
	OrganizationID string
	ProjectID      string
	MonitorID      string
	Version        int64
}

func (scope Scope) validate() error {
	if scope.OrganizationID == "" || scope.ProjectID == "" || scope.MonitorID == "" {
		return errors.New("a health query requires Organization, Project, and Monitor identity")
	}
	return nil
}

type Store interface {
	ProcessRunRecorded(context.Context, RunRecordedV1, ProcessIDs, time.Time) error
	GetHealth(context.Context, Scope) (Snapshot, bool, error)
	ListStaleHealth(context.Context, time.Time, time.Duration, int) ([]StaleTarget, error)
	MarkHealthStale(context.Context, StaleTarget, string, string, time.Time, time.Duration) error
}

type IDGenerator interface {
	NewUUIDv7(time.Time) (string, error)
}

type Clock interface{ Now() time.Time }

type Service struct {
	store Store
	clock Clock
	uuids IDGenerator
}

func NewService(store Store, clock Clock, uuids IDGenerator) *Service {
	if store == nil || clock == nil || uuids == nil {
		panic("health.Service requires a store, clock, and UUID generator")
	}
	return &Service{store: store, clock: clock, uuids: uuids}
}

func (service *Service) HandleRunRecorded(
	ctx context.Context, eventID, organizationID string, payload []byte,
) error {
	var event RunRecordedV1
	if err := json.Unmarshal(payload, &event); err != nil {
		return ErrPayloadInvalid
	}
	if err := validateRunRecorded(event); err != nil || event.EventID != eventID {
		return ErrPayloadInvalid
	}
	if event.OrganizationID != organizationID {
		return ErrOrganizationMismatch
	}
	now := service.clock.Now().UTC()
	ids := ProcessIDs{}
	var err error
	if ids.CandidateID, err = service.uuids.NewUUIDv7(now); err != nil {
		return err
	}
	if ids.ConfirmationEventID, err = service.uuids.NewUUIDv7(now); err != nil {
		return err
	}
	if ids.TransitionID, err = service.uuids.NewUUIDv7(now); err != nil {
		return err
	}
	if ids.TransitionEventID, err = service.uuids.NewUUIDv7(now); err != nil {
		return err
	}
	return service.store.ProcessRunRecorded(ctx, event, ids, now)
}

func (service *Service) Get(ctx context.Context, scope Scope) (Snapshot, bool, error) {
	if err := scope.validate(); err != nil {
		return Snapshot{}, false, err
	}
	return service.store.GetHealth(ctx, scope)
}

func (service *Service) SweepStale(ctx context.Context, executionCeiling time.Duration) (int, error) {
	now := service.clock.Now().UTC()
	targets, err := service.store.ListStaleHealth(ctx, now, executionCeiling, 100)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, target := range targets {
		transitionID, err := service.uuids.NewUUIDv7(now)
		if err != nil {
			return processed, err
		}
		eventID, err := service.uuids.NewUUIDv7(now)
		if err != nil {
			return processed, err
		}
		if err := service.store.MarkHealthStale(
			ctx, target, transitionID, eventID, now, executionCeiling); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (service *Service) ServeStaleness(
	ctx context.Context, executionCeiling, interval time.Duration, onError func(error),
) error {
	if executionCeiling <= 0 || interval <= 0 {
		return errors.New("health staleness worker requires positive intervals")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := service.SweepStale(ctx, executionCeiling); err != nil && ctx.Err() == nil && onError != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func validateRunRecorded(event RunRecordedV1) error {
	if event.EventID == "" || event.OrganizationID == "" || event.RunID == "" || event.MonitorID == "" {
		return ErrPayloadInvalid
	}
	if event.AggregateType != "run" || event.AggregateID != event.RunID || event.AggregateVersion != 1 {
		return ErrPayloadInvalid
	}
	if event.RevisionNumber < 1 || event.Location == "" {
		return ErrPayloadInvalid
	}
	if !isUTC(event.OccurredAt) || !isUTC(event.ScheduledFor) {
		return ErrPayloadInvalid
	}
	switch event.Kind {
	case "scheduled", "confirmation", "manual":
	default:
		return ErrPayloadInvalid
	}
	switch event.Outcome {
	case "passed", "failed", "errored", "timedout", "cancelled", "skipped":
	default:
		return ErrPayloadInvalid
	}
	return nil
}
