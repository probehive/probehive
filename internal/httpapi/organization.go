package httpapi

import (
	"net/http"
	"strings"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
)

func (server *Server) organizationsRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		server.listOrganizations(w, r)
	case http.MethodPost:
		server.createOrganization(w, r)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (server *Server) listOrganizations(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireAdministrator(w, r); !ok {
		return
	}
	values, err := server.organizations.List(r.Context())
	if err != nil {
		server.internalError(w, r, "list Organizations", err)
		return
	}
	responses := make([]api.OrganizationResponse, len(values))
	for index, value := range values {
		responses[index] = toOrganizationResponse(value)
	}
	writeJSON(w, http.StatusOK, responses)
}

func (server *Server) createOrganization(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.protectUnsafe(w, r, true); !ok {
		return
	}
	var request api.CreateOrganizationRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := server.organizations.Provision(r.Context(), organization.ProvisionCommand{
		Slug: valueOrEmpty(request.Slug), DisplayName: valueOrEmpty(request.DisplayName),
	})
	if err != nil {
		server.internalError(w, r, "provision Organization", err)
		return
	}
	switch result.Kind {
	case organization.ProvisionCreated:
		response := toOrganizationResponse(result.Details)
		w.Header().Set("Location", "/api/v1/organizations/"+response.ID)
		writeJSON(w, http.StatusCreated, response)
	case organization.ProvisionReplayed:
		writeJSON(w, http.StatusOK, toOrganizationResponse(result.Details))
	case organization.ProvisionSlugConflict:
		writeCodedProblem(w, http.StatusConflict, result.Code, organization.SlugConflictTitle, result.Detail)
	case organization.ProvisionInvalid:
		writeValidationProblem(w, organizationFailurePairs(result.Failures))
	default:
		server.internalError(w, r, "provision Organization", errUnexpectedResult)
	}
}

func (server *Server) organizationItem(w http.ResponseWriter, r *http.Request) {
	id, ok := canonicalUUID(r.PathValue("organizationId"))
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
	details, found, err := server.organizations.Get(r.Context(), organization.ID(id))
	if err != nil {
		server.internalError(w, r, "get Organization", err)
		return
	}
	if !found {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toOrganizationResponse(details))
}

func toOrganizationResponse(details organization.Details) api.OrganizationResponse {
	return api.OrganizationResponse{
		ID: string(details.Organization.ID), Slug: details.Organization.Slug,
		DisplayName: details.Organization.DisplayName, CreatedAt: details.Organization.CreatedAt,
		DefaultProject: api.ProjectResponse{
			ID: string(details.DefaultProject.ID), OrganizationID: string(details.DefaultProject.OrganizationID),
			Name: details.DefaultProject.Name, IsDefault: details.DefaultProject.IsDefault,
			CreatedAt: details.DefaultProject.CreatedAt,
		},
	}
}

func organizationFailurePairs(failures []organization.ValidationFailure) [][3]string {
	triples := make([][3]string, len(failures))
	for index, failure := range failures {
		triples[index] = [3]string{failure.Code, failure.Field, failure.Message}
	}
	return triples
}

func canonicalUUID(value string) (string, bool) {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return "", false
	}
	for index := 0; index < len(value); index++ {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		character := value[index]
		if !((character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return "", false
		}
	}
	return strings.ToLower(value), true
}
