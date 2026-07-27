package httpapi

import (
	"net/http"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/user"
)

func (server *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	complete, err := server.users.SetupStatus(r.Context())
	if err != nil {
		server.internalError(w, r, "get setup status", err)
		return
	}
	writeJSON(w, http.StatusOK, api.SetupStatusResponse{SetupComplete: complete})
}

func (server *Server) setupAdministrator(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if _, ok := server.prepareCredentialUnsafe(w, r); !ok {
		return
	}
	var request api.CreateFirstAdministratorRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := server.users.CreateFirstAdministrator(r.Context(), user.CreateFirstAdministratorCommand{
		Email: valueOrEmpty(request.Email), DisplayName: valueOrEmpty(request.DisplayName),
		Password: valueOrEmpty(request.Password),
	})
	if err != nil {
		server.internalError(w, r, "create first administrator", err)
		return
	}
	switch result.Kind {
	case user.CreateFirstAdministratorCreated:
		// Provision before issuing the session: if provisioning fails the operator has an
		// administrator but no session, signs in normally, and creates an Organization by
		// hand, which is exactly the pre-ADR-0018 path rather than a half-signed-in state.
		provisioned, err := server.organizations.ProvisionBootstrap(r.Context())
		if err != nil {
			server.internalError(w, r, "provision bootstrap Organization", err)
			return
		}
		if provisioned.Kind != organization.ProvisionCreated && provisioned.Kind != organization.ProvisionReplayed {
			server.internalError(w, r, "provision bootstrap Organization", errUnexpectedResult)
			return
		}
		if err := server.createSession(w, r, result.User); err != nil {
			server.internalError(w, r, "create setup session", err)
			return
		}
		writeJSON(w, http.StatusCreated, api.SetupResponse{
			User: toUserResponse(result.User), Organization: toOrganizationResponse(provisioned.Details),
		})
	case user.CreateFirstAdministratorAlreadyCompleted:
		writeProblem(w, http.StatusConflict, user.SetupAlreadyCompletedTitle, user.SetupAlreadyCompletedDetail)
	case user.CreateFirstAdministratorInvalid:
		writeValidationProblem(w, userFailurePairs(result.Failures))
	default:
		server.internalError(w, r, "create first administrator", errUnexpectedResult)
	}
}

func (server *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if _, ok := server.prepareCredentialUnsafe(w, r); !ok {
		return
	}
	var request api.LoginRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := server.users.Authenticate(r.Context(), user.AuthenticateCommand{
		Email: valueOrEmpty(request.Email), Password: valueOrEmpty(request.Password),
	})
	if err != nil {
		server.internalError(w, r, "authenticate local user", err)
		return
	}
	if result.Kind != user.AuthenticateSuccess {
		writeProblem(w, http.StatusUnauthorized, user.InvalidCredentialsTitle, user.InvalidCredentialsDetail)
		return
	}
	if err := server.createSession(w, r, result.User); err != nil {
		server.internalError(w, r, "create login session", err)
		return
	}
	writeJSON(w, http.StatusOK, toSessionResponse(result.User))
}

func (server *Server) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	principal, ok := server.protectUnsafe(w, r, false)
	if !ok {
		return
	}
	if err := server.sessions.DeleteByTokenHash(r.Context(), principal.record.TokenHash); err != nil {
		server.internalError(w, r, "delete session", err)
		return
	}
	server.expireSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) session(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	principal, ok := server.requireAuthentication(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toSessionResponse(principal.account))
}

func toSessionResponse(account user.User) api.SessionResponse {
	return api.SessionResponse{
		UserID: string(account.ID), Email: account.Email,
		DisplayName: account.DisplayName, Role: account.Role,
	}
}

func toUserResponse(account user.User) api.UserResponse {
	return api.UserResponse{
		ID: string(account.ID), Email: account.Email, DisplayName: account.DisplayName,
		Role: account.Role, CreatedAt: account.CreatedAt,
	}
}

func userFailurePairs(failures []user.ValidationFailure) [][2]string {
	pairs := make([][2]string, len(failures))
	for index, failure := range failures {
		pairs[index] = [2]string{failure.Field, failure.Message}
	}
	return pairs
}
