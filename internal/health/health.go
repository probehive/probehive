// Package health owns evaluated Monitor health and confirmation semantics.
package health

import (
	"errors"
	"fmt"
	"time"
)

const PolicyVersion = "phase1.v1"

type State string

const (
	StateUnknown  State = "unknown"
	StateHealthy  State = "healthy"
	StateDegraded State = "degraded"
	StateDown     State = "down"
)

type Evidence string

const (
	EvidencePassing       Evidence = "passing"
	EvidenceFailing       Evidence = "failing"
	EvidenceLocationFault Evidence = "location-fault"
	EvidenceIndeterminate Evidence = "indeterminate"
)

type Direction string

const (
	DirectionFailure  Direction = "failure"
	DirectionRecovery Direction = "recovery"
)

type CandidateState string

const (
	CandidatePending      CandidateState = "pending"
	CandidateConfirmed    CandidateState = "confirmed"
	CandidateContradicted CandidateState = "contradicted"
	CandidateSuperseded   CandidateState = "superseded"
	CandidateStale        CandidateState = "stale"
)

type Counts struct {
	Configured    int
	Eligible      int
	Responding    int
	Passing       int
	Failing       int
	LocationFault int
	Indeterminate int
	Missing       int
}

func CountsFor(evidence Evidence) Counts {
	counts := Counts{Configured: 1, Eligible: 1, Responding: 1}
	switch evidence {
	case EvidencePassing:
		counts.Passing = 1
	case EvidenceFailing:
		counts.Failing = 1
	case EvidenceLocationFault:
		counts.LocationFault = 1
	case EvidenceIndeterminate:
		counts.Indeterminate = 1
	}
	return counts
}

type Candidate struct {
	ID                     string
	OrganizationID         string
	ProjectID              string
	MonitorID              string
	SourceRevisionNumber   int
	Direction              Direction
	ExpectedEvidence       Evidence
	State                  CandidateState
	TriggeringRunID        string
	TriggeringScheduledFor time.Time
	TriggeringEventID      string
	RequestedAt            time.Time
}

type Snapshot struct {
	OrganizationID            string
	ProjectID                 string
	MonitorID                 string
	State                     State
	StableState               State
	PolicyVersion             string
	Version                   int64
	SourceRevisionNumber      int
	LastScheduledFor          time.Time
	LastDeterminateFinishedAt time.Time
	LastRunID                 string
	LastRunScheduledFor       time.Time
	Candidate                 *Candidate
	Counts                    Counts
	TransitionedAt            time.Time
	UpdatedAt                 time.Time
}

func Initial(organizationID, projectID, monitorID string, now time.Time) (Snapshot, error) {
	if organizationID == "" || projectID == "" || monitorID == "" {
		return Snapshot{}, errors.New("health requires Organization, Project, and Monitor identity")
	}
	if !isUTC(now) {
		return Snapshot{}, errors.New("health timestamps must be UTC")
	}
	return Snapshot{
		OrganizationID: organizationID, ProjectID: projectID, MonitorID: monitorID,
		State: StateUnknown, StableState: StateUnknown, PolicyVersion: PolicyVersion,
		TransitionedAt: now, UpdatedAt: now,
	}, nil
}

type Input struct {
	EventID              string
	RunID                string
	Kind                 string
	Outcome              string
	FailureCode          string
	RevisionNumber       int
	LatestRevisionNumber int
	ScheduledFor         time.Time
	FinishedAt           time.Time
	CandidateID          string
	Now                  time.Time
	NewCandidateID       string
}

type Decision struct {
	Snapshot            Snapshot
	PreviousState       State
	Evidence            Evidence
	Late                bool
	SupersededRevision  bool
	IgnoredConfirmation bool
	CandidateCreated    *Candidate
	CandidateCompleted  *Candidate
	Transitioned        bool
}

