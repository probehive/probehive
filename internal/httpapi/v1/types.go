// Package v1 defines ProbeHive's deliberately versioned HTTP wire types.
package v1

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"
)

type SetupStatusResponse struct {
	SetupComplete bool `json:"setupComplete"`
}

type AntiforgeryTokenResponse struct {
	HeaderName   string `json:"headerName"`
	RequestToken string `json:"requestToken"`
}

type SessionResponse struct {
	UserID      string `json:"userId"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}

type UserResponse struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
}

// SetupResponse is the first-administrator setup result. Setup provisions the
// installation's Organization as well, so a new installation is immediately usable.
type SetupResponse struct {
	User         UserResponse         `json:"user"`
	Organization OrganizationResponse `json:"organization"`
}

type ProjectResponse struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	Name           string    `json:"name"`
	IsDefault      bool      `json:"isDefault"`
	CreatedAt      time.Time `json:"createdAt"`
}

type OrganizationResponse struct {
	ID             string          `json:"id"`
	Slug           string          `json:"slug"`
	DisplayName    string          `json:"displayName"`
	CreatedAt      time.Time       `json:"createdAt"`
	DefaultProject ProjectResponse `json:"defaultProject"`
}

type MonitorResponse struct {
	ID                   string    `json:"id"`
	OrganizationID       string    `json:"organizationId"`
	ProjectID            string    `json:"projectId"`
	Name                 string    `json:"name"`
	CheckType            string    `json:"checkType"`
	State                string    `json:"state"`
	IntervalSeconds      int       `json:"intervalSeconds"`
	LatestRevisionNumber int       `json:"latestRevisionNumber"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

type MonitorHealthResponse struct {
	OrganizationID            string                   `json:"organizationId"`
	ProjectID                 string                   `json:"projectId"`
	MonitorID                 string                   `json:"monitorId"`
	State                     string                   `json:"state"`
	StableState               string                   `json:"stableState"`
	PolicyVersion             string                   `json:"policyVersion"`
	Version                   int64                    `json:"version"`
	SourceRevisionNumber      *int                     `json:"sourceRevisionNumber"`
	LastScheduledFor          *time.Time               `json:"lastScheduledFor"`
	LastDeterminateFinishedAt *time.Time               `json:"lastDeterminateFinishedAt"`
	LastRunID                 *string                  `json:"lastRunId"`
	LastRunScheduledFor       *time.Time               `json:"lastRunScheduledFor"`
	Candidate                 *HealthCandidateResponse `json:"candidate"`
	Counts                    HealthCountsResponse     `json:"counts"`
	TransitionedAt            time.Time                `json:"transitionedAt"`
	UpdatedAt                 time.Time                `json:"updatedAt"`
}

type HealthCandidateResponse struct {
	ID                     string    `json:"id"`
	Direction              string    `json:"direction"`
	ExpectedEvidence       string    `json:"expectedEvidence"`
	SourceRevisionNumber   int       `json:"sourceRevisionNumber"`
	TriggeringRunID        string    `json:"triggeringRunId"`
	TriggeringScheduledFor time.Time `json:"triggeringScheduledFor"`
	RequestedAt            time.Time `json:"requestedAt"`
}

type HealthCountsResponse struct {
	Configured    int `json:"configured"`
	Eligible      int `json:"eligible"`
	Responding    int `json:"responding"`
	Passing       int `json:"passing"`
	Failing       int `json:"failing"`
	LocationFault int `json:"locationFault"`
	Indeterminate int `json:"indeterminate"`
	Missing       int `json:"missing"`
}

type IncidentPageResponse struct {
	Items      []IncidentResponse `json:"items"`
	NextCursor *string            `json:"nextCursor"`
}

type IncidentResponse struct {
	ID                   string                     `json:"id"`
	OrganizationID       string                     `json:"organizationId"`
	ProjectID            string                     `json:"projectId"`
	MonitorID            string                     `json:"monitorId"`
	State                string                     `json:"state"`
	Version              int64                      `json:"version"`
	OpenedTransitionID   string                     `json:"openedTransitionId"`
	AcknowledgedBy       *string                    `json:"acknowledgedBy"`
	AcknowledgedAt       *time.Time                 `json:"acknowledgedAt"`
	ResolvedTransitionID *string                    `json:"resolvedTransitionId"`
	ResolvedAt           *time.Time                 `json:"resolvedAt"`
	CreatedAt            time.Time                  `json:"createdAt"`
	UpdatedAt            time.Time                  `json:"updatedAt"`
	Timeline             []IncidentTimelineResponse `json:"timeline"`
}

type IncidentTimelineResponse struct {
	ID                    string                `json:"id"`
	IncidentVersion       int64                 `json:"incidentVersion"`
	Kind                  string                `json:"kind"`
	HealthTransitionID    *string               `json:"healthTransitionId"`
	ActorUserID           *string               `json:"actorUserId"`
	OldHealthState        *string               `json:"oldHealthState"`
	NewHealthState        *string               `json:"newHealthState"`
	PolicyVersion         *string               `json:"policyVersion"`
	CausalRunID           *string               `json:"causalRunId"`
	CausalRunScheduledFor *time.Time            `json:"causalRunScheduledFor"`
	Counts                *HealthCountsResponse `json:"counts"`
	OccurredAt            time.Time             `json:"occurredAt"`
}

type AlertPageResponse struct {
	Items      []AlertResponse `json:"items"`
	NextCursor *string         `json:"nextCursor"`
}

type AlertResponse struct {
	ID              string    `json:"id"`
	OrganizationID  string    `json:"organizationId"`
	ProjectID       string    `json:"projectId"`
	MonitorID       string    `json:"monitorId"`
	IncidentID      string    `json:"incidentId"`
	IncidentVersion int64     `json:"incidentVersion"`
	Kind            string    `json:"kind"`
	OccurredAt      time.Time `json:"occurredAt"`
	CreatedAt       time.Time `json:"createdAt"`
}

type MonitorRevisionResponse struct {
	ID                 string          `json:"id"`
	MonitorID          string          `json:"monitorId"`
	RevisionNumber     int             `json:"revisionNumber"`
	CheckType          string          `json:"checkType"`
	CheckSchemaVersion int             `json:"checkSchemaVersion"`
	CheckConfiguration json.RawMessage `json:"checkConfiguration"`
	CreatedAt          time.Time       `json:"createdAt"`
}

type RunPageResponse struct {
	Items      []RunResponse `json:"items"`
	NextCursor *string       `json:"nextCursor"`
}

type RunResponse struct {
	ID             string                     `json:"id"`
	OrganizationID string                     `json:"organizationId"`
	ProjectID      string                     `json:"projectId"`
	MonitorID      string                     `json:"monitorId"`
	RevisionNumber int                        `json:"revisionNumber"`
	Location       string                     `json:"location"`
	ScheduledFor   time.Time                  `json:"scheduledFor"`
	Kind           string                     `json:"kind"`
	Outcome        *string                    `json:"outcome"`
	StartedAt      *time.Time                 `json:"startedAt"`
	FinishedAt     *time.Time                 `json:"finishedAt"`
	LeaseExpiresAt *time.Time                 `json:"leaseExpiresAt"`
	Confirmation   *ConfirmationCauseResponse `json:"confirmation"`
}

type ConfirmationCauseResponse struct {
	CandidateID            string    `json:"candidateId"`
	TriggeringRunID        string    `json:"triggeringRunId"`
	TriggeringScheduledFor time.Time `json:"triggeringScheduledFor"`
	CausationEventID       string    `json:"causationEventId"`
	PolicyVersion          string    `json:"policyVersion"`
}

type ObservationResponse struct {
	RunID                string                    `json:"runId"`
	OrganizationID       string                    `json:"organizationId"`
	ScheduledFor         time.Time                 `json:"scheduledFor"`
	FailureCode          string                    `json:"failureCode"`
	FailureClass         string                    `json:"failureClass"`
	DurationMicroseconds int64                     `json:"durationMicroseconds"`
	Phases               ObservationPhasesResponse `json:"phases"`
	HTTP                 *HTTPObservationResponse  `json:"http"`
}

type ObservationPhasesResponse struct {
	ConnectMicroseconds   int64 `json:"connectMicroseconds"`
	TLSMicroseconds       int64 `json:"tlsMicroseconds"`
	FirstByteMicroseconds int64 `json:"firstByteMicroseconds"`
}

type HTTPObservationResponse struct {
	StatusCode    int                     `json:"statusCode"`
	Protocol      string                  `json:"protocol"`
	RedirectCount int                     `json:"redirectCount"`
	BodyBytes     int64                   `json:"bodyBytes"`
	BodyTruncated bool                    `json:"bodyTruncated"`
	TLS           *TLSObservationResponse `json:"tls"`
}

type TLSObservationResponse struct {
	Version              string     `json:"version"`
	CipherSuite          string     `json:"cipherSuite"`
	CertificateExpiresAt *time.Time `json:"certificateExpiresAt"`
}

type CreateFirstAdministratorRequest struct {
	Email       *string `json:"email"`
	DisplayName *string `json:"displayName"`
	Password    *string `json:"password"`
}

type LoginRequest struct {
	Email    *string `json:"email"`
	Password *string `json:"password"`
}

type CreateOrganizationRequest struct {
	Slug        *string `json:"slug"`
	DisplayName *string `json:"displayName"`
}

type RenameOrganizationRequest struct {
	DisplayName *string `json:"displayName"`
}

type CreateMonitorRequest struct {
	Name      *string `json:"name"`
	CheckType *string `json:"checkType"`
	// IntervalSeconds is optional. Omitting it takes the product default; a Monitor is
	// created in Draft and cannot run before it has a revision, so the frequency is not a
	// decision the caller has to make up front (ADR 0026).
	IntervalSeconds *Integer `json:"intervalSeconds"`
}

type ChangeMonitorIntervalRequest struct {
	IntervalSeconds Integer `json:"intervalSeconds"`
}

type RenameMonitorRequest struct {
	Name *string `json:"name"`
}

type ChangeMonitorStateRequest struct {
	State *string `json:"state"`
}

// Integer accepts only JSON integers. A missing field retains the zero value,
// while an explicit JSON null is a malformed request.
type Integer int

func (value *Integer) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("expected an integer, found null")
	}
	var decoded int
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*value = Integer(decoded)
	return nil
}

type CreateMonitorRevisionRequest struct {
	CheckSchemaVersion Integer         `json:"checkSchemaVersion"`
	CheckConfiguration json.RawMessage `json:"checkConfiguration"`
}

// ValidationError is one field-level failure. Code is the stable contract identifier
// clients localize from; Message is current English documentation (ADR 0019).
type ValidationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ProblemDetails struct {
	Type   string                       `json:"type"`
	Title  string                       `json:"title"`
	Status int                          `json:"status"`
	Code   string                       `json:"code,omitempty"`
	Detail string                       `json:"detail,omitempty"`
	Errors map[string][]ValidationError `json:"errors,omitempty"`
}
