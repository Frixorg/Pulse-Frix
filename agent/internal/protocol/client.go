// Package protocol implements the OUTBOUND agent -> control-plane client.
//
// Key properties (see docs/AGENT_PROTOCOL.md):
//   - outbound only; the agent never listens for inbound management traffic
//   - every request is signed with the agent's Ed25519 key
//   - timestamp + nonce give replay protection
//   - the client NEVER receives or executes arbitrary commands; it only sends
//   - transient failures retry with exponential backoff + jitter
package protocol

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/frix-me/pulse/agent/internal/version"
)

// ErrUnauthorized means the control plane refused this agent's credential
// outright: the agent id is unknown, revoked, or bound to a different key.
//
// It is deliberately distinct from a transient failure. Retrying it is
// pointless — nothing about the next attempt will differ — and treating it as
// transient is how an agent ends up buffering forever while its dashboard
// shows no servers at all. The caller is expected to re-enroll instead.
var ErrUnauthorized = errors.New("control plane rejected this agent credential")

// ErrReenrollUnsupported means this control plane predates the signed
// re-enrolment endpoint. The caller falls back to token enrolment rather than
// mistaking an older server for a refusal.
var ErrReenrollUnsupported = errors.New("control plane has no re-enrolment endpoint")

const (
	enrollPath   = "/api/v1/agents/enroll"
	reenrollPath = "/api/v1/agents/reenroll"
	ingestPath   = "/api/v1/agents/ingest"
)

// EnrollRequest is sent (unsigned, authenticated by the enrollment token) to
// register the agent and its public key with the control plane.
type EnrollRequest struct {
	EnrollmentToken string            `json:"enrollment_token"`
	InstallationID  string            `json:"installation_id"`
	PublicKey       string            `json:"public_key"`
	ProtocolVersion string            `json:"protocol_version"`
	Fingerprint     map[string]string `json:"fingerprint"`
}

// ReenrollRequest re-binds an agent that already holds a keypair. Unlike
// EnrollRequest it is SIGNED with that keypair, and possession of the key is
// what the control plane trusts: the enrollment token is single-use and is
// long spent by the time an agent needs to recover.
type ReenrollRequest struct {
	InstallationID  string            `json:"installation_id"`
	PublicKey       string            `json:"public_key"`
	PreviousAgentID string            `json:"previous_agent_id,omitempty"`
	ProtocolVersion string            `json:"protocol_version"`
	Fingerprint     map[string]string `json:"fingerprint"`
	// EnrollmentToken is sent only when the agent still holds one. It lets a
	// control plane that has genuinely never seen this key adopt it, instead
	// of refusing an agent that has no other way back in.
	EnrollmentToken string `json:"enrollment_token,omitempty"`
}

// EnrollResponse carries the server-assigned identifiers.
type EnrollResponse struct {
	ServerID string `json:"server_id"`
	AgentID  string `json:"agent_id"`
	Protocol string `json:"protocol"`
}

// Enroll registers the agent with the control plane using a short-lived
// enrollment token. It is the only unsigned call (the token authenticates it).
func Enroll(ctx context.Context, baseURL string, req EnrollRequest) (*EnrollResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+enrollPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", version.UserAgent())
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("enroll failed: %s", serverMessage(resp))
	}
	var out EnrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Reenroll re-binds an agent the control plane no longer recognises, proving
// identity with the agent's own key rather than a token it can no longer have.
//
// ErrUnauthorized here is meaningful and final: the control plane has no record
// of this key and no token was accepted, so a human has to issue a fresh
// tracking key. Every other error is worth retrying.
func Reenroll(ctx context.Context, baseURL string, signer ed25519.PrivateKey, req ReenrollRequest) (*EnrollResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+reenrollPath, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	signRequest(httpReq, signer, req.PreviousAgentID, reenrollPath, payload)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// A control plane that predates this endpoint answers 404. That is an old
	// server, not a refusal, so the caller falls back to token enrolment.
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrReenrollUnsupported
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: %s", ErrUnauthorized, serverMessage(resp))
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("reenroll failed: %s", serverMessage(resp))
	}
	var out EnrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Client posts signed messages to the control plane.
