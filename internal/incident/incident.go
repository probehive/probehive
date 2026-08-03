// Package incident owns one-Monitor automatic Incident lifecycle.
package incident

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const TopicIncidentTransitionedV1 = "incident.transitioned.v1"

type State string

const (
	StateOpen         State = "open"
	StateAcknowledged State = "acknowledged"
	StateResolved     State = "resolved"
)

type TimelineKind string

const (
	TimelineOpened       TimelineKind = "opened"
	TimelineAcknowledged TimelineKind = "acknowledged"
	TimelineResolved     TimelineKind = "resolved"
)

var (
	ErrPayloadInvalid       = errors.New("invalid Incident event payload")
	ErrOrganizationMismatch = errors.New("Incident event Organization does not match its outbox row")
	ErrConflict             = errors.New("Incident state does not allow this transition")
	ErrVersionGap           = errors.New("Incident projection is waiting for an earlier health transition")
)

type Incident struct {
	ID                   string
	OrganizationID       string
	ProjectID            string
	MonitorID            string
	State                State
	Version              int64
	OpenedTransitionID   string
	AcknowledgedBy       string
	AcknowledgedAt       time.Time
	ResolvedTransitionID string
	ResolvedAt           time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Timeline             []TimelineEntry
}

type TimelineEntry struct {
	ID                    string
	IncidentID            string
	IncidentVersion       int64
	Kind                  TimelineKind
	HealthTransitionID    string
	ActorUserID           string
	OldHealthState        string
	NewHealthState        string
	PolicyVersion         string
	CausalRunID           string
	CausalRunScheduledFor time.Time
	Counts                *Counts
	OccurredAt            time.Time
}

type Counts struct {
	Configured    int
	Eligible      int
	Responding    int
	Passing       int
	Failing       int
	LocationFault int
	Indeterminate int
	Missing       int
}

type HealthTransitionedV1 struct {
	EventID          string    `json:"eventId"`
	OrganizationID   string    `json:"organizationId"`
	OccurredAt       time.Time `json:"occurredAt"`
	AggregateType    string    `json:"aggregateType"`
	AggregateID      string    `json:"aggregateId"`
	AggregateVersion int64     `json:"aggregateVersion"`
	CausationID      string    `json:"causationId"`
	TransitionID     string    `json:"transitionId"`
	MonitorID        string    `json:"monitorId"`
	ProjectID        string    `json:"projectId"`
	OldState         string    `json:"oldState"`
	NewState         string    `json:"newState"`
	PolicyVersion    string    `json:"policyVersion"`
}

type IncidentTransitionedV1 struct {
	EventID          string    `json:"eventId"`
	OrganizationID   string    `json:"organizationId"`
	OccurredAt       time.Time `json:"occurredAt"`
	AggregateType    string    `json:"aggregateType"`
	AggregateID      string    `json:"aggregateId"`
	AggregateVersion int64     `json:"aggregateVersion"`
	CausationID      string    `json:"causationId"`
	IncidentID       string    `json:"incidentId"`
	ProjectID        string    `json:"projectId"`
	MonitorID        string    `json:"monitorId"`
	Transition       string    `json:"transition"`
}

type Scope struct {
	OrganizationID string
	ProjectID      string
	MonitorID      string
}

const MaxPageSize = 100

type Cursor struct {
	CreatedAt time.Time
	ID        string
}

type ListQuery struct {
	PageSize int
	Cursor   *Cursor
}

type Page struct {
	Incidents  []Incident
	NextCursor *Cursor
}

func (scope Scope) Validate() error {
	if scope.OrganizationID == "" || scope.ProjectID == "" || scope.MonitorID == "" {
		return errors.New("an Incident query requires Organization, Project, and Monitor identity")
	}
	return nil
}

type ProcessIDs struct {
	IncidentID   string
	TimelineID   string
	AlertEventID string
}

