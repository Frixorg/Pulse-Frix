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
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/frix-me/pulse/agent/internal/version"
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
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/agents/enroll", bytes.NewReader(body))
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
		return nil, fmt.Errorf("enroll failed: status %d", resp.StatusCode)
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

// Envelope wraps every message with its type and the sending agent.
type Envelope struct {
	Type      string          `json:"type"`
	AgentID   string          `json:"agent_id"`
	Protocol  string          `json:"protocol"`
	Timestamp time.Time       `json:"timestamp"`
	Body      json.RawMessage `json:"body"`
}

// Send posts a message of the given type with a JSON body to /api/v1/agents/ingest.
// It signs the request and retries transient failures with exponential backoff.
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
		if err := c.doSigned(ctx, "/api/v1/agents/ingest", payload); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("send %s failed after %d attempts: %w", msgType, maxAttempts, lastErr)
}

func (c *Client) doSigned(ctx context.Context, path string, payload []byte) error {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce := newNonce()
	bodyHash := sha256.Sum256(payload)

	// Signed message binds method, path, timestamp, nonce and body hash.
	signingInput := fmt.Sprintf("POST|%s|%s|%s|%s", path, ts, nonce, hex.EncodeToString(bodyHash[:]))
	sig := ed25519.Sign(c.signer, []byte(signingInput))

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())
	req.Header.Set("X-Pulse-Agent-Id", c.agentID)
	req.Header.Set("X-Pulse-Timestamp", ts)
	req.Header.Set("X-Pulse-Nonce", nonce)
	req.Header.Set("X-Pulse-Signature", base64.StdEncoding.EncodeToString(sig))

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("server status %d (retryable)", resp.StatusCode)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("server status %d", resp.StatusCode)
	}
	return nil
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