func Evaluate(current Snapshot, input Input) (Decision, error) {
	if err := validateSnapshot(current); err != nil {
		return Decision{}, err
	}
	if input.EventID == "" || input.RunID == "" || input.RevisionNumber < 1 || input.LatestRevisionNumber < 1 {
		return Decision{}, errors.New("health evidence requires event, Run, and revision identity")
	}
	if !isUTC(input.ScheduledFor) || (!input.FinishedAt.IsZero() && !isUTC(input.FinishedAt)) || !isUTC(input.Now) {
		return Decision{}, errors.New("health evidence timestamps must be UTC")
	}
	decision := Decision{Snapshot: current, PreviousState: current.State}
	decision.Evidence = Classify(input.Kind, input.Outcome, input.FailureCode)
	decision.Snapshot.UpdatedAt = input.Now

	if input.Kind == "manual" {
		return decision, nil
	}
	if input.RevisionNumber != input.LatestRevisionNumber {
		decision.SupersededRevision = true
		return decision, nil
	}
	if input.Kind == "scheduled" && !current.LastScheduledFor.IsZero() && input.ScheduledFor.Before(current.LastScheduledFor) {
		decision.Late = true
		return decision, nil
	}
	if input.Kind == "scheduled" {
		decision.Snapshot.LastScheduledFor = input.ScheduledFor
	}
	if input.Kind == "confirmation" {
		candidate := current.Candidate
		if candidate == nil || input.CandidateID == "" || candidate.ID != input.CandidateID ||
			candidate.State != CandidatePending || candidate.SourceRevisionNumber != input.RevisionNumber {
			decision.IgnoredConfirmation = true
			return decision, nil
		}
	}
	decision.Snapshot.SourceRevisionNumber = input.RevisionNumber
	decision.Snapshot.LastRunID = input.RunID
	decision.Snapshot.LastRunScheduledFor = input.ScheduledFor
	decision.Snapshot.Counts = CountsFor(decision.Evidence)
	if decision.Evidence == EvidencePassing || decision.Evidence == EvidenceFailing {
		if input.FinishedAt.IsZero() {
			return Decision{}, errors.New("determinate health evidence requires a finish instant")
		}
		decision.Snapshot.LastDeterminateFinishedAt = input.FinishedAt
	}

	if input.Kind == "confirmation" {
		return evaluateConfirmation(decision, input)
	}
	if input.Kind != "scheduled" {
		return decision, nil
	}
	return evaluateScheduled(decision, input)
}

func evaluateScheduled(decision Decision, input Input) (Decision, error) {
	if decision.Evidence != EvidencePassing && decision.Evidence != EvidenceFailing {
		return decision, nil
	}
	if decision.Snapshot.Candidate != nil {
		candidate := *decision.Snapshot.Candidate
		candidate.State = CandidateSuperseded
		decision.CandidateCompleted = &candidate
		decision.Snapshot.Candidate = nil
	}

	stable := decision.Snapshot.StableState
	if decision.Snapshot.State != StateDegraded {
		stable = decision.Snapshot.State
	}
	switch stable {
	case StateUnknown:
		if decision.Evidence == EvidencePassing {
			setState(&decision, StateHealthy, StateHealthy, input.Now)
			return decision, nil
		}
		return beginCandidate(decision, input, DirectionFailure, EvidenceFailing)
	case StateHealthy:
		if decision.Evidence == EvidencePassing {
			setState(&decision, StateHealthy, StateHealthy, input.Now)
			return decision, nil
		}
		return beginCandidate(decision, input, DirectionFailure, EvidenceFailing)
	case StateDown:
		if decision.Evidence == EvidenceFailing {
			setState(&decision, StateDown, StateDown, input.Now)
			return decision, nil
		}
		return beginCandidate(decision, input, DirectionRecovery, EvidencePassing)
	default:
		return Decision{}, fmt.Errorf("health stable state %q cannot start evaluation", stable)
	}
}

func beginCandidate(
	decision Decision, input Input, direction Direction, expected Evidence,
) (Decision, error) {
	if input.NewCandidateID == "" {
		return Decision{}, errors.New("a health transition candidate requires an identifier")
	}
	candidate := Candidate{
		ID:                     input.NewCandidateID,
		OrganizationID:         decision.Snapshot.OrganizationID,
		ProjectID:              decision.Snapshot.ProjectID,
		MonitorID:              decision.Snapshot.MonitorID,
		SourceRevisionNumber:   input.RevisionNumber,
		Direction:              direction,
		ExpectedEvidence:       expected,
		State:                  CandidatePending,
		TriggeringRunID:        input.RunID,
		TriggeringScheduledFor: input.ScheduledFor,
		TriggeringEventID:      input.EventID,
		RequestedAt:            input.Now,
	}
	decision.Snapshot.Candidate = &candidate
	decision.CandidateCreated = &candidate
	setState(&decision, StateDegraded, decision.Snapshot.StableState, input.Now)
	return decision, nil
}

