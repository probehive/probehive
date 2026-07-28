package run

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	// DefaultPageSize is the number of Runs returned when a caller does not choose a page
	// size. MaxPageSize is the public ceiling for one high-volume history query.
	DefaultPageSize = 50
	MaxPageSize     = 500
)

// Scope carries the complete ownership path required to query a Monitor's Runs.
type Scope struct {
	OrganizationID string
	ProjectID      string
	MonitorID      string
}

func (scope Scope) validate() error {
	if scope.OrganizationID == "" || scope.ProjectID == "" || scope.MonitorID == "" {
		return errors.New("a Run query requires Organization, Project, and Monitor identity")
	}
	return nil
}

// Cursor is the exclusive upper bound for the next page. The Run identifier is the
// tie-breaker for two locations or manual executions sharing one scheduled instant.
type Cursor struct {
	ScheduledFor time.Time
	ID           ID
}

func (cursor Cursor) validate() error {
	if cursor.ID == "" || !isUTC(cursor.ScheduledFor) {
		return errors.New("a Run cursor requires an identifier and UTC scheduled instant")
	}
	return nil
}

// ListQuery is a bounded, newest-first Run history query. NotBefore is inclusive; Cursor is
// exclusive. The optional filters use the same stored vocabulary as Run itself.
type ListQuery struct {
	NotBefore time.Time
	Cursor    *Cursor
	PageSize  int
	Outcome   Outcome
	Kind      Kind
	Location  string
}

func (query ListQuery) validate() error {
	if !isUTC(query.NotBefore) {
		return errors.New("a Run query requires a UTC lower time bound")
	}
	if query.Cursor != nil {
		if err := query.Cursor.validate(); err != nil {
			return err
		}
	}
	if query.PageSize < 1 || query.PageSize > MaxPageSize {
		return fmt.Errorf("a Run page is 1 to %d rows", MaxPageSize)
	}
	if query.Outcome != "" && query.Outcome != OutcomeSkipped && !validExecutionOutcome(query.Outcome) {
		return fmt.Errorf("unknown Run outcome filter %q", query.Outcome)
	}
	if query.Kind != "" && !validKind(query.Kind) {
		return fmt.Errorf("unknown Run kind filter %q", query.Kind)
	}
	if len(query.Location) > MaxLocationLength {
		return fmt.Errorf("a Probe Location filter is at most %d bytes", MaxLocationLength)
	}
	return nil
}

// Page is one bounded page of Run history.
type Page struct {
	Runs       []Run
	NextCursor *Cursor
}

// QueryStore is the persistence port consumed only by Run history queries. It stays
// separate from Store so read composition cannot accidentally acquire lease mutation.
type QueryStore interface {
	MonitorExists(context.Context, Scope) (bool, error)
	// ListRuns returns at most query.PageSize rows and whether another row follows them.
	ListRuns(context.Context, Scope, ListQuery) ([]Run, bool, error)
	FindScopedRun(context.Context, Scope, ID) (Run, bool, error)
	FindObservation(context.Context, string, ID, time.Time) (Observation, bool, error)
}

// QueryService owns the read-only Run and Observation use cases.
type QueryService struct {
	store QueryStore
}

func NewQueryService(store QueryStore) *QueryService {
	if store == nil {
		panic("run.QueryService requires a query store")
	}
	return &QueryService{store: store}
}

// List returns newest-first Run history, or found=false when the Monitor is outside the
// complete Organization and Project scope.
func (service *QueryService) List(
	ctx context.Context, scope Scope, query ListQuery,
) (Page, bool, error) {
	if err := scope.validate(); err != nil {
		return Page{}, false, err
	}
	if err := query.validate(); err != nil {
		return Page{}, false, err
	}
	exists, err := service.store.MonitorExists(ctx, scope)
	if err != nil || !exists {
		return Page{}, exists, err
	}
	values, more, err := service.store.ListRuns(ctx, scope, query)
	if err != nil {
		return Page{}, false, err
	}
	if values == nil {
		values = []Run{}
	}
	page := Page{Runs: values}
	if more {
		if len(values) == 0 {
			return Page{}, false, errors.New("Run query reported another page without returning a row")
		}
		last := values[len(values)-1]
		page.NextCursor = &Cursor{ScheduledFor: last.Slot.ScheduledFor, ID: last.ID}
	}
	return page, true, nil
}

// Get returns one Run only through its complete ownership scope.
func (service *QueryService) Get(
	ctx context.Context, scope Scope, id ID,
) (Run, bool, error) {
	if err := scope.validate(); err != nil {
		return Run{}, false, err
	}
	if id == "" {
		return Run{}, false, errors.New("a Run query requires a Run identifier")
	}
	return service.store.FindScopedRun(ctx, scope, id)
}

// GetObservation returns the bounded detail of one completed execution. In-flight and
// skipped Runs correctly have no Observation. A completed execution without one is corrupt:
// completion writes both in one transaction, so hiding that condition as 404 would conceal
// a broken invariant.
func (service *QueryService) GetObservation(
	ctx context.Context, scope Scope, id ID,
) (Observation, bool, error) {
	value, found, err := service.Get(ctx, scope, id)
	if err != nil || !found {
		return Observation{}, found, err
	}
	if value.InFlight() || value.Outcome == OutcomeSkipped {
		return Observation{}, false, nil
	}
	observation, found, err := service.store.FindObservation(
		ctx, scope.OrganizationID, id, value.Slot.ScheduledFor,
	)
	if err != nil {
		return Observation{}, false, err
	}
	if !found {
		return Observation{}, false, errors.New("a completed Run has no Observation")
	}
	return observation, true, nil
}
