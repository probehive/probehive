// Package statuspage owns status-page configuration and publication use cases.
package statuspage

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	TitleInvalidCode              = "statusPage.title.invalid"
	ComponentsInvalidCode         = "statusPage.components.invalid"
	ComponentLabelInvalidCode     = "statusPage.component.label.invalid"
	ComponentMonitorInvalidCode   = "statusPage.component.monitor.invalid"
	ComponentMonitorDuplicateCode = "statusPage.component.monitor.duplicate"
	MonitorUnavailableCode        = "statusPage.component.monitorUnavailable"
	ConcurrentUpdateCode          = "statusPage.concurrentUpdate"
	AlreadyPublishedCode          = "statusPage.alreadyPublished"
	DraftMissingCode              = "statusPage.draftMissing"
)

const (
	TitleValidationMessage            = "A status page title is 1 to 100 characters after trimming."
	ComponentsValidationMessage       = "A status page requires from 1 through 50 explicitly selected components."
	ComponentLabelValidationMessage   = "A component label is 1 to 100 characters after trimming."
	ComponentMonitorValidationMessage = "Each component requires a canonical Monitor identifier."
	ComponentMonitorDuplicateMessage  = "A Monitor can appear only once on a status page."
	MonitorUnavailableDetail          = "One or more selected Monitors are unavailable for status configuration."
	ConcurrentUpdateDetail            = "The status page changed before the update completed. Reload and try again."
	UpdateRejectedTitle               = "Status page update rejected"
	PublicationRejectedTitle          = "Status page publication rejected"
	AlreadyPublishedDetail            = "The status page is already published."
	DraftMissingDetail                = "Save the status page draft before publishing it."
)

const MaxComponents = 50

var (
	ErrConcurrentUpdate   = errors.New("status page modified concurrently")
	ErrMonitorUnavailable = errors.New("status page Monitor unavailable")
	ErrAlreadyPublished   = errors.New("status page already published")
	ErrDraftMissing       = errors.New("status page draft missing")
)

type ID string
type ComponentID string

type TokenHash [sha256.Size]byte

// Publication retains only the anonymous capability hash and its activation instant.
type Publication struct {
	TokenHash   TokenHash
	PublishedAt time.Time
}

// Component is a public label and deterministic position associated with one Monitor.
// It never copies Monitor configuration or monitoring evidence.
type Component struct {
	ID        ComponentID
	MonitorID string
	Label     string
	Position  int
}

// Draft is private Organization-owned status configuration. Publication is a separate use case.
type Draft struct {
	ID             ID
	OrganizationID string
	Title          string
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Components     []Component
	Publication    *Publication
}

func RestoreDraft(
	id ID, organizationID, title string, version int64, createdAt, updatedAt time.Time,
	components []Component,
) (Draft, error) {
	return restoreDraft(id, organizationID, title, version, createdAt, updatedAt, components, nil)
}

func RestorePublishedDraft(
	id ID, organizationID, title string, version int64, createdAt, updatedAt time.Time,
	components []Component, publication Publication,
) (Draft, error) {
	return restoreDraft(id, organizationID, title, version, createdAt, updatedAt, components, &publication)
}

func restoreDraft(
	id ID, organizationID, title string, version int64, createdAt, updatedAt time.Time,
	components []Component, publication *Publication,
) (Draft, error) {
	if id == "" || organizationID == "" || version < 1 {
		return Draft{}, errors.New("a status page requires identity, Organization scope, and a positive version")
	}
	if normalized, ok := NormalizeLabel(title); !ok || normalized != title {
		return Draft{}, errors.New("invalid status page title")
	}
	if !isUTC(createdAt) || !isUTC(updatedAt) || updatedAt.Before(createdAt) {
		return Draft{}, errors.New("invalid status page timestamps")
	}
	if len(components) < 1 || len(components) > MaxComponents {
		return Draft{}, errors.New("invalid status component count")
	}
	seenIDs := make(map[ComponentID]struct{}, len(components))
	seenMonitors := make(map[string]struct{}, len(components))
	copyComponents := make([]Component, len(components))
	for index, component := range components {
		if component.ID == "" || component.MonitorID == "" || component.Position != index {
			return Draft{}, errors.New("invalid status component identity or position")
		}
		if normalized, ok := NormalizeLabel(component.Label); !ok || normalized != component.Label {
			return Draft{}, errors.New("invalid status component label")
		}
		if _, exists := seenIDs[component.ID]; exists {
			return Draft{}, errors.New("duplicate status component identity")
		}
		if _, exists := seenMonitors[component.MonitorID]; exists {
			return Draft{}, errors.New("duplicate status component Monitor")
		}
		seenIDs[component.ID] = struct{}{}
		seenMonitors[component.MonitorID] = struct{}{}
		copyComponents[index] = component
	}
	var restoredPublication *Publication
	if publication != nil {
		if publication.TokenHash == (TokenHash{}) || !isUTC(publication.PublishedAt) {
			return Draft{}, errors.New("invalid status page publication")
		}
		value := *publication
		restoredPublication = &value
	}
	return Draft{
		ID: id, OrganizationID: organizationID, Title: title, Version: version,
		CreatedAt: createdAt, UpdatedAt: updatedAt, Components: copyComponents,
		Publication: restoredPublication,
	}, nil
}

func ValidPublicationToken(value string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == value
}

func HashPublicationToken(value string) (TokenHash, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != value {
		return TokenHash{}, false
	}
	return sha256.Sum256(raw), true
}

type PublicComponent struct {
	Label       string
	State       string
	UpdatedAt   time.Time
	Maintenance bool
}

type PublicPage struct {
	Title      string
	Components []PublicComponent
}

func RestorePublicPage(title string, components []PublicComponent) (PublicPage, error) {
	if normalized, ok := NormalizeLabel(title); !ok || normalized != title {
		return PublicPage{}, errors.New("invalid public status page title")
	}
	if len(components) < 1 || len(components) > MaxComponents {
		return PublicPage{}, errors.New("invalid public status component count")
	}
	copyComponents := make([]PublicComponent, len(components))
	for index, component := range components {
		if normalized, ok := NormalizeLabel(component.Label); !ok || normalized != component.Label {
			return PublicPage{}, errors.New("invalid public status component label")
		}
		switch component.State {
		case "unknown", "healthy", "degraded", "down":
		default:
			return PublicPage{}, errors.New("invalid public status component state")
		}
		if !isUTC(component.UpdatedAt) {
			return PublicPage{}, errors.New("invalid public status component update instant")
		}
		copyComponents[index] = component
	}
	return PublicPage{Title: title, Components: copyComponents}, nil
}

// NormalizeLabel applies the shared title/component-label boundary in UTF-16 units.
func NormalizeLabel(candidate string) (string, bool) {
	normalized := strings.TrimSpace(candidate)
	length := len(utf16.Encode([]rune(normalized)))
	return normalized, length >= 1 && length <= 100
}

func isUTC(value time.Time) bool { return !value.IsZero() && value.Location() == time.UTC }
