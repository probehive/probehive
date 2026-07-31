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

	"github.com/probehive/probehive/internal/alert"
	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
)

const (
	alertPageSizeInvalidCode = "alert.query.pageSize.invalid"
	alertCursorInvalidCode   = "alert.query.cursor.invalid"
)

var alertQueryMessages = map[string]string{
	alertPageSizeInvalidCode: "pageSize must be one integer from 1 through 100.",
	alertCursorInvalidCode:   "cursor must be one cursor returned by this endpoint.",
}

type alertCursorPayload struct {
	Version    int       `json:"version"`
	OccurredAt time.Time `json:"occurredAt"`
	ID         string    `json:"id"`
}

func (server *Server) monitorAlerts(w http.ResponseWriter, r *http.Request) {
	monitorValue, ok := monitorScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if _, ok := server.requireOrganizationPermission(
		w, r, monitorValue.OrganizationID, organization.PermissionAlertRead,
	); !ok {
		return
	}
	query, failures := parseAlertListQuery(r.URL.Query())
	if len(failures) != 0 {
		writeValidationProblem(w, failures)
		return
	}
	page, found, err := server.alerts.List(r.Context(), alert.Scope{
		OrganizationID: monitorValue.OrganizationID,
		ProjectID:      monitorValue.ProjectID,
		MonitorID:      string(monitorValue.MonitorID),
	}, query)
	if err != nil {
		server.internalError(w, r, "list Alerts", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	items := make([]api.AlertResponse, len(page.Alerts))
	for index, value := range page.Alerts {
		items[index] = toAlertResponse(value)
	}
	var nextCursor *string
	if page.NextCursor != nil {
		encoded, encodeErr := encodeAlertCursor(*page.NextCursor)
		if encodeErr != nil {
			server.internalError(w, r, "encode Alert cursor", encodeErr)
			return
		}
		nextCursor = &encoded
	}
	writeJSON(w, http.StatusOK, api.AlertPageResponse{Items: items, NextCursor: nextCursor})
}

func parseAlertListQuery(values url.Values) (alert.ListQuery, [][3]string) {
	query := alert.ListQuery{PageSize: 50}
	var failures [][3]string
	if value, present, valid := oneQueryValue(values, "pageSize"); present {
		if !valid {
			failures = append(failures, alertQueryFailure(alertPageSizeInvalidCode, "pageSize"))
		} else if parsed, err := strconv.Atoi(value); err != nil || parsed < 1 || parsed > alert.MaxPageSize {
			failures = append(failures, alertQueryFailure(alertPageSizeInvalidCode, "pageSize"))
		} else {
			query.PageSize = parsed
		}
	}
	if value, present, valid := oneQueryValue(values, "cursor"); present {
		if !valid {
			failures = append(failures, alertQueryFailure(alertCursorInvalidCode, "cursor"))
		} else if parsed, err := decodeAlertCursor(value); err != nil {
			failures = append(failures, alertQueryFailure(alertCursorInvalidCode, "cursor"))
		} else {
			query.Cursor = &parsed
		}
	}
	return query, failures
}

func alertQueryFailure(code, field string) [3]string {
	return [3]string{code, field, alertQueryMessages[code]}
}

func encodeAlertCursor(cursor alert.Cursor) (string, error) {
	payload, err := json.Marshal(alertCursorPayload{
		Version: 1, OccurredAt: cursor.OccurredAt.UTC(), ID: cursor.ID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeAlertCursor(value string) (alert.Cursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return alert.Cursor{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var decoded alertCursorPayload
	if err = decoder.Decode(&decoded); err != nil {
		return alert.Cursor{}, err
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return alert.Cursor{}, errors.New("Alert cursor contains trailing JSON")
	}
	id, ok := canonicalUUID(decoded.ID)
	if decoded.Version != 1 || !ok || decoded.OccurredAt.IsZero() {
		return alert.Cursor{}, errors.New("Alert cursor is not valid")
	}
	return alert.Cursor{OccurredAt: decoded.OccurredAt.UTC(), ID: id}, nil
}

func toAlertResponse(value alert.Alert) api.AlertResponse {
	return api.AlertResponse{
		ID: value.ID, OrganizationID: value.OrganizationID, ProjectID: value.ProjectID,
		MonitorID: value.MonitorID, IncidentID: value.IncidentID,
		IncidentVersion: value.IncidentVersion, Kind: string(value.Kind),
		OccurredAt: value.OccurredAt, CreatedAt: value.CreatedAt,
	}
}
