package httpapi

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/probehive/probehive/internal/health"
	"github.com/probehive/probehive/internal/incident"
	"github.com/probehive/probehive/internal/maintenance"
	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/run"
)

type memoryHealthStore struct {
	mu        sync.Mutex
	monitors  *memoryMonitorStore
	snapshots map[string]health.Snapshot
}

func (store *memoryHealthStore) ProcessRunRecorded(
	context.Context, health.RunRecordedV1, health.ProcessIDs, time.Time,
) error {
	return nil
}

func (store *memoryHealthStore) ListStaleHealth(
	context.Context, time.Time, time.Duration, int,
) ([]health.StaleTarget, error) {
	return nil, nil
}

func (store *memoryHealthStore) MarkHealthStale(
	context.Context, health.StaleTarget, string, string, time.Time, time.Duration,
) error {
	return nil
}

func (store *memoryHealthStore) GetHealth(
	ctx context.Context, scope health.Scope,
) (health.Snapshot, bool, error) {
	store.mu.Lock()
	if store.snapshots != nil {
		if value, found := store.snapshots[scope.OrganizationID+"/"+scope.ProjectID+"/"+scope.MonitorID]; found {
			store.mu.Unlock()
			return value, true, nil
		}
	}
	store.mu.Unlock()
	value, found, err := store.monitors.FindMonitor(ctx, monitor.Scope{
		OrganizationID: scope.OrganizationID,
		ProjectID:      scope.ProjectID,
		MonitorID:      monitor.ID(scope.MonitorID),
	})
	if err != nil || !found {
		return health.Snapshot{}, found, err
	}
	snapshot, err := health.Initial(scope.OrganizationID, scope.ProjectID, scope.MonitorID, value.CreatedAt)
	return snapshot, err == nil, err
}

type memoryIncidentStore struct {
	mu          sync.Mutex
	monitors    *memoryMonitorStore
	incidents   map[string]incident.Incident
	health      *memoryHealthStore
	maintenance *memoryMaintenanceStore
	runs        *memoryRunStore
}

func (store *memoryIncidentStore) ProcessHealthTransition(
	context.Context, incident.HealthTransitionedV1, incident.ProcessIDs, time.Time,
) error {
	return nil
}

func (store *memoryIncidentStore) ListIncidents(
	ctx context.Context, scope incident.Scope, query incident.ListQuery,
) ([]incident.Incident, bool, bool, error) {
	_, found, err := store.monitors.FindMonitor(ctx, monitor.Scope{
		OrganizationID: scope.OrganizationID, ProjectID: scope.ProjectID,
		MonitorID: monitor.ID(scope.MonitorID),
	})
	if err != nil || !found {
		return nil, false, found, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	values := make([]incident.Incident, 0)
	for _, value := range store.incidents {
		if value.OrganizationID == scope.OrganizationID && value.ProjectID == scope.ProjectID &&
			value.MonitorID == scope.MonitorID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID > values[j].ID
		}
		return values[i].CreatedAt.After(values[j].CreatedAt)
	})
	if query.Cursor != nil {
		filtered := values[:0]
		for _, value := range values {
			if value.CreatedAt.Before(query.Cursor.CreatedAt) ||
				(value.CreatedAt.Equal(query.Cursor.CreatedAt) && value.ID < query.Cursor.ID) {
				filtered = append(filtered, value)
			}
		}
		values = filtered
	}
	more := len(values) > query.PageSize
	if more {
		values = values[:query.PageSize]
	}
	return values, more, true, nil
}

func (store *memoryIncidentStore) GetIncident(
	_ context.Context, scope incident.Scope, id string,
) (incident.Incident, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, found := store.incidents[id]
	if !found || value.OrganizationID != scope.OrganizationID ||
		value.ProjectID != scope.ProjectID || value.MonitorID != scope.MonitorID {
		return incident.Incident{}, false, nil
	}
	return value, true, nil
}

