package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/organization"
)

var errUnexpectedResult = errors.New("use case returned an unknown result kind")

func (server *Server) readMonitors(
	w http.ResponseWriter, r *http.Request, organizationID string,
) (*authenticatedSession, bool) {
	return server.requireOrganizationPermission(w, r, organizationID, organization.PermissionMonitorRead)
}

func (server *Server) writeMonitors(
	w http.ResponseWriter, r *http.Request, organizationID string,
) (*authenticatedSession, bool) {
	return server.protectUnsafe(w, r, func(
		writer http.ResponseWriter, request *http.Request,
	) (*authenticatedSession, bool) {
		return server.requireOrganizationPermission(
			writer, request, organizationID, organization.PermissionMonitorWrite,
		)
	})
}

func (server *Server) monitorsRoot(w http.ResponseWriter, r *http.Request) {
	organizationID, projectID, ok := projectScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := server.readMonitors(w, r, organizationID); !ok {
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
		if _, ok := server.writeMonitors(w, r, organizationID); !ok {
			return
		}
		var request api.CreateMonitorRequest
		if !decodeJSON(w, r, &request) {
			return
		}
		result, err := server.monitors.Create(r.Context(), monitor.CreateCommand{
			OrganizationID: organizationID, ProjectID: projectID,
			Name: valueOrEmpty(request.Name), CheckType: valueOrEmpty(request.CheckType),
			IntervalSeconds: intervalOrDefault(request.IntervalSeconds),
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
	if _, ok = server.readMonitors(w, r, scope.OrganizationID); !ok {
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
	if _, ok = server.writeMonitors(w, r, scope.OrganizationID); !ok {
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
	if _, ok = server.writeMonitors(w, r, scope.OrganizationID); !ok {
		return
	}
	var request api.ChangeMonitorStateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := server.monitors.ChangeState(r.Context(), scope, valueOrEmpty(request.State))
	server.writeMonitorUpdate(w, r, "change Monitor state", monitor.StateTransitionRejectedTitle, result, err)
}

func (server *Server) changeMonitorInterval(w http.ResponseWriter, r *http.Request) {
	scope, ok := monitorScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w, http.MethodPut)
		return
	}
	if _, ok = server.writeMonitors(w, r, scope.OrganizationID); !ok {
		return
	}
	var request api.ChangeMonitorIntervalRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := server.monitors.ChangeInterval(r.Context(), scope, int(request.IntervalSeconds))
	server.writeMonitorUpdate(w, r, "change Monitor interval", monitor.IntervalRejectedTitle, result, err)
}

// intervalOrDefault treats an omitted interval as the product default. An explicit zero is
// left alone so it reaches the validator and is rejected with the interval code, rather than
// being silently read as "no preference".
func intervalOrDefault(value *api.Integer) int {
	if value == nil {
		return monitor.DefaultIntervalSeconds
	}
	return int(*value)
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
		writeCodedProblem(w, http.StatusConflict, result.Code, conflictTitle, result.Detail)
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
		if _, ok := server.readMonitors(w, r, scope.OrganizationID); !ok {
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
		if _, ok := server.writeMonitors(w, r, scope.OrganizationID); !ok {
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
			writeCodedProblem(w, http.StatusConflict, result.Code, monitor.RevisionRejectedTitle, result.Detail)
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
	if _, ok = server.readMonitors(w, r, scope.OrganizationID); !ok {
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
		IntervalSeconds:      value.IntervalSeconds,
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

func monitorFailurePairs(failures []monitor.ValidationFailure) [][3]string {
	triples := make([][3]string, len(failures))
	for index, failure := range failures {
		triples[index] = [3]string{failure.Code, failure.Field, failure.Message}
	}
	return triples
}
