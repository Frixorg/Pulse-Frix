package httpx

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/frix-me/pulse/api/internal/auth"
	"github.com/frix-me/pulse/api/internal/config"
	"github.com/frix-me/pulse/api/internal/model"
	"github.com/frix-me/pulse/api/internal/store"
)

// newAgentTestServer builds a Server directly so the handlers can be called
// without the per-IP enrollment limiter, which would otherwise throttle a test
// that makes more than three re-enrolment attempts from 127.0.0.1.
func newAgentTestServer(t *testing.T) (*Server, *store.Memory, string) {
	t.Helper()
	st := store.NewMemory()
	hash, _ := auth.HashPassword("supersecret123")
	if _, _, err := st.SeedOrgOwner("Test", "owner@example.com", hash); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	_, mem, err := st.FindLoginByEmail("owner@example.com")
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	cfg := config.Load()
	cfg.Env = "development"
	return New(cfg, st, slog.New(slog.NewTextHandler(discard{}, nil))), st, mem.OrgID
}

// signedReenroll builds a re-enrolment request signed the way the agent signs
// it. A fresh random nonce keeps each call past the replay cache.
func signedReenroll(t *testing.T, priv ed25519.PrivateKey, req reenrollRequest) *http.Request {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, reenrollPath, bytes.NewReader(body))
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	nonce := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256(body)
	input := "POST|" + reenrollPath + "|" + ts + "|" + nonce + "|" + hex.EncodeToString(sum[:])
	r.Header.Set("X-Pulse-Timestamp", ts)
	r.Header.Set("X-Pulse-Nonce", nonce)
	r.Header.Set("X-Pulse-Signature", base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(input))))
	return r
}

// enrolledAgent puts a live server + agent in the store, as first-time
// enrolment would have done.
func enrolledAgent(t *testing.T, st *store.Memory, orgID, pubKey string) *model.Agent {
	t.Helper()
	srv := &model.Server{ServerID: auth.NewID("srv"), Hostname: "vps-1", Mode: "cloud",
		Status: model.HealthUnknown, LastSeenAt: time.Now().UTC()}
	if err := st.UpsertServer(orgID, srv); err != nil {
		t.Fatal(err)
	}
	agent := &model.Agent{OrgID: orgID, ServerID: srv.ServerID, AgentID: auth.NewID("agt"),
		PublicKey: pubKey, ProtocolVersion: "1.0", LastSeenAt: time.Now().UTC()}
	if err := st.CreateAgent(agent); err != nil {
		t.Fatal(err)
	}
	return agent
}

func newAgentKey(t *testing.T) (string, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pub), priv
}

func decodeIDs(t *testing.T, rec *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var out struct {
		ServerID string `json:"server_id"`
		AgentID  string `json:"agent_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad response body %q: %v", rec.Body.String(), err)
	}
	return out.ServerID, out.AgentID
}

// The recovery this endpoint exists for: the agent still holds the key it
// enrolled with, its single-use token is long spent, and it gets its binding
// back without a human touching anything.
func TestReenrollRebindsKnownKeyWithoutToken(t *testing.T) {
	s, st, orgID := newAgentTestServer(t)
	pub, priv := newAgentKey(t)
	agent := enrolledAgent(t, st, orgID, pub)

	rec := httptest.NewRecorder()
	s.handleReenroll(rec, signedReenroll(t, priv, reenrollRequest{
		InstallationID:  "ins_1",
		PublicKey:       pub,
		PreviousAgentID: agent.AgentID,
		ProtocolVersion: "1.0",
		Fingerprint:     map[string]string{"hostname": "vps-1"},
	}))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 rebind, got %d: %s", rec.Code, rec.Body.String())
	}
	serverID, agentID := decodeIDs(t, rec)
	if agentID != agent.AgentID || serverID != agent.ServerID {
		t.Errorf("rebind must return the existing binding, got agent=%s server=%s want agent=%s server=%s",
			agentID, serverID, agent.AgentID, agent.ServerID)
	}
}

// Re-enrolment must not quietly undo a revocation an operator performed.
func TestReenrollRefusesRevokedAgent(t *testing.T) {
	s, st, orgID := newAgentTestServer(t)
	pub, priv := newAgentKey(t)
	agent := enrolledAgent(t, st, orgID, pub)
	if err := st.RevokeAgent(orgID, agent.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleReenroll(rec, signedReenroll(t, priv, reenrollRequest{
		InstallationID:  "ins_1",
		PublicKey:       pub,
		PreviousAgentID: agent.AgentID,
		ProtocolVersion: "1.0",
	}))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for a revoked agent, got %d: %s", rec.Code, rec.Body.String())
	}
}

// A signature proves key ownership, never authorisation. An unknown key with no
// token must be refused, or anyone could register themselves a server.
func TestReenrollRefusesUnknownKeyWithoutToken(t *testing.T) {
	s, _, _ := newAgentTestServer(t)
	pub, priv := newAgentKey(t)

	rec := httptest.NewRecorder()
	s.handleReenroll(rec, signedReenroll(t, priv, reenrollRequest{
		InstallationID:  "ins_1",
		PublicKey:       pub,
		ProtocolVersion: "1.0",
	}))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for an unknown key with no token, got %d: %s", rec.Code, rec.Body.String())
	}
}

// The other half of the fix: a control plane that has genuinely lost its data
// can adopt the agent again once the operator issues a fresh token, keeping the
// agent's existing keypair.
func TestReenrollAdoptsUnknownKeyWithValidToken(t *testing.T) {
	s, st, orgID := newAgentTestServer(t)
	pub, priv := newAgentKey(t)

	plain, hash, err := auth.GenerateToken("pst")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateEnrollmentToken(&model.EnrollmentToken{
		OrgID: orgID, TokenHash: hash, ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleReenroll(rec, signedReenroll(t, priv, reenrollRequest{
		InstallationID:  "ins_1",
		PublicKey:       pub,
		ProtocolVersion: "1.0",
		Fingerprint:     map[string]string{"hostname": "vps-1"},
		EnrollmentToken: plain,
	}))

	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201 adopt, got %d: %s", rec.Code, rec.Body.String())
	}
	serverID, agentID := decodeIDs(t, rec)
	if serverID == "" || agentID == "" {
		t.Fatal("adopt must return both ids")
	}
	// The new binding has to carry the agent's own key, or the very next
	// ingest is refused all over again.
	got, err := st.GetAgentByAgentID(agentID)
	if err != nil {
		t.Fatalf("adopted agent is not resolvable: %v", err)
	}
	if got.PublicKey != pub {
		t.Error("the adopted agent was not bound to the key it signed with")
	}
}

// A body signed with the wrong key must never be accepted, however plausible
// the public key in it looks.
func TestReenrollRejectsBadSignature(t *testing.T) {
	s, st, orgID := newAgentTestServer(t)
	pub, _ := newAgentKey(t)
	_, otherPriv := newAgentKey(t)
	agent := enrolledAgent(t, st, orgID, pub)

	rec := httptest.NewRecorder()
	// Claims the enrolled agent's key, but signs with a different one.
	s.handleReenroll(rec, signedReenroll(t, otherPriv, reenrollRequest{
		InstallationID:  "ins_1",
		PublicKey:       pub,
		PreviousAgentID: agent.AgentID,
		ProtocolVersion: "1.0",
	}))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for a mismatched signature, got %d: %s", rec.Code, rec.Body.String())
	}
}
