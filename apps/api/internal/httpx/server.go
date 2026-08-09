package httpx

import (
	"net/http"
	"sync"
	"time"

	"log/slog"

	"github.com/frix-me/pulse/api/internal/audit"
	"github.com/frix-me/pulse/api/internal/auth"
	"github.com/frix-me/pulse/api/internal/config"
	"github.com/frix-me/pulse/api/internal/metricsproxy"
	"github.com/frix-me/pulse/api/internal/rbac"
	"github.com/frix-me/pulse/api/internal/store"
)

// Server bundles the API dependencies and builds the HTTP handler.
type Server struct {
	cfg     *config.Config
	store   store.Store
	logger  *slog.Logger
	audit   *audit.Recorder
	metrics *metricsproxy.Client

	loginLimiter  *Limiter
	enrollLimiter *Limiter
	ingestLimiter *Limiter

	nonceOnce   sync.Once
	nonceCacheV *nonceCache
}

// New constructs a Server.
func New(cfg *config.Config, st store.Store, logger *slog.Logger) *Server {
	return &Server{
		cfg:           cfg,
		store:         st,
		logger:        logger,
		audit:         audit.New(st, logger),
		metrics:       metricsproxy.New(cfg.MetricsURL),
		loginLimiter:  NewLimiter(0.2, 5),  // ~5 attempts then 1 / 5s
		enrollLimiter: NewLimiter(0.1, 3),  // strict on enrollment
		ingestLimiter: NewLimiter(50, 100), // agent ingestion (per IP)
	}
}

// Handler builds the fully-wired http.Handler with global middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health/readiness (unauthenticated, no secrets).
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, map[string]string{"status": "ready"}) })

	// Auth
	mux.Handle("POST /api/v1/auth/login", s.loginLimiter.Middleware()(http.HandlerFunc(s.handleLogin)))
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v1/auth/session", s.handleSession)

	// Servers
	mux.HandleFunc("GET /api/v1/servers", s.requirePerm(rbac.ServerRead, s.handleListServers))
	mux.HandleFunc("GET /api/v1/servers/{id}", s.requirePerm(rbac.ServerRead, s.handleGetServer))
	mux.HandleFunc("GET /api/v1/servers/{id}/summary", s.requirePerm(rbac.ServerRead, s.handleServerSummary))
	mux.HandleFunc("DELETE /api/v1/servers/{id}", s.requirePerm(rbac.ServerManage, s.handleDeleteServer))

	// Discovery-derived views
	mux.HandleFunc("GET /api/v1/servers/{id}/discovery", s.requirePerm(rbac.ServerRead, s.handleDiscovery))
	mux.HandleFunc("GET /api/v1/servers/{id}/topology", s.requirePerm(rbac.ServerRead, s.handleTopology))
	mux.HandleFunc("GET /api/v1/servers/{id}/services", s.requirePerm(rbac.ServerRead, s.handleServices))
	mux.HandleFunc("GET /api/v1/servers/{id}/containers", s.requirePerm(rbac.ServerRead, s.handleContainers))
	mux.HandleFunc("GET /api/v1/servers/{id}/databases", s.requirePerm(rbac.ServerRead, s.handleDatabases))
	mux.HandleFunc("GET /api/v1/servers/{id}/applications", s.requirePerm(rbac.ServerRead, s.handleApplications))
	mux.HandleFunc("GET /api/v1/servers/{id}/domains", s.requirePerm(rbac.ServerRead, s.handleDomains))
	mux.HandleFunc("GET /api/v1/servers/{id}/security", s.requirePerm(rbac.ServerRead, s.handleSecurity))
	mux.HandleFunc("GET /api/v1/servers/{id}/metrics", s.requirePerm(rbac.ServerRead, s.handleMetrics))

	// Agents & enrollment
	mux.HandleFunc("POST /api/v1/agents/enrollment-tokens", s.requirePerm(rbac.ServerManage, s.handleCreateEnrollment))
	mux.Handle("POST /api/v1/agents/enroll", s.enrollLimiter.Middleware()(http.HandlerFunc(s.handleEnroll)))
	mux.Handle("POST /api/v1/agents/ingest", s.ingestLimiter.Middleware()(http.HandlerFunc(s.handleIngest)))
	mux.HandleFunc("POST /api/v1/agents/{id}/revoke", s.requirePerm(rbac.ServerManage, s.handleRevokeAgent))

	// Alerts, events, audit
	mux.HandleFunc("GET /api/v1/alerts", s.requirePerm(rbac.AlertRead, s.handleListAlerts))
	mux.HandleFunc("POST /api/v1/alerts", s.requirePerm(rbac.AlertManage, s.handleCreateAlert))
	mux.HandleFunc("GET /api/v1/alerts/instances", s.requirePerm(rbac.AlertRead, s.handleAlertInstances))
	mux.HandleFunc("GET /api/v1/events", s.requirePerm(rbac.EventRead, s.handleListEvents))
	mux.HandleFunc("GET /api/v1/audit", s.requirePerm(rbac.AuditRead, s.handleListAudit))

	// Global middleware chain (outermost first).
	return Chain(mux,
		WithRequestID,
		Recover(s.logger),
		Logger(s.logger),
		SecurityHeaders,
		CORS(s.cfg.CORSOrigins),
		Authenticate(s.store),
	)
}

// HTTPServer returns a configured *http.Server with sane timeouts.
func (s *Server) HTTPServer() *http.Server {
	return &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// --- auth helpers ---

// requirePerm wraps a handler, enforcing authentication and a permission. This
// is the API-layer authorization gate; the store layer enforces org scoping
// again (never trust the frontend / a single layer).
func (s *Server) requirePerm(p rbac.Permission, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.FromContext(r.Context())
		if !ok {
			Fail(w, r, http.StatusUnauthorized, CodeAuth, "authentication required")
			return
		}
		if !rbac.Can(principal.Role, p) {
			Fail(w, r, http.StatusForbidden, CodePermission, "insufficient permissions")
			return
		}
		h(w, r)
	}
}

// principal returns the authenticated principal (guaranteed by requirePerm).
func (s *Server) principal(r *http.Request) *auth.Principal {
	p, _ := auth.FromContext(r.Context())
	return p
}
