package maintenance

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"
)

var testScope = Scope{
	OrganizationID: "00000000-0000-7000-8000-000000000001",
	ProjectID:      "00000000-0000-7000-8000-000000000002",
	MonitorID:      "00000000-0000-7000-8000-000000000003",
}

func TestWindowRequiresIdentityAndMonitorScope(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	windowID := ID("00000000-0000-7000-8000-000000000010")
	tests := []struct {
		name  string
		id    ID
		scope Scope
	}{
		{name: "missing window id", scope: testScope},
		{
			name: "missing Organization id", id: windowID,
			scope: Scope{ProjectID: testScope.ProjectID, MonitorID: testScope.MonitorID},
		},
		{
			name: "missing Project id", id: windowID,
			scope: Scope{OrganizationID: testScope.OrganizationID, MonitorID: testScope.MonitorID},
		},
		{
			name: "missing Monitor id", id: windowID,
			scope: Scope{OrganizationID: testScope.OrganizationID, ProjectID: testScope.ProjectID},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewWindow(test.id, test.scope, now, now.Add(time.Hour), now)
			if err == nil {
				t.Fatal("NewWindow() succeeded without complete identity and Monitor scope")
			}
		})
	}
}

func TestCreateValidatesUTCAndBoundedHalfOpenInterval(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		starts time.Time
		ends   time.Time
		codes  []string
	}{
		{
			name:  "missing",
			codes: []string{StartsAtInvalidCode, EndsAtInvalidCode},
		},
		{
			name:   "non UTC",
			starts: now.Add(time.Hour).In(time.FixedZone("CST", 8*60*60)),
			ends:   now.Add(2 * time.Hour).In(time.FixedZone("CST", 8*60*60)),
			codes:  []string{StartsAtInvalidCode, EndsAtInvalidCode},
		},
		{
			name:   "past start",
			starts: now.Add(-time.Nanosecond),
			ends:   now.Add(time.Hour),
			codes:  []string{StartsAtInvalidCode},
		},
		{
			name:   "empty half open interval",
			starts: now,
			ends:   now,
			codes:  []string{EndsAtInvalidCode},
		},
		{
			name:   "duration exceeds bound",
			starts: now,
			ends:   now.Add(MaxDuration + time.Nanosecond),
			codes:  []string{DurationInvalidCode},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(newMemoryStore(), fixedClock{now}, &sequenceIDs{})
			result, err := service.Create(t.Context(), CreateCommand{
				Scope: testScope, StartsAt: test.starts, EndsAt: test.ends,
			})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if result.Kind != CreateInvalid {
				t.Fatalf("Create() kind = %d, want invalid", result.Kind)
			}
			var codes []string
			for _, failure := range result.Failures {
				codes = append(codes, failure.Code)
			}
			if !equalStrings(codes, test.codes) {
				t.Fatalf("Create() codes = %v, want %v", codes, test.codes)
			}
		})
	}
}

func TestWindowStatusUsesHalfOpenBounds(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	window, err := NewWindow(
		"00000000-0000-7000-8000-000000000010",
		testScope,
		now.Add(time.Hour),
		now.Add(2*time.Hour),
		now,
	)
	if err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		at   time.Time
		want Status
	}{
		{now, StatusUpcoming},
		{window.StartsAt, StatusActive},
		{window.EndsAt.Add(-time.Nanosecond), StatusActive},
		{window.EndsAt, StatusEnded},
	}
	for _, check := range checks {
		if got := window.Status(check.at); got != check.want {
			t.Errorf("Status(%s) = %q, want %q", check.at, got, check.want)
		}
	}
}

func TestWindowAppliesAtRetainsHistoricalCancellationSemantics(t *testing.T) {
	now := time.Date(2026, time.August, 11, 10, 0, 0, 0, time.UTC)
	window, err := NewWindow(
		"window", testScope, now.Add(time.Hour), now.Add(2*time.Hour), now,
	)
	if err != nil {
		t.Fatal(err)
	}
	cancelledAt := now.Add(90 * time.Minute)
	if err := window.Cancel(cancelledAt); err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		at   time.Time
		want bool
	}{
		{window.StartsAt.Add(-time.Nanosecond), false},
		{window.StartsAt, true},
		{cancelledAt.Add(-time.Nanosecond), true},
		{cancelledAt, false},
		{window.EndsAt, false},
		{window.StartsAt.In(time.FixedZone("UTC+08:00", 8*60*60)), false},
	}
	for _, check := range checks {
		if got := window.AppliesAt(check.at); got != check.want {
			t.Errorf("AppliesAt(%s) = %v, want %v", check.at, got, check.want)
		}
	}
}