func (store *memoryIncidentStore) AcknowledgeIncident(
	ctx context.Context, scope incident.Scope, id, actorID, timelineID string, now time.Time,
) (incident.Incident, bool, error) {
	store.mu.Lock()
	value, found := store.incidents[id]
	if !found || value.OrganizationID != scope.OrganizationID ||
		value.ProjectID != scope.ProjectID || value.MonitorID != scope.MonitorID {
		store.mu.Unlock()
		return incident.Incident{}, false, nil
	}
	if value.State == incident.StateResolved {
		store.mu.Unlock()
		return incident.Incident{}, true, incident.ErrConflict
	}
	if value.State == incident.StateOpen {
		value.State = incident.StateAcknowledged
		value.Version++
		value.AcknowledgedBy, value.AcknowledgedAt, value.UpdatedAt = actorID, now, now
		value.Timeline = append(value.Timeline, incident.TimelineEntry{
			ID: timelineID, IncidentID: id, IncidentVersion: value.Version,
			Kind: incident.TimelineAcknowledged, ActorUserID: actorID, OccurredAt: now,
		})
		store.incidents[id] = value
	}
	store.mu.Unlock()
	return store.GetIncident(ctx, scope, id)
}

func (store *memoryIncidentStore) ListInbox(
	ctx context.Context, organizationID string, query incident.InboxQuery, now time.Time,
) ([]incident.InboxItem, bool, bool, error) {
	store.mu.Lock()
	values := make([]incident.Incident, 0)
	for _, value := range store.incidents {
		if value.OrganizationID != organizationID {
			continue
		}
		if query.State == incident.InboxStateActive &&
			value.State == incident.StateResolved {
			continue
		}
		if query.State != "" && query.State != incident.InboxStateActive &&
			string(value.State) != string(query.State) {
			continue
		}
		values = append(values, value)
	}
	store.mu.Unlock()
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID > values[j].ID
		}
		return values[i].CreatedAt.After(values[j].CreatedAt)
	})
	if query.Cursor != nil {
		filtered := values[:0]
		for _, value := range values {
			if value.CreatedAt.Before(query.Cursor.CreatedAt) ||
				(value.CreatedAt.Equal(query.Cursor.CreatedAt) && value.ID < query.Cursor.ID) {
				filtered = append(filtered, value)
			}
		}
		values = filtered
	}
	items := make([]incident.InboxItem, 0, len(values))
	for _, value := range values {
		monitorValue, found, err := store.monitors.FindMonitor(ctx, monitor.Scope{
			OrganizationID: value.OrganizationID, ProjectID: value.ProjectID,
			MonitorID: monitor.ID(value.MonitorID),
		})
		if err != nil {
			return nil, false, false, err
		}
		if !found {
			continue
		}
		item := incident.InboxItem{
			Incident: value, MonitorName: monitorValue.Name, MonitorState: string(monitorValue.State),
		}
		if store.health != nil {
			snapshot, healthFound, err := store.health.GetHealth(ctx, health.Scope{
				OrganizationID: value.OrganizationID, ProjectID: value.ProjectID,
				MonitorID: string(value.MonitorID),
			})
			if err != nil {
				return nil, false, false, err
			}
			if healthFound {
				item.Health = &incident.InboxHealth{State: string(snapshot.State), UpdatedAt: snapshot.UpdatedAt}
			}
		}
		if store.maintenance != nil {
			windows, _, err := store.maintenance.ListWindows(ctx, maintenance.Scope{
				OrganizationID: value.OrganizationID, ProjectID: value.ProjectID,
				MonitorID: string(value.MonitorID),
			}, now)
			if err != nil {
				return nil, false, false, err
			}
			for _, window := range windows {
				status := window.Status(now)
				if status == maintenance.StatusActive || status == maintenance.StatusUpcoming {
					item.Maintenance = &incident.InboxMaintenance{
						ID: string(window.ID), State: string(status),
						StartsAt: window.StartsAt, EndsAt: window.EndsAt,
					}
					break
				}
			}
		}
		for _, entry := range value.Timeline {
			if entry.Kind != incident.TimelineOpened || entry.CausalRunID == "" {
				continue
			}
			available := true
			if store.runs != nil {
				_, available, err = store.runs.FindScopedRun(ctx, run.Scope{
					OrganizationID: value.OrganizationID, ProjectID: value.ProjectID,
					MonitorID: string(value.MonitorID),
				}, run.ID(entry.CausalRunID))
				if err != nil {
					return nil, false, false, err
				}
			}
			item.OpeningRun = &incident.InboxRun{
				ID: entry.CausalRunID, ScheduledFor: entry.CausalRunScheduledFor, Available: available,
			}
			break
		}
		items = append(items, item)
	}
	more := len(items) > query.PageSize
	if more {
		items = items[:query.PageSize]
	}
	return items, more, true, nil
}
