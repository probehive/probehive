// Package maintenance owns one-time Monitor maintenance-window policy and use cases.
package maintenance

import (
	"errors"
	"time"
)

const (
	StartsAtInvalidCode  = "maintenance.startsAt.invalid"
	EndsAtInvalidCode    = "maintenance.endsAt.invalid"
	DurationInvalidCode  = "maintenance.duration.invalid"
	OverlapCode          = "maintenance.overlap"
	WindowEndedCode      = "maintenance.window.ended"
	ConcurrentUpdateCode = "maintenance.concurrentUpdate"
)

const (
	StartsAtValidationMessage = "The start must be an explicit UTC instant that is not in the past."
	EndsAtValidationMessage   = "The end must be an explicit UTC instant after the start."
	DurationValidationMessage = "A maintenance window cannot be longer than 30 days."
	OverlapDetail             = "The maintenance window overlaps another current or upcoming window for this Monitor."
	WindowEndedDetail         = "An ended maintenance window cannot be cancelled."
	ConcurrentUpdateDetail    = "The maintenance window was modified concurrently; retry against its current state."
	CreateRejectedTitle       = "Maintenance window rejected"
	CancelRejectedTitle       = "Maintenance cancellation rejected"
)

const MaxDuration = 30 * 24 * time.Hour

var (
	ErrMonitorNotFound  = errors.New("maintenance Monitor not found")
	ErrOverlap          = errors.New("maintenance window overlaps an existing window")
	ErrConcurrentUpdate = errors.New("maintenance window modified concurrently")
	ErrWindowEnded      = errors.New(WindowEndedDetail)
)

type ID string

type Scope struct {
	OrganizationID string
	ProjectID      string
	MonitorID      string
}

func (scope Scope) valid() bool {
	return scope.OrganizationID != "" && scope.ProjectID != "" && scope.MonitorID != ""
}

type Status string

const (
	StatusUpcoming  Status = "upcoming"
	StatusActive    Status = "active"
	StatusEnded     Status = "ended"
	StatusCancelled Status = "cancelled"
)

// Window is a durable one-time half-open maintenance interval [StartsAt, EndsAt).
type Window struct {
	ID             ID
	OrganizationID string
	ProjectID      string
	MonitorID      string
	StartsAt       time.Time
	EndsAt         time.Time
	CreatedAt      time.Time
	CancelledAt    *time.Time
	// Version is the PostgreSQL xmin value used only for optimistic persistence.
	Version uint32
}

func NewWindow(id ID, scope Scope, startsAt, endsAt, createdAt time.Time) (Window, error) {
	return RestoreWindow(id, scope, startsAt, endsAt, createdAt, nil, 0)
}

func RestoreWindow(
	id ID,
	scope Scope,
	startsAt, endsAt, createdAt time.Time,
	cancelledAt *time.Time,
	version uint32,
) (Window, error) {
	if id == "" || !scope.valid() {
		return Window{}, errors.New("a maintenance window requires identity and Monitor scope")
	}
	if !isUTC(startsAt) || !isUTC(endsAt) || !isUTC(createdAt) {
		return Window{}, errors.New("persisted maintenance timestamps must be UTC")
	}
	if endsAt.Sub(startsAt) <= 0 || endsAt.Sub(startsAt) > MaxDuration {
		return Window{}, errors.New("invalid maintenance time bounds")
	}
	if createdAt.After(startsAt) {
		return Window{}, errors.New("maintenance cannot begin before it is created")
	}
	var restoredCancellation *time.Time
	if cancelledAt != nil {
		if !isUTC(*cancelledAt) || cancelledAt.Before(createdAt) || !cancelledAt.Before(endsAt) {
			return Window{}, errors.New("invalid maintenance cancellation instant")
		}
		value := cancelledAt.UTC()
		restoredCancellation = &value
	}
	return Window{
		ID: id, OrganizationID: scope.OrganizationID, ProjectID: scope.ProjectID,
		MonitorID: scope.MonitorID, StartsAt: startsAt.UTC(), EndsAt: endsAt.UTC(),
		CreatedAt: createdAt.UTC(), CancelledAt: restoredCancellation, Version: version,
	}, nil
}

func (value Window) Scope() Scope {
	return Scope{
		OrganizationID: value.OrganizationID,
		ProjectID:      value.ProjectID,
		MonitorID:      value.MonitorID,
	}
}

func (value Window) Status(now time.Time) Status {
	if value.CancelledAt != nil {
		return StatusCancelled
	}
	if now.Before(value.StartsAt) {
		return StatusUpcoming
	}
	if now.Before(value.EndsAt) {
		return StatusActive
	}
	return StatusEnded
}

// Cancel retains the window and records when it stopped applying. Repeated cancellation is idempotent.
func (value *Window) Cancel(at time.Time) error {
	if value.CancelledAt != nil {
		return nil
	}
	if !isUTC(at) || at.Before(value.CreatedAt) {
		return errors.New("invalid maintenance cancellation instant")
	}
	if !at.Before(value.EndsAt) {
		return ErrWindowEnded
	}
	cancelledAt := at.UTC()
	value.CancelledAt = &cancelledAt
	return nil
}

func explicitUTC(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	_, offset := value.Zone()
	return offset == 0
}

func isUTC(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC
}
