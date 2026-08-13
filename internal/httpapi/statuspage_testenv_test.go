package httpapi

import (
	"context"
	"sync"
	"time"

	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/statuspage"
)

type memoryStatusPageStore struct {
	mu         sync.Mutex
	monitors   *memoryMonitorStore
	draft      statuspage.Draft
	found      bool
	publicPage statuspage.PublicPage
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

func (store *memoryStatusPageStore) Publish(
	_ context.Context, organizationID string, publication statuspage.Publication,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.found || store.draft.OrganizationID != organizationID {
		return statuspage.ErrDraftMissing
	}
	if store.draft.Publication != nil {
		return statuspage.ErrAlreadyPublished
	}
	store.draft.Publication = &publication
	return nil
}

func (store *memoryStatusPageStore) Revoke(_ context.Context, organizationID string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.found && store.draft.OrganizationID == organizationID {
		store.draft.Publication = nil
	}
	return nil
}

func (store *memoryStatusPageStore) FindPublicPage(
	_ context.Context, tokenHash statuspage.TokenHash, _ time.Time,
) (statuspage.PublicPage, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.found || store.draft.Publication == nil || store.draft.Publication.TokenHash != tokenHash {
		return statuspage.PublicPage{}, false, nil
	}
	if store.publicPage.Title != "" {
		return store.publicPage, true, nil
	}
	components := make([]statuspage.PublicComponent, len(store.draft.Components))
	for index, component := range store.draft.Components {
		components[index] = statuspage.PublicComponent{Label: component.Label, State: "unknown", UpdatedAt: store.draft.UpdatedAt}
	}
	page, err := statuspage.RestorePublicPage(store.draft.Title, components)
	return page, err == nil, err
}
