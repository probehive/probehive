package httpapi

import (
	"net/http"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/webhook"
)

func (server *Server) prepareWebhookSigningSecret(w http.ResponseWriter, r *http.Request) {
	command, ok := server.webhookRotationCommand(w, r)
	if !ok {
		return
	}
	result, err := server.webhooks.PrepareRotation(r.Context(), command)
	server.writeWebhookRotationResult(w, r, "prepare Webhook signing secret", result, err, true)
}

func (server *Server) activateWebhookSigningSecret(w http.ResponseWriter, r *http.Request) {
	command, ok := server.webhookRotationCommand(w, r)
	if !ok {
		return
	}
	result, err := server.webhooks.ActivateRotation(r.Context(), command)
	server.writeWebhookRotationResult(w, r, "activate Webhook signing secret", result, err, false)
}

func (server *Server) retireWebhookSigningSecret(w http.ResponseWriter, r *http.Request) {
	command, ok := server.webhookRotationCommand(w, r)
	if !ok {
		return
	}
	result, err := server.webhooks.RetireRotation(r.Context(), command)
	server.writeWebhookRotationResult(w, r, "retire Webhook signing secret", result, err, false)
}

func (server *Server) webhookRotationCommand(
	w http.ResponseWriter, r *http.Request,
) (webhook.RotationCommand, bool) {
	organizationID, ok := canonicalUUID(r.PathValue("organizationId"))
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return webhook.RotationCommand{}, false
	}
	integrationID, ok := canonicalUUID(r.PathValue("integrationId"))
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return webhook.RotationCommand{}, false
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return webhook.RotationCommand{}, false
	}
	if _, ok = server.protectUnsafe(w, r, func(
		writer http.ResponseWriter, request *http.Request,
	) (*authenticatedSession, bool) {
		return server.requireOrganizationPermission(
			writer, request, organizationID, organization.PermissionIntegrationManage,
		)
	}); !ok {
		return webhook.RotationCommand{}, false
	}

	var request api.WebhookIntegrationVersionRequest
	if !decodeJSON(w, r, &request) {
		return webhook.RotationCommand{}, false
	}
	return webhook.RotationCommand{
		OrganizationID:  organizationID,
		IntegrationID:   integrationID,
		ExpectedVersion: int64(request.Version),
	}, true
}

func (server *Server) writeWebhookRotationResult(
	w http.ResponseWriter,
	r *http.Request,
	operation string,
	result webhook.RotationResult,
	err error,
	preparing bool,
) {
	if err != nil {
		server.internalError(w, r, operation, err)
		return
	}
	switch result.Kind {
	case webhook.RotationPrepared:
		if !preparing {
			server.internalError(w, r, operation, errUnexpectedResult)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusCreated, api.PrepareWebhookSigningSecretResponse{
			Integration:   toWebhookIntegrationResponse(result.Integration),
			SecretVersion: result.SecretVersion,
			SigningSecret: result.SigningSecret,
		})
	case webhook.RotationUpdated:
		if preparing {
			server.internalError(w, r, operation, errUnexpectedResult)
			return
		}
		writeJSON(w, http.StatusOK, toWebhookIntegrationResponse(result.Integration))
	case webhook.RotationInvalid:
		writeValidationProblem(w, webhookFailurePairs(result.Failures))
	case webhook.RotationNotFound:
		writeStatusProblem(w, http.StatusNotFound)
	case webhook.RotationConflict:
		writeCodedProblem(
			w, http.StatusConflict, result.Code,
			"Webhook signing-secret rotation conflict", result.Detail,
		)
	case webhook.RotationKeyringUnavailable:
		writeCodedProblem(
			w, http.StatusServiceUnavailable, result.Code,
			"Webhook configuration unavailable", result.Detail,
		)
	default:
		server.internalError(w, r, operation, errUnexpectedResult)
	}
}