type Client struct {
	baseURL string
	agentID string
	signer  ed25519.PrivateKey
	http    *http.Client
}

// New creates a protocol client.
func New(baseURL, agentID string, signer ed25519.PrivateKey) *Client {
	return &Client{
		baseURL: baseURL,
		agentID: agentID,
		signer:  signer,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// Rebind points the client at the agent id a re-enrolment just issued. It is
// called from the agent's single send loop, between sends.
func (c *Client) Rebind(agentID string) { c.agentID = agentID }

// Envelope wraps every message with its type and the sending agent.
type Envelope struct {
	Type      string          `json:"type"`
	AgentID   string          `json:"agent_id"`
	Protocol  string          `json:"protocol"`
	Timestamp time.Time       `json:"timestamp"`
	Body      json.RawMessage `json:"body"`
}

// Send posts a message of the given type with a JSON body to the ingest
// endpoint. It signs the request and retries transient failures with
// exponential backoff. A rejected credential comes back immediately, wrapped in
// ErrUnauthorized, because no number of retries will change it.
func (c *Client) Send(ctx context.Context, msgType string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	env := Envelope{
		Type:      msgType,
		AgentID:   c.agentID,
		Protocol:  version.Protocol,
		Timestamp: time.Now().UTC(),
		Body:      raw,
	}
	payload, err := json.Marshal(env)
	if err != nil {
		return err
	}

	const maxAttempts = 5
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if err := sleepBackoff(ctx, attempt); err != nil {
				return err
			}
		}
		err := c.doSigned(ctx, ingestPath, payload)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrUnauthorized) {
			return err
		}
		lastErr = err
	}
	return fmt.Errorf("send %s failed after %d attempts: %w", msgType, maxAttempts, lastErr)
}

func (c *Client) doSigned(ctx context.Context, path string, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	signRequest(req, c.signer, c.agentID, path, payload)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: %s", ErrUnauthorized, serverMessage(resp))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("server status %d (retryable)", resp.StatusCode)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("server status %d", resp.StatusCode)
	}
	return nil
}

// signRequest signs a payload and sets the headers that carry the proof. The
// signed message binds method, path, timestamp, nonce and body hash, so a
// captured request cannot be replayed against a different endpoint.
func signRequest(req *http.Request, signer ed25519.PrivateKey, agentID, path string, payload []byte) {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce := newNonce()
	bodyHash := sha256.Sum256(payload)
	signingInput := fmt.Sprintf("POST|%s|%s|%s|%s", path, ts, nonce, hex.EncodeToString(bodyHash[:]))
	sig := ed25519.Sign(signer, []byte(signingInput))

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())
	req.Header.Set("X-Pulse-Agent-Id", agentID)
	req.Header.Set("X-Pulse-Timestamp", ts)
	req.Header.Set("X-Pulse-Nonce", nonce)
	req.Header.Set("X-Pulse-Signature", base64.StdEncoding.EncodeToString(sig))
}

// serverMessage pulls the human-readable message out of an API error body, so
// the agent log says what the control plane actually objected to instead of a
// bare status code. That sentence is usually the operator's whole next step.
func serverMessage(resp *http.Response) string {
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&body); err != nil {
		return resp.Status
	}
	if body.Error.Message == "" {
		return resp.Status
	}
	return body.Error.Message
}

func newNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// sleepBackoff waits exponentially longer between attempts, with jitter, and
// respects context cancellation.
func sleepBackoff(ctx context.Context, attempt int) error {
	base := time.Duration(math.Pow(2, float64(attempt))) * 250 * time.Millisecond
	jitterBytes := make([]byte, 2)
	_, _ = rand.Read(jitterBytes)
	jitter := time.Duration(int(jitterBytes[0])) * time.Millisecond
	wait := base + jitter
	if wait > 30*time.Second {
		wait = 30 * time.Second
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
		return nil
	}
}
