package httpx

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/frix-me/pulse/api/internal/auth"
	"github.com/frix-me/pulse/api/internal/model"
)

// supportedProtocols is the server's accepted protocol version range.
var supportedProtocols = map[string]bool{"1.0": true}

// --- enrollment token creation (dashboard) ---

func (s *Server) handleCreateEnrollment(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	plain, hash, err := auth.GenerateToken("pst")
	if err != nil {
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "could not create token")
		return
	}
	tok := &model.EnrollmentToken{
		OrgID:     p.OrgID,
		TokenHash: hash,
		ExpiresAt: time.Now().Add(s.cfg.EnrollmentTTL()).UTC(),
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateEnrollmentToken(tok); err != nil {
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "could not persist token")
		return
	}
	s.audit.Record(p.OrgID, p.Email, "agent.enrollment_token.create", "success", clientIP(r), nil)
	// The plain token is shown ONCE.
	JSON(w, http.StatusCreated, map[string]any{
		"enrollment_token": plain,
		"expires_at":       tok.ExpiresAt,
	})
}

// --- enroll (agent, rate-limited) ---

type enrollRequest struct {
	EnrollmentToken string            `json:"enrollment_token"`
	InstallationID  string            `json:"installation_id"`
	PublicKey       string            `json:"public_key"`
	ProtocolVersion string            `json:"protocol_version"`
	Fingerprint     map[string]string `json:"fingerprint"`
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	var req enrollRequest
	if err := decodeJSON(r, &req, 8192); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "invalid request body")
		return
	}
	if !supportedProtocols[req.ProtocolVersion] {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "unsupported protocol version")
		return
	}
	if req.PublicKey == "" || req.EnrollmentToken == "" {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "missing enrollment fields")
		return
	}

	tok, err := s.store.ConsumeEnrollmentToken(auth.HashToken(req.EnrollmentToken), time.Now())
	if err != nil {
		s.audit.Record("", req.InstallationID, "agent.enroll", "failure", clientIP(r), nil)
		Fail(w, r, http.StatusUnauthorized, CodeAuth, "invalid or expired enrollment token")
		return
	}

	srv, agent, err := s.registerAgent(tok.OrgID, req.PublicKey, req.ProtocolVersion,
		req.Fingerprint["hostname"], time.Now().UTC())
	if err != nil {
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "could not register agent")
		return
	}
	s.audit.Record(tok.OrgID, req.InstallationID, "agent.enroll", "success", clientIP(r),
		map[string]any{"server_id": srv.ServerID})
	JSON(w, http.StatusCreated, map[string]any{
		"server_id": srv.ServerID,
		"agent_id":  agent.AgentID,
		"protocol":  req.ProtocolVersion,
	})
}

// --- ingest (agent, signed, replay-protected) ---

type ingestEnvelope struct {
	Type     string          `json:"type"`
	AgentID  string          `json:"agent_id"`
	Protocol string          `json:"protocol"`
	Body     json.RawMessage `json:"body"`
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8*1024*1024))
	if err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "could not read body")
		return
	}

	agentID := r.Header.Get("X-Pulse-Agent-Id")
	if agentID == "" {
		Fail(w, r, http.StatusUnauthorized, CodeAuth, "missing signature headers")
		return
	}
	agent, err := s.store.GetAgentByAgentID(agentID)
	if err != nil {
		// The wording matters: this message reaches the agent's log, and it is
		// the operator's whole next step. "Unknown agent" on its own has left
		// people staring at an empty dashboard with nothing to act on.
		Fail(w, r, http.StatusUnauthorized, CodeAuth,
			"unknown or revoked agent — the agent should re-enroll; if it cannot, "+
				"generate a fresh enrollment token and reinstall it on that host")
		return
	}
	if err := s.verifySignedRequest(r, body, agent.PublicKey, ingestPath); err != nil {
		Fail(w, r, http.StatusUnauthorized, CodeAuth, err.Error())
		return
	}

	var env ingestEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "invalid envelope")
		return
	}

	now := time.Now().UTC()
	switch env.Type {
	case "discovery":
		_ = s.store.SaveDiscovery(agent.OrgID, agent.ServerID, env.Body)
		_ = s.store.EnsureServer(agent.OrgID, agent.ServerID, hostnameFromSnapshot(env.Body), now, model.HealthHealthy)
	case "metrics":
		_ = s.store.SaveMetrics(agent.OrgID, agent.ServerID, env.Body)
		_ = s.store.AppendMetricSample(agent.OrgID, agent.ServerID, now, env.Body)
		_ = s.store.EnsureServer(agent.OrgID, agent.ServerID, "", now, model.HealthHealthy)
	case "logs":
		_ = s.store.SaveLogs(agent.OrgID, agent.ServerID, env.Body)
		_ = s.store.EnsureServer(agent.OrgID, agent.ServerID, "", now, model.HealthHealthy)
	case "heartbeat", "hello", "health":
		_ = s.store.EnsureServer(agent.OrgID, agent.ServerID, "", now, model.HealthHealthy)
	default:
		Fail(w, r, http.StatusBadRequest, CodeValidation, "unknown message type")
		return
	}
	JSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (s *Server) handleRevokeAgent(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	if err := s.store.RevokeAgent(p.OrgID, r.PathValue("id"), time.Now().UTC()); err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "agent not found")
		return
	}
	s.audit.Record(p.OrgID, p.Email, "agent.revoke", "success", clientIP(r),
		map[string]any{"agent": r.PathValue("id")})
	JSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// --- nonce cache (replay protection) ---

type nonceCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

// nonces lazily initialises the per-server nonce cache.
func (s *Server) nonces() *nonceCache {
	s.nonceOnce.Do(func() { s.nonceCacheV = &nonceCache{seen: map[string]time.Time{}} })
	return s.nonceCacheV
}

// checkAndStore returns false if the nonce was already seen (replay); otherwise
// records it and returns true. Old entries are pruned opportunistically.
func (n *nonceCache) checkAndStore(nonce string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	if _, ok := n.seen[nonce]; ok {
		return false
	}
	// Prune entries older than the timestamp window.
	for k, t := range n.seen {
		if now.Sub(t) > 2*time.Minute {
			delete(n.seen, k)
		}
	}
	n.seen[nonce] = now
	return true
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// hostnameFromSnapshot pulls the hostname out of a discovery snapshot body so a
// self-healed server row can carry a friendly name.
func hostnameFromSnapshot(b json.RawMessage) string {
	var s struct {
		Hostname string `json:"hostname"`
	}
	_ = json.Unmarshal(b, &s)
	return s.Hostname
}

// Signed-request paths. The signature binds the path it was made for, so a
// captured ingest request cannot be replayed against re-enrolment. They are
// constants rather than r.URL.Path because a proxy that rewrites the path in
// front of the API must not silently invalidate every agent's signature.
const (
	ingestPath   = "/api/v1/agents/ingest"
	reenrollPath = "/api/v1/agents/reenroll"
)

// verifySignedRequest checks the Ed25519 proof carried in the Pulse headers
// against the public key the caller names, over the exact bytes received.
//
// ingest passes the key it has ON RECORD for the agent id, which is what makes
// it an authentication check. Re-enrolment passes the key FROM THE BODY, which
// makes it only a proof of key possession — authorisation there comes from
// already knowing the key, or from an enrollment token.
func (s *Server) verifySignedRequest(r *http.Request, body []byte, publicKeyB64, path string) error {
	tsStr := r.Header.Get("X-Pulse-Timestamp")
	nonce := r.Header.Get("X-Pulse-Nonce")
	sigB64 := r.Header.Get("X-Pulse-Signature")
	if tsStr == "" || nonce == "" || sigB64 == "" {
		return errors.New("missing signature headers")
	}
	// Timestamp window (±60s) mitigates replay.
	tsMillis, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil || absDuration(time.Since(time.UnixMilli(tsMillis))) > 60*time.Second {
		return errors.New("stale or invalid timestamp")
	}
	// Single-use nonce.
	if !s.nonces().checkAndStore(nonce) {
		return errors.New("replayed request")
	}
	pub, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("invalid agent key")
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return errors.New("invalid signature encoding")
	}
	bodyHash := sha256.Sum256(body)
	signingInput := "POST|" + path + "|" + tsStr + "|" + nonce + "|" + hex.EncodeToString(bodyHash[:])
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(signingInput), sig) {
		return errors.New("signature verification failed")
	}
	return nil
}

// registerAgent creates the server row and the agent binding for a key this
// control plane has just accepted. Shared by first-time enrolment and by the
// re-enrolment path that adopts a key it has never seen before.
func (s *Server) registerAgent(orgID, publicKey, protocolVersion, hostname string, now time.Time) (*model.Server, *model.Agent, error) {
	srv := &model.Server{
		ServerID:   auth.NewID("srv"),
		Hostname:   hostname,
		Mode:       "cloud",
		Status:     model.HealthUnknown,
		LastSeenAt: now,
	}
	if err := s.store.UpsertServer(orgID, srv); err != nil {
		return nil, nil, err
	}
	agent := &model.Agent{
		OrgID:           orgID,
		ServerID:        srv.ServerID,
		AgentID:         auth.NewID("agt"),
		PublicKey:       publicKey,
		ProtocolVersion: protocolVersion,
		LastSeenAt:      now,
	}
	if err := s.store.CreateAgent(agent); err != nil {
		return nil, nil, err
	}
	return srv, agent, nil
}

// --- re-enroll (agent, signed with its own key, rate-limited) ---

