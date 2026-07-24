package user

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, candidate, want string
		valid                 bool
	}{
		{"normalize", "  ADMIN@Example.COM  ", "admin@example.com", true},
		{"one at", "a@b", "a@b", true},
		{"missing at", "example.test", "", false},
		{"empty local", "@example.test", "", false},
		{"empty domain", "admin@", "", false},
		{"two ats", "a@b@c", "", false},
		{"interior Unicode space", "a@ex\u2003ample", "", false},
		{"control", "a@b\n.test", "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, valid := NormalizeEmail(test.candidate)
			if got != test.want || valid != test.valid {
				t.Fatalf("NormalizeEmail(%q) = %q, %v; want %q, %v", test.candidate, got, valid, test.want, test.valid)
			}
		})
	}
	long := strings.Repeat("a", 252) + "@b"
	if _, valid := NormalizeEmail(long); !valid {
		t.Fatal("254 UTF-16-unit email rejected")
	}
	if _, valid := NormalizeEmail("x" + long); valid {
		t.Fatal("255 UTF-16-unit email accepted")
	}
}

func TestDisplayNameAndPasswordCountUTF16CodeUnits(t *testing.T) {
	t.Parallel()
	if got, ok := NormalizeDisplayName("\u2003 Admin \u2003"); !ok || got != "Admin" {
		t.Fatalf("NormalizeDisplayName = %q, %v", got, ok)
	}
	if !ValidPassword(strings.Repeat("\U0001f600", 6)) {
		t.Fatal("six supplementary runes should meet the 12-unit minimum")
	}
	if ValidPassword(strings.Repeat("\U0001f600", 65)) {
		t.Fatal("65 supplementary runes should exceed the 128-unit maximum")
	}
	if !ValidPassword(" 1234567890 ") {
		t.Fatal("password whitespace must not be trimmed")
	}
}

func TestUserRequiresNormalizedValuesAndUTC(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	value, err := NewUser("user", "admin@example.test", "Admin", AdministratorRole, "hash", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = value.ReplacePasswordHash("replacement"); err != nil || value.PasswordHash != "replacement" {
		t.Fatalf("ReplacePasswordHash = %v, user %#v", err, value)
	}
	if _, err = NewUser("user", "ADMIN@example.test", "Admin", AdministratorRole, "hash", now); err == nil {
		t.Fatal("NewUser accepted a non-normalized email")
	}
	if _, err = NewUser("user", "admin@example.test", " Admin ", AdministratorRole, "hash", now); err == nil {
		t.Fatal("NewUser accepted a non-normalized display name")
	}
	if _, err = NewUser("user", "admin@example.test", "Admin", "Viewer", "hash", now); err == nil {
		t.Fatal("NewUser accepted an unknown role")
	}
	if _, err = NewUser("user", "admin@example.test", "Admin", AdministratorRole, "hash", now.In(time.FixedZone("offset", 3600))); err == nil {
		t.Fatal("NewUser accepted a non-UTC timestamp")
	}
}

func TestSessionHasFixedExpiryAndHashOnlyIdentity(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	hash := testHash(1)
	session, err := NewSession(hash, "user", now)
	if err != nil {
		t.Fatal(err)
	}
	if session.ExpiresAt != now.Add(12*time.Hour) || session.Expired(now.Add(12*time.Hour-time.Nanosecond)) || !session.Expired(now.Add(12*time.Hour)) {
		t.Fatalf("unexpected fixed expiry behavior: %#v", session)
	}
	if _, err = RestoreSession(hash, "user", now, now.Add(13*time.Hour)); err == nil {
		t.Fatal("RestoreSession accepted a sliding or non-contract lifetime")
	}
	if _, err = NewSession(TokenHash{}, "user", now); err == nil {
		t.Fatal("NewSession accepted an empty hash")
	}
}

func TestSessionAntiforgeryRecordsRequireSessionBinding(t *testing.T) {
	t.Parallel()
	expires := time.Date(2026, 7, 24, 1, 0, 0, 0, time.UTC)
	selector, request, sessionHash := testHash(1), testHash(2), testHash(3)
	bound, err := NewSessionAntiforgeryRecord(selector, request, sessionHash, expires)
	if err != nil || !bound.BoundTo(sessionHash) || bound.BoundTo(testHash(4)) {
		t.Fatalf("bound record = %#v, error %v", bound, err)
	}
	if !bound.Expired(expires) || bound.Expired(expires.Add(-time.Nanosecond)) {
		t.Fatal("antiforgery expiry boundary is wrong")
	}
	if _, err = NewSessionAntiforgeryRecord(TokenHash{}, request, sessionHash, expires); err == nil {
		t.Fatal("antiforgery record accepted an empty selector hash")
	}
	if _, err = NewSessionAntiforgeryRecord(selector, request, TokenHash{}, expires); err == nil {
		t.Fatal("antiforgery record accepted an empty session hash")
	}
}

func testHash(first byte) TokenHash {
	var value TokenHash
	value[0] = first
	return value
}
