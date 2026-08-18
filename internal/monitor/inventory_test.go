package monitor

import (
	"context"
	"reflect"
	"testing"
	"time"
)

type fakeInventoryStore struct {
	projectExists bool
	items         []InventoryItem
	total         int
	query         InventoryQuery
	now           time.Time
}

func (store *fakeInventoryStore) ProjectExists(context.Context, string, string) (bool, error) {
	return store.projectExists, nil
}

func (store *fakeInventoryStore) ListMonitorInventory(
	_ context.Context, _, _ string, query InventoryQuery, now time.Time,
) ([]InventoryItem, int, error) {
	store.query = query
	store.now = now
	return store.items, store.total, nil
}

func TestInventoryListPreservesBoundedQueryAndBuildsPage(t *testing.T) {
	t.Parallel()
	now := testTime()
	query := InventoryQuery{
		Search: "API", State: StateActive, Health: InventoryHealthDown,
		RunOutcome: InventoryRunFailed, Maintenance: InventoryMaintenanceActive,
		Sort: InventorySortUpdatedAt, Direction: InventoryDirectionDescending,
		Page: 2, PageSize: 10,
	}
	items := []InventoryItem{{Monitor: draftMonitor(0)}}
	store := &fakeInventoryStore{projectExists: true, items: items, total: 11}
	page, found, err := NewInventoryService(store, fixedClock{value: now}).List(context.Background(), "org", "project", query)
	if err != nil || !found {
		t.Fatalf("List() found/error = %v/%v", found, err)
	}
	if !reflect.DeepEqual(page.Items, items) || page.Total != 11 || page.Page != 2 || page.PageSize != 10 {
		t.Fatalf("List() page = %#v", page)
	}
	if !reflect.DeepEqual(store.query, query) || !store.now.Equal(now) || store.now.Location() != time.UTC {
		t.Fatalf("store query/now = %#v/%v", store.query, store.now)
	}
}

func TestInventoryListRejectsInvalidQueriesBeforeStore(t *testing.T) {
	t.Parallel()
	valid := InventoryQuery{Sort: InventorySortName, Direction: InventoryDirectionAscending, Page: 1, PageSize: DefaultInventoryPageSize}
	tests := []struct {
		name   string
		mutate func(*InventoryQuery)
	}{
		{"unnormalized search", func(query *InventoryQuery) { query.Search = " API " }},
		{"lifecycle", func(query *InventoryQuery) { query.State = "running" }},
		{"health", func(query *InventoryQuery) { query.Health = "stale" }},
		{"Run", func(query *InventoryQuery) { query.RunOutcome = "unknown" }},
		{"maintenance", func(query *InventoryQuery) { query.Maintenance = "ended" }},
		{"sort", func(query *InventoryQuery) { query.Sort = "health" }},
		{"direction", func(query *InventoryQuery) { query.Direction = "forward" }},
		{"page", func(query *InventoryQuery) { query.Page = 0 }},
		{"page size", func(query *InventoryQuery) { query.PageSize = MaxInventoryPageSize + 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := valid
			test.mutate(&query)
			store := &fakeInventoryStore{projectExists: true}
			if _, _, err := NewInventoryService(store, fixedClock{value: testTime()}).List(context.Background(), "org", "project", query); err == nil {
				t.Fatal("List() unexpectedly accepted invalid query")
			}
			if !store.now.IsZero() {
				t.Fatal("invalid query reached the inventory store")
			}
		})
	}
}

func TestInventoryListReturnsEmptySliceAndHidesMissingProject(t *testing.T) {
	t.Parallel()
	query := InventoryQuery{Sort: InventorySortName, Direction: InventoryDirectionAscending, Page: 1, PageSize: DefaultInventoryPageSize}
	page, found, err := NewInventoryService(&fakeInventoryStore{projectExists: true}, fixedClock{value: testTime()}).List(context.Background(), "org", "project", query)
	if err != nil || !found || page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("empty List() = (%#v, %v, %v)", page, found, err)
	}
	_, found, err = NewInventoryService(&fakeInventoryStore{}, fixedClock{value: testTime()}).List(context.Background(), "org", "missing", query)
	if err != nil || found {
		t.Fatalf("missing List() found/error = %v/%v", found, err)
	}
}