type Store interface {
	ProcessHealthTransition(context.Context, HealthTransitionedV1, ProcessIDs, time.Time) error
	ListIncidents(context.Context, Scope, ListQuery) ([]Incident, bool, bool, error)
	GetIncident(context.Context, Scope, string) (Incident, bool, error)
	AcknowledgeIncident(context.Context, Scope, string, string, string, time.Time) (Incident, bool, error)
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
		panic("incident.Service requires a store, clock, and UUID generator")
	}
	return &Service{store: store, clock: clock, uuids: uuids}
}

func (service *Service) HandleHealthTransition(ctx context.Context, eventID, organizationID string, payload []byte) error {
	var event HealthTransitionedV1
	if err := json.Unmarshal(payload, &event); err != nil {
		return ErrPayloadInvalid
	}
	if err := validateHealthTransition(event); err != nil || event.EventID != eventID {
		return ErrPayloadInvalid
	}
	if event.OrganizationID != organizationID {
		return ErrOrganizationMismatch
	}
	now := service.clock.Now().UTC()
	incidentID, err := service.uuids.NewUUIDv7(now)
	if err != nil {
		return err
	}
	timelineID, err := service.uuids.NewUUIDv7(now)
	if err != nil {
		return err
	}
	alertEventID, err := service.uuids.NewUUIDv7(now)
	if err != nil {
		return err
	}
	return service.store.ProcessHealthTransition(ctx, event, ProcessIDs{
		IncidentID: incidentID, TimelineID: timelineID, AlertEventID: alertEventID,
	}, now)
}

func (service *Service) List(ctx context.Context, scope Scope, query ListQuery) (Page, bool, error) {
	if err := scope.Validate(); err != nil {
		return Page{}, false, err
	}
	if query.PageSize < 1 || query.PageSize > MaxPageSize {
		return Page{}, false, errors.New("an Incident page is 1 to 100 rows")
	}
	if query.Cursor != nil {
		if query.Cursor.ID == "" || !isUTC(query.Cursor.CreatedAt) {
			return Page{}, false, errors.New("an Incident cursor requires identity and a UTC creation instant")
		}
	}
	values, more, found, err := service.store.ListIncidents(ctx, scope, query)
	if err != nil || !found {
		return Page{}, found, err
	}
	page := Page{Incidents: values}
	if more && len(values) != 0 {
		last := values[len(values)-1]
		page.NextCursor = &Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return page, true, nil
}

func (service *Service) Get(ctx context.Context, scope Scope, id string) (Incident, bool, error) {
	if err := scope.Validate(); err != nil {
		return Incident{}, false, err
	}
	if id == "" {
		return Incident{}, false, errors.New("an Incident identifier is required")
	}
	return service.store.GetIncident(ctx, scope, id)
}

func (service *Service) Acknowledge(ctx context.Context, scope Scope, id, actorID string) (Incident, bool, error) {
	if err := scope.Validate(); err != nil {
		return Incident{}, false, err
	}
	if id == "" || actorID == "" {
		return Incident{}, false, errors.New("Incident and actor identity are required")
	}
	now := service.clock.Now().UTC()
	timelineID, err := service.uuids.NewUUIDv7(now)
	if err != nil {
		return Incident{}, false, err
	}
	return service.store.AcknowledgeIncident(ctx, scope, id, actorID, timelineID, now)
}

func validateHealthTransition(event HealthTransitionedV1) error {
	if event.EventID == "" || event.OrganizationID == "" || event.TransitionID == "" ||
		event.MonitorID == "" || event.ProjectID == "" || event.PolicyVersion == "" {
		return ErrPayloadInvalid
	}
	if event.AggregateType != "monitorHealth" || event.AggregateID != event.MonitorID ||
		event.AggregateVersion < 1 || event.OldState == event.NewState || !isUTC(event.OccurredAt) {
		return ErrPayloadInvalid
	}
	switch event.OldState {
	case "unknown", "healthy", "degraded", "down":
	default:
		return ErrPayloadInvalid
	}
	switch event.NewState {
	case "unknown", "healthy", "degraded", "down":
	default:
		return ErrPayloadInvalid
	}
	return nil
}

func isUTC(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	_, offset := value.Zone()
	return offset == 0
}
