package outbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryDelayIsDeterministicAndBounded(t *testing.T) {
	t.Parallel()
	previous := time.Duration(0)
	for attempt := 1; attempt <= 20; attempt++ {
		first := RetryDelay("00000000-0000-7000-8000-000000000001", attempt)
		second := RetryDelay("00000000-0000-7000-8000-000000000001", attempt)
		if first != second {
			t.Fatalf("attempt %d delay is not deterministic: %v != %v", attempt, first, second)
		}
		base := retryBaseForAttempt(attempt)
		if first < base || first > RetryCap || first > base+base/5 {
			t.Fatalf("attempt %d delay %v is outside [%v, %v]", attempt, first, base, base+base/5)
		}
		if first < previous && first != RetryCap {
			t.Fatalf("attempt %d delay %v is below prior %v", attempt, first, previous)
		}
		previous = first
	}
}

func TestDispatcherOutcomes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 28, 6, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		entry      Entry
		handler    Handler
		wantResult TickResult
		wantCode   string
		wantDead   bool
	}{
		{
			name:       "success",
			entry:      Entry{ID: "success", OrganizationID: "organization", Topic: "known", Attempts: 1},
			handler:    HandlerFunc(func(context.Context, Entry) error { return nil }),
			wantResult: TickResult{Claimed: 1, Succeeded: 1},
		},
		{
			name:       "unknown topic is permanent",
			entry:      Entry{ID: "unknown", OrganizationID: "organization", Topic: "unknown", Attempts: 1},
			handler:    HandlerFunc(func(context.Context, Entry) error { return nil }),
			wantResult: TickResult{Claimed: 1, DeadLettered: 1},
			wantCode:   CodeTopicUnknown, wantDead: true,
		},
		{
			name:       "handler retries",
			entry:      Entry{ID: "retry", OrganizationID: "organization", Topic: "known", Attempts: 1},
			handler:    HandlerFunc(func(context.Context, Entry) error { return errors.New("temporary") }),
			wantResult: TickResult{Claimed: 1, Retried: 1},
			wantCode:   CodeHandlerFailed,
		},
		{
			name:       "maximum attempts dead letters",
			entry:      Entry{ID: "exhausted", OrganizationID: "organization", Topic: "known", Attempts: MaxAttempts},
			handler:    HandlerFunc(func(context.Context, Entry) error { return errors.New("temporary") }),
			wantResult: TickResult{Claimed: 1, DeadLettered: 1},
			wantCode:   CodeHandlerFailed, wantDead: true,
		},
		{
			name:  "fresh version gap ignores ordinary attempt limit",
			entry: Entry{ID: "gap-new", OrganizationID: "organization", Topic: "known", Attempts: MaxAttempts},
			handler: HandlerFunc(func(context.Context, Entry) error {
				return Transient(CodeAggregateVersionGap)
			}),
			wantResult: TickResult{Claimed: 1, Retried: 1},
			wantCode:   CodeAggregateVersionGap,
		},
		{
			name: "expired version gap dead letters",
			entry: Entry{
				ID: "gap-old", OrganizationID: "organization", Topic: "known", Attempts: 50,
				GapFirstSeenAt: now.Add(-AggregateGapTimeout),
			},
			handler: HandlerFunc(func(context.Context, Entry) error {
				return Transient(CodeAggregateVersionGap)
			}),
			wantResult: TickResult{Claimed: 1, DeadLettered: 1},
			wantCode:   CodeAggregateVersionGap, wantDead: true,
		},
		{
			name:  "explicit permanent payload failure",
			entry: Entry{ID: "payload", OrganizationID: "organization", Topic: "known", Attempts: 1},
			handler: HandlerFunc(func(context.Context, Entry) error {
				return Permanent(CodePayloadInvalid)
			}),
			wantResult: TickResult{Claimed: 1, DeadLettered: 1},
			wantCode:   CodePayloadInvalid, wantDead: true,
		},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			store := &dispatcherStore{entries: []Entry{testCase.entry}}
			dispatcher, err := New(Config{
				Store:    store,
				Handlers: map[string]Handler{"known": testCase.handler},
				Clock:    testClock{now: now}, UUIDs: testIDs{},
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := dispatcher.Tick(context.Background(), now)
			if err != nil {
				t.Fatal(err)
			}
			if result != testCase.wantResult {
				t.Fatalf("Tick result = %#v, want %#v", result, testCase.wantResult)
			}
			if store.failureCode != testCase.wantCode || store.dead != testCase.wantDead {
				t.Fatalf("failure = %q dead=%v, want %q dead=%v",
					store.failureCode, store.dead, testCase.wantCode, testCase.wantDead)
			}
			if testCase.wantCode == CodeAggregateVersionGap && !testCase.wantDead {
				if store.next.After(now.Add(AggregateGapTimeout)) {
					t.Fatalf("version-gap retry %v exceeds deadline", store.next)
				}
			}
		})
	}
}

func TestCleanupUsesAcceptedRetention(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 28, 7, 0, 0, 0, time.UTC)
	store := &dispatcherStore{}
	dispatcher, err := New(Config{
		Store: store, Handlers: map[string]Handler{"known": HandlerFunc(func(context.Context, Entry) error { return nil })},
		Clock: testClock{now: now}, UUIDs: testIDs{},
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatcher.cleanup(context.Background(), now)
	if !store.cleanupBefore.Equal(now.Add(-Retention)) {
		t.Fatalf("cleanup before = %v, want %v", store.cleanupBefore, now.Add(-Retention))
	}
}

type dispatcherStore struct {
	entries       []Entry
	succeeded     bool
	failureCode   string
	next          time.Time
	dead          bool
	cleanupBefore time.Time
}

func (store *dispatcherStore) Claim(
	context.Context, string, time.Time, time.Time, int,
) ([]Entry, error) {
	return append([]Entry(nil), store.entries...), nil
}

func (store *dispatcherStore) Succeed(
	context.Context, Entry, string, time.Time,
) error {
	store.succeeded = true
	return nil
}

func (store *dispatcherStore) Fail(
	_ context.Context, _ Entry, _ string, code string,
	next, _ time.Time, dead bool,
) error {
	store.failureCode, store.next, store.dead = code, next, dead
	return nil
}

func (store *dispatcherStore) Cleanup(_ context.Context, before time.Time) error {
	store.cleanupBefore = before
	return nil
}

type testClock struct{ now time.Time }

func (clock testClock) Now() time.Time { return clock.now }

type testIDs struct{}

func (testIDs) NewUUIDv7(time.Time) (string, error) { return "holder", nil }

func retryBaseForAttempt(attempt int) time.Duration {
	delay := RetryBase
	for index := 1; index < attempt && delay < RetryCap; index++ {
		if delay > RetryCap/2 {
			return RetryCap
		}
		delay *= 2
	}
	if delay > RetryCap {
		return RetryCap
	}
	return delay
}
