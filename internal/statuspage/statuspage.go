// Package statuspage owns private status-page configuration and its use cases.
package statuspage

import (
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
)

const MaxComponents = 50

var (
	ErrConcurrentUpdate   = errors.New("status page modified concurrently")
	ErrMonitorUnavailable = errors.New("status page Monitor unavailable")
)

type ID string
type ComponentID string

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
}

func RestoreDraft(
	id ID, organizationID, title string, version int64, createdAt, updatedAt time.Time,
	components []Component,
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
	return Draft{
		ID: id, OrganizationID: organizationID, Title: title, Version: version,
		CreatedAt: createdAt, UpdatedAt: updatedAt, Components: copyComponents,
	}, nil
}

// NormalizeLabel applies the shared title/component-label boundary in UTF-16 units.
func NormalizeLabel(candidate string) (string, bool) {
	normalized := strings.TrimSpace(candidate)
	length := len(utf16.Encode([]rune(normalized)))
	return normalized, length >= 1 && length <= 100
}

func isUTC(value time.Time) bool { return !value.IsZero() && value.Location() == time.UTC }
