// Package organization owns Organization provisioning and Project identity.
package organization

import (
	"errors"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	// DefaultProjectName is the source name assigned transactionally at provisioning.
	DefaultProjectName = "Default"
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

// ProjectID identifies a Project.
type ProjectID string

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
