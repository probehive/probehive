package httpapi

import (
	"net/http"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/webhook"
)

func (server *Server) webhookIntegrationsRoot(w http.ResponseWriter, r *http.Request) {
	organizationID, ok := canonicalUUID(r.PathValue("organizationId"))
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		server.listWebhookIntegrations(w, r, organizationID)
	case http.MethodPost:
		server.createWebhookIntegration(w, r, organizationID)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (server *Server) listWebhookIntegrations(
	w http.ResponseWriter, r *http.Request, organizationID string,
) {
	if _, ok := server.requireOrganizationPermission(
		w, r, organizationID, organization.PermissionIntegrationManage,
	); !ok {
		return
	}
	values, err := server.webhooks.List(r.Context(), organizationID)
	if err != nil {
		server.internalError(w, r, "list Webhook Integrations", err)
		return
	}
	responses := make([]api.WebhookIntegrationResponse, len(values))
	for index, value := range values {
		responses[index] = toWebhookIntegrationResponse(value)
	}
	writeJSON(w, http.StatusOK, responses)
}

func (server *Server) createWebhookIntegration(
	w http.ResponseWriter, r *http.Request, organizationID string,
) {
	if _, ok := server.protectUnsafe(w, r, func(
		writer http.ResponseWriter, request *http.Request,
	) (*authenticatedSession, bool) {
		return server.requireOrganizationPermission(
			writer, request, organizationID, organization.PermissionIntegrationManage,
		)
	}); !ok {
		return
	}

	var request api.CreateWebhookIntegrationRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := server.webhooks.Create(r.Context(), webhook.CreateCommand{
		OrganizationID: organizationID,
		Name:           valueOrEmpty(request.Name),
		DestinationURL: valueOrEmpty(request.DestinationURL),
	})
	if err != nil {
		server.internalError(w, r, "create Webhook Integration", err)
		return
	}
	switch result.Kind {
	case webhook.CreateCreated:
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusCreated, api.CreateWebhookIntegrationResponse{
			Integration:   toWebhookIntegrationResponse(result.Integration),
			SigningSecret: result.SigningSecret,
		})
	case webhook.CreateInvalid:
		writeValidationProblem(w, webhookFailurePairs(result.Failures))
	case webhook.CreateConflict:
		writeCodedProblem(
			w, http.StatusConflict, result.Code,
			"Webhook Integration name already in use", result.Detail,
		)
	case webhook.CreateKeyringUnavailable:
		writeCodedProblem(
			w, http.StatusServiceUnavailable, result.Code,
			"Webhook configuration unavailable", result.Detail,
		)
	default:
		server.internalError(w, r, "create Webhook Integration", errUnexpectedResult)
	}
}

func toWebhookIntegrationResponse(value webhook.Integration) api.WebhookIntegrationResponse {
	return api.WebhookIntegrationResponse{
		ID: value.ID, OrganizationID: value.OrganizationID, Name: value.Name,
		DestinationURL: value.DestinationURL, Enabled: value.Enabled,
		Version: value.Version, ActiveSecretVersion: value.ActiveSecretVersion,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func webhookFailurePairs(failures []webhook.ValidationFailure) [][3]string {
	triples := make([][3]string, len(failures))
	for index, failure := range failures {
		triples[index] = [3]string{failure.Code, failure.Field, failure.Message}
	}
	return triples
}
