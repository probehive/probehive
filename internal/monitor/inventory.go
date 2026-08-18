package monitor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultInventoryPageSize = 25
	MaxInventoryPageSize     = 100
	MaxInventoryPage         = 1000
	MaxInventorySearchLength = 100
)

type InventoryHealthFilter string

const (
	InventoryHealthNotEvaluated InventoryHealthFilter = "notEvaluated"
	InventoryHealthUnknown      InventoryHealthFilter = "unknown"
	InventoryHealthHealthy      InventoryHealthFilter = "healthy"
	InventoryHealthDegraded     InventoryHealthFilter = "degraded"
	InventoryHealthDown         InventoryHealthFilter = "down"
)

type InventoryRunFilter string

const (
	InventoryRunNotRun     InventoryRunFilter = "notRun"
	InventoryRunInProgress InventoryRunFilter = "inProgress"
	InventoryRunPassed     InventoryRunFilter = "passed"
	InventoryRunFailed     InventoryRunFilter = "failed"
	InventoryRunErrored    InventoryRunFilter = "errored"
	InventoryRunTimedOut   InventoryRunFilter = "timedout"
	InventoryRunCancelled  InventoryRunFilter = "cancelled"
	InventoryRunSkipped    InventoryRunFilter = "skipped"
)

type InventoryMaintenanceFilter string

const (
	InventoryMaintenanceNone     InventoryMaintenanceFilter = "none"
	InventoryMaintenanceUpcoming InventoryMaintenanceFilter = "upcoming"
	InventoryMaintenanceActive   InventoryMaintenanceFilter = "active"
)

type InventorySort string

const (
	InventorySortName      InventorySort = "name"
	InventorySortCreatedAt InventorySort = "createdAt"
	InventorySortUpdatedAt InventorySort = "updatedAt"
)

type InventoryDirection string

const (
	InventoryDirectionAscending  InventoryDirection = "asc"
	InventoryDirectionDescending InventoryDirection = "desc"
)

// InventoryQuery is a bounded Project-scoped Monitor workbench query. Every operational
// dimension remains separate so lifecycle, evaluated health, Run outcome, and maintenance
// are never inferred from one another.
type InventoryQuery struct {
	Search      string
	State       State
	Health      InventoryHealthFilter
	RunOutcome  InventoryRunFilter
	Maintenance InventoryMaintenanceFilter
	Sort        InventorySort
	Direction   InventoryDirection
	Page        int
	PageSize    int
}

func (query InventoryQuery) validate() error {
	if query.Search != strings.TrimSpace(query.Search) || utf16Length(query.Search) > MaxInventorySearchLength {
		return fmt.Errorf("a Monitor inventory search is at most %d characters after trimming", MaxInventorySearchLength)
	}
	if query.State != "" && !validState(query.State) {
		return fmt.Errorf("unknown Monitor lifecycle filter %q", query.State)
	}
	if query.Health != "" && !validInventoryHealth(query.Health) {
		return fmt.Errorf("unknown Monitor health filter %q", query.Health)
	}
	if query.RunOutcome != "" && !validInventoryRun(query.RunOutcome) {
		return fmt.Errorf("unknown Monitor Run filter %q", query.RunOutcome)
	}
	if query.Maintenance != "" && !validInventoryMaintenance(query.Maintenance) {
		return fmt.Errorf("unknown Monitor maintenance filter %q", query.Maintenance)
	}
	if !validInventorySort(query.Sort) {
		return fmt.Errorf("unknown Monitor inventory sort %q", query.Sort)
	}
	if query.Direction != InventoryDirectionAscending && query.Direction != InventoryDirectionDescending {
		return fmt.Errorf("unknown Monitor inventory direction %q", query.Direction)
	}
	if query.Page < 1 || query.Page > MaxInventoryPage {
		return fmt.Errorf("a Monitor inventory page is 1 to %d", MaxInventoryPage)
	}
	if query.PageSize < 1 || query.PageSize > MaxInventoryPageSize {
		return fmt.Errorf("a Monitor inventory page size is 1 to %d", MaxInventoryPageSize)
	}
	return nil
}

