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

// Stable error codes. A code is contract under ADR 0019; the English text beside
// it is documentation and may be reworded freely.
const (
	NameInvalidCode               = "monitor.name.invalid"
	IntervalInvalidCode           = "monitor.interval.invalid"
	CheckTypeInvalidCode          = "monitor.checkType.invalid"
	CheckTypeUnsupportedCode      = "monitor.checkType.unsupported"
	TargetStateInvalidCode        = "monitor.state.invalidTarget"
	ConcurrentUpdateCode          = "monitor.concurrentUpdate"
	ArchivedReadOnlyCode          = "monitor.archived.readOnly"
	ActivationWithoutRevisionCode = "monitor.state.activationWithoutRevision"
	StateTransitionNotAllowedCode = "monitor.state.transitionNotAllowed"
)

// Current English text for the codes above. Not contract; clients translate the code.
const (
	NameValidationMessage           = "A Monitor name is 1 to 100 characters after trimming."
	IntervalValidationMessage       = "An execution interval is a whole number of seconds from 30 through 86400."
	CheckTypeValidationMessage      = "A check type is 1 to 50 characters of lowercase ASCII letters and digits with single interior hyphens, starting with a letter."
	TargetStateValidationMessage    = "The target state must be one of: active, paused, archived."
	ConcurrentUpdateDetail          = "The Monitor was modified concurrently; retry against its current state."
	ArchivedReadOnlyDetail          = "An archived Monitor is read-only."
	ActivationWithoutRevisionDetail = "A Monitor cannot be activated before it has a revision."
	RenameRejectedTitle             = "Monitor rename rejected"
	IntervalRejectedTitle           = "Monitor interval rejected"
	StateTransitionRejectedTitle    = "Monitor state transition rejected"
	RevisionRejectedTitle           = "Monitor revision rejected"
)

// Execution interval bounds, in whole seconds (ADR 0026). The interval is a Monitor field
// rather than check configuration: the scheduler must read it without decoding a
// check-type-specific document, and every check type would otherwise repeat it.
//
// An operator may raise the effective minimum for an installation but never lower it past
// MinIntervalSeconds. Unlike the timeout ceilings of ADR 0024 this is a floor, because a
// shorter interval is more load rather than less.
const (
	MinIntervalSeconds     = 30
	MaxIntervalSeconds     = 86400
	DefaultIntervalSeconds = 60
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
	ID             ID
	OrganizationID string
	ProjectID      string
	Name           string
	CheckType      string
	State          State
	// IntervalSeconds is how often the Monitor is due. Changing it is an ordinary mutable
	// update and appends no Revision: a Revision snapshots check configuration, and how
	// often a check runs is scheduling policy rather than check semantics (ADR 0026).
	IntervalSeconds      int
	LatestRevisionNumber int
	CreatedAt            time.Time
	UpdatedAt            time.Time
	// Version is the PostgreSQL xmin value used only for optimistic persistence.
	Version uint32
}

// NewMonitor creates a draft Monitor with no revisions.
func NewMonitor(id ID, organizationID, projectID, name, checkType string, intervalSeconds int, createdAt time.Time) (Monitor, error) {
	return RestoreMonitor(id, organizationID, projectID, name, checkType, StateDraft, intervalSeconds, 0, createdAt, createdAt, 0)
}

// RestoreMonitor validates a Monitor loaded from persistence.
func RestoreMonitor(
	id ID,
	organizationID, projectID, name, checkType string,
	state State,
	intervalSeconds int,
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
	if _, ok := ValidateIntervalSeconds(intervalSeconds); !ok {
		return Monitor{}, errors.New("invalid execution interval")
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
		IntervalSeconds:      intervalSeconds,
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

// ChangeInterval sets how often a non-archived Monitor is due. It appends no Revision and
// never changes lifecycle state (ADR 0026).
func (value *Monitor) ChangeInterval(intervalSeconds int, now time.Time) error {
	if value.State == StateArchived {
		return errors.New(ArchivedReadOnlyDetail)
	}
	validated, ok := ValidateIntervalSeconds(intervalSeconds)
	if !ok {
		return errors.New("invalid execution interval")
	}
	if !isUTC(now) {
		return errors.New("persisted timestamps must be UTC")
	}
	value.IntervalSeconds = validated
	value.UpdatedAt = now
	return nil
}

// LifecycleError is a rejected lifecycle transition. It carries the stable code
// clients localize from (ADR 0019) alongside the current English message.
type LifecycleError struct {
	Code    string
	Message string
}

func (failure *LifecycleError) Error() string { return failure.Message }

// TransitionTo applies the Monitor lifecycle state machine. A rejection that the
// API surfaces as 409 is always a *LifecycleError; other errors are programming faults.
func (value *Monitor) TransitionTo(target State, now time.Time) error {
	if !isUTC(now) {
		return errors.New("persisted timestamps must be UTC")
	}
	if value.State == StateArchived {
		return &LifecycleError{Code: ArchivedReadOnlyCode, Message: ArchivedReadOnlyDetail}
	}
	valid := false
	switch target {
	case StateActive:
		if value.State == StateDraft || value.State == StatePaused {
			if value.LatestRevisionNumber == 0 {
				return &LifecycleError{
					Code: ActivationWithoutRevisionCode, Message: ActivationWithoutRevisionDetail,
				}
			}
			valid = true
		}
	case StatePaused:
		valid = value.State == StateActive
	case StateArchived:
		valid = value.State == StateDraft || value.State == StateActive || value.State == StatePaused
	}
	if !valid {
		return &LifecycleError{
			Code: StateTransitionNotAllowedCode,
			Message: fmt.Sprintf(
				"A Monitor cannot move from '%s' to '%s'.", displayState(value.State), displayState(target),
			),
		}
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

// ValidationFailure is one field-level use-case validation failure. Code is the
// stable contract identifier (ADR 0019); Message is current English documentation.
type ValidationFailure struct {
	Code    string
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

// ValidateIntervalSeconds validates a whole-second execution interval against the platform
// bounds. An operator floor is applied at scheduling time rather than here, so raising an
// installation's minimum does not silently rewrite Monitors already configured below it
// (ADR 0026).
func ValidateIntervalSeconds(candidate int) (int, bool) {
	if candidate < MinIntervalSeconds || candidate > MaxIntervalSeconds {
		return 0, false
	}
	return candidate, true
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
func displayState(state State) string {
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
