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

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write JSON response", "error", err)
	}
}

func writeProblem(w http.ResponseWriter, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(api.ProblemDetails{
		Type: "about:blank", Title: title, Status: status, Detail: detail,
	}); err != nil {
		slog.Error("write Problem Details response", "error", err)
	}
}

func writeValidationProblem(w http.ResponseWriter, failures [][2]string) {
	errorsByField := make(map[string][]string)
	for _, failure := range failures {
		errorsByField[failure[0]] = append(errorsByField[failure[0]], failure[1])
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
	writeProblem(w, status, title, "")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	var payload json.RawMessage
	if err := decoder.Decode(&payload); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "The request body is not valid JSON for this endpoint.")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "The request body must contain exactly one JSON value.")
		return false
	}
	if !utf8.Valid(payload) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "The request body is not valid JSON for this endpoint.")
		return false
	}
	if bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "The request body must be a JSON object.")
		return false
	}
	if err := json.Unmarshal(payload, destination); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "The request body is not valid JSON for this endpoint.")
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
	writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
}