func NormalizeInventorySearch(value string) (string, bool) {
	normalized := strings.TrimSpace(value)
	return normalized, normalized != "" && utf16Length(normalized) <= MaxInventorySearchLength
}

func ValidInventoryHealth(value string) bool {
	return validInventoryHealth(InventoryHealthFilter(value))
}
func ValidInventoryRun(value string) bool { return validInventoryRun(InventoryRunFilter(value)) }
func ValidInventoryMaintenance(value string) bool {
	return validInventoryMaintenance(InventoryMaintenanceFilter(value))
}
func ValidInventorySort(value string) bool { return validInventorySort(InventorySort(value)) }

func validInventoryHealth(value InventoryHealthFilter) bool {
	switch value {
	case InventoryHealthNotEvaluated, InventoryHealthUnknown, InventoryHealthHealthy,
		InventoryHealthDegraded, InventoryHealthDown:
		return true
	default:
		return false
	}
}

func validInventoryRun(value InventoryRunFilter) bool {
	switch value {
	case InventoryRunNotRun, InventoryRunInProgress, InventoryRunPassed, InventoryRunFailed,
		InventoryRunErrored, InventoryRunTimedOut, InventoryRunCancelled, InventoryRunSkipped:
		return true
	default:
		return false
	}
}

func validInventoryMaintenance(value InventoryMaintenanceFilter) bool {
	switch value {
	case InventoryMaintenanceNone, InventoryMaintenanceUpcoming, InventoryMaintenanceActive:
		return true
	default:
		return false
	}
}

func validInventorySort(value InventorySort) bool {
	switch value {
	case InventorySortName, InventorySortCreatedAt, InventorySortUpdatedAt:
		return true
	default:
		return false
	}
}

type InventoryHealth struct {
	State     string
	UpdatedAt time.Time
}
type InventoryRun struct {
	ID           string
	Outcome      string
	ScheduledFor time.Time
}
type InventoryMaintenance struct {
	State    InventoryMaintenanceFilter
	WindowID string
	StartsAt time.Time
	EndsAt   time.Time
}
type InventoryItem struct {
	Monitor     Monitor
	Health      *InventoryHealth
	LastRun     *InventoryRun
	Maintenance InventoryMaintenance
}
type InventoryPage struct {
	Items    []InventoryItem
	Page     int
	PageSize int
	Total    int
}

type InventoryStore interface {
	ProjectExists(context.Context, string, string) (bool, error)
	ListMonitorInventory(context.Context, string, string, InventoryQuery, time.Time) ([]InventoryItem, int, error)
}
type InventoryService struct {
	store InventoryStore
	clock Clock
}

func NewInventoryService(store InventoryStore, clock Clock) *InventoryService {
	if store == nil || clock == nil {
		panic("monitor.InventoryService requires a store and clock")
	}
	return &InventoryService{store: store, clock: clock}
}

func (service *InventoryService) List(ctx context.Context, organizationID, projectID string, query InventoryQuery) (InventoryPage, bool, error) {
	if organizationID == "" || projectID == "" {
		return InventoryPage{}, false, errors.New("a Monitor inventory query requires Organization and Project identity")
	}
	if err := query.validate(); err != nil {
		return InventoryPage{}, false, err
	}
	exists, err := service.store.ProjectExists(ctx, organizationID, projectID)
	if err != nil || !exists {
		return InventoryPage{}, exists, err
	}
	items, total, err := service.store.ListMonitorInventory(ctx, organizationID, projectID, query, service.clock.Now().UTC())
	if err != nil {
		return InventoryPage{}, false, err
	}
	if total < 0 || total < len(items) {
		return InventoryPage{}, false, errors.New("Monitor inventory store returned an invalid total")
	}
	if items == nil {
		items = []InventoryItem{}
	}
	return InventoryPage{Items: items, Page: query.Page, PageSize: query.PageSize, Total: total}, true, nil
}
