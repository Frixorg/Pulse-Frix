package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/frix-me/pulse/api/internal/auth"
	"github.com/frix-me/pulse/api/internal/model"
	"github.com/frix-me/pulse/api/internal/store"
)

// Middleware is a standard http middleware.
type Middleware func(http.Handler) http.Handler

type ctxKeyReqID struct{}

// RequestID returns the correlation id for the request context.
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyReqID{}).(string); ok {
		return v
	}
	return ""
}

// WithRequestID assigns a correlation id to each request.
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := "req_" + randomHex(12)
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), ctxKeyReqID{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Recover turns panics into safe 500s (never leaking a stack trace to clients).
func Recover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered", "error", rec, "path", r.URL.Path, "request_id", RequestID(r.Context()))
					Fail(w, r, http.StatusInternalServerError, CodeInternal, "internal error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Logger emits a structured JSON log line per request (never logging secrets).
func Logger(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(sw, r)
			logger.Info("http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"ip", clientIP(r),
				"request_id", RequestID(r.Context()),
			)
		})
	}
}

// SecurityHeaders sets conservative security headers including a strict CSP.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// CORS allows only the configured origins (credentials enabled for cookies).
func CORS(origins []string) Middleware {
	allowed := map[string]bool{}
	for _, o := range origins {
		allowed[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && allowed[origin] {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Vary", "Origin")
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Authenticate resolves the session cookie into a Principal and attaches it to
// the context. It does not reject unauthenticated requests; RequireAuth does.
func Authenticate(st store.Store) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(SessionCookie)
			if err == nil && cookie.Value != "" {
				if sess, err := st.GetSession(auth.HashToken(cookie.Value)); err == nil {
					if u, err := st.GetUser(sess.OrgID, sess.UserID); err == nil {
						mem, _ := st.GetMembership(sess.OrgID, sess.UserID)
						p := &auth.Principal{UserID: u.ID, OrgID: sess.OrgID, Email: u.Email, Role: roleOf(mem)}
						r = r.WithContext(auth.WithPrincipal(r.Context(), p))
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Limiter is a simple per-key token-bucket rate limiter.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   float64
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewLimiter creates a limiter with the given steady rate and burst.
func NewLimiter(ratePerSec, burst float64) *Limiter {
	return &Limiter{buckets: map[string]*bucket{}, rate: ratePerSec, burst: burst}
}

// Allow reports whether an action for key may proceed now.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &bucket{tokens: l.burst - 1, last: now}
		return true
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = min(l.burst, b.tokens+elapsed*l.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Middleware returns a middleware that rate-limits by client IP.
func (l *Limiter) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.Allow(clientIP(r)) {
				w.Header().Set("Retry-After", "1")
				Fail(w, r, http.StatusTooManyRequests, CodeRateLimited, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Chain composes middlewares left-to-right around a handler.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// --- helpers ---

// SessionCookie is the name of the session cookie.
const SessionCookie = "pulse_session"

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// roleOf extracts the role from a membership, defaulting to viewer.
func roleOf(m *model.Membership) model.Role {
	if m == nil || m.Role == "" {
		return model.RoleViewer
	}
	return m.Role
}
