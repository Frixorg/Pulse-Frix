package httpx

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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

	srv := &model.Server{
		ServerID:   auth.NewID("srv"),
		Hostname:   req.Fingerprint["hostname"],
		Mode:       "cloud",
		Status:     model.HealthUnknown,
		LastSeenAt: time.Now().UTC(),
	}
	if err := s.store.UpsertServer(tok.OrgID, srv); err != nil {
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "could not register server")
		return
	}
	agent := &model.Agent{
		OrgID:           tok.OrgID,
		ServerID:        srv.ServerID,
		AgentID:         auth.NewID("agt"),
		PublicKey:       req.PublicKey,
		ProtocolVersion: req.ProtocolVersion,
		LastSeenAt:      time.Now().UTC(),
	}
	if err := s.store.CreateAgent(agent); err != nil {
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
	tsStr := r.Header.Get("X-Pulse-Timestamp")
	nonce := r.Header.Get("X-Pulse-Nonce")
	sigB64 := r.Header.Get("X-Pulse-Signature")
	if agentID == "" || tsStr == "" || nonce == "" || sigB64 == "" {
		Fail(w, r, http.StatusUnauthorized, CodeAuth, "missing signature headers")
		return
	}

	// Timestamp window (±60s) mitigates replay.
	tsMillis, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil || absDuration(time.Since(time.UnixMilli(tsMillis))) > 60*time.Second {
		Fail(w, r, http.StatusUnauthorized, CodeAuth, "stale or invalid timestamp")
		return
	}
	// Single-use nonce.
	if !s.nonces().checkAndStore(nonce) {
		Fail(w, r, http.StatusUnauthorized, CodeAuth, "replayed request")
		return
	}

	agent, err := s.store.GetAgentByAgentID(agentID)
	if err != nil {
		Fail(w, r, http.StatusUnauthorized, CodeAuth, "unknown or revoked agent")
		return
	}
	pub, err := base64.StdEncoding.DecodeString(agent.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		Fail(w, r, http.StatusUnauthorized, CodeAuth, "invalid agent key")
		return
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		Fail(w, r, http.StatusUnauthorized, CodeAuth, "invalid signature encoding")
		return
	}
	bodyHash := sha256.Sum256(body)
	signingInput := "POST|/api/v1/agents/ingest|" + tsStr + "|" + nonce + "|" + hex.EncodeToString(bodyHash[:])
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(signingInput), sig) {
		Fail(w, r, http.StatusUnauthorized, CodeAuth, "signature verification failed")
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
