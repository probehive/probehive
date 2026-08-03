package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"unicode/utf8"

	api "github.com/probehive/probehive/internal/httpapi/v1"
)

const maxRequestBodyBytes = 1 << 20

// Transport-level problem codes. Feature packages own their own codes; these cover
// failures the HTTP layer itself decides.
const (
	unauthorizedCode     = "auth.unauthorized"
	forbiddenCode        = "auth.forbidden"
	notFoundCode         = "resource.notFound"
	methodNotAllowedCode = "request.methodNotAllowed"
	rateLimitedCode      = "request.rateLimited"
	malformedRequestCode = "request.malformed"
	internalErrorCode    = "server.internalError"
)

func statusProblemCode(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return unauthorizedCode
	case http.StatusForbidden:
		return forbiddenCode
	case http.StatusNotFound:
		return notFoundCode
	case http.StatusMethodNotAllowed:
		return methodNotAllowedCode
	case http.StatusTooManyRequests:
		return rateLimitedCode
	default:
		return ""
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write JSON response", "error", err)
	}
}

func writeProblem(w http.ResponseWriter, status int, title, detail string) {
	writeCodedProblem(w, status, "", title, detail)
}

// writeCodedProblem writes a Problem Details body carrying the stable code clients
// localize from. An empty code omits the member.
func writeCodedProblem(w http.ResponseWriter, status int, code, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(api.ProblemDetails{
		Type: "about:blank", Title: title, Status: status, Code: code, Detail: detail,
	}); err != nil {
		slog.Error("write Problem Details response", "error", err)
	}
}

// writeValidationProblem groups failures by field path, preserving encounter order
// within each field. Each entry carries its stable code.
func writeValidationProblem(w http.ResponseWriter, failures [][3]string) {
	errorsByField := make(map[string][]api.ValidationError)
	for _, failure := range failures {
		errorsByField[failure[1]] = append(
			errorsByField[failure[1]], api.ValidationError{Code: failure[0], Message: failure[2]},
		)
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusBadRequest)
	if err := json.NewEncoder(w).Encode(api.ProblemDetails{
		Type: "about:blank", Title: "One or more validation errors occurred.",
		Status: http.StatusBadRequest, Errors: errorsByField,
	}); err != nil {
		slog.Error("write validation Problem Details response", "error", err)
	}
}

func writeStatusProblem(w http.ResponseWriter, status int) {
	title := http.StatusText(status)
	if title == "" {
		title = "HTTP error"
	}
	writeCodedProblem(w, status, statusProblemCode(status), title, "")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	var payload json.RawMessage
	if err := decoder.Decode(&payload); err != nil {
		writeCodedProblem(w, http.StatusBadRequest, malformedRequestCode, "Bad Request", "The request body is not valid JSON for this endpoint.")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeCodedProblem(w, http.StatusBadRequest, malformedRequestCode, "Bad Request", "The request body must contain exactly one JSON value.")
		return false
	}
	if !utf8.Valid(payload) {
		writeCodedProblem(w, http.StatusBadRequest, malformedRequestCode, "Bad Request", "The request body is not valid JSON for this endpoint.")
		return false
	}
	if bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		writeCodedProblem(w, http.StatusBadRequest, malformedRequestCode, "Bad Request", "The request body must be a JSON object.")
		return false
	}
	if err := json.Unmarshal(payload, destination); err != nil {
		writeCodedProblem(w, http.StatusBadRequest, malformedRequestCode, "Bad Request", "The request body is not valid JSON for this endpoint.")
		return false
	}
	return true
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func methodNotAllowed(w http.ResponseWriter, allowed ...string) {
	for _, method := range allowed {
		w.Header().Add("Allow", method)
	}
	writeStatusProblem(w, http.StatusMethodNotAllowed)
}

func (server *Server) internalError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	server.logger.Error("request failed", "operation", operation, "method", r.Method, "path", r.URL.Path, "error", err)
	writeCodedProblem(w, http.StatusInternalServerError, internalErrorCode, "Internal Server Error", "")
}
