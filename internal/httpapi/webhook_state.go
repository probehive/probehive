package httpapi

import (
	"net/http"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/webhook"
)

func (server *Server) changeWebhookIntegrationState(w http.ResponseWriter, r *http.Request) {
	organizationID, ok := canonicalUUID(r.PathValue("organizationId"))
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	integrationID, ok := canonicalUUID(r.PathValue("integrationId"))
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w, http.MethodPut)
		return
	}
	if _, ok := server.protectUnsafe(w, r, func(
		writer http.ResponseWriter, request *http.Request,
	) (*authenticatedSession, bool) {
		return server.requireOrganizationPermission(
			writer, request, organizationID, organization.PermissionIntegrationManage,
		)
	}); !ok {
		return
	}

	var request api.WebhookIntegrationStateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := server.webhooks.SetEnabled(r.Context(), webhook.StateCommand{
		OrganizationID: organizationID, IntegrationID: integrationID,
		ExpectedVersion: int64(request.Version), Enabled: request.Enabled,
	})
	if err != nil {
		server.internalError(w, r, "change Webhook Integration state", err)
		return
	}
	switch result.Kind {
	case webhook.StateUpdated:
		writeJSON(w, http.StatusOK, toWebhookIntegrationResponse(result.Integration))
	case webhook.StateInvalid:
		writeValidationProblem(w, webhookFailurePairs(result.Failures))
	case webhook.StateNotFound:
		writeStatusProblem(w, http.StatusNotFound)
	case webhook.StateConflict:
		writeCodedProblem(
			w, http.StatusConflict, result.Code,
			"Webhook Integration state conflict", result.Detail,
		)
	default:
		server.internalError(w, r, "change Webhook Integration state", errUnexpectedResult)
	}
}
