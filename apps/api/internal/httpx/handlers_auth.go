package httpx

import (
	"net/http"
	"strings"

	"github.com/frix-me/pulse/api/internal/auth"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req, 4096); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "invalid request body")
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Email == "" || req.Password == "" {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "email and password are required")
		return
	}

	user, mem, err := s.store.FindLoginByEmail(req.Email)
	// Always run a verify to reduce timing side-channels for unknown users.
	valid := false
	if err == nil && user != nil {
		valid = auth.VerifyPassword(req.Password, user.PasswordHash)
	} else {
		_ = auth.VerifyPassword(req.Password, "pbkdf2-sha256$210000$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	}
	if err != nil || !valid || mem == nil {
		s.audit.Record("", req.Email, "auth.login", "failure", clientIP(r), nil)
		Fail(w, r, http.StatusUnauthorized, CodeAuth, "invalid credentials")
		return
	}

	// The cookie carries a random secret; the store keys on its hash so a DB
	// leak does not reveal usable session tokens.
	cookieValue, _, err := auth.GenerateToken("pss")
	if err != nil {
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "could not create session")
		return
	}
	sess := newSession(mem.OrgID, user.ID, s.cfg.SessionTTL())
	sess.ID = auth.HashToken(cookieValue)
	if err := s.store.CreateSession(sess); err != nil {
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "could not persist session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    cookieValue,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.Env == "production",
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
	})
	s.audit.Record(mem.OrgID, user.Email, "auth.login", "success", clientIP(r), nil)
	JSON(w, http.StatusOK, sessionPayload(user.Email, mem.Role))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		_ = s.store.DeleteSession(auth.HashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	JSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.FromContext(r.Context())
	if !ok {
		Fail(w, r, http.StatusUnauthorized, CodeAuth, "not authenticated")
		return
	}
	JSON(w, http.StatusOK, sessionPayload(p.Email, p.Role))
}
