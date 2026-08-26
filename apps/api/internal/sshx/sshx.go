// Package sshx powers the browser SSH console: the control plane opens an
// outbound SSH connection to a host the operator names, allocates a PTY, and
// bridges the byte stream to the dashboard over a WebSocket.
//
// This is the one place in Pulse that can change a server, so it is fenced off
// deliberately:
//
//   - The agent is NOT involved. Agents remain read-only and still never accept
//     a command from the control plane (see docs/SAFETY_MODEL.md). The console
//     is an ordinary SSH client that happens to run inside the API.
//   - It is off unless the operator sets PULSE_SSH_CONSOLE=true.
//   - It requires the ssh.exec permission (owner/admin only — never viewer).
//   - Credentials are supplied per session, held only for the duration of the
//     dial, and never written to the store, the logs or an audit record.
//   - The default build does not even contain an SSH client; Dial is compiled
//     in only with `-tags ssh`.
package sshx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Errors surfaced to the API layer. Messages are safe to show a user.
var (
	ErrUnsupported     = errors.New("the SSH console is not compiled into this build")
	ErrDisabled        = errors.New("the SSH console is disabled on this control plane")
	ErrNoAuth          = errors.New("provide a password or a private key")
	ErrBadKey          = errors.New("the private key could not be parsed")
	ErrAuth            = errors.New("the server rejected these credentials")
	ErrHostKeyMismatch = errors.New("the host key does not match the fingerprint this browser trusted")
	ErrNotFound        = errors.New("ssh session not found")
	ErrAlreadyAttached = errors.New("this ssh session is already open in another tab")
	ErrTooMany         = errors.New("too many open ssh sessions")
	ErrSessionClosed   = errors.New("the ssh session has ended")
)

// Limits. Terminals are cheap but not free, and an abandoned session holds a
// real connection open on the operator's server.
const (
	maxSessionsPerUser = 4
	maxSessionsTotal   = 64
	attachWindow       = 60 * time.Second // time to open the WebSocket after dialling
	idleLifetime       = 8 * time.Hour    // hard ceiling on a single session
)

// Credentials are supplied by the operator per session and never persisted.
type Credentials struct {
	Host       string
	Port       int
	User       string
	Password   string
	PrivateKey string
	Passphrase string
	// KnownFingerprint pins the host key the browser trusted previously
	// ("SHA256:..."). Empty on a first connection — trust on first use, with
	// the fingerprint shown so the operator can verify it out of band.
	KnownFingerprint string
}

// SetupStep is one thing the setup routine did, skipped or found on the host.
// It is shown to the operator verbatim, so keep the wording plain.
type SetupStep struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | skipped | warn | error
	Detail string `json:"detail"`
}

// SetupResult is what one-click setup accomplished. PrivateKey is handed to the
// browser exactly once and is never persisted by the control plane.
type SetupResult struct {
	Steps       []SetupStep       `json:"steps"`
	Info        map[string]string `json:"info"` // sshd settings observed, never changed
	PublicKey   string            `json:"public_key"`
	PrivateKey  string            `json:"private_key"`
	Fingerprint string            `json:"fingerprint"`
	Verified    bool              `json:"verified"` // a fresh login with the new key worked
	Warnings    []string          `json:"warnings"`
}

// Terminal is a live interactive shell on a remote host.
type Terminal interface {
	// Read returns the remote terminal's output (stdout and stderr merged by
	// the PTY, exactly as a real terminal would see it).
	Read(p []byte) (int, error)
	// Write sends keystrokes to the remote shell.
	Write(p []byte) (int, error)
	// Resize tells the remote PTY the window changed.
	Resize(cols, rows int) error
	// Close ends the shell and the underlying connection.
	Close() error
}

