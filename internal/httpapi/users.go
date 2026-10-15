package httpapi

import (
	"net/http"

	api "github.com/probehive/probehive/internal/httpapi/v1"
	"github.com/probehive/probehive/internal/user"
)

func (server *Server) usersRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		server.listUsers(w, r)
	case http.MethodPost:
		server.createUser(w, r)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (server *Server) userItem(w http.ResponseWriter, r *http.Request) {
	id, ok := canonicalUUID(r.PathValue("userId"))
	if !ok {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w, http.MethodPut)
		return
	}
	if _, ok = server.protectUnsafe(w, r, server.requireInstanceAdministrator); !ok {
		return
	}
	var request api.UpdateUserRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := server.users.UpdateUser(r.Context(), user.UpdateUserCommand{
		ID: user.ID(id), Email: valueOrEmpty(request.Email), DisplayName: valueOrEmpty(request.DisplayName),
	})
	if err != nil {
		server.internalError(w, r, "update user", err)
		return
	}
	switch result.Kind {
	case user.UpdateUserUpdated:
		writeJSON(w, http.StatusOK, toUserResponse(result.User))
	case user.UpdateUserNotFound:
		writeStatusProblem(w, http.StatusNotFound)
	case user.UpdateUserDuplicateEmail:
		writeCodedProblem(w, http.StatusConflict, user.EmailConflictCode, user.EmailConflictTitle, user.EmailConflictDetail)
	case user.UpdateUserInvalid:
		writeValidationProblem(w, userFailurePairs(result.Failures))
	default:
		server.internalError(w, r, "update user", errUnexpectedResult)
	}
}

func (server *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.requireInstanceAdministrator(w, r); !ok {
		return
	}
	values, err := server.users.List(r.Context())
	if err != nil {
		server.internalError(w, r, "list users", err)
		return
	}
	responses := make([]api.UserResponse, len(values))
	for index, value := range values {
		responses[index] = toUserResponse(value)
	}
	writeJSON(w, http.StatusOK, responses)
}

func (server *Server) createUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := server.protectUnsafe(w, r, server.requireInstanceAdministrator); !ok {
		return
	}
	var request api.CreateUserRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := server.users.CreateUser(r.Context(), user.CreateUserCommand{
		Email: valueOrEmpty(request.Email), DisplayName: valueOrEmpty(request.DisplayName),
		Password: valueOrEmpty(request.Password),
	})
	if err != nil {
		server.internalError(w, r, "create user", err)
		return
	}
	switch result.Kind {
	case user.CreateUserCreated:
		// The user is written first; memberships follow. An instance is one team, so
		// the new user becomes an Administrator of every existing Organization. If
		// this second write fails the user exists with no Organizations yet, and any
		// Organization created afterwards includes every user, closing the gap.
		if err := server.organizations.AddMemberToAllOrganizations(r.Context(), string(result.User.ID)); err != nil {
			server.internalError(w, r, "add user to Organizations", err)
			return
		}
		response := toUserResponse(result.User)
		w.Header().Set("Location", "/api/v1/users/"+response.ID)
		writeJSON(w, http.StatusCreated, response)
	case user.CreateUserDuplicateEmail:
		writeCodedProblem(w, http.StatusConflict, user.EmailConflictCode, user.EmailConflictTitle, user.EmailConflictDetail)
	case user.CreateUserInvalid:
		writeValidationProblem(w, userFailurePairs(result.Failures))
	default:
		server.internalError(w, r, "create user", errUnexpectedResult)
	}
}
