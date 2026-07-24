// Package organization owns Organization provisioning and Project identity.
package organization

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	// DefaultProjectName is the source name assigned transactionally at provisioning.
	DefaultProjectName = "Default"
	// SlugValidationMessage is the public validation message for Organization slugs.
	SlugValidationMessage = "A slug is 3 to 63 characters of lowercase ASCII letters and digits with single interior hyphens, starting and ending with a letter or digit."
	// DisplayNameValidationMessage is the public validation message for display names.
	DisplayNameValidationMessage = "A display name is 1 to 100 characters after trimming."
	// SlugConflictTitle is the public title for a non-idempotent slug collision.
	SlugConflictTitle = "Organization slug already in use"
)

var (
	// ErrDuplicateSlug is returned by Store.Create when a concurrent insert wins.
	ErrDuplicateSlug = errors.New("organization slug already exists")
	// ErrDefaultProjectMissing reports a broken provisioning invariant in persistence.
	ErrDefaultProjectMissing = errors.New("organization has no default Project")
)

// ID identifies an Organization.
type ID string

// ProjectID identifies a Project.
type ProjectID string

// Organization is the tenant root.
type Organization struct {
	ID          ID
	Slug        string
	DisplayName string
	CreatedAt   time.Time
}

// NewOrganization restores or creates an Organization while enforcing its invariants.
func NewOrganization(id ID, slug, displayName string, createdAt time.Time) (Organization, error) {
	if id == "" {
		return Organization{}, errors.New("an Organization requires an id")
	}
	if normalized, ok := ValidateSlug(slug); !ok || normalized != slug {
		return Organization{}, errors.New("invalid Organization slug")
	}
	if normalized, ok := NormalizeDisplayName(displayName); !ok || normalized != displayName {
		return Organization{}, errors.New("invalid Organization display name")
	}
	if !isUTC(createdAt) {
		return Organization{}, errors.New("persisted timestamps must be UTC")
	}
	return Organization{ID: id, Slug: slug, DisplayName: displayName, CreatedAt: createdAt}, nil
}

// Project is an administrative owner of Monitors within an Organization.
type Project struct {
	ID             ProjectID
	OrganizationID ID
	Name           string
	IsDefault      bool
	CreatedAt      time.Time
}

// NewDefaultProject creates the required default Project.
func NewDefaultProject(id ProjectID, organizationID ID, createdAt time.Time) (Project, error) {
	if id == "" || organizationID == "" {
		return Project{}, errors.New("a Project requires ids")
	}
	if !isUTC(createdAt) {
		return Project{}, errors.New("persisted timestamps must be UTC")
	}
	return Project{
		ID: id, OrganizationID: organizationID, Name: DefaultProjectName,
		IsDefault: true, CreatedAt: createdAt,
	}, nil
}

// Details contains an Organization and its required default Project.
type Details struct {
	Organization   Organization
	DefaultProject Project
}

// ValidationFailure is one field-level use-case validation failure.
type ValidationFailure struct {
	Field   string
	Message string
}

// ValidateSlug validates and returns an Organization slug without normalization.
func ValidateSlug(candidate string) (string, bool) {
	if len(candidate) < 3 || len(candidate) > 63 {
		return "", false
	}
	previousHyphen := false
	for i := 0; i < len(candidate); i++ {
		character := candidate[i]
		if character == '-' {
			if i == 0 || i == len(candidate)-1 || previousHyphen {
				return "", false
			}
			previousHyphen = true
			continue
		}
		if !isLowerASCII(character) && !isASCIIDigit(character) {
			return "", false
		}
		previousHyphen = false
	}
	return candidate, true
}

// NormalizeDisplayName trims Unicode surrounding whitespace and enforces 100 UTF-16 code units.
func NormalizeDisplayName(candidate string) (string, bool) {
	normalized := strings.TrimSpace(candidate)
	length := utf16Length(normalized)
	if length < 1 || length > 100 {
		return "", false
	}
	return normalized, true
}

// SlugConflictDetail returns the exact public detail for a non-idempotent collision.
func SlugConflictDetail(slug string) string {
	return fmt.Sprintf("An Organization with slug '%s' already exists with a different display name.", slug)
}

func isLowerASCII(character byte) bool { return character >= 'a' && character <= 'z' }
func isASCIIDigit(character byte) bool { return character >= '0' && character <= '9' }

func utf16Length(value string) int {
	length := 0
	for _, character := range value {
		length += utf16.RuneLen(character)
	}
	return length
}

func isUTC(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	_, offset := value.Zone()
	return offset == 0
}
