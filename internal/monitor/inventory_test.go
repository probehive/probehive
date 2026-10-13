package monitor

import (
	"context"
	"reflect"
	"testing"
	"time"
)

type fakeInventoryStore struct {
	items []InventoryItem
	total int
	query InventoryQuery
	now   time.Time
}

func (store *fakeInventoryStore) ListMonitorInventory(
	_ context.Context, _ string, query InventoryQuery, now time.Time,
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
	store := &fakeInventoryStore{items: items, total: 11}
	page, err := NewInventoryService(store, fixedClock{value: now}).List(context.Background(), "org", query)
	if err != nil {
		t.Fatalf("List() error = %v", err)
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
			store := &fakeInventoryStore{}
			if _, err := NewInventoryService(store, fixedClock{value: testTime()}).List(context.Background(), "org", query); err == nil {
				t.Fatal("List() unexpectedly accepted invalid query")
			}
			if !store.now.IsZero() {
				t.Fatal("invalid query reached the inventory store")
			}
		})
	}
}

func TestInventoryListReturnsEmptySlice(t *testing.T) {
	t.Parallel()
	query := InventoryQuery{Sort: InventorySortName, Direction: InventoryDirectionAscending, Page: 1, PageSize: DefaultInventoryPageSize}
	page, err := NewInventoryService(&fakeInventoryStore{}, fixedClock{value: testTime()}).List(context.Background(), "org", query)
	if err != nil || page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("empty List() = (%#v, %v)", page, err)
	}
}
