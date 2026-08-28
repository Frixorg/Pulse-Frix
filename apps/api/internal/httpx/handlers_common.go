package httpx

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/frix-me/pulse/api/internal/auth"
	"github.com/frix-me/pulse/api/internal/model"
	"github.com/frix-me/pulse/api/internal/rbac"
)

// newSession builds a session with a CSRF secret and expiry.
func newSession(orgID, userID string, ttl time.Duration) *model.Session {
	return &model.Session{
		UserID:     userID,
		OrgID:      orgID,
		CSRFSecret: auth.NewID("csrf"),
		ExpiresAt:  time.Now().Add(ttl).UTC(),
	}
}

// sessionPayload is the shape returned by /auth/login and /auth/session.
//
// hasPassword tells the dashboard whether password management applies to this
// account at all. It is false for identity-provider logins, which is the normal
// case on Pulse Cloud — those people sign in with Google or Telegram and have
// no Pulse password, so Settings must not offer to change one.
func sessionPayload(email string, role model.Role, hasPassword bool) map[string]any {
	perms := rbac.Permissions(role)
	strs := make([]string, len(perms))
	for i, p := range perms {
		strs[i] = string(p)
	}
	return map[string]any{
		"email":        email,
		"role":         string(role),
		"permissions":  strs,
		"has_password": hasPassword,
	}
}

// issueSession creates a session and sets the session cookie. Shared by
// password login and OIDC callbacks. The cookie carries a random secret; the
// store keys on its hash.
func (s *Server) issueSession(w http.ResponseWriter, orgID, userID string) error {
	cookieValue, _, err := auth.GenerateToken("pss")
	if err != nil {
		return err
	}
	sess := newSession(orgID, userID, s.cfg.SessionTTL())
	sess.ID = auth.HashToken(cookieValue)
	if err := s.store.CreateSession(sess); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    cookieValue,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.Env == "production",
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
		MaxAge:   int(s.cfg.SessionTTL().Seconds()),
	})
	return nil
}

// clearSessionCookie expires the session cookie in the browser. Its attributes
// must match the ones issueSession set, or the browser keeps the old cookie.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// decodeJSON reads a size-limited JSON body, rejecting unknown fields.
func decodeJSON(r *http.Request, v any, maxBytes int64) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBytes))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// limitInt clamps a value into [min,max].
func limitInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
