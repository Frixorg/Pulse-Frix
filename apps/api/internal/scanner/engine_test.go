package scanner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// findCheck returns a check definition by id, or fails the test.
func findCheck(t *testing.T, id string) checkDef {
	t.Helper()
	for _, d := range registry() {
		if d.meta.ID == id {
			return d
		}
	}
	t.Fatalf("check %q not in registry", id)
	return checkDef{}
}

// runDirect invokes a single check against an input, collecting its findings.
// It bypasses the manager/SSRF client so active checks can hit an httptest
// server on loopback.
func runDirect(t *testing.T, id string, in *Input) []Finding {
	t.Helper()
	d := findCheck(t, id)
	var out []Finding
	emit := func(f Finding) {
		f.CheckID = d.meta.ID
		if f.Category == "" {
			f.Category = d.meta.Category
		}
		out = append(out, f)
	}
	d.run(context.Background(), in, emit, func(string, string) {})
	return out
}

func TestPassiveFindsPublicDatabase(t *testing.T) {
	in := &Input{
		ServerID: "srv_1",
		Resources: []Resource{
			{Type: "database", Name: "postgres", Attributes: map[string]any{"exposure": "public"}},
			{Type: "docker_container", Name: "app", Status: "running", Attributes: map[string]any{"privileged": true}},
		},
	}
	m := NewManager(nil)
	st := m.RunPassiveSync(in)

	if st.Status != ScanDone {
		t.Fatalf("status = %q, want done", st.Status)
	}
	if !hasFinding(st.Findings, "db-public-postgres") {
		t.Errorf("expected localhost-isolation finding for public postgres")
	}
	if !hasFinding(st.Findings, "priv-app") {
		t.Errorf("expected privileged-container finding")
	}
	// The check that produced a finding must be marked "issues".
	if statusOf(st.Checks, "localhost-isolation") != StatusIssues {
		t.Errorf("localhost-isolation check should be 'issues'")
	}
	// A check with no matching resource must pass, not error.
	if statusOf(st.Checks, "docker-exposure") != StatusPass {
		t.Errorf("docker-exposure should pass when nothing is exposed")
	}
}

func TestActiveHeaderCookieBannerChecks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "nginx/1.21.0")
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "abc"}) // no Secure/HttpOnly/SameSite
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer srv.Close()

	in := &Input{
		Targets:    []Target{{URL: srv.URL, FQDN: "test.local", TLS: false}},
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	headers := runDirect(t, "http-security-headers", in)
	if len(headers) == 0 {
		t.Errorf("expected missing-security-header findings, got none")
	}
	cookies := runDirect(t, "cookie-security", in)
	if !hasFinding(cookies, "cookie-test.local-sid") {
		t.Errorf("expected insecure-cookie finding, got %+v", cookies)
	}
	banner := runDirect(t, "server-banner", in)
	if !hasFinding(banner, "banner-test.local") {
		t.Errorf("expected version-banner finding, got %+v", banner)
	}
	clickjack := runDirect(t, "clickjacking", in)
	if !hasFinding(clickjack, "clickjack-test.local") {
		t.Errorf("expected clickjacking finding, got %+v", clickjack)
	}
}

func TestActiveChecksSkippedWithoutTargets(t *testing.T) {
	in := &Input{ServerID: "srv_2"}
	m := NewManager(nil)
	id := m.StartScan(in, ModeActive, nil)

	// Wait for the background scan to finish.
	deadline := time.Now().Add(5 * time.Second)
	var st ScanState
	for time.Now().Before(deadline) {
		s, ok := m.Get(id)
		if ok && s.Status == ScanDone {
			st = s
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st.Status != ScanDone {
		t.Fatalf("scan did not finish; status=%q", st.Status)
	}
	if len(st.Findings) != 0 {
		t.Errorf("active scan with no targets should produce no findings, got %d", len(st.Findings))
	}
	for _, c := range st.Checks {
		if c.Kind == KindActive && c.Status != StatusSkipped {
			t.Errorf("active check %q should be skipped without targets, got %q", c.ID, c.Status)
		}
	}
}

func TestSqlErrorSignature(t *testing.T) {
	if sqlErrorSignature("... You have an error in your SQL syntax ...") == "" {
		t.Errorf("expected a MySQL signature match")
	}
	if sqlErrorSignature("perfectly normal page") != "" {
		t.Errorf("did not expect a match on clean content")
	}
}

func hasFinding(fs []Finding, id string) bool {
	for _, f := range fs {
		if f.ID == id {
			return true
		}
	}
	return false
}

func statusOf(cs []Check, id string) string {
	for _, c := range cs {
		if c.ID == id {
			return c.Status
		}
	}
	return ""
}
