package httpapi

import (
	"net/http"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/webhook"
)

func (server *Server) alertDeliveries(w http.ResponseWriter, r *http.Request) {
	monitorValue, ok := monitorScope(r)
	alertID, alertOK := canonicalUUID(r.PathValue("alertId"))
	if !ok || !alertOK {
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
	values, found, err := server.webhooks.ListDeliveryAudit(
		r.Context(), webhook.DeliveryScope{
			OrganizationID: monitorValue.OrganizationID,
			ProjectID:      monitorValue.ProjectID,
			MonitorID:      string(monitorValue.MonitorID),
			AlertID:        alertID,
		},
	)
	if err != nil {
		server.internalError(w, r, "list Alert delivery audit", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	items := make([]api.AlertDeliveryResponse, len(values))
	for index, value := range values {
		items[index] = toAlertDeliveryResponse(value)
	}
	writeJSON(w, http.StatusOK, api.AlertDeliveryPageResponse{Items: items})
}

func toAlertDeliveryResponse(value webhook.DeliveryAudit) api.AlertDeliveryResponse {
	attempts := make([]api.DeliveryAttemptResponse, len(value.Attempts))
	for index, attempt := range value.Attempts {
		var failureCode *string
		if attempt.FailureCode != "" {
			code := attempt.FailureCode
			failureCode = &code
		}
		attempts[index] = api.DeliveryAttemptResponse{
			Sequence:    attempt.Sequence,
			StartedAt:   attempt.StartedAt,
			FinishedAt:  attempt.FinishedAt,
			Outcome:     attempt.Outcome,
			HTTPStatus:  attempt.HTTPStatus,
			FailureCode: failureCode,
		}
	}
	return api.AlertDeliveryResponse{
		ID:                 value.DeliveryID,
		Channel:            webhook.DeliveryChannel,
		IntegrationID:      value.IntegrationID,
		IntegrationVersion: value.IntegrationVersion,
		SecretVersion:      value.SecretVersion,
		RoutedAt:           value.RoutedAt,
		Attempts:           attempts,
	}
}
