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
