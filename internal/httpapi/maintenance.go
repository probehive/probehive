package httpapi

import (
	"net/http"
	"time"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/maintenance"
	"github.com/probehive/probehive/internal/organization"
)

func (server *Server) readMaintenance(
	w http.ResponseWriter, r *http.Request, organizationID string,
) (*authenticatedSession, bool) {
	return server.requireOrganizationPermission(
		w, r, organizationID, organization.PermissionMaintenanceRead,
	)
}

func (server *Server) writeMaintenance(
	w http.ResponseWriter, r *http.Request, organizationID string,
) (*authenticatedSession, bool) {
	return server.protectUnsafe(w, r, func(
		writer http.ResponseWriter, request *http.Request,
	) (*authenticatedSession, bool) {
		return server.requireOrganizationPermission(
			writer, request, organizationID, organization.PermissionMaintenanceWrite,
		)
	})
}

func (server *Server) maintenanceWindows(w http.ResponseWriter, r *http.Request) {
	scope, ok := maintenanceScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok = server.readMaintenance(w, r, scope.OrganizationID); !ok {
			return
		}
		values, found, err := server.maintenance.List(r.Context(), scope)
		if err != nil {
			server.internalError(w, r, "list maintenance windows", err)
			return
		}
		if !found {
			writeStatusProblem(w, http.StatusNotFound)
			return
		}
		now := server.clock.Now().UTC()
		responses := make([]api.MaintenanceWindowResponse, len(values))
		for index, value := range values {
			responses[index] = toMaintenanceWindowResponse(value, now)
		}
		writeJSON(w, http.StatusOK, responses)
	case http.MethodPost:
		if _, ok = server.writeMaintenance(w, r, scope.OrganizationID); !ok {
			return
		}
		var request api.CreateMaintenanceWindowRequest
		if !decodeJSON(w, r, &request) {
			return
		}
		result, err := server.maintenance.Create(r.Context(), maintenance.CreateCommand{
			Scope: scope, StartsAt: timeOrZero(request.StartsAt), EndsAt: timeOrZero(request.EndsAt),
		})
		if err != nil {
			server.internalError(w, r, "create maintenance window", err)
			return
		}
		switch result.Kind {
		case maintenance.CreateCreated:
			response := toMaintenanceWindowResponse(result.Window, server.clock.Now().UTC())
			w.Header().Set("Location", maintenanceWindowPath(scope, response.ID))
			writeJSON(w, http.StatusCreated, response)
		case maintenance.CreateMonitorNotFound:
			writeStatusProblem(w, http.StatusNotFound)
		case maintenance.CreateInvalid:
			writeValidationProblem(w, maintenanceFailurePairs(result.Failures))
		case maintenance.CreateConflict:
			writeCodedProblem(
				w, http.StatusConflict, result.Code, maintenance.CreateRejectedTitle, result.Detail,
			)
		default:
			server.internalError(w, r, "create maintenance window", errUnexpectedResult)
		}
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (server *Server) maintenanceWindowItem(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := maintenanceWindowScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if _, ok = server.readMaintenance(w, r, scope.OrganizationID); !ok {
		return
	}
	value, found, err := server.maintenance.Get(r.Context(), scope, id)
	if err != nil {
		server.internalError(w, r, "get maintenance window", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toMaintenanceWindowResponse(value, server.clock.Now().UTC()))
}

func (server *Server) cancelMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	scope, id, ok := maintenanceWindowScope(r)
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if _, ok = server.writeMaintenance(w, r, scope.OrganizationID); !ok {
		return
	}
	result, err := server.maintenance.Cancel(r.Context(), scope, id)
	if err != nil {
		server.internalError(w, r, "cancel maintenance window", err)
		return
	}
	switch result.Kind {
	case maintenance.CancelCancelled:
		writeJSON(
			w, http.StatusOK,
			toMaintenanceWindowResponse(result.Window, server.clock.Now().UTC()),
		)
	case maintenance.CancelNotFound:
		writeStatusProblem(w, http.StatusNotFound)
	case maintenance.CancelConflict:
		writeCodedProblem(
			w, http.StatusConflict, result.Code, maintenance.CancelRejectedTitle, result.Detail,
		)
	default:
		server.internalError(w, r, "cancel maintenance window", errUnexpectedResult)
	}
}

func maintenanceScope(r *http.Request) (maintenance.Scope, bool) {
	monitorValue, ok := monitorScope(r)
	return maintenance.Scope{
		OrganizationID: monitorValue.OrganizationID,
		ProjectID:      monitorValue.ProjectID,
		MonitorID:      string(monitorValue.MonitorID),
	}, ok
}

func maintenanceWindowScope(r *http.Request) (maintenance.Scope, maintenance.ID, bool) {
	scope, scopeOK := maintenanceScope(r)
	id, idOK := canonicalUUID(r.PathValue("maintenanceWindowId"))
	return scope, maintenance.ID(id), scopeOK && idOK
}

func maintenanceWindowPath(scope maintenance.Scope, id string) string {
	return monitorPath(scope.OrganizationID, scope.ProjectID, scope.MonitorID) +
		"/maintenance-windows/" + id
}

func timeOrZero(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func toMaintenanceWindowResponse(
	value maintenance.Window, now time.Time,
) api.MaintenanceWindowResponse {
	return api.MaintenanceWindowResponse{
		ID: string(value.ID), OrganizationID: value.OrganizationID,
		ProjectID: value.ProjectID, MonitorID: value.MonitorID,
		StartsAt: value.StartsAt, EndsAt: value.EndsAt, Status: string(value.Status(now)),
		CreatedAt: value.CreatedAt, CancelledAt: value.CancelledAt,
	}
}

func maintenanceFailurePairs(failures []maintenance.ValidationFailure) [][3]string {
	triples := make([][3]string, len(failures))
	for index, failure := range failures {
		triples[index] = [3]string{failure.Code, failure.Field, failure.Message}
	}
	return triples
}