type reenrollRequest struct {
	InstallationID  string            `json:"installation_id"`
	PublicKey       string            `json:"public_key"`
	PreviousAgentID string            `json:"previous_agent_id"`
	ProtocolVersion string            `json:"protocol_version"`
	Fingerprint     map[string]string `json:"fingerprint"`
	EnrollmentToken string            `json:"enrollment_token"`
}

// handleReenroll re-binds an agent whose credential this control plane no
// longer accepts, without demanding a fresh enrollment token.
//
// It exists because enrollment tokens are single-use and short-lived, so an
// agent that loses its binding — a control plane restarted on an ephemeral
// store, a rebuilt database, an agent id that drifted — had no way back and
// simply stopped reporting, for good. The agent still holds the Ed25519 key it
// enrolled with, so this request is SIGNED with that key.
//
// Three outcomes, in order:
//
//   - the key is known and live    → hand back its existing ids (idempotent)
//   - the key is known and REVOKED → refuse; a revocation has to stick
//   - the key is unknown           → adopt it ONLY with a valid enrollment
//     token, on exactly the terms a first-time install would get
func (s *Server) handleReenroll(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8192))
	if err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "could not read body")
		return
	}
	var req reenrollRequest
	if err := json.Unmarshal(body, &req); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "invalid request body")
		return
	}
	if !supportedProtocols[req.ProtocolVersion] {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "unsupported protocol version")
		return
	}
	if req.PublicKey == "" {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "missing public key")
		return
	}
	// Verified against the key IN THE BODY. That proves the caller holds the
	// matching private key — the one thing that makes a tokenless re-bind safe
	// — and nothing more, which is why an unknown key still needs a token.
	if err := s.verifySignedRequest(r, body, req.PublicKey, reenrollPath); err != nil {
		s.audit.Record("", req.InstallationID, "agent.reenroll", "failure", clientIP(r), nil)
		Fail(w, r, http.StatusUnauthorized, CodeAuth, err.Error())
		return
	}

	now := time.Now().UTC()
	existing, lookupErr := s.store.GetAgentByPublicKey(req.PublicKey)
	switch {
	case lookupErr == nil && existing.RevokedAt != nil:
		s.audit.Record(existing.OrgID, req.InstallationID, "agent.reenroll", "failure", clientIP(r),
			map[string]any{"reason": "revoked", "agent": existing.AgentID})
		Fail(w, r, http.StatusUnauthorized, CodeAuth,
			"this agent was revoked, and re-enrolling cannot undo that. Uninstall the agent on that "+
				"host, then install it again with a fresh enrollment token.")
		return

	case lookupErr == nil:
		// Known and live: nothing to create. Hand back the binding it already
		// has and refresh the server row so it stops looking offline.
		_ = s.store.EnsureServer(existing.OrgID, existing.ServerID,
			req.Fingerprint["hostname"], now, model.HealthHealthy)
		s.audit.Record(existing.OrgID, req.InstallationID, "agent.reenroll", "success", clientIP(r),
			map[string]any{"server_id": existing.ServerID, "agent_id": existing.AgentID, "path": "rebind"})
		JSON(w, http.StatusOK, map[string]any{
			"server_id": existing.ServerID,
			"agent_id":  existing.AgentID,
			"protocol":  req.ProtocolVersion,
		})
		return
	}

	// The key is unknown to this control plane, so a signature buys nothing on
	// its own: it needs the same enrollment token a first-time install needs.
	if req.EnrollmentToken == "" {
		s.audit.Record("", req.InstallationID, "agent.reenroll", "failure", clientIP(r),
			map[string]any{"reason": "unknown_key_no_token"})
		Fail(w, r, http.StatusUnauthorized, CodeAuth,
			"this control plane has no record of this agent. Generate a fresh enrollment token in "+
				"the dashboard and re-run the install command on that host.")
		return
	}
	tok, err := s.store.ConsumeEnrollmentToken(auth.HashToken(req.EnrollmentToken), now)
	if err != nil {
		s.audit.Record("", req.InstallationID, "agent.reenroll", "failure", clientIP(r),
			map[string]any{"reason": "bad_token"})
		Fail(w, r, http.StatusUnauthorized, CodeAuth,
			"invalid or expired enrollment token — generate a fresh one in the dashboard")
		return
	}
	srv, agent, err := s.registerAgent(tok.OrgID, req.PublicKey, req.ProtocolVersion,
		req.Fingerprint["hostname"], now)
	if err != nil {
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "could not register agent")
		return
	}
	s.audit.Record(tok.OrgID, req.InstallationID, "agent.reenroll", "success", clientIP(r),
		map[string]any{"server_id": srv.ServerID, "agent_id": agent.AgentID, "path": "adopt"})
	JSON(w, http.StatusCreated, map[string]any{
		"server_id": srv.ServerID,
		"agent_id":  agent.AgentID,
		"protocol":  req.ProtocolVersion,
	})
}