func TestCreateRejectsOverlapAndAllowsAdjacentOrCancelledIntervals(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	clock := &mutableClock{value: now}
	store := newMemoryStore()
	service := NewService(store, clock, &sequenceIDs{})

	first := createWindow(t, service, now.Add(time.Hour), now.Add(2*time.Hour))
	overlap, err := service.Create(t.Context(), CreateCommand{
		Scope: testScope, StartsAt: first.StartsAt.Add(30 * time.Minute), EndsAt: first.EndsAt.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if overlap.Kind != CreateConflict || overlap.Code != OverlapCode {
		t.Fatalf("overlap result = %#v", overlap)
	}

	adjacent := createWindow(t, service, first.EndsAt, first.EndsAt.Add(time.Hour))
	cancelled, err := service.Cancel(t.Context(), testScope, first.ID)
	if err != nil || cancelled.Kind != CancelCancelled || cancelled.Window.CancelledAt == nil {
		t.Fatalf("Cancel() result/error = %#v/%v", cancelled, err)
	}
	if cancelled.Window.Status(now) != StatusCancelled {
		t.Fatalf("cancelled Status() = %q", cancelled.Window.Status(now))
	}
	replacement := createWindow(t, service, first.StartsAt, first.EndsAt)
	if replacement.ID == first.ID || adjacent.ID == first.ID {
		t.Fatal("UUID generator reused a maintenance window id")
	}

	listed, found, err := service.List(t.Context(), testScope)
	if err != nil || !found {
		t.Fatalf("List() found/error = %v/%v", found, err)
	}
	if len(listed) != 3 {
		t.Fatalf("List() length = %d, want cancelled, replacement, and adjacent", len(listed))
	}
	if listed[0].ID != first.ID || listed[0].CancelledAt == nil {
		t.Fatalf("List()[0] = %#v, want retained cancelled window", listed[0])
	}
	clock.value = first.EndsAt
	listed, found, err = service.List(t.Context(), testScope)
	if err != nil || !found {
		t.Fatalf("List() after end found/error = %v/%v", found, err)
	}
	if len(listed) != 1 || listed[0].ID != adjacent.ID {
		t.Fatalf("List() after end = %#v, want only adjacent active window", listed)
	}

}

func TestCancelIsIdempotentAndRejectsEndedWindow(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	clock := &mutableClock{value: now}
	service := NewService(newMemoryStore(), clock, &sequenceIDs{})
	window := createWindow(t, service, now.Add(time.Hour), now.Add(2*time.Hour))

	first, err := service.Cancel(t.Context(), testScope, window.ID)
	if err != nil || first.Kind != CancelCancelled {
		t.Fatalf("first Cancel() result/error = %#v/%v", first, err)
	}
	second, err := service.Cancel(t.Context(), testScope, window.ID)
	if err != nil || second.Kind != CancelCancelled {
		t.Fatalf("second Cancel() result/error = %#v/%v", second, err)
	}
	if !first.Window.CancelledAt.Equal(*second.Window.CancelledAt) {
		t.Fatalf("repeated cancellation changed instant: %v then %v", first.Window.CancelledAt, second.Window.CancelledAt)
	}

	ended := createWindow(t, service, now.Add(3*time.Hour), now.Add(4*time.Hour))
	clock.value = ended.EndsAt
	result, err := service.Cancel(t.Context(), testScope, ended.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != CancelConflict || result.Code != WindowEndedCode {
		t.Fatalf("ended Cancel() = %#v", result)
	}
}

func createWindow(t *testing.T, service *Service, startsAt, endsAt time.Time) Window {
	t.Helper()
	result, err := service.Create(t.Context(), CreateCommand{
		Scope: testScope, StartsAt: startsAt, EndsAt: endsAt,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Kind != CreateCreated {
		t.Fatalf("Create() result = %#v", result)
	}
	return result.Window
}

type fixedClock struct{ value time.Time }

func (clock fixedClock) Now() time.Time { return clock.value }

type mutableClock struct{ value time.Time }

func (clock *mutableClock) Now() time.Time { return clock.value }

type sequenceIDs struct {
	mu   sync.Mutex
	next int
}

func (generator *sequenceIDs) NewUUIDv7(time.Time) (string, error) {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	generator.next++
	return "00000000-0000-7000-8000-0000000000" + string(rune('0'+generator.next)), nil
}

type memoryStore struct {
	mu      sync.Mutex
	windows map[ID]Window
}

func newMemoryStore() *memoryStore {
	return &memoryStore{windows: make(map[ID]Window)}
}

func (store *memoryStore) CreateWindow(_ context.Context, value Window) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, existing := range store.windows {
		if existing.Scope() == value.Scope() && existing.CancelledAt == nil &&
			existing.StartsAt.Before(value.EndsAt) && existing.EndsAt.After(value.StartsAt) {
			return ErrOverlap
		}
	}
	value.Version = 1
	store.windows[value.ID] = value
	return nil
}

func (store *memoryStore) ListWindows(
	_ context.Context, scope Scope, endsAfter time.Time,
) ([]Window, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var values []Window
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

func (store *memoryStore) FindWindow(
	_ context.Context, scope Scope, id ID,
) (Window, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, found := store.windows[id]
	if !found || value.Scope() != scope {
		return Window{}, false, nil
	}
	return value, true, nil
}

func (store *memoryStore) CancelWindow(
	_ context.Context, value Window, expectedVersion uint32,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	current, found := store.windows[value.ID]
	if !found || current.Version != expectedVersion {
		return ErrConcurrentUpdate
	}
	value.Version = current.Version + 1
	store.windows[value.ID] = value
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
