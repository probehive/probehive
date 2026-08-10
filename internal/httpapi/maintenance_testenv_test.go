package httpapi

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/probehive/probehive/internal/maintenance"
	"github.com/probehive/probehive/internal/monitor"
)

type memoryMaintenanceStore struct {
	mu       sync.Mutex
	monitors *memoryMonitorStore
	windows  map[maintenance.ID]maintenance.Window
}

func newMemoryMaintenanceStore(monitors *memoryMonitorStore) *memoryMaintenanceStore {
	return &memoryMaintenanceStore{
		monitors: monitors,
		windows:  make(map[maintenance.ID]maintenance.Window),
	}
}

func (store *memoryMaintenanceStore) monitorExists(
	ctx context.Context, scope maintenance.Scope,
) (bool, error) {
	_, found, err := store.monitors.FindMonitor(ctx, monitor.Scope{
		OrganizationID: scope.OrganizationID,
		ProjectID:      scope.ProjectID,
		MonitorID:      monitor.ID(scope.MonitorID),
	})
	return found, err
}

func (store *memoryMaintenanceStore) CreateWindow(
	ctx context.Context, value maintenance.Window,
) error {
	exists, err := store.monitorExists(ctx, value.Scope())
	if err != nil {
		return err
	}
	if !exists {
		return maintenance.ErrMonitorNotFound
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, existing := range store.windows {
		if existing.Scope() == value.Scope() && existing.CancelledAt == nil &&
			existing.StartsAt.Before(value.EndsAt) && existing.EndsAt.After(value.StartsAt) {
			return maintenance.ErrOverlap
		}
	}
	value.Version = 1
	store.windows[value.ID] = value
	return nil
}

func (store *memoryMaintenanceStore) ListWindows(
	ctx context.Context, scope maintenance.Scope, endsAfter time.Time,
) ([]maintenance.Window, bool, error) {
	exists, err := store.monitorExists(ctx, scope)
	if err != nil || !exists {
		return nil, exists, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	values := make([]maintenance.Window, 0)
	for _, value := range store.windows {
		if value.Scope() == scope && value.EndsAt.After(endsAfter) {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(left, right int) bool {
		if values[left].StartsAt.Equal(values[right].StartsAt) {
			return values[left].ID < values[right].ID
		}
		return values[left].StartsAt.Before(values[right].StartsAt)
	})
	return values, true, nil
}

func (store *memoryMaintenanceStore) FindWindow(
	ctx context.Context, scope maintenance.Scope, id maintenance.ID,
) (maintenance.Window, bool, error) {
	exists, err := store.monitorExists(ctx, scope)
	if err != nil || !exists {
		return maintenance.Window{}, false, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	value, found := store.windows[id]
	if !found || value.Scope() != scope {
		return maintenance.Window{}, false, nil
	}
	return value, true, nil
}

func (store *memoryMaintenanceStore) CancelWindow(
	_ context.Context, value maintenance.Window, expectedVersion uint32,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	current, found := store.windows[value.ID]
	if !found || current.Scope() != value.Scope() || current.Version != expectedVersion {
		return maintenance.ErrConcurrentUpdate
	}
	value.Version = current.Version + 1
	store.windows[value.ID] = value
	return nil
}
