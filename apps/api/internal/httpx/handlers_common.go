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
func sessionPayload(email string, role model.Role) map[string]any {
	perms := rbac.Permissions(role)
	strs := make([]string, len(perms))
	for i, p := range perms {
		strs[i] = string(p)
	}
	return map[string]any{
		"email":       email,
		"role":        string(role),
		"permissions": strs,
	}
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
