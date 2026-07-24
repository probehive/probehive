package organization

import (
	"strings"
	"testing"
	"time"
)

func TestValidateSlug(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		candidate string
		valid     bool
	}{
		{"minimum", "abc", true},
		{"interior hyphen", "a-b", true},
		{"maximum", strings.Repeat("a", 63), true},
		{"too short", "ab", false},
		{"too long", strings.Repeat("a", 64), false},
		{"leading hyphen", "-abc", false},
		{"trailing hyphen", "abc-", false},
		{"double hyphen", "a--b", false},
		{"uppercase", "Acme", false},
		{"unicode", "caf\u00e9", false},
		{"underscore", "a_b", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, valid := ValidateSlug(test.candidate)
			if valid != test.valid {
				t.Fatalf("ValidateSlug(%q) validity = %v, want %v", test.candidate, valid, test.valid)
			}
			if valid && got != test.candidate {
				t.Fatalf("ValidateSlug(%q) = %q", test.candidate, got)
			}
		})
	}
}

func TestNormalizeDisplayNameUsesUTF16LengthAndUnicodeTrim(t *testing.T) {
	t.Parallel()
	got, valid := NormalizeDisplayName("\u2003 Acme \u2003")
	if !valid || got != "Acme" {
		t.Fatalf("NormalizeDisplayName = %q, %v", got, valid)
	}
	if _, valid = NormalizeDisplayName(strings.Repeat("\U0001f600", 50)); !valid {
		t.Fatal("50 supplementary runes should be exactly 100 UTF-16 code units")
	}
	if _, valid = NormalizeDisplayName(strings.Repeat("\U0001f600", 51)); valid {
		t.Fatal("51 supplementary runes should exceed 100 UTF-16 code units")
	}
}

func TestDomainConstructorsEnforceUTCAndProjectInvariants(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 1, 2, 3, 0, time.UTC)
	organization, err := NewOrganization("org", "acme", "Acme", now)
	if err != nil {
		t.Fatal(err)
	}
	project, err := NewDefaultProject("project", organization.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if project.Name != DefaultProjectName || !project.IsDefault || project.CreatedAt != organization.CreatedAt {
		t.Fatalf("unexpected default Project: %#v", project)
	}
	nonUTC := now.In(time.FixedZone("offset", 3600))
	if _, err = NewOrganization("org", "acme", "Acme", nonUTC); err == nil {
		t.Fatal("NewOrganization accepted a non-UTC timestamp")
	}
	if _, err = NewDefaultProject("project", "org", nonUTC); err == nil {
		t.Fatal("NewDefaultProject accepted a non-UTC timestamp")
	}
}

func TestSlugConflictDetail(t *testing.T) {
	t.Parallel()
	want := "An Organization with slug 'acme' already exists with a different display name."
	if got := SlugConflictDetail("acme"); got != want {
		t.Fatalf("SlugConflictDetail = %q, want %q", got, want)
	}
}
