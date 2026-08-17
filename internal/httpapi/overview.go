package httpapi

import (
	"net/http"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/overview"
)

func (server *Server) organizationOverview(w http.ResponseWriter, r *http.Request) {
	organizationID, ok := canonicalUUID(r.PathValue("organizationId"))
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	principal, ok := server.requireOrganizationPermission(
		w, r, organizationID, organization.PermissionOrganizationRead,
	)
	if !ok {
		return
	}
	membership, found, err := server.organizations.Membership(
		r.Context(), organization.ID(organizationID), string(principal.account.ID),
	)
	if err != nil {
		server.internalError(w, r, "resolve Organization overview permissions", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	value, found, err := server.overview.Get(r.Context(), organizationID)
	if err != nil {
		server.internalError(w, r, "get Organization overview", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toOrganizationOverviewResponse(value, membership.Role))
}

func toOrganizationOverviewResponse(
	value overview.Summary, role organization.Role,
) api.OrganizationOverviewResponse {
	response := api.OrganizationOverviewResponse{
		OrganizationID: value.OrganizationID,
		Capabilities: api.OrganizationOverviewCapabilities{
			ManageOrganization: role.Permits(organization.PermissionOrganizationWrite),
			ManageIntegrations: role.Permits(organization.PermissionIntegrationManage),
			ManageStatusPage:   role.Permits(organization.PermissionStatusPageWrite),
		},
	}
	if role.Permits(organization.PermissionMonitorRead) {
		response.Monitors = &api.OrganizationOverviewMonitorCounts{
			Total: value.Monitors.Total, Draft: value.Monitors.Draft,
			Active: value.Monitors.Active, Paused: value.Monitors.Paused,
			Archived: value.Monitors.Archived,
		}
		response.Health = &api.OrganizationOverviewHealthCounts{
			NotEvaluated: value.Health.NotEvaluated, Unknown: value.Health.Unknown,
			Healthy: value.Health.Healthy, Degraded: value.Health.Degraded, Down: value.Health.Down,
		}
	}
	if role.Permits(organization.PermissionIncidentRead) {
		preview := make([]api.OrganizationOverviewActiveIncident, len(value.ActiveIncidents))
		for index, incident := range value.ActiveIncidents {
			preview[index] = api.OrganizationOverviewActiveIncident{
				ID: incident.ID, ProjectID: incident.ProjectID, MonitorID: incident.MonitorID,
				MonitorName: incident.MonitorName, State: incident.State, UpdatedAt: incident.UpdatedAt,
			}
		}
		response.Incidents = &api.OrganizationOverviewIncidentSummary{
			Active: value.Incidents.Active, Open: value.Incidents.Open,
			Acknowledged:  value.Incidents.Acknowledged,
			ActivePreview: preview, ActivePreviewTruncated: value.ActiveIncidentsTruncated,
		}
	}
	if response.Capabilities.ManageIntegrations {
		response.Integrations = &api.OrganizationOverviewIntegrationCounts{
			Total: value.Integrations.Total, Enabled: value.Integrations.Enabled,
		}
	}
	if response.Capabilities.ManageStatusPage {
		response.StatusPage = &api.OrganizationOverviewStatusPageState{
			Configured: value.StatusPage.Configured, Published: value.StatusPage.Published,
		}
	}
	return response
}
