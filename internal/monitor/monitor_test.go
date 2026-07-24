package monitor

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNormalizeNameUsesUnicodeTrimAndUTF16Length(t *testing.T) {
	t.Parallel()
	if got, ok := NormalizeName("\u2003 API \u2003"); !ok || got != "API" {
		t.Fatalf("NormalizeName = %q, %v", got, ok)
	}
	if _, ok := NormalizeName(strings.Repeat("\U0001f600", 50)); !ok {
		t.Fatal("100 UTF-16 units rejected")
	}
	if _, ok := NormalizeName(strings.Repeat("\U0001f600", 51)); ok {
		t.Fatal("102 UTF-16 units accepted")
	}
}

func TestValidateCheckType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		valid bool
	}{
		{"h", true}, {"http", true}, {"http-2", true}, {strings.Repeat("a", 50), true},
		{"", false}, {"2http", false}, {"HTTP", false}, {"http-", false}, {"http--tls", false},
		{"http_tls", false}, {strings.Repeat("a", 51), false},
	}
	for _, test := range tests {
		if got, valid := ValidateCheckType(test.value); valid != test.valid || (valid && got != test.value) {
			t.Errorf("ValidateCheckType(%q) = %q, %v", test.value, got, valid)
		}
	}
}

func TestNewMonitorStartsDraftWithIdenticalTimestamps(t *testing.T) {
	t.Parallel()
	now := testTime()
	value, err := NewMonitor("monitor", "org", "project", "API", "http", now)
	if err != nil {
		t.Fatal(err)
	}
	if value.State != StateDraft || value.LatestRevisionNumber != 0 || value.CreatedAt != now || value.UpdatedAt != now {
		t.Fatalf("unexpected Monitor: %#v", value)
	}
}

func TestTransitionLifecycle(t *testing.T) {
	t.Parallel()
	now := testTime()
	tests := []struct {
		name       string
		state      State
		revisions  int
		target     State
		wantDetail string
	}{
		{"draft activation without revision", StateDraft, 0, StateActive, ActivationWithoutRevisionDetail},
		{"draft activation", StateDraft, 1, StateActive, ""},
		{"paused resume", StatePaused, 1, StateActive, ""},
		{"active pause", StateActive, 1, StatePaused, ""},
		{"draft archive", StateDraft, 0, StateArchived, ""},
		{"active archive", StateActive, 1, StateArchived, ""},
		{"paused archive", StatePaused, 1, StateArchived, ""},
		{"active active", StateActive, 1, StateActive, "A Monitor cannot move from 'Active' to 'Active'."},
		{"draft pause", StateDraft, 1, StatePaused, "A Monitor cannot move from 'Draft' to 'Paused'."},
		{"archived terminal", StateArchived, 1, StateActive, ArchivedReadOnlyDetail},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := RestoreMonitor("monitor", "org", "project", "API", "http", test.state, test.revisions, now, now, 1)
			if err != nil {
				t.Fatal(err)
			}
			err = value.TransitionTo(test.target, now.Add(time.Minute))
			if test.wantDetail == "" {
				if err != nil || value.State != test.target || value.UpdatedAt != now.Add(time.Minute) {
					t.Fatalf("transition = %#v, %v", value, err)
				}
			} else if err == nil || err.Error() != test.wantDetail {
				t.Fatalf("error = %v, want %q", err, test.wantDetail)
			}
		})
	}
}

func TestRenameAndRecordRevisionRespectArchivedAndSequence(t *testing.T) {
	t.Parallel()
	now := testTime()
	value, _ := NewMonitor("monitor", "org", "project", "API", "http", now)
	if err := value.Rename("Renamed", now.Add(time.Minute)); err != nil || value.Name != "Renamed" || value.State != StateDraft {
		t.Fatalf("Rename = %#v, %v", value, err)
	}
	if err := value.RecordRevision(2, now); err == nil {
		t.Fatal("RecordRevision accepted a skipped number")
	}
	if err := value.RecordRevision(1, now.Add(2*time.Minute)); err != nil || value.LatestRevisionNumber != 1 {
		t.Fatalf("RecordRevision = %#v, %v", value, err)
	}
	value.State = StateArchived
	if err := value.Rename("Nope", now); err == nil || err.Error() != ArchivedReadOnlyDetail {
		t.Fatalf("Rename error = %v", err)
	}
	if err := value.RecordRevision(2, now); err == nil || err.Error() != ArchivedReadOnlyDetail {
		t.Fatalf("RecordRevision error = %v", err)
	}
}

func TestRevisionCopiesConfigurationAndEnforcesInvariants(t *testing.T) {
	t.Parallel()
	now := testTime()
	configuration := json.RawMessage(`{"url":"https://example.test"}`)
	revision, err := NewRevision("revision", "monitor", "org", 1, "http", 1, configuration, now)
	if err != nil {
		t.Fatal(err)
	}
	configuration[2] = 'X'
	if string(revision.CheckConfiguration) != `{"url":"https://example.test"}` {
		t.Fatal("NewRevision retained mutable configuration storage")
	}
	if _, err = NewRevision("revision", "monitor", "org", 0, "http", 1, json.RawMessage(`{}`), now); err == nil {
		t.Fatal("NewRevision accepted revision zero")
	}
	if _, err = NewRevision("revision", "monitor", "org", 1, "http", 0, json.RawMessage(`{}`), now); err == nil {
		t.Fatal("NewRevision accepted schema zero")
	}
	if _, err = NewRevision("revision", "monitor", "org", 1, "http", 1, json.RawMessage(`nope`), now); err == nil {
		t.Fatal("NewRevision accepted invalid JSON")
	}
}

func TestRestoreMonitorRejectsImpossiblePersistedState(t *testing.T) {
	t.Parallel()
	now := testTime()
	if _, err := RestoreMonitor("monitor", "org", "project", "API", "http", StateActive, 0, now, now, 1); err == nil {
		t.Fatal("RestoreMonitor accepted active without a revision")
	}
	if _, err := RestoreMonitor("monitor", "org", "project", "API", "http", "unknown", 0, now, now, 1); err == nil {
		t.Fatal("RestoreMonitor accepted an unknown state")
	}
	if _, err := RestoreMonitor("monitor", "org", "project", "API", "http", StateDraft, 0, now.In(time.FixedZone("offset", 3600)), now, 1); err == nil {
		t.Fatal("RestoreMonitor accepted a non-UTC timestamp")
	}
}

func testTime() time.Time { return time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC) }
