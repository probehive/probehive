package httpapi

import (
	"context"
	"sort"
	"sync"

	"github.com/probehive/probehive/internal/alert"
	"github.com/probehive/probehive/internal/monitor"
)

type memoryAlertStore struct {
	mu       sync.Mutex
	monitors *memoryMonitorStore
	alerts   []alert.Alert
}

func (store *memoryAlertStore) ProjectIncidentTransition(
	context.Context, alert.IncidentTransitionedV1, alert.Alert,
) error {
	return nil
}

func (store *memoryAlertStore) ListAlerts(
	ctx context.Context, scope alert.Scope, query alert.ListQuery,
) ([]alert.Alert, bool, bool, error) {
	if _, found, err := store.monitors.FindMonitor(ctx, monitor.Scope{
		OrganizationID: scope.OrganizationID,
		ProjectID:      scope.ProjectID,
		MonitorID:      monitor.ID(scope.MonitorID),
	}); err != nil || !found {
		return nil, false, found, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	values := make([]alert.Alert, 0)
	for _, value := range store.alerts {
		if value.OrganizationID != scope.OrganizationID ||
			value.ProjectID != scope.ProjectID || value.MonitorID != scope.MonitorID {
			continue
		}
		if query.Cursor != nil {
			after := value.OccurredAt.Before(query.Cursor.OccurredAt) ||
				(value.OccurredAt.Equal(query.Cursor.OccurredAt) && value.ID < query.Cursor.ID)
			if !after {
				continue
			}
		}
		values = append(values, value)
	}
	sort.Slice(values, func(left, right int) bool {
		if values[left].OccurredAt.Equal(values[right].OccurredAt) {
			return values[left].ID > values[right].ID
		}
		return values[left].OccurredAt.After(values[right].OccurredAt)
	})
	more := len(values) > query.PageSize
	if more {
		values = values[:query.PageSize]
	}
	return values, more, true, nil
}
