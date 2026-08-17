package httpapi

import (
	"context"
	"sync"

	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/overview"
)

type memoryOverviewStore struct {
	mu            sync.Mutex
	organizations *memoryOrganizationStore
	summaries     map[string]overview.Summary
}

func newMemoryOverviewStore(organizations *memoryOrganizationStore) *memoryOverviewStore {
	return &memoryOverviewStore{organizations: organizations, summaries: make(map[string]overview.Summary)}
}

func (store *memoryOverviewStore) GetOverview(
	_ context.Context, organizationID string, incidentLimit int,
) (overview.Summary, bool, error) {
	store.organizations.mu.Lock()
	_, organizationFound := store.organizations.byID[organization.ID(organizationID)]
	store.organizations.mu.Unlock()
	if !organizationFound {
		return overview.Summary{}, false, nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	value, found := store.summaries[organizationID]
	if !found {
		value.OrganizationID = organizationID
	}
	if value.ActiveIncidents == nil {
		value.ActiveIncidents = []overview.ActiveIncident{}
	}
	if len(value.ActiveIncidents) > incidentLimit {
		value.ActiveIncidents = value.ActiveIncidents[:incidentLimit]
	}
	value.ActiveIncidentsTruncated = value.Incidents.Active > len(value.ActiveIncidents)
	return value, true, nil
}

func (store *memoryOverviewStore) seed(value overview.Summary) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.summaries[value.OrganizationID] = value
}
