package organization

import (
	"strings"
	"testing"
	"time"
)

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
	organization, err := NewOrganization("org", "Acme", now)
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
	if _, err = NewOrganization("org", "Acme", nonUTC); err == nil {
		t.Fatal("NewOrganization accepted a non-UTC timestamp")
	}
	if _, err = NewDefaultProject("project", "org", nonUTC); err == nil {
		t.Fatal("NewDefaultProject accepted a non-UTC timestamp")
	}
}
