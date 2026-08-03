// Package alert owns immutable notification intents and their query model.
package alert

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type Kind string

const (
	KindIncidentOpened   Kind = "incident.opened"
	KindIncidentResolved Kind = "incident.resolved"
)

var (
	ErrPayloadInvalid       = errors.New("invalid Alert event payload")
	ErrOrganizationMismatch = errors.New("Alert event Organization does not match its outbox row")
)

type Alert struct {
	ID              string
	OrganizationID  string
	ProjectID       string
	MonitorID       string
	IncidentID      string
	IncidentVersion int64
	Kind            Kind
	OccurredAt      time.Time
	CreatedAt       time.Time
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

func (scope Scope) Validate() error {
	if scope.OrganizationID == "" || scope.ProjectID == "" || scope.MonitorID == "" {
		return errors.New("an Alert query requires Organization, Project, and Monitor identity")
	}
	return nil
}

const MaxPageSize = 100

type Cursor struct {
	OccurredAt time.Time
	ID         string
}

type ListQuery struct {
	PageSize int
	Cursor   *Cursor
}

type Page struct {
	Alerts     []Alert
	NextCursor *Cursor
}

type Store interface {
	ProjectIncidentTransition(context.Context, IncidentTransitionedV1, Alert, IDGenerator) error
	ListAlerts(context.Context, Scope, ListQuery) ([]Alert, bool, bool, error)
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
		panic("alert.Service requires a store, clock, and UUID generator")
	}
	return &Service{store: store, clock: clock, uuids: uuids}
}

func (service *Service) HandleIncidentTransition(
	ctx context.Context, eventID, organizationID string, payload []byte,
) error {
	var event IncidentTransitionedV1
	if err := json.Unmarshal(payload, &event); err != nil {
		return ErrPayloadInvalid
	}
	kind, err := validateIncidentTransition(event)
	if err != nil || event.EventID != eventID {
		return ErrPayloadInvalid
	}
	if event.OrganizationID != organizationID {
		return ErrOrganizationMismatch
	}
	now := service.clock.Now().UTC()
	id, err := service.uuids.NewUUIDv7(now)
	if err != nil {
		return err
	}
	value := Alert{
		ID: id, OrganizationID: event.OrganizationID, ProjectID: event.ProjectID,
		MonitorID: event.MonitorID, IncidentID: event.IncidentID,
		IncidentVersion: event.AggregateVersion, Kind: kind,
		OccurredAt: event.OccurredAt.UTC(), CreatedAt: now,
	}
	return service.store.ProjectIncidentTransition(ctx, event, value, service.uuids)
}

func (service *Service) List(ctx context.Context, scope Scope, query ListQuery) (Page, bool, error) {
	if err := scope.Validate(); err != nil {
		return Page{}, false, err
	}
	if query.PageSize < 1 || query.PageSize > MaxPageSize {
		return Page{}, false, errors.New("an Alert page is 1 to 100 rows")
	}
	if query.Cursor != nil {
		if query.Cursor.ID == "" || !isUTC(query.Cursor.OccurredAt) {
			return Page{}, false, errors.New("an Alert cursor requires identity and a UTC occurrence instant")
		}
	}
	values, more, found, err := service.store.ListAlerts(ctx, scope, query)
	if err != nil || !found {
		return Page{}, found, err
	}
	page := Page{Alerts: values}
	if more && len(values) != 0 {
		last := values[len(values)-1]
		page.NextCursor = &Cursor{OccurredAt: last.OccurredAt, ID: last.ID}
	}
	return page, true, nil
}

func validateIncidentTransition(event IncidentTransitionedV1) (Kind, error) {
	if event.EventID == "" || event.OrganizationID == "" || event.IncidentID == "" ||
		event.ProjectID == "" || event.MonitorID == "" || event.CausationID == "" {
		return "", ErrPayloadInvalid
	}
	if event.AggregateType != "incident" || event.AggregateID != event.IncidentID ||
		event.AggregateVersion < 1 || !isUTC(event.OccurredAt) {
		return "", ErrPayloadInvalid
	}
	switch event.Transition {
	case "opened":
		return KindIncidentOpened, nil
	case "resolved":
		return KindIncidentResolved, nil
	default:
		return "", ErrPayloadInvalid
	}
}

func isUTC(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	_, offset := value.Zone()
	return offset == 0
}
