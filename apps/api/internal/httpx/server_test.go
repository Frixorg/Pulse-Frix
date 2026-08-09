package httpx

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/frix-me/pulse/api/internal/auth"
	"github.com/frix-me/pulse/api/internal/config"
	"github.com/frix-me/pulse/api/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *store.Memory) {
	t.Helper()
	st := store.NewMemory()
	hash, _ := auth.HashPassword("supersecret123")
	if _, _, err := st.SeedOrgOwner("Test", "owner@example.com", hash); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	cfg := config.Load()
	cfg.Env = "development" // so the session cookie is not Secure-only
	srv := New(cfg, st, slog.New(slog.NewTextHandler(discard{}, nil)))
	return httptest.NewServer(srv.Handler()), st
}

func TestLoginAndAuthenticatedAccess(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()
	client := ts.Client()

	// Unauthenticated access is rejected.
	resp, err := client.Get(ts.URL + "/api/v1/servers")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 unauthenticated, got %d", resp.StatusCode)
	}

	// Wrong password fails.
	resp, err = client.Post(ts.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader(`{"email":"owner@example.com","password":"wrong"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 for wrong password, got %d", resp.StatusCode)
	}

	// Correct login returns 200 and sets a session cookie.
	resp, err = client.Post(ts.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader(`{"email":"owner@example.com","password":"supersecret123"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 login, got %d", resp.StatusCode)
	}
	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == SessionCookie {
			sessionCookie = c
		}
	}
	resp.Body.Close()
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("login did not set a session cookie")
	}

	// Authenticated access works when the session cookie is attached.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/servers", nil)
	req.AddCookie(sessionCookie)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 authenticated, got %d", resp.StatusCode)
	}
}

func TestHealthz(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()
	resp, err := ts.Client().Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz want 200, got %d", resp.StatusCode)
	}
}

// discard is an io.Writer that drops log output in tests.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
