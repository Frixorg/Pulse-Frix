package auth

import (
	"context"

	"github.com/frix-me/pulse/api/internal/model"
)

// Principal is the authenticated caller for a request. OrgID is ALWAYS derived
// from the session/token here — never from a request parameter — which is the
// backbone of multi-tenant isolation. See docs/DATA_MODEL.md#multi-tenancy.
type Principal struct {
	UserID string
	OrgID  string
	Email  string
	Role   model.Role
	// HasPassword is false for an identity-provider account that has never set
	// one. The dashboard uses it to decide whether password management applies
	// at all: on Pulse Cloud people sign in with Google or Telegram and have no
	// password to change.
	HasPassword bool
}

type ctxKey struct{}

// WithPrincipal stores the principal in the request context.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext retrieves the principal, if any.
func FromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(*Principal)
	return p, ok
}
