// Package httpapi composes the versioned ProbeHive HTTP API and browser security profile.
package httpapi

import (
	"context"
	"crypto/rand"
	_ "embed"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path"
	"time"

	"github.com/probehive/probehive/internal/monitor"
	"github.com/probehive/probehive/internal/organization"
	"github.com/probehive/probehive/internal/user"
)

const defaultCredentialAttemptsPerMinute = 10

type Clock interface {
	Now() time.Time
}

type Config struct {
	Organizations               *organization.Service
	Users                       *user.Service
	Monitors                    *monitor.Service
	Sessions                    user.SessionStore
	Antiforgery                 user.AntiforgeryStore
	Clock                       Clock
	Ready                       func(context.Context) error
	Random                      io.Reader
	Logger                      *slog.Logger
	Development                 bool
	CredentialAttemptsPerMinute int
	PublicOrigin                string
}

type Server struct {
	organizations *organization.Service
	users         *user.Service
	monitors      *monitor.Service
	sessions      user.SessionStore
	antiforgery   user.AntiforgeryStore
	clock         Clock
	ready         func(context.Context) error
	random        io.Reader
	logger        *slog.Logger
	development   bool
	credentials   *credentialLimiter
	publicOrigin  string
	mux           *http.ServeMux
}

func New(config Config) (*Server, error) {
	if config.Organizations == nil || config.Users == nil || config.Monitors == nil ||
		config.Sessions == nil || config.Antiforgery == nil || config.Clock == nil || config.Ready == nil {
		return nil, errors.New("httpapi requires feature services, security stores, a clock, and readiness check")
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.CredentialAttemptsPerMinute == 0 {
		config.CredentialAttemptsPerMinute = defaultCredentialAttemptsPerMinute
	}
	if config.CredentialAttemptsPerMinute < 1 {
		return nil, errors.New("credential attempts per minute must be positive")
	}
	publicOrigin, err := normalizePublicOrigin(config.PublicOrigin)
	if err != nil {
		return nil, err
	}

	server := &Server{
		organizations: config.Organizations, users: config.Users, monitors: config.Monitors,
		sessions: config.Sessions, antiforgery: config.Antiforgery, clock: config.Clock,
		ready: config.Ready, random: config.Random, logger: config.Logger,
		development:  config.Development,
		publicOrigin: publicOrigin,
	}
	server.credentials = newCredentialLimiter(config.CredentialAttemptsPerMinute, config.Clock.Now)
	server.mux = server.routes()
	return server, nil
}

func (server *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !canonicalPath(r.URL.Path) {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			server.logger.Error("request panic", "method", r.Method, "path", r.URL.Path, "panic", recovered)
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		}
	}()
	server.mux.ServeHTTP(w, r)
}

func canonicalPath(value string) bool {
	if value == "" || value[0] != '/' {
		return false
	}
	cleaned := path.Clean(value)
	if value[len(value)-1] == '/' && cleaned != "/" {
		cleaned += "/"
	}
	return value == cleaned
}

func (server *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", server.health)
	mux.HandleFunc("/readyz", server.readiness)
	mux.HandleFunc("/openapi/v1.json", server.openapi)
	mux.HandleFunc("/api/v1/setup/status", server.setupStatus)
	mux.HandleFunc("/api/v1/setup/admin", server.setupAdministrator)
	mux.HandleFunc("/api/v1/auth/antiforgery", server.issueAntiforgery)
	mux.HandleFunc("/api/v1/auth/login", server.login)
	mux.HandleFunc("/api/v1/auth/logout", server.logout)
	mux.HandleFunc("/api/v1/auth/session", server.session)
	mux.HandleFunc("/api/v1/organizations", server.organizationsRoot)
	mux.HandleFunc("/api/v1/organizations/{organizationId}", server.organizationItem)
	mux.HandleFunc("/api/v1/organizations/{organizationId}/projects/{projectId}/monitors", server.monitorsRoot)
	mux.HandleFunc("/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}", server.monitorItem)
	mux.HandleFunc("/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/name", server.renameMonitor)
	mux.HandleFunc("/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/state", server.changeMonitorState)
	mux.HandleFunc("/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions", server.monitorRevisions)
	mux.HandleFunc("/api/v1/organizations/{organizationId}/projects/{projectId}/monitors/{monitorId}/revisions/{revisionNumber}", server.monitorRevisionItem)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { writeStatusProblem(w, http.StatusNotFound) })
	return mux
}

func (server *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte("Healthy"))
	}
}

func (server *Server) readiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	status, body := http.StatusOK, "Healthy"
	if err := server.ready(ctx); err != nil {
		server.logger.Warn("readiness check failed", "error", err)
		status, body = http.StatusServiceUnavailable, "Unhealthy"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(body))
	}
}

func (server *Server) openapi(w http.ResponseWriter, r *http.Request) {
	if !server.development {
		writeStatusProblem(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(openAPIDocument)
	}
}

//go:embed openapi_v1.json
var openAPIDocument []byte
