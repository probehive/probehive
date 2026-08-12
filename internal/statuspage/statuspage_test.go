package statuspage

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testClock struct{ now time.Time }

func (clock testClock) Now() time.Time { return clock.now }

type sequenceUUIDs struct{ values []string }

func (generator *sequenceUUIDs) NewUUIDv7(time.Time) (string, error) {
	value := generator.values[0]
	generator.values = generator.values[1:]
	return value, nil
}

type memoryStore struct {
	draft statuspageDraft
	found bool
	err   error
}
type statuspageDraft = Draft

func (store *memoryStore) FindDraft(context.Context, string) (Draft, bool, error) {
	return store.draft, store.found, nil
}
func (store *memoryStore) ReplaceDraft(_ context.Context, draft Draft, _ int64) error {
	if store.err != nil {
		return store.err
	}
	store.draft, store.found = draft, true
	return nil
}

func TestReplaceValidatesThenCreatesAnOrderedPrivateDraft(t *testing.T) {
	now := time.Date(2026, 8, 12, 1, 2, 3, 0, time.UTC)
	store := &memoryStore{}
	service := NewService(store, testClock{now}, &sequenceUUIDs{values: []string{
		"00000000-0000-7000-8000-000000000001",
		"00000000-0000-7000-8000-000000000002",
		"00000000-0000-7000-8000-000000000003",
	}})
	result, err := service.Replace(t.Context(), ReplaceCommand{
		OrganizationID: "organization", Title: "  Service Status  ",
		Components: []ComponentInput{
			{MonitorID: "monitor-b", Label: " API "},
			{MonitorID: "monitor-a", Label: "Website"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != ReplaceUpdated || result.Draft.Title != "Service Status" ||
		result.Draft.Version != 1 || len(result.Draft.Components) != 2 {
		t.Fatalf("Replace() = %#v", result)
	}
	if result.Draft.Components[0].MonitorID != "monitor-b" ||
		result.Draft.Components[0].Label != "API" || result.Draft.Components[0].Position != 0 ||
		result.Draft.Components[1].MonitorID != "monitor-a" || result.Draft.Components[1].Position != 1 {
		t.Fatalf("components = %#v", result.Draft.Components)
	}
}

func TestReplacePreservesComponentIdentityAndRejectsInvalidInput(t *testing.T) {
	now := time.Date(2026, 8, 12, 1, 2, 3, 0, time.UTC)
	existing, err := RestoreDraft(
		"page", "organization", "Status", 3, now, now,
		[]Component{
			{ID: "component-a", MonitorID: "monitor-a", Label: "A", Position: 0},
			{ID: "component-b", MonitorID: "monitor-b", Label: "B", Position: 1},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{draft: existing, found: true}
	service := NewService(store, testClock{now.Add(time.Minute)}, &sequenceUUIDs{values: []string{
		"component-c",
	}})
	updated, err := service.Replace(t.Context(), ReplaceCommand{
		OrganizationID: "organization", Title: "Status", Version: 3,
		Components: []ComponentInput{
			{MonitorID: "monitor-b", Label: "B renamed"},
			{MonitorID: "monitor-c", Label: "C"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Draft.Components[0].ID != "component-b" ||
		updated.Draft.Components[1].ID != "component-c" || updated.Draft.Version != 4 {
		t.Fatalf("updated draft = %#v", updated.Draft)
	}

	invalid, err := service.Replace(t.Context(), ReplaceCommand{
		OrganizationID: "organization", Version: -1,
		Components: []ComponentInput{
			{MonitorID: "monitor-a", Label: ""},
			{MonitorID: "monitor-a", Label: "A"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCodes := []string{
		TitleInvalidCode, ComponentLabelInvalidCode,
		ComponentMonitorDuplicateCode, ConcurrentUpdateCode,
	}
	if invalid.Kind != ReplaceInvalid || len(invalid.Failures) != len(wantCodes) {
		t.Fatalf("invalid result = %#v", invalid)
	}
	for index, code := range wantCodes {
		if invalid.Failures[index].Code != code {
			t.Fatalf("failure %d = %#v, want %s", index, invalid.Failures[index], code)
		}
	}
}

func TestReplaceMapsUnavailableAndConcurrentStoreResults(t *testing.T) {
	now := time.Date(2026, 8, 12, 1, 2, 3, 0, time.UTC)
	command := ReplaceCommand{
		OrganizationID: "organization", Title: "Status",
		Components: []ComponentInput{{MonitorID: "monitor", Label: "API"}},
	}
	for _, test := range []struct {
		name string
		err  error
		kind ReplaceKind
		code string
	}{
		{"unavailable", ErrMonitorUnavailable, ReplaceInvalid, MonitorUnavailableCode},
		{"concurrent", ErrConcurrentUpdate, ReplaceConflict, ConcurrentUpdateCode},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(&memoryStore{err: test.err}, testClock{now}, &sequenceUUIDs{values: []string{"page", "component"}})
			result, err := service.Replace(t.Context(), command)
			if err != nil {
				t.Fatal(err)
			}
			if result.Kind != test.kind {
				t.Fatalf("kind = %v, want %v", result.Kind, test.kind)
			}
			if test.kind == ReplaceInvalid {
				if len(result.Failures) != 1 || result.Failures[0].Code != test.code {
					t.Fatalf("failures = %#v", result.Failures)
				}
			} else if result.Code != test.code {
				t.Fatalf("code = %q, want %q", result.Code, test.code)
			}
		})
	}
	if !errors.Is(ErrConcurrentUpdate, ErrConcurrentUpdate) {
		t.Fatal("sentinel mismatch")
	}
}
