package httpx

import (
	"net/http"
	"net/url"
)

// handleAuthProviders lists the configured OIDC providers so the frontend can
// render only the buttons that will actually work.
func (s *Server) handleAuthProviders(w http.ResponseWriter, r *http.Request) {
	type prov struct {
		Name  string `json:"name"`
		Start string `json:"start"`
	}
	out := []prov{}
	if s.oidc != nil {
		for _, n := range s.oidc.Names() {
			out = append(out, prov{Name: n, Start: "/api/v1/auth/" + n + "/start"})
		}
	}
	JSON(w, http.StatusOK, map[string]any{"providers": out})
}

// handleOIDCStart kicks off the Authorization Code + PKCE flow and redirects the
// browser to the provider.
func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	if s.oidc == nil || !s.oidc.Has(name) {
		http.Redirect(w, r, "/login?error=provider_not_configured", http.StatusFound)
		return
	}
	authURL, err := s.oidc.Start(r.Context(), name)
	if err != nil {
		s.logger.Warn("oidc start failed", "provider", name, "error", err)
		http.Redirect(w, r, "/login?error=oidc_unavailable", http.StatusFound)
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleOIDCCallback completes the flow: validates state, exchanges the code,
// creates/links the user, and issues a Pulse session.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	if s.oidc == nil || !s.oidc.Has(name) {
		http.Redirect(w, r, "/login?error=provider_not_configured", http.StatusFound)
		return
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		http.Redirect(w, r, "/login?error="+url.QueryEscape(e), http.StatusFound)
		return
	}
	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, "/login?error=missing_code", http.StatusFound)
		return
	}

	claims, err := s.oidc.Callback(r.Context(), name, code, state)
	if err != nil {
		s.logger.Warn("oidc callback failed", "provider", name, "error", err)
		http.Redirect(w, r, "/login?error=oidc_failed", http.StatusFound)
		return
	}

	user, mem, err := s.store.UpsertOIDCUser(claims.Provider, claims.Subject, claims.Email, claims.Name)
	if err != nil || mem == nil || user == nil {
		s.logger.Error("oidc user upsert failed", "provider", name, "error", err)
		http.Redirect(w, r, "/login?error=account_error", http.StatusFound)
		return
	}
	if err := s.issueSession(w, mem.OrgID, user.ID); err != nil {
		s.logger.Error("oidc session failed", "error", err)
		http.Redirect(w, r, "/login?error=session_error", http.StatusFound)
		return
	}

	actor := user.Email
	if actor == "" {
		actor = user.Name
	}
	if actor == "" {
		actor = user.ID
	}
	s.audit.Record(mem.OrgID, actor, "auth.login.oidc", "success", clientIP(r), map[string]any{"provider": name})
	http.Redirect(w, r, "/app", http.StatusFound)
}