// Session is one open console. It is owned by exactly one user in one org.
type Session struct {
	ID          string    `json:"session_id"`
	OrgID       string    `json:"-"`
	UserID      string    `json:"-"`
	ServerID    string    `json:"server_id"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	User        string    `json:"username"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`

	mu       sync.Mutex
	term     Terminal
	attached bool
	closed   bool
}

// Read pulls remote output. It is called from a single pump goroutine.
func (s *Session) Read(p []byte) (int, error) {
	s.mu.Lock()
	t, closed := s.term, s.closed
	s.mu.Unlock()
	if closed || t == nil {
		return 0, ErrSessionClosed
	}
	return t.Read(p)
}

// Write forwards keystrokes to the remote shell.
func (s *Session) Write(p []byte) (int, error) {
	s.mu.Lock()
	t, closed := s.term, s.closed
	s.mu.Unlock()
	if closed || t == nil {
		return 0, ErrSessionClosed
	}
	return t.Write(p)
}

// Resize forwards a window-size change.
func (s *Session) Resize(cols, rows int) error {
	s.mu.Lock()
	t, closed := s.term, s.closed
	s.mu.Unlock()
	if closed || t == nil {
		return ErrSessionClosed
	}
	return t.Resize(cols, rows)
}

func (s *Session) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	t := s.term
	s.term = nil
	s.mu.Unlock()
	if t != nil {
		_ = t.Close()
	}
}

// Manager owns the live sessions and enforces the limits above.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	logger   *slog.Logger
	enabled  bool
	stop     chan struct{}
}

// NewManager creates a Manager. `enabled` reflects PULSE_SSH_CONSOLE; the
// console additionally needs an SSH client compiled in (see Compiled).
func NewManager(logger *slog.Logger, enabled bool) *Manager {
	m := &Manager{
		sessions: map[string]*Session{},
		logger:   logger,
		enabled:  enabled,
		stop:     make(chan struct{}),
	}
	go m.reap()
	return m
}

// Enabled reports whether a console session can be opened at all.
func (m *Manager) Enabled() bool { return m.enabled && Compiled }

// Unavailable explains, in one sentence a user can act on, why the console is
// not available. It returns nil when the console is usable.
func (m *Manager) Unavailable() error {
	if !Compiled {
		return ErrUnsupported
	}
	if !m.enabled {
		return ErrDisabled
	}
	return nil
}

// Open dials the host and registers the resulting session. On any failure it
// returns an error safe to show the user plus the host fingerprint we saw (so
// a mismatch can be reported precisely).
func (m *Manager) Open(ctx context.Context, orgID, userID, serverID string, c Credentials, cols, rows int) (*Session, string, error) {
	if err := m.Unavailable(); err != nil {
		return nil, "", err
	}
	if err := m.checkQuota(userID); err != nil {
		return nil, "", err
	}

	term, fingerprint, err := Dial(ctx, c, cols, rows)
	if err != nil {
		return nil, fingerprint, err
	}

	id, err := newID()
	if err != nil {
		_ = term.Close()
		return nil, fingerprint, err
	}
	s := &Session{
		ID:          id,
		OrgID:       orgID,
		UserID:      userID,
		ServerID:    serverID,
		Host:        c.Host,
		Port:        c.Port,
		User:        c.User,
		Fingerprint: fingerprint,
		CreatedAt:   time.Now().UTC(),
		term:        term,
	}

	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	return s, fingerprint, nil
}

// Setup installs a Pulse-generated key so later logins need no password. It
// changes exactly one thing on the host — an entry in the user's
// authorized_keys — and only reports (never edits) sshd's own configuration:
// rewriting sshd_config remotely is how people lock themselves out.
func (m *Manager) Setup(ctx context.Context, c Credentials, label string) (*SetupResult, error) {
	if err := m.Unavailable(); err != nil {
		return nil, err
	}
	return InstallKey(ctx, c, label)
}

// Get returns a session owned by this user.
func (m *Manager) Get(orgID, userID, id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.OrgID != orgID || s.UserID != userID {
		return nil, false
	}
	return s, true
}

// Attach claims a session for one WebSocket. A session can only be attached
// once — a second tab gets ErrAlreadyAttached rather than a split terminal.
func (m *Manager) Attach(s *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSessionClosed
	}
	if s.attached {
		return ErrAlreadyAttached
	}
	s.attached = true
	return nil
}

// CloseSession ends a session and forgets it.
func (m *Manager) CloseSession(s *Session) {
	if s == nil {
		return
	}
	m.mu.Lock()
	delete(m.sessions, s.ID)
	m.mu.Unlock()
	s.close()
}

// Shutdown ends every session (called when the API stops).
func (m *Manager) Shutdown() {
	close(m.stop)
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.sessions = map[string]*Session{}
	m.mu.Unlock()
	for _, s := range all {
		s.close()
	}
}

func (m *Manager) checkQuota(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sessions) >= maxSessionsTotal {
		return ErrTooMany
	}
	n := 0
	for _, s := range m.sessions {
		if s.UserID == userID {
			n++
		}
	}
	if n >= maxSessionsPerUser {
		return ErrTooMany
	}
	return nil
}

// reap closes sessions nobody ever attached to (a dialled connection whose tab
// was closed before the WebSocket opened) and sessions past the hard ceiling.
func (m *Manager) reap() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case now := <-t.C:
			var dead []*Session
			m.mu.Lock()
			for id, s := range m.sessions {
				s.mu.Lock()
				stale := (!s.attached && now.Sub(s.CreatedAt) > attachWindow) ||
					now.Sub(s.CreatedAt) > idleLifetime ||
					s.closed
				s.mu.Unlock()
				if stale {
					delete(m.sessions, id)
					dead = append(dead, s)
				}
			}
			m.mu.Unlock()
			for _, s := range dead {
				s.close()
			}
			if len(dead) > 0 && m.logger != nil {
				m.logger.Info("ssh console: reaped idle sessions", "count", len(dead))
			}
		}
	}
}

func newID() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ssh_" + hex.EncodeToString(b), nil
}
