package httpapi

import (
	"context"
	"sync"

	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/statuspage"
)

type memoryStatusPageStore struct {
	mu       sync.Mutex
	monitors *memoryMonitorStore
	draft    statuspage.Draft
	found    bool
}

func (store *memoryStatusPageStore) FindDraft(
	_ context.Context, organizationID string,
) (statuspage.Draft, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.found || store.draft.OrganizationID != organizationID {
		return statuspage.Draft{}, false, nil
	}
	return store.draft, true, nil
}

func (store *memoryStatusPageStore) ReplaceDraft(
	ctx context.Context, draft statuspage.Draft, expectedVersion int64,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if (!store.found && expectedVersion != 0) ||
		(store.found && store.draft.Version != expectedVersion) {
		return statuspage.ErrConcurrentUpdate
	}
	for _, component := range draft.Components {
		store.monitors.mu.Lock()
		available := false
		for _, value := range store.monitors.monitors {
			if string(value.ID) == component.MonitorID &&
				value.OrganizationID == draft.OrganizationID && value.State != monitor.StateArchived {
				available = true
				break
			}
		}
		store.monitors.mu.Unlock()
		if !available {
			return statuspage.ErrMonitorUnavailable
		}
	}
	store.draft, store.found = draft, true
	return nil
}
