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

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/run"
)

const (
	runNotBeforeInvalidCode = "run.query.notBefore.invalid"
	runPageSizeInvalidCode  = "run.query.pageSize.invalid"
	runCursorInvalidCode    = "run.query.cursor.invalid"
	runOutcomeInvalidCode   = "run.query.outcome.invalid"
	runKindInvalidCode      = "run.query.kind.invalid"
	runLocationInvalidCode  = "run.query.location.invalid"
)

var runQueryMessages = map[string]string{
	runNotBeforeInvalidCode: "notBefore must be one RFC 3339 timestamp.",
	runPageSizeInvalidCode:  "pageSize must be one integer from 1 through 500.",
	runCursorInvalidCode:    "cursor must be one cursor returned by this endpoint.",
	runOutcomeInvalidCode:   "outcome must be passed, failed, errored, timedout, cancelled, or skipped.",
	runKindInvalidCode:      "kind must be scheduled, confirmation, or manual.",
	runLocationInvalidCode:  "location must be one identifier from 1 through 63 bytes.",
}

type runCursorPayload struct {
	Version      int       `json:"version"`
	ScheduledFor time.Time `json:"scheduledFor"`
	ID           string    `json:"id"`
}

func (server *Server) monitorRuns(w http.ResponseWriter, r *http.Request) {
	scope, ok := queryRunScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if _, ok = server.readMonitors(w, r, scope.OrganizationID); !ok {
		return
	}
	query, failures := parseRunListQuery(r.URL.Query())
	if len(failures) != 0 {
		writeValidationProblem(w, failures)
		return
	}
	page, found, err := server.runs.List(r.Context(), scope, query)
	if err != nil {
		server.internalError(w, r, "list Runs", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	items := make([]api.RunResponse, len(page.Runs))
	for index, value := range page.Runs {
		items[index] = toRunResponse(scope.ProjectID, value)
	}
	var nextCursor *string
	if page.NextCursor != nil {
		encoded, encodeErr := encodeRunCursor(*page.NextCursor)
		if encodeErr != nil {
			server.internalError(w, r, "encode Run cursor", encodeErr)
			return
		}
		nextCursor = &encoded
	}
	writeJSON(w, http.StatusOK, api.RunPageResponse{Items: items, NextCursor: nextCursor})
}

func (server *Server) monitorRun(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := queryRunIdentity(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if _, ok = server.readMonitors(w, r, scope.OrganizationID); !ok {
		return
	}
	value, found, err := server.runs.Get(r.Context(), scope, id)
	if err != nil {
		server.internalError(w, r, "get Run", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toRunResponse(scope.ProjectID, value))
}

func (server *Server) monitorRunObservation(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := queryRunIdentity(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if _, ok = server.readMonitors(w, r, scope.OrganizationID); !ok {
		return
	}
	value, found, err := server.runs.GetObservation(r.Context(), scope, id)
	if err != nil {
		server.internalError(w, r, "get Observation", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toObservationResponse(value))
}

func queryRunScope(r *http.Request) (run.Scope, bool) {
	scope, ok := monitorScope(r)
	return run.Scope{
		OrganizationID: scope.OrganizationID,
		ProjectID:      scope.ProjectID,
		MonitorID:      string(scope.MonitorID),
	}, ok
}

func queryRunIdentity(r *http.Request) (run.Scope, run.ID, bool) {
	scope, scopeOK := queryRunScope(r)
	id, idOK := canonicalUUID(r.PathValue("runId"))
	return scope, run.ID(id), scopeOK && idOK
}

func parseRunListQuery(values url.Values) (run.ListQuery, [][3]string) {
	query := run.ListQuery{PageSize: run.DefaultPageSize}
	var failures [][3]string

	notBefore, present, valid := oneQueryValue(values, "notBefore")
	if !present || !valid {
		failures = append(failures, runQueryFailure(runNotBeforeInvalidCode, "notBefore"))
	} else if parsed, err := time.Parse(time.RFC3339, notBefore); err != nil {
		failures = append(failures, runQueryFailure(runNotBeforeInvalidCode, "notBefore"))
	} else {
		query.NotBefore = parsed.UTC()
	}

	if value, present, valid := oneQueryValue(values, "pageSize"); present {
		if !valid {
			failures = append(failures, runQueryFailure(runPageSizeInvalidCode, "pageSize"))
		} else if parsed, err := strconv.Atoi(value); err != nil || parsed < 1 || parsed > run.MaxPageSize {
			failures = append(failures, runQueryFailure(runPageSizeInvalidCode, "pageSize"))
		} else {
			query.PageSize = parsed
		}
	}

	if value, present, valid := oneQueryValue(values, "cursor"); present {
		if !valid {
			failures = append(failures, runQueryFailure(runCursorInvalidCode, "cursor"))
		} else if parsed, err := decodeRunCursor(value); err != nil {
			failures = append(failures, runQueryFailure(runCursorInvalidCode, "cursor"))
		} else {
			query.Cursor = &parsed
		}
	}

	if value, present, valid := oneQueryValue(values, "outcome"); present {
		if !valid || !validRunOutcome(value) {
			failures = append(failures, runQueryFailure(runOutcomeInvalidCode, "outcome"))
		} else {
			query.Outcome = run.Outcome(value)
		}
	}

	if value, present, valid := oneQueryValue(values, "kind"); present {
		if !valid || !validRunKind(value) {
			failures = append(failures, runQueryFailure(runKindInvalidCode, "kind"))
		} else {
			query.Kind = run.Kind(value)
		}
	}

	if value, present, valid := oneQueryValue(values, "location"); present {
		if !valid || len(value) > run.MaxLocationLength {
			failures = append(failures, runQueryFailure(runLocationInvalidCode, "location"))
		} else {
			query.Location = value
		}
	}

	return query, failures
}

func oneQueryValue(values url.Values, name string) (string, bool, bool) {
	found, present := values[name]
	if !present {
		return "", false, true
	}
	if len(found) != 1 || found[0] == "" {
		return "", true, false
	}
	return found[0], true, true
}

func runQueryFailure(code, field string) [3]string {
	return [3]string{code, field, runQueryMessages[code]}
}

func validRunOutcome(value string) bool {
	switch run.Outcome(value) {
	case run.OutcomePassed, run.OutcomeFailed, run.OutcomeErrored,
		run.OutcomeTimedOut, run.OutcomeCancelled, run.OutcomeSkipped:
		return true
	default:
		return false
	}
}

func validRunKind(value string) bool {
	switch run.Kind(value) {
	case run.KindScheduled, run.KindConfirmation, run.KindManual:
		return true
	default:
		return false
	}
}

func encodeRunCursor(cursor run.Cursor) (string, error) {
	payload, err := json.Marshal(runCursorPayload{
		Version: 1, ScheduledFor: cursor.ScheduledFor.UTC(), ID: string(cursor.ID),
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeRunCursor(value string) (run.Cursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return run.Cursor{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var decoded runCursorPayload
	if err = decoder.Decode(&decoded); err != nil {
		return run.Cursor{}, err
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return run.Cursor{}, errors.New("Run cursor contains trailing JSON")
	}
	id, ok := canonicalUUID(decoded.ID)
	if decoded.Version != 1 || !ok || decoded.ScheduledFor.IsZero() {
		return run.Cursor{}, errors.New("Run cursor is not valid")
	}
	return run.Cursor{ScheduledFor: decoded.ScheduledFor.UTC(), ID: run.ID(id)}, nil
}

func toRunResponse(projectID string, value run.Run) api.RunResponse {
	response := api.RunResponse{
		ID:             string(value.ID),
		OrganizationID: value.Slot.OrganizationID,
		ProjectID:      projectID,
		MonitorID:      value.Slot.MonitorID,
		RevisionNumber: value.Slot.RevisionNumber,
		Location:       value.Slot.Location,
		ScheduledFor:   value.Slot.ScheduledFor,
		Kind:           string(value.Kind),
		Outcome:        optionalString(string(value.Outcome)),
		StartedAt:      optionalInstant(value.StartedAt),
		FinishedAt:     optionalInstant(value.FinishedAt),
		LeaseExpiresAt: optionalInstant(value.LeaseExpiresAt),
	}
	if value.Confirmation != nil {
		response.Confirmation = &api.ConfirmationCauseResponse{
			CandidateID:            value.Confirmation.CandidateID,
			TriggeringRunID:        string(value.Confirmation.TriggeringRunID),
			TriggeringScheduledFor: value.Confirmation.TriggeringScheduledFor,
			CausationEventID:       value.Confirmation.CausationEventID,
			PolicyVersion:          value.Confirmation.PolicyVersion,
		}
	}
	return response
}

func toObservationResponse(value run.Observation) api.ObservationResponse {
	response := api.ObservationResponse{
		RunID:                string(value.RunID),
		OrganizationID:       value.OrganizationID,
		ScheduledFor:         value.ScheduledFor,
		FailureCode:          value.FailureCode,
		FailureClass:         value.FailureClass,
		DurationMicroseconds: value.Duration.Microseconds(),
		Phases: api.ObservationPhasesResponse{
			ConnectMicroseconds:   value.Phases.Connect.Microseconds(),
			TLSMicroseconds:       value.Phases.TLS.Microseconds(),
			FirstByteMicroseconds: value.Phases.FirstByte.Microseconds(),
		},
	}
	if value.HTTP != nil {
		httpDetail := api.HTTPObservationResponse{
			StatusCode:    value.HTTP.StatusCode,
			Protocol:      value.HTTP.Protocol,
			RedirectCount: value.HTTP.RedirectCount,
			BodyBytes:     value.HTTP.BodyBytes,
			BodyTruncated: value.HTTP.BodyTruncated,
		}
		if value.HTTP.TLS != nil {
			httpDetail.TLS = &api.TLSObservationResponse{
				Version:              value.HTTP.TLS.Version,
				CipherSuite:          value.HTTP.TLS.CipherSuite,
				CertificateExpiresAt: optionalInstant(value.HTTP.TLS.CertificateExpiresAt),
			}
		}
		response.HTTP = &httpDetail
	}
	return response
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalInstant(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	instant := value.UTC()
	return &instant
}
