// Package organization owns Organization provisioning and Project identity.
package organization

import (
	"errors"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	// BootstrapDisplayName is the display name of the Organization first-administrator
	// setup provisions. Setup accepts no Organization input; renaming is a separate
	// operation.
	BootstrapDisplayName = "Default"
)

// Stable error codes. Each code is part of the published contract; the English text beside
// it is documentation and may be reworded freely.
const (
	DisplayNameInvalidCode = "organization.displayName.invalid"
)

// Current English text for the codes above. Not contract; clients translate the code.
const (
	DisplayNameValidationMessage = "A display name is 1 to 100 characters after trimming."
)

var (
	// ErrDefaultProjectMissing reports a broken provisioning invariant in persistence.
	ErrDefaultProjectMissing = errors.New("organization has no default Project")
)

// ID identifies an Organization.
type ID string

// Organization is the tenant root. It carries no caller-chosen identifier: the
// generated id is the only stable key, and every route, reference, and foreign
// key uses it.
type Organization struct {
	ID          ID
	DisplayName string
	CreatedAt   time.Time
}

// NewOrganization restores or creates an Organization while enforcing its invariants.
func NewOrganization(id ID, displayName string, createdAt time.Time) (Organization, error) {
	if id == "" {
		return Organization{}, errors.New("an Organization requires an id")
	}
	if normalized, ok := NormalizeDisplayName(displayName); !ok || normalized != displayName {
		return Organization{}, errors.New("invalid Organization display name")
	}
	if !isUTC(createdAt) {
		return Organization{}, errors.New("persisted timestamps must be UTC")
	}
	return Organization{ID: id, DisplayName: displayName, CreatedAt: createdAt}, nil
}

// Details contains an Organization on its own; Monitors hang directly off it.
type Details struct {
	Organization Organization
}

// ValidationFailure is one field-level use-case validation failure. Code is the
// stable contract identifier; Message is current English documentation.
type ValidationFailure struct {
	Code    string
	Field   string
	Message string
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
