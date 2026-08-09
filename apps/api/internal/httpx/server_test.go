package httpx

import (
	"log/slog"
	"net/http"
	"net/http/cookiejar"
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
	client.Jar, _ = cookiejar.New(nil)

	// Unauthenticated access is rejected.
	resp, err := client.Get(ts.URL + "/api/v1/servers")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 unauthenticated, got %d", resp.StatusCode)
	}

	// Wrong password fails.
	resp, _ = client.Post(ts.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader(`{"email":"owner@example.com","password":"wrong"}`))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 for wrong password, got %d", resp.StatusCode)
	}

	// Correct login sets a session cookie (the client jar stores it).
	resp, err = client.Post(ts.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader(`{"email":"owner@example.com","password":"supersecret123"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 login, got %d", resp.StatusCode)
	}

	// Now authenticated access works.
	resp, err = client.Get(ts.URL + "/api/v1/servers")
	if err != nil {
		t.Fatal(err)
	}
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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz want 200, got %d", resp.StatusCode)
	}
}

// discard is an io.Writer that drops log output in tests.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
