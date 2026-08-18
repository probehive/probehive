package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/probehive/probehive/internal/health"
	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/incident"
	"github.com/probehive/probehive/internal/organization"
)

const (
	incidentPageSizeInvalidCode = "incident.query.pageSize.invalid"
	incidentCursorInvalidCode   = "incident.query.cursor.invalid"
)

var incidentQueryMessages = map[string]string{
	incidentPageSizeInvalidCode: "pageSize must be one integer from 1 through 100.",
	incidentCursorInvalidCode:   "cursor must be one cursor returned by this endpoint.",
}

type incidentCursorPayload struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

func (server *Server) monitorHealthState(w http.ResponseWriter, r *http.Request) {
	scope, ok := monitorScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if _, ok := server.readMonitors(w, r, scope.OrganizationID); !ok {
		return
	}
	value, found, err := server.monitorHealth.Get(r.Context(), health.Scope{
		OrganizationID: scope.OrganizationID, ProjectID: scope.ProjectID, MonitorID: string(scope.MonitorID),
	})
	if err != nil {
		server.internalError(w, r, "get Monitor health", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toMonitorHealthResponse(value))
}

func (server *Server) monitorIncidents(w http.ResponseWriter, r *http.Request) {
	scope, ok := monitorScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if _, ok := server.requireOrganizationPermission(
		w, r, scope.OrganizationID, organization.PermissionIncidentRead); !ok {
		return
	}
	query, failures := parseIncidentListQuery(r.URL.Query())
	if len(failures) != 0 {
		writeValidationProblem(w, failures)
		return
	}
	page, found, err := server.incidents.List(r.Context(), incident.Scope{
		OrganizationID: scope.OrganizationID, ProjectID: scope.ProjectID, MonitorID: string(scope.MonitorID),
	}, query)
	if err != nil {
		server.internalError(w, r, "list Incidents", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	items := make([]api.IncidentResponse, len(page.Incidents))
	for index, value := range page.Incidents {
		items[index] = toIncidentResponse(value)
	}
	var nextCursor *string
	if page.NextCursor != nil {
		encoded, encodeErr := encodeIncidentCursor(*page.NextCursor)
		if encodeErr != nil {
			server.internalError(w, r, "encode Incident cursor", encodeErr)
			return
		}
		nextCursor = &encoded
	}
	writeJSON(w, http.StatusOK, api.IncidentPageResponse{Items: items, NextCursor: nextCursor})
}

func (server *Server) monitorIncident(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := incidentIdentity(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if _, ok := server.requireOrganizationPermission(
		w, r, scope.OrganizationID, organization.PermissionIncidentRead); !ok {
		return
	}
	value, found, err := server.incidents.Get(r.Context(), scope, id)
	if err != nil {
		server.internalError(w, r, "get Incident", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toIncidentResponse(value))
}

func (server *Server) acknowledgeIncident(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := incidentIdentity(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	principal, ok := server.protectUnsafe(w, r, func(
		writer http.ResponseWriter, request *http.Request,
	) (*authenticatedSession, bool) {
		return server.requireOrganizationPermission(
			writer, request, scope.OrganizationID, organization.PermissionIncidentWrite)
	})
	if !ok {
		return
	}
	value, found, err := server.incidents.Acknowledge(
		r.Context(), scope, id, string(principal.account.ID))
	switch {
	case errors.Is(err, incident.ErrConflict):
		writeProblem(w, http.StatusConflict, "Incident acknowledgement rejected",
			"A resolved Incident cannot be acknowledged.")
	case err != nil:
		server.internalError(w, r, "acknowledge Incident", err)
	case !found:
		writeStatusProblem(w, http.StatusNotFound)
	default:
		writeJSON(w, http.StatusOK, toIncidentResponse(value))
	}
}

func incidentIdentity(r *http.Request) (incident.Scope, string, bool) {
	monitorValue, ok := monitorScope(r)
	id, idOK := canonicalUUID(r.PathValue("incidentId"))
	if !ok || !idOK {
		return incident.Scope{}, "", false
	}
	return incident.Scope{
		OrganizationID: monitorValue.OrganizationID,
		ProjectID:      monitorValue.ProjectID,
		MonitorID:      string(monitorValue.MonitorID),
	}, id, true
}

func parseIncidentListQuery(values url.Values) (incident.ListQuery, [][3]string) {
	query := incident.ListQuery{PageSize: 50}
	var failures [][3]string
	if value, present, valid := oneQueryValue(values, "pageSize"); present {
		if !valid {
			failures = append(failures, incidentQueryFailure(incidentPageSizeInvalidCode, "pageSize"))
		} else if parsed, err := strconv.Atoi(value); err != nil || parsed < 1 || parsed > incident.MaxPageSize {
			failures = append(failures, incidentQueryFailure(incidentPageSizeInvalidCode, "pageSize"))
		} else {
			query.PageSize = parsed
		}
	}
	if value, present, valid := oneQueryValue(values, "cursor"); present {
		if !valid {
			failures = append(failures, incidentQueryFailure(incidentCursorInvalidCode, "cursor"))
		} else if parsed, err := decodeIncidentCursor(value); err != nil {
			failures = append(failures, incidentQueryFailure(incidentCursorInvalidCode, "cursor"))
		} else {
			query.Cursor = &parsed
		}
	}
	return query, failures
}

func incidentQueryFailure(code, field string) [3]string {
	return [3]string{code, field, incidentQueryMessages[code]}
}

func encodeIncidentCursor(cursor incident.Cursor) (string, error) {
	payload, err := json.Marshal(incidentCursorPayload{
		Version: 1, CreatedAt: cursor.CreatedAt.UTC(), ID: cursor.ID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeIncidentCursor(value string) (incident.Cursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return incident.Cursor{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var decoded incidentCursorPayload
	if err = decoder.Decode(&decoded); err != nil {
		return incident.Cursor{}, err
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return incident.Cursor{}, errors.New("Incident cursor contains trailing JSON")
	}
	id, ok := canonicalUUID(decoded.ID)
	if decoded.Version != 1 || !ok || decoded.CreatedAt.IsZero() {
		return incident.Cursor{}, errors.New("Incident cursor is not valid")
	}
	return incident.Cursor{CreatedAt: decoded.CreatedAt.UTC(), ID: id}, nil
}

func toMonitorHealthResponse(value health.Snapshot) api.MonitorHealthResponse {
	response := api.MonitorHealthResponse{
		OrganizationID: value.OrganizationID, ProjectID: value.ProjectID, MonitorID: value.MonitorID,
		State: string(value.State), StableState: string(value.StableState),
		PolicyVersion: value.PolicyVersion, Version: value.Version,
		LastScheduledFor:          optionalInstant(value.LastScheduledFor),
		LastDeterminateFinishedAt: optionalInstant(value.LastDeterminateFinishedAt),
		LastRunID:                 optionalString(value.LastRunID),
		LastRunScheduledFor:       optionalInstant(value.LastRunScheduledFor),
		Counts: api.HealthCountsResponse{
			Configured: value.Counts.Configured, Eligible: value.Counts.Eligible,
			Responding: value.Counts.Responding, Passing: value.Counts.Passing,
			Failing: value.Counts.Failing, LocationFault: value.Counts.LocationFault,
			Indeterminate: value.Counts.Indeterminate, Missing: value.Counts.Missing,
		},
		TransitionedAt: value.TransitionedAt, UpdatedAt: value.UpdatedAt,
	}
	if value.SourceRevisionNumber != 0 {
		revision := value.SourceRevisionNumber
		response.SourceRevisionNumber = &revision
	}
	if value.Candidate != nil {
		response.Candidate = &api.HealthCandidateResponse{
			ID: value.Candidate.ID, Direction: string(value.Candidate.Direction),
			ExpectedEvidence:       string(value.Candidate.ExpectedEvidence),
			SourceRevisionNumber:   value.Candidate.SourceRevisionNumber,
			TriggeringRunID:        value.Candidate.TriggeringRunID,
			TriggeringScheduledFor: value.Candidate.TriggeringScheduledFor,
			RequestedAt:            value.Candidate.RequestedAt,
		}
	}
	return response
}

func toIncidentResponse(value incident.Incident) api.IncidentResponse {
	timeline := make([]api.IncidentTimelineResponse, len(value.Timeline))
	for index, entry := range value.Timeline {
		timeline[index] = api.IncidentTimelineResponse{
			ID: entry.ID, IncidentVersion: entry.IncidentVersion, Kind: string(entry.Kind),
			HealthTransitionID:    optionalString(entry.HealthTransitionID),
			ActorUserID:           optionalString(entry.ActorUserID),
			OldHealthState:        optionalString(entry.OldHealthState),
			NewHealthState:        optionalString(entry.NewHealthState),
			PolicyVersion:         optionalString(entry.PolicyVersion),
			CausalRunID:           optionalString(entry.CausalRunID),
			CausalRunScheduledFor: optionalInstant(entry.CausalRunScheduledFor),
			OccurredAt:            entry.OccurredAt,
		}
		if entry.Counts != nil {
			timeline[index].Counts = &api.HealthCountsResponse{
				Configured: entry.Counts.Configured, Eligible: entry.Counts.Eligible,
				Responding: entry.Counts.Responding, Passing: entry.Counts.Passing,
				Failing: entry.Counts.Failing, LocationFault: entry.Counts.LocationFault,
				Indeterminate: entry.Counts.Indeterminate, Missing: entry.Counts.Missing,
			}
		}
	}
	return api.IncidentResponse{
		ID: value.ID, OrganizationID: value.OrganizationID, ProjectID: value.ProjectID,
		MonitorID: value.MonitorID, State: string(value.State), Version: value.Version,
		OpenedTransitionID:   value.OpenedTransitionID,
		AcknowledgedBy:       optionalString(value.AcknowledgedBy),
		AcknowledgedAt:       optionalInstant(value.AcknowledgedAt),
		ResolvedTransitionID: optionalString(value.ResolvedTransitionID),
		ResolvedAt:           optionalInstant(value.ResolvedAt),
		CreatedAt:            value.CreatedAt, UpdatedAt: value.UpdatedAt, Timeline: timeline,
	}
}

const incidentInboxStateInvalidCode = "incident.inbox.state.invalid"

var incidentInboxQueryMessages = map[string]string{
	incidentInboxStateInvalidCode: "state must be one of active, open, acknowledged, or resolved.",
}

func (server *Server) organizationIncidents(w http.ResponseWriter, r *http.Request) {
	organizationID, ok := canonicalUUID(r.PathValue("organizationId"))
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if _, ok := server.requireOrganizationPermission(
		w, r, organizationID, organization.PermissionIncidentRead,
	); !ok {
		return
	}
	query, failures := parseIncidentInboxQuery(r.URL.Query())
	if len(failures) != 0 {
		writeValidationProblem(w, failures)
		return
	}
	page, found, err := server.incidents.ListInbox(r.Context(), organizationID, query)
	if err != nil {
		server.internalError(w, r, "list Organization Incidents", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	items := make([]api.IncidentInboxItemResponse, len(page.Items))
	for index, value := range page.Items {
		items[index] = toIncidentInboxItemResponse(value)
	}
	var nextCursor *string
	if page.NextCursor != nil {
		encoded, encodeErr := encodeIncidentCursor(*page.NextCursor)
		if encodeErr != nil {
			server.internalError(w, r, "encode Incident inbox cursor", encodeErr)
			return
		}
		nextCursor = &encoded
	}
	writeJSON(w, http.StatusOK, api.IncidentInboxPageResponse{Items: items, NextCursor: nextCursor})
}

func parseIncidentInboxQuery(values url.Values) (incident.InboxQuery, [][3]string) {
	query := incident.InboxQuery{PageSize: 50}
	var failures [][3]string
	if value, present, valid := oneQueryValue(values, "pageSize"); present {
		if !valid {
			failures = append(failures, incidentQueryFailure(incidentPageSizeInvalidCode, "pageSize"))
		} else if parsed, err := strconv.Atoi(value); err != nil || parsed < 1 || parsed > incident.MaxPageSize {
			failures = append(failures, incidentQueryFailure(incidentPageSizeInvalidCode, "pageSize"))
		} else {
			query.PageSize = parsed
		}
	}
	if value, present, valid := oneQueryValue(values, "state"); present {
		if !valid || !incidentInboxStateValid(value) {
			failures = append(failures, [3]string{incidentInboxStateInvalidCode, "state", incidentInboxQueryMessages[incidentInboxStateInvalidCode]})
		} else {
			query.State = incident.InboxState(value)
		}
	}
	if value, present, valid := oneQueryValue(values, "cursor"); present {
		if !valid {
			failures = append(failures, incidentQueryFailure(incidentCursorInvalidCode, "cursor"))
		} else if parsed, err := decodeIncidentCursor(value); err != nil {
			failures = append(failures, incidentQueryFailure(incidentCursorInvalidCode, "cursor"))
		} else {
			query.Cursor = &parsed
		}
	}
	return query, failures
}

func incidentInboxStateValid(value string) bool {
	switch incident.InboxState(value) {
	case incident.InboxStateActive, incident.InboxStateOpen,
		incident.InboxStateAcknowledged, incident.InboxStateResolved:
		return true
	default:
		return false
	}
}

func toIncidentInboxItemResponse(value incident.InboxItem) api.IncidentInboxItemResponse {
	response := api.IncidentInboxItemResponse{
		Incident: api.IncidentInboxIncidentResponse{
			ID: value.Incident.ID, OrganizationID: value.Incident.OrganizationID,
			ProjectID: value.Incident.ProjectID, MonitorID: value.Incident.MonitorID,
			State: string(value.Incident.State), Version: value.Incident.Version,
			OpenedTransitionID:   value.Incident.OpenedTransitionID,
			AcknowledgedBy:       optionalString(value.Incident.AcknowledgedBy),
			AcknowledgedAt:       optionalInstant(value.Incident.AcknowledgedAt),
			ResolvedTransitionID: optionalString(value.Incident.ResolvedTransitionID),
			ResolvedAt:           optionalInstant(value.Incident.ResolvedAt),
			CreatedAt:            value.Incident.CreatedAt, UpdatedAt: value.Incident.UpdatedAt,
		},
		Monitor: api.IncidentInboxMonitorResponse{
			ID: value.Incident.MonitorID, Name: value.MonitorName, State: value.MonitorState,
		},
	}
	if value.Health != nil {
		response.Health = &api.IncidentInboxHealthResponse{State: value.Health.State, UpdatedAt: value.Health.UpdatedAt}
	}
	if value.Maintenance != nil {
		response.Maintenance = &api.IncidentInboxMaintenanceResponse{
			ID: value.Maintenance.ID, State: value.Maintenance.State,
			StartsAt: value.Maintenance.StartsAt, EndsAt: value.Maintenance.EndsAt,
		}
	}
	if value.OpeningRun != nil {
		response.OpeningRun = &api.IncidentInboxRunResponse{
			ID: value.OpeningRun.ID, ScheduledFor: value.OpeningRun.ScheduledFor,
			Available: value.OpeningRun.Available,
		}
	}
	return response
}
