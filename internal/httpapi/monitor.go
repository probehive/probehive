package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/monitor"
)

var errUnexpectedResult = errors.New("use case returned an unknown result kind")

func (server *Server) monitorsRoot(w http.ResponseWriter, r *http.Request) {
	organizationID, projectID, ok := projectScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := server.requireAdministrator(w, r); !ok {
			return
		}
		values, found, err := server.monitors.List(r.Context(), organizationID, projectID)
		if err != nil {
			server.internalError(w, r, "list Monitors", err)
			return
		}
		if !found {
			writeStatusProblem(w, http.StatusNotFound)
			return
		}
		responses := make([]api.MonitorResponse, len(values))
		for index, value := range values {
			responses[index] = toMonitorResponse(value)
		}
		writeJSON(w, http.StatusOK, responses)
	case http.MethodPost:
		if _, ok := server.protectUnsafe(w, r, true); !ok {
			return
		}
		var request api.CreateMonitorRequest
		if !decodeJSON(w, r, &request) {
			return
		}
		result, err := server.monitors.Create(r.Context(), monitor.CreateCommand{
			OrganizationID: organizationID, ProjectID: projectID,
			Name: valueOrEmpty(request.Name), CheckType: valueOrEmpty(request.CheckType),
		})
		if err != nil {
			server.internalError(w, r, "create Monitor", err)
			return
		}
		switch result.Kind {
		case monitor.CreateCreated:
			response := toMonitorResponse(result.Monitor)
			w.Header().Set("Location", monitorPath(organizationID, projectID, response.ID))
			writeJSON(w, http.StatusCreated, response)
		case monitor.CreateProjectNotFound:
			writeStatusProblem(w, http.StatusNotFound)
		case monitor.CreateInvalid:
			writeValidationProblem(w, monitorFailurePairs(result.Failures))
		default:
			server.internalError(w, r, "create Monitor", errUnexpectedResult)
		}
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (server *Server) monitorItem(w http.ResponseWriter, r *http.Request) {
	scope, ok := monitorScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if _, ok = server.requireAdministrator(w, r); !ok {
		return
	}
	value, found, err := server.monitors.Get(r.Context(), scope)
	if err != nil {
		server.internalError(w, r, "get Monitor", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toMonitorResponse(value))
}

func (server *Server) renameMonitor(w http.ResponseWriter, r *http.Request) {
	scope, ok := monitorScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w, http.MethodPut)
		return
	}
	if _, ok = server.protectUnsafe(w, r, true); !ok {
		return
	}
	var request api.RenameMonitorRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := server.monitors.Rename(r.Context(), scope, valueOrEmpty(request.Name))
	server.writeMonitorUpdate(w, r, "rename Monitor", monitor.RenameRejectedTitle, result, err)
}

func (server *Server) changeMonitorState(w http.ResponseWriter, r *http.Request) {
	scope, ok := monitorScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w, http.MethodPut)
		return
	}
	if _, ok = server.protectUnsafe(w, r, true); !ok {
		return
	}
	var request api.ChangeMonitorStateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := server.monitors.ChangeState(r.Context(), scope, valueOrEmpty(request.State))
	server.writeMonitorUpdate(w, r, "change Monitor state", monitor.StateTransitionRejectedTitle, result, err)
}

func (server *Server) writeMonitorUpdate(
	w http.ResponseWriter, r *http.Request, operation, conflictTitle string,
	result monitor.UpdateResult, err error,
) {
	if err != nil {
		server.internalError(w, r, operation, err)
		return
	}
	switch result.Kind {
	case monitor.UpdateUpdated:
		writeJSON(w, http.StatusOK, toMonitorResponse(result.Monitor))
	case monitor.UpdateNotFound:
		writeStatusProblem(w, http.StatusNotFound)
	case monitor.UpdateInvalid:
		writeValidationProblem(w, monitorFailurePairs(result.Failures))
	case monitor.UpdateConflict:
		writeProblem(w, http.StatusConflict, conflictTitle, result.Detail)
	default:
		server.internalError(w, r, operation, errUnexpectedResult)
	}
}