func evaluateConfirmation(decision Decision, input Input) (Decision, error) {
	candidate := decision.Snapshot.Candidate
	if candidate == nil || input.CandidateID == "" || candidate.ID != input.CandidateID || candidate.State != CandidatePending {
		return decision, nil
	}
	if candidate.SourceRevisionNumber != input.RevisionNumber {
		return decision, nil
	}
	if decision.Evidence != EvidencePassing && decision.Evidence != EvidenceFailing {
		return decision, nil
	}

	completed := *candidate
	decision.Snapshot.Candidate = nil
	if decision.Evidence == candidate.ExpectedEvidence {
		completed.State = CandidateConfirmed
		switch candidate.Direction {
		case DirectionFailure:
			setState(&decision, StateDown, StateDown, input.Now)
		case DirectionRecovery:
			setState(&decision, StateHealthy, StateHealthy, input.Now)
		default:
			return Decision{}, fmt.Errorf("unknown health candidate direction %q", candidate.Direction)
		}
	} else {
		completed.State = CandidateContradicted
		setState(&decision, decision.Snapshot.StableState, decision.Snapshot.StableState, input.Now)
	}
	decision.CandidateCompleted = &completed
	return decision, nil
}

func setState(decision *Decision, state, stable State, now time.Time) {
	if decision.Snapshot.State != state {
		decision.Snapshot.State = state
		decision.Snapshot.Version++
		decision.Snapshot.TransitionedAt = now
		decision.Transitioned = true
	}
	decision.Snapshot.StableState = stable
}

func Classify(kind, outcome, code string) Evidence {
	if kind == "manual" || outcome == "" || outcome == "cancelled" || outcome == "skipped" {
		return EvidenceIndeterminate
	}
	switch outcome {
	case "passed":
		if code == "" {
			return EvidencePassing
		}
	case "failed":
		if code == "probe.http.status.unexpected" {
			return EvidenceFailing
		}
	case "timedout":
		if code == "probe.execution.timeout" {
			return EvidenceFailing
		}
	case "errored":
		switch code {
		case "probe.http.redirect.tooMany",
			"probe.tls.certificateInvalid",
			"probe.transport.failed",
			"outbound.resolution.failed",
			"outbound.resolution.empty",
			"outbound.connect.failed":
			return EvidenceFailing
		case "outbound.address.mismatch":
			return EvidenceLocationFault
		}
	}
	return EvidenceIndeterminate
}

func StaleAfter(effectiveInterval, executionCeiling time.Duration) (time.Duration, error) {
	if effectiveInterval <= 0 || executionCeiling <= 0 {
		return 0, errors.New("health staleness requires positive interval and execution ceiling")
	}
	intervalBound := 3 * effectiveInterval
	executionBound := 2 * executionCeiling
	if executionBound > intervalBound {
		return executionBound, nil
	}
	return intervalBound, nil
}

func MarkStale(current Snapshot, now time.Time) (Decision, error) {
	if err := validateSnapshot(current); err != nil {
		return Decision{}, err
	}
	if !isUTC(now) {
		return Decision{}, errors.New("health timestamps must be UTC")
	}
	decision := Decision{Snapshot: current, PreviousState: current.State, Evidence: EvidenceIndeterminate}
	if current.State == StateUnknown {
		return decision, nil
	}
	if current.Candidate != nil {
		candidate := *current.Candidate
		candidate.State = CandidateStale
		decision.CandidateCompleted = &candidate
		decision.Snapshot.Candidate = nil
	}
	setState(&decision, StateUnknown, StateUnknown, now)
	decision.Snapshot.UpdatedAt = now
	decision.Snapshot.Counts = Counts{Configured: 1, Eligible: 1, Missing: 1}
	return decision, nil
}

func validateSnapshot(value Snapshot) error {
	if value.OrganizationID == "" || value.ProjectID == "" || value.MonitorID == "" {
		return errors.New("health requires Organization, Project, and Monitor identity")
	}
	if value.PolicyVersion != PolicyVersion {
		return fmt.Errorf("unsupported health policy %q", value.PolicyVersion)
	}
	if !validState(value.State) || !validStableState(value.StableState) || value.Version < 0 {
		return errors.New("invalid health state")
	}
	return nil
}

func validState(value State) bool {
	return value == StateUnknown || value == StateHealthy || value == StateDegraded || value == StateDown
}

func validStableState(value State) bool {
	return value == StateUnknown || value == StateHealthy || value == StateDown
}

func isUTC(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Equal(value.UTC())
}
