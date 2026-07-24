// Package monitor owns Monitor identity, lifecycle, immutable revisions, and use cases.
package monitor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	// NameValidationMessage is the exact public Monitor-name validation message.
	NameValidationMessage = "A Monitor name is 1 to 100 characters after trimming."
	// CheckTypeValidationMessage is the exact public check-type format message.
	CheckTypeValidationMessage = "A check type is 1 to 50 characters of lowercase ASCII letters and digits with single interior hyphens, starting with a letter."
	// TargetStateValidationMessage is the exact public state-target validation message.
	TargetStateValidationMessage = "The target state must be one of: active, paused, archived."
	// ConcurrentUpdateDetail is returned for every lost optimistic-concurrency race.
	ConcurrentUpdateDetail = "The Monitor was modified concurrently; retry against its current state."
	// ArchivedReadOnlyDetail is returned for every mutation of an archived Monitor.
	ArchivedReadOnlyDetail = "An archived Monitor is read-only."
	// ActivationWithoutRevisionDetail rejects activation before configuration exists.
	ActivationWithoutRevisionDetail = "A Monitor cannot be activated before it has a revision."
	// RenameRejectedTitle is the public rename conflict title.
	RenameRejectedTitle = "Monitor rename rejected"
	// StateTransitionRejectedTitle is the public lifecycle conflict title.
	StateTransitionRejectedTitle = "Monitor state transition rejected"
	// RevisionRejectedTitle is the public revision conflict title.
	RevisionRejectedTitle = "Monitor revision rejected"
)

// ID identifies a Monitor.
type ID string

// RevisionID identifies an immutable Monitor revision.
type RevisionID string

// State is Monitor lifecycle state, distinct from execution or health state.
type State string

const (
	StateDraft    State = "draft"
	StateActive   State = "active"
	StatePaused   State = "paused"
	StateArchived State = "archived"
)

// Monitor is the long-lived check identity owned by one Organization and Project.
type Monitor struct {
	ID                   ID
	OrganizationID       string
	ProjectID            string
	Name                 string
	CheckType            string
	State                State
	LatestRevisionNumber int
	CreatedAt            time.Time
	UpdatedAt            time.Time
	// Version is the PostgreSQL xmin value used only for optimistic persistence.
	Version uint32
}

// NewMonitor creates a draft Monitor with no revisions.
func NewMonitor(id ID, organizationID, projectID, name, checkType string, createdAt time.Time) (Monitor, error) {
	return RestoreMonitor(id, organizationID, projectID, name, checkType, StateDraft, 0, createdAt, createdAt, 0)
}

// RestoreMonitor validates a Monitor loaded from persistence.
func RestoreMonitor(
	id ID,
	organizationID, projectID, name, checkType string,
	state State,
	latestRevisionNumber int,
	createdAt, updatedAt time.Time,
	version uint32,
) (Monitor, error) {
	if id == "" || organizationID == "" || projectID == "" {
		return Monitor{}, errors.New("a Monitor requires identity and tenant scope")
	}
	if normalized, ok := NormalizeName(name); !ok || normalized != name {
		return Monitor{}, errors.New("invalid Monitor name")
	}
	if validated, ok := ValidateCheckType(checkType); !ok || validated != checkType {
		return Monitor{}, errors.New("invalid check type")
	}
	if !validState(state) {
		return Monitor{}, errors.New("unknown Monitor state")
	}
	if latestRevisionNumber < 0 {
		return Monitor{}, errors.New("latest revision number cannot be negative")
	}
	if (state == StateActive || state == StatePaused) && latestRevisionNumber == 0 {
		return Monitor{}, errors.New("active and paused Monitors require a revision")
	}
	if !isUTC(createdAt) || !isUTC(updatedAt) {
		return Monitor{}, errors.New("persisted timestamps must be UTC")
	}
	return Monitor{
		ID: id, OrganizationID: organizationID, ProjectID: projectID,
		Name: name, CheckType: checkType, State: state,
		LatestRevisionNumber: latestRevisionNumber,
		CreatedAt:            createdAt, UpdatedAt: updatedAt, Version: version,
	}, nil
}

// Rename changes only the name and update timestamp of a non-archived Monitor.
func (value *Monitor) Rename(name string, now time.Time) error {
	if value.State == StateArchived {
		return errors.New(ArchivedReadOnlyDetail)
	}
	if normalized, ok := NormalizeName(name); !ok || normalized != name {
		return errors.New("invalid normalized Monitor name")
	}
	if !isUTC(now) {
		return errors.New("persisted timestamps must be UTC")
	}
	value.Name = name
	value.UpdatedAt = now
	return nil
}

