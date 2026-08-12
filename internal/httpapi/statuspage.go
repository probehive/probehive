package httpapi

import (
	"fmt"
	"net/http"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/statuspage"
)

func (server *Server) statusPageDraft(w http.ResponseWriter, r *http.Request) {
	organizationID, ok := canonicalUUID(r.PathValue("organizationId"))
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok = server.requireOrganizationPermission(
			w, r, organizationID, organization.PermissionStatusPageWrite,
		); !ok {
			return
		}
		draft, found, err := server.statusPages.Get(r.Context(), organizationID)
		if err != nil {
			server.internalError(w, r, "get status page draft", err)
			return
		}
		if !found {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, toStatusPageDraftResponse(draft))
	case http.MethodPut:
		if _, ok = server.protectUnsafe(w, r, func(
			writer http.ResponseWriter, request *http.Request,
		) (*authenticatedSession, bool) {
			return server.requireOrganizationPermission(
				writer, request, organizationID, organization.PermissionStatusPageWrite,
			)
		}); !ok {
			return
		}
		var request api.ReplaceStatusPageDraftRequest
		if !decodeJSON(w, r, &request) {
			return
		}
		components := make([]statuspage.ComponentInput, len(request.Components))
		canonicalFailures := make([]statuspage.ValidationFailure, 0)
		for index, component := range request.Components {
			monitorID, canonical := canonicalUUID(valueOrEmpty(component.MonitorID))
			if !canonical {
				canonicalFailures = append(canonicalFailures, statuspage.ValidationFailure{
					Code:    statuspage.ComponentMonitorInvalidCode,
					Field:   fmt.Sprintf("components[%d].monitorId", index),
					Message: statuspage.ComponentMonitorValidationMessage,
				})
			}
			components[index] = statuspage.ComponentInput{
				MonitorID: monitorID, Label: valueOrEmpty(component.Label),
			}
		}
		if len(canonicalFailures) != 0 {
			writeValidationProblem(w, statusPageFailurePairs(canonicalFailures))
			return
		}
		result, err := server.statusPages.Replace(r.Context(), statuspage.ReplaceCommand{
			OrganizationID: organizationID, Title: valueOrEmpty(request.Title),
			Version: int64(request.Version), Components: components,
		})
		if err != nil {
			server.internalError(w, r, "replace status page draft", err)
			return
		}
		switch result.Kind {
		case statuspage.ReplaceUpdated:
			writeJSON(w, http.StatusOK, toStatusPageDraftResponse(result.Draft))
		case statuspage.ReplaceInvalid:
			writeValidationProblem(w, statusPageFailurePairs(result.Failures))
		case statuspage.ReplaceConflict:
			writeCodedProblem(
				w, http.StatusConflict, result.Code,
				statuspage.UpdateRejectedTitle, result.Detail,
			)
		default:
			server.internalError(w, r, "replace status page draft", errUnexpectedResult)
		}
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPut)
	}
}

func toStatusPageDraftResponse(value statuspage.Draft) api.StatusPageDraftResponse {
	components := make([]api.StatusComponentResponse, len(value.Components))
	for index, component := range value.Components {
		components[index] = api.StatusComponentResponse{
			ID: string(component.ID), MonitorID: component.MonitorID,
			Label: component.Label, Position: component.Position,
		}
	}
	return api.StatusPageDraftResponse{
		ID: string(value.ID), OrganizationID: value.OrganizationID,
		Title: value.Title, Version: value.Version, Components: components,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func statusPageFailurePairs(failures []statuspage.ValidationFailure) [][3]string {
	triples := make([][3]string, len(failures))
	for index, failure := range failures {
		triples[index] = [3]string{failure.Code, failure.Field, failure.Message}
	}
	return triples
}
