package httpx

import (
	"encoding/json"
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

// newUnprovisionedServer is newTestServer without the seeded owner, so the
// first-boot wizard is open.
func newUnprovisionedServer(t *testing.T) (*httptest.Server, *store.Memory) {
	t.Helper()
	st := store.NewMemory()
	cfg := config.Load()
	cfg.Env = "development" // so the session cookie is not Secure-only
	cfg.Mode = "local"      // the wizard is only ever offered self-hosted
	srv := New(cfg, st, slog.New(slog.NewTextHandler(discard{}, nil)))
	return httptest.NewServer(srv.Handler()), st
}

func postJSON(t *testing.T, c *http.Client, url, body string) *http.Response {
	t.Helper()
	resp, err := c.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// jarClient returns an INDEPENDENT cookie-carrying client. httptest's own
// Client() is shared across callers, so assigning a jar to it would leak
// cookies between the clients a test means to keep separate.
func jarClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

func TestSetupStatusAndProvisioning(t *testing.T) {
	ts, _ := newUnprovisionedServer(t)
	defer ts.Close()
	client := jarClient(t)

	// A store with no accounts reports that setup is needed.
	resp, err := client.Get(ts.URL + "/api/v1/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	var status struct {
		NeedsSetup bool `json:"needs_setup"`
		MinPwdLen  int  `json:"min_password_length"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&status)
	resp.Body.Close()
	if !status.NeedsSetup {
		t.Fatal("an empty deployment must report needs_setup")
	}
	if status.MinPwdLen < 12 {
		t.Errorf("min password length: got %d", status.MinPwdLen)
	}

	// A password that fails the policy is refused, and setup stays open.
	resp = postJSON(t, client, ts.URL+"/api/v1/setup",
		`{"email":"owner@example.com","password":"short"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for a weak password, got %d", resp.StatusCode)
	}

	// An invalid address is refused too.
	resp = postJSON(t, client, ts.URL+"/api/v1/setup",
		`{"email":"not-an-email","password":"correct horse battery"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for an invalid email, got %d", resp.StatusCode)
	}

	// A valid request provisions the owner and signs it in.
	resp = postJSON(t, client, ts.URL+"/api/v1/setup",
		`{"email":"Owner@Example.com","password":"correct horse battery"}`)
	var session struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&session)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("want 201, got %d", resp.StatusCode)
	}
	if session.Email != "owner@example.com" {
		t.Errorf("email should be normalised, got %q", session.Email)
	}
	if session.Role != "owner" {
		t.Errorf("role: got %q", session.Role)
	}

	// The session cookie works straight away.
	resp, err = client.Get(ts.URL + "/api/v1/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the wizard should leave the browser signed in, got %d", resp.StatusCode)
	}
}

func TestSetupClosesOnceProvisioned(t *testing.T) {
	ts, _ := newTestServer(t) // already has a seeded owner
	defer ts.Close()
	client := jarClient(t)

	resp, err := client.Get(ts.URL + "/api/v1/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	var status struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&status)
	resp.Body.Close()
	if status.NeedsSetup {
		t.Fatal("a provisioned deployment must not report needs_setup")
	}

	// And the endpoint itself refuses, not just the UI.
	resp = postJSON(t, client, ts.URL+"/api/v1/setup",
		`{"email":"intruder@example.com","password":"correct horse battery"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("want 409 once provisioned, got %d", resp.StatusCode)
	}
}

// signIn logs the seeded owner in on a jar-backed client and returns the
// session cookie it received.
func signIn(t *testing.T, ts *httptest.Server, client *http.Client) *http.Cookie {
	t.Helper()
	resp := postJSON(t, client, ts.URL+"/api/v1/auth/login",
		`{"email":"owner@example.com","password":"supersecret123"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login failed with %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == SessionCookie {
			return c
		}
	}
	t.Fatal("login set no session cookie")
	return nil
}

func TestChangePasswordRequiresCurrentAndRotatesSessions(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()
	client := jarClient(t)
	oldCookie := signIn(t, ts, client)

	// The wrong current password changes nothing.
	resp := postJSON(t, client, ts.URL+"/api/v1/account/password",
		`{"current_password":"wrong","new_password":"a brand new secret"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 for a wrong current password, got %d", resp.StatusCode)
	}

	// The real change succeeds.
	resp = postJSON(t, client, ts.URL+"/api/v1/account/password",
		`{"current_password":"supersecret123","new_password":"a brand new secret"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	// Every session held before the change is dead, including this browser's
	// previous cookie.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/session", nil)
	req.AddCookie(oldCookie)
	bare := &http.Client{} // no jar, so only the cookie we attach is sent
	stale, err := bare.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	stale.Body.Close()
	if stale.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the pre-change cookie must stop working, got %d", stale.StatusCode)
	}

	// The browser that made the change got a fresh cookie and stays signed in.
	resp, err = client.Get(ts.URL + "/api/v1/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the caller should have been re-issued a session, got %d", resp.StatusCode)
	}

	// And the new password is the one that works from now on.
	fresh := jarClient(t)
	resp = postJSON(t, fresh, ts.URL+"/api/v1/auth/login",
		`{"email":"owner@example.com","password":"a brand new secret"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the new password should authenticate, got %d", resp.StatusCode)
	}
}

func TestChangePasswordEnforcesPolicy(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()
	client := jarClient(t)
	signIn(t, ts, client)

	for _, body := range []string{
		`{"current_password":"supersecret123","new_password":"short"}`,
		`{"current_password":"supersecret123","new_password":"supersecret123"}`,
	} {
		resp := postJSON(t, client, ts.URL+"/api/v1/account/password", body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("want 400 for %s, got %d", body, resp.StatusCode)
		}
	}
}

func TestChangeEmailRequiresCurrentPassword(t *testing.T) {
	ts, st := newTestServer(t)
	defer ts.Close()
	client := jarClient(t)
	signIn(t, ts, client)

	resp := postJSON(t, client, ts.URL+"/api/v1/account/email",
		`{"current_password":"wrong","email":"new@example.com"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 for a wrong current password, got %d", resp.StatusCode)
	}
	if _, _, err := st.FindLoginByEmail("new@example.com"); err == nil {
		t.Fatal("a rejected request must not have changed the address")
	}

	resp = postJSON(t, client, ts.URL+"/api/v1/account/email",
		`{"current_password":"supersecret123","email":"garbage"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for an invalid address, got %d", resp.StatusCode)
	}

	resp = postJSON(t, client, ts.URL+"/api/v1/account/email",
		`{"current_password":"supersecret123","email":"New@Example.com"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if _, _, err := st.FindLoginByEmail("new@example.com"); err != nil {
		t.Errorf("the new address should resolve: %v", err)
	}
	if _, _, err := st.FindLoginByEmail("owner@example.com"); err == nil {
		t.Error("the old address must stop resolving")
	}
}

func TestAccountEndpointsRejectUnauthenticated(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()
	client := jarClient(t)

	for _, path := range []string{"/api/v1/account/email", "/api/v1/account/password"} {
		resp := postJSON(t, client, ts.URL+path, `{"current_password":"x","email":"a@example.com"}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: want 401, got %d", path, resp.StatusCode)
		}
	}
}

// Guard against a regression in the hashing choice: what is stored must never
// be the password itself, and must verify.
func TestSeededOwnerPasswordIsHashed(t *testing.T) {
	ts, st := newTestServer(t)
	defer ts.Close()
	user, _, err := st.FindLoginByEmail("owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(user.PasswordHash, "supersecret123") {
		t.Fatal("the password appears in the stored hash")
	}
	if !strings.HasPrefix(user.PasswordHash, "pbkdf2-sha256$") {
		t.Errorf("unexpected hash format: %q", user.PasswordHash)
	}
	if !auth.VerifyPassword("supersecret123", user.PasswordHash) {
		t.Error("the stored hash does not verify the password")
	}
}