func (server *Server) monitorRevisions(w http.ResponseWriter, r *http.Request) {
	scope, ok := monitorScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := server.requireAdministrator(w, r); !ok {
			return
		}
		values, found, err := server.monitors.ListRevisions(r.Context(), scope)
		if err != nil {
			server.internalError(w, r, "list Monitor revisions", err)
			return
		}
		if !found {
			writeStatusProblem(w, http.StatusNotFound)
			return
		}
		responses := make([]api.MonitorRevisionResponse, len(values))
		for index, value := range values {
			responses[index] = toMonitorRevisionResponse(value)
		}
		writeJSON(w, http.StatusOK, responses)
	case http.MethodPost:
		if _, ok := server.protectUnsafe(w, r, true); !ok {
			return
		}
		var request api.CreateMonitorRevisionRequest
		if !decodeJSON(w, r, &request) {
			return
		}
		result, err := server.monitors.CreateRevision(
			r.Context(), scope, int(request.CheckSchemaVersion), request.CheckConfiguration,
		)
		if err != nil {
			server.internalError(w, r, "create Monitor revision", err)
			return
		}
		switch result.Kind {
		case monitor.RevisionCreated:
			response := toMonitorRevisionResponse(result.Revision)
			w.Header().Set("Location", monitorPath(scope.OrganizationID, scope.ProjectID, string(scope.MonitorID))+"/revisions/"+strconv.Itoa(response.RevisionNumber))
			writeJSON(w, http.StatusCreated, response)
		case monitor.RevisionMonitorNotFound:
			writeStatusProblem(w, http.StatusNotFound)
		case monitor.RevisionInvalid:
			writeValidationProblem(w, monitorFailurePairs(result.Failures))
		case monitor.RevisionConflict:
			writeProblem(w, http.StatusConflict, monitor.RevisionRejectedTitle, result.Detail)
		default:
			server.internalError(w, r, "create Monitor revision", errUnexpectedResult)
		}
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (server *Server) monitorRevisionItem(w http.ResponseWriter, r *http.Request) {
	scope, ok := monitorScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	revisionNumber, err := strconv.ParseInt(r.PathValue("revisionNumber"), 10, 32)
	if err != nil {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if _, ok = server.requireAdministrator(w, r); !ok {
		return
	}
	value, found, err := server.monitors.GetRevision(r.Context(), scope, int(revisionNumber))
	if err != nil {
		server.internalError(w, r, "get Monitor revision", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toMonitorRevisionResponse(value))
}

func projectScope(r *http.Request) (string, string, bool) {
	organizationID, organizationOK := canonicalUUID(r.PathValue("organizationId"))
	projectID, projectOK := canonicalUUID(r.PathValue("projectId"))
	return organizationID, projectID, organizationOK && projectOK
}

func monitorScope(r *http.Request) (monitor.Scope, bool) {
	organizationID, projectID, projectOK := projectScope(r)
	monitorID, monitorOK := canonicalUUID(r.PathValue("monitorId"))
	return monitor.Scope{
		OrganizationID: organizationID, ProjectID: projectID, MonitorID: monitor.ID(monitorID),
	}, projectOK && monitorOK
}

func monitorPath(organizationID, projectID, monitorID string) string {
	return "/api/v1/organizations/" + organizationID + "/projects/" + projectID + "/monitors/" + monitorID
}

func toMonitorResponse(value monitor.Monitor) api.MonitorResponse {
	return api.MonitorResponse{
		ID: string(value.ID), OrganizationID: value.OrganizationID, ProjectID: value.ProjectID,
		Name: value.Name, CheckType: value.CheckType, State: string(value.State),
		LatestRevisionNumber: value.LatestRevisionNumber,
		CreatedAt:            value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func toMonitorRevisionResponse(value monitor.Revision) api.MonitorRevisionResponse {
	return api.MonitorRevisionResponse{
		ID: string(value.ID), MonitorID: string(value.MonitorID), RevisionNumber: value.RevisionNumber,
		CheckType: value.CheckType, CheckSchemaVersion: value.CheckSchemaVersion,
		CheckConfiguration: value.CheckConfiguration, CreatedAt: value.CreatedAt,
	}
}

func monitorFailurePairs(failures []monitor.ValidationFailure) [][2]string {
	pairs := make([][2]string, len(failures))
	for index, failure := range failures {
		pairs[index] = [2]string{failure.Field, failure.Message}
	}
	return pairs
}