// TransitionTo applies the Monitor lifecycle state machine.
func (value *Monitor) TransitionTo(target State, now time.Time) error {
	if !isUTC(now) {
		return errors.New("persisted timestamps must be UTC")
	}
	if value.State == StateArchived {
		return errors.New(ArchivedReadOnlyDetail)
	}
	valid := false
	switch target {
	case StateActive:
		if value.State == StateDraft || value.State == StatePaused {
			if value.LatestRevisionNumber == 0 {
				return errors.New(ActivationWithoutRevisionDetail)
			}
			valid = true
		}
	case StatePaused:
		valid = value.State == StateActive
	case StateArchived:
		valid = value.State == StateDraft || value.State == StateActive || value.State == StatePaused
	}
	if !valid {
		return fmt.Errorf("A Monitor cannot move from '%s' to '%s'.", pascalState(value.State), pascalState(target))
	}
	value.State = target
	value.UpdatedAt = now
	return nil
}

// RecordRevision advances the immutable revision counter by exactly one.
func (value *Monitor) RecordRevision(revisionNumber int, now time.Time) error {
	if value.State == StateArchived {
		return errors.New(ArchivedReadOnlyDetail)
	}
	if revisionNumber != value.LatestRevisionNumber+1 {
		return fmt.Errorf("revision number %d does not follow latest revision %d", revisionNumber, value.LatestRevisionNumber)
	}
	if !isUTC(now) {
		return errors.New("persisted timestamps must be UTC")
	}
	value.LatestRevisionNumber = revisionNumber
	value.UpdatedAt = now
	return nil
}

// Revision is an append-only validated configuration snapshot.
type Revision struct {
	ID                 RevisionID
	MonitorID          ID
	OrganizationID     string
	RevisionNumber     int
	CheckType          string
	CheckSchemaVersion int
	CheckConfiguration json.RawMessage
	CreatedAt          time.Time
}

// NewRevision creates or restores an immutable revision.
func NewRevision(
	id RevisionID,
	monitorID ID,
	organizationID string,
	revisionNumber int,
	checkType string,
	checkSchemaVersion int,
	checkConfiguration json.RawMessage,
	createdAt time.Time,
) (Revision, error) {
	if id == "" || monitorID == "" || organizationID == "" {
		return Revision{}, errors.New("a Monitor revision requires identity and tenant scope")
	}
	if revisionNumber < 1 {
		return Revision{}, errors.New("revision numbers start at 1")
	}
	if _, ok := ValidateCheckType(checkType); !ok {
		return Revision{}, errors.New("invalid check type")
	}
	if checkSchemaVersion < 1 {
		return Revision{}, errors.New("check schema versions start at 1")
	}
	if len(checkConfiguration) == 0 || !json.Valid(checkConfiguration) {
		return Revision{}, errors.New("a revision requires a valid configuration document")
	}
	if !isUTC(createdAt) {
		return Revision{}, errors.New("persisted timestamps must be UTC")
	}
	configurationCopy := append(json.RawMessage(nil), checkConfiguration...)
	return Revision{
		ID: id, MonitorID: monitorID, OrganizationID: organizationID,
		RevisionNumber: revisionNumber, CheckType: checkType,
		CheckSchemaVersion: checkSchemaVersion, CheckConfiguration: configurationCopy,
		CreatedAt: createdAt,
	}, nil
}

// ValidationFailure is one field-level use-case validation failure.
type ValidationFailure struct {
	Field   string
	Message string
}

// NormalizeName trims Unicode surrounding whitespace and enforces 100 UTF-16 code units.
func NormalizeName(candidate string) (string, bool) {
	normalized := strings.TrimSpace(candidate)
	length := utf16Length(normalized)
	if length < 1 || length > 100 {
		return "", false
	}
	return normalized, true
}

// ValidateCheckType validates a stable lowercase check-category identifier.
func ValidateCheckType(candidate string) (string, bool) {
	if len(candidate) < 1 || len(candidate) > 50 || !isLowerASCII(candidate[0]) {
		return "", false
	}
	previousHyphen := false
	for index := 1; index < len(candidate); index++ {
		character := candidate[index]
		if character == '-' {
			if index == len(candidate)-1 || previousHyphen {
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

func UnsupportedCheckTypeMessage(checkType string) string {
	return fmt.Sprintf("The check type '%s' is not supported by this build.", checkType)
}

func validState(state State) bool {
	return state == StateDraft || state == StateActive || state == StatePaused || state == StateArchived
}
func pascalState(state State) string {
	switch state {
	case StateDraft:
		return "Draft"
	case StateActive:
		return "Active"
	case StatePaused:
		return "Paused"
	case StateArchived:
		return "Archived"
	default:
		return string(state)
	}
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
