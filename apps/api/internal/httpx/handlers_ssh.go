package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/frix-me/pulse/api/internal/rbac"
	"github.com/frix-me/pulse/api/internal/sshx"
	"github.com/frix-me/pulse/api/internal/wsx"
)

// The SSH console is the one Pulse feature that can change a server, so every
// entry point here is explicit about it: it is disabled unless the operator
// turns it on, it needs the ssh.exec permission, and each connection is
// audited (host, port and username — never the credentials).

// sshOpenRequest is the body of POST /servers/{id}/ssh/sessions.
type sshOpenRequest struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthMethod string `json:"auth_method"` // "password" | "key"
	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase"`
	// KnownFingerprint is what this browser trusted last time for host:port.
	KnownFingerprint string `json:"known_fingerprint"`
	Cols             int    `json:"cols"`
	Rows             int    `json:"rows"`
}

type sshOpenResponse struct {
	SessionID       string `json:"session_id"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username"`
	Fingerprint     string `json:"fingerprint"`
	FirstConnection bool   `json:"first_connection"`
	AttachWithinSec int    `json:"attach_within_sec"`
}

// handleSSHCapabilities tells the dashboard whether to offer a terminal at all,
// and if not, exactly what the operator has to do about it.
func (s *Server) handleSSHCapabilities(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	reason := ""
	if err := s.ssh.Unavailable(); err != nil {
		switch {
		case errors.Is(err, sshx.ErrUnsupported):
			reason = "This API build has no SSH client compiled in. Rebuild it with -tags ssh " +
				"(Docker: --build-arg TAGS=ssh) — see docs/SSH_CONSOLE.md."
		default:
			reason = "The SSH console is turned off. Set PULSE_SSH_CONSOLE=true on the API and restart it."
		}
	}
	JSON(w, http.StatusOK, map[string]any{
		"enabled":      s.ssh.Enabled(),
		"reason":       reason,
		"default_port": 22,
		// Viewers never get a shell, however the deployment is configured.
		"can_use": rbac.Can(p.Role, rbac.SSHExec),
	})
}

// handleSSHOpen dials the host and parks the live session for the WebSocket to
// pick up. Credentials live only for the duration of this request.
func (s *Server) handleSSHOpen(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	if err := s.ssh.Unavailable(); err != nil {
		Fail(w, r, http.StatusNotImplemented, CodeConfig, err.Error())
		return
	}
	srv, err := s.store.GetServer(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "server not found")
		return
	}

	var req sshOpenRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "invalid request body")
		return
	}
	creds, verr := credentialsFrom(&req)
	if verr != "" {
		Fail(w, r, http.StatusBadRequest, CodeValidation, verr)
		return
	}
	host, user, port := creds.Host, creds.User, creds.Port

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	sess, fingerprint, err := s.ssh.Open(ctx, p.OrgID, p.UserID, srv.ServerID, creds, req.Cols, req.Rows)
	// Wipe the secrets from our copy as soon as the dial is done.
	creds.Password, creds.PrivateKey, creds.Passphrase = "", "", ""
	if err != nil {
		s.audit.Record(p.OrgID, p.Email, "ssh.connect", "failure", clientIP(r), map[string]any{
			"server": srv.ID, "host": host, "port": port, "username": user, "error": err.Error(),
		})
		status, code := sshFailure(err)
		if errors.Is(err, sshx.ErrHostKeyMismatch) {
			// Give the UI the fingerprint we actually saw so it can show the
			// operator both values instead of a bare "mismatch".
			JSON(w, status, map[string]any{"error": map[string]any{
				"code": code, "message": err.Error(), "request_id": RequestID(r.Context()),
				"fingerprint": fingerprint,
			}})
			return
		}
		Fail(w, r, status, code, err.Error())
		return
	}

	s.audit.Record(p.OrgID, p.Email, "ssh.connect", "success", clientIP(r), map[string]any{
		"server": srv.ID, "host": host, "port": port, "username": user,
		"session": sess.ID, "fingerprint": fingerprint,
	})
	JSON(w, http.StatusCreated, sshOpenResponse{
		SessionID:       sess.ID,
		Host:            sess.Host,
		Port:            sess.Port,
		Username:        sess.User,
		Fingerprint:     fingerprint,
		FirstConnection: req.KnownFingerprint == "",
		AttachWithinSec: 60,
	})
}

// handleSSHSetup is the "Set up SSH on my VPS" button. Using one login the
// operator supplies, it authorises a freshly generated key on the host so every
// later console session needs no password.
//
// It touches exactly one file — the user's own authorized_keys — and never
// edits sshd_config: a remote sshd rewrite is how people lock themselves out.
// The generated private key is returned to the browser once and is not stored
// by the control plane.
func (s *Server) handleSSHSetup(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	if err := s.ssh.Unavailable(); err != nil {
		Fail(w, r, http.StatusNotImplemented, CodeConfig, err.Error())
		return
	}
	srv, err := s.store.GetServer(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "server not found")
		return
	}

	var req sshOpenRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "invalid request body")
		return
	}
	creds, verr := credentialsFrom(&req)
	if verr != "" {
		Fail(w, r, http.StatusBadRequest, CodeValidation, verr)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	result, err := s.ssh.Setup(ctx, creds, srv.Hostname)
	creds.Password, creds.PrivateKey, creds.Passphrase = "", "", ""
	if err != nil {
		s.audit.Record(p.OrgID, p.Email, "ssh.setup", "failure", clientIP(r), map[string]any{
			"server": srv.ID, "host": creds.Host, "port": creds.Port, "username": creds.User,
			"error": err.Error(),
		})
		status, code := sshFailure(err)
		if result != nil {
			// Partial progress is still worth showing: the operator can see how
			// far setup got before it stopped.
			JSON(w, status, map[string]any{"error": map[string]any{
				"code": code, "message": err.Error(), "request_id": RequestID(r.Context()),
				"steps": result.Steps, "info": result.Info,
			}})
			return
		}
		Fail(w, r, status, code, err.Error())
		return
	}

	s.audit.Record(p.OrgID, p.Email, "ssh.setup", "success", clientIP(r), map[string]any{
		"server": srv.ID, "host": creds.Host, "port": creds.Port, "username": creds.User,
		"public_key": result.PublicKey, "verified": result.Verified,
	})
	JSON(w, http.StatusOK, result)
}

// credentialsFrom validates a request body and builds Credentials. It returns a
// non-empty string describing the problem when the body is unusable.
func credentialsFrom(req *sshOpenRequest) (sshx.Credentials, string) {
	host := strings.TrimSpace(req.Host)
	user := strings.TrimSpace(req.Username)
	switch {
	case host == "":
		return sshx.Credentials{}, "a host or IP address is required"
	case strings.Contains(host, "/") || strings.Contains(host, " "):
		return sshx.Credentials{}, "host must be a hostname or IP address, not a URL"
	case user == "":
		return sshx.Credentials{}, "a username is required"
	}
	port := req.Port
	if port == 0 {
		port = 22
	}
	if port < 1 || port > 65535 {
		return sshx.Credentials{}, "port must be between 1 and 65535"
	}

	creds := sshx.Credentials{
		Host:             host,
		Port:             port,
		User:             user,
		KnownFingerprint: strings.TrimSpace(req.KnownFingerprint),
	}
	if req.AuthMethod == "key" {
		creds.PrivateKey = req.PrivateKey
		creds.Passphrase = req.Passphrase
	} else {
		creds.Password = req.Password
	}
	return creds, ""
}

// handleSSHAttach upgrades to a WebSocket and pumps bytes both ways.
//
// Wire protocol (deliberately tiny):
//
//	browser -> API   binary frame            keystrokes, verbatim
//	browser -> API   text frame {"type":"resize","cols":N,"rows":M}
//	API -> browser   binary frame            terminal output, verbatim
//	API -> browser   text frame {"type":"status"|"exit","message":"..."}
func (s *Server) handleSSHAttach(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	sess, ok := s.ssh.Get(p.OrgID, p.UserID, r.PathValue("sid"))
	if !ok {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "ssh session not found or already closed")
		return
	}
	if !wsx.IsUpgrade(r) {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "this endpoint expects a websocket upgrade")
		return
	}
	if err := s.ssh.Attach(sess); err != nil {
		Fail(w, r, http.StatusConflict, CodeValidation, err.Error())
		return
	}

	conn, err := wsx.Accept(w, r)
	if err != nil {
		// The handshake failed; there is no usable connection left to answer on.
		s.logger.Warn("ssh console: websocket upgrade failed", "error", err, "request_id", RequestID(r.Context()))
		s.ssh.CloseSession(sess)
		return
	}

	_ = conn.WriteText(mustJSON(map[string]any{
		"type": "status", "state": "connected",
		"host": sess.Host, "port": sess.Port, "username": sess.User,
		"fingerprint": sess.Fingerprint,
	}))

	done := make(chan struct{})

	// Remote -> browser.
	go func() {
		defer close(done)
		buf := make([]byte, 32<<10)
		for {
			n, rerr := sess.Read(buf)
			if n > 0 {
				if werr := conn.WriteBinary(buf[:n]); werr != nil {
					return
				}
			}
			if rerr != nil {
				_ = conn.WriteText(mustJSON(map[string]any{
					"type": "exit", "message": "session ended",
				}))
				conn.CloseWith(wsx.CloseNormal, "session ended")
				return
			}
		}
	}()

	// Keepalive: idle terminals are normal, but proxies in between drop quiet
	// connections. A ping every 25s keeps the tunnel open.
	go func() {
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if err := conn.WritePing(); err != nil {
					return
				}
			}
		}
	}()

	// Browser -> remote (this goroutine owns the read side).
readLoop:
	for {
		op, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		switch op {
		case wsx.OpBinary:
			if _, err := sess.Write(data); err != nil {
				conn.CloseWith(wsx.CloseNormal, "session ended")
				break readLoop
			}
		case wsx.OpText:
			var msg struct {
				Type string `json:"type"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
				Data string `json:"data"`
			}
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "resize":
				_ = sess.Resize(msg.Cols, msg.Rows)
			case "data": // fallback for clients that cannot send binary frames
				if msg.Data != "" {
					_, _ = sess.Write([]byte(msg.Data))
				}
			}
		}
	}

	// Close the terminal first: the remote->browser pump is blocked in
	// sess.Read() and only unblocks once the SSH session goes away. Waiting on
	// `done` before that would hang this handler on an idle shell.
	s.ssh.CloseSession(sess)
	_ = conn.Close()
	<-done
	s.audit.Record(p.OrgID, p.Email, "ssh.disconnect", "success", clientIP(r), map[string]any{
		"session": sess.ID, "host": sess.Host, "port": sess.Port, "username": sess.User,
		"duration_sec": int(time.Since(sess.CreatedAt).Seconds()),
	})
}

// handleSSHClose ends a session from the UI ("Disconnect").
func (s *Server) handleSSHClose(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	sess, ok := s.ssh.Get(p.OrgID, p.UserID, r.PathValue("sid"))
	if !ok {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "ssh session not found")
		return
	}
	s.ssh.CloseSession(sess)
	JSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

// sshFailure maps a dial error onto an HTTP status the UI can branch on.
func sshFailure(err error) (int, string) {
	switch {
	case errors.Is(err, sshx.ErrUnsupported), errors.Is(err, sshx.ErrDisabled):
		return http.StatusNotImplemented, CodeConfig
	case errors.Is(err, sshx.ErrHostKeyMismatch):
		return http.StatusConflict, "SSH_HOST_KEY_MISMATCH"
	case errors.Is(err, sshx.ErrAuth), errors.Is(err, sshx.ErrNoAuth), errors.Is(err, sshx.ErrBadKey):
		return http.StatusUnauthorized, "SSH_AUTH_FAILED"
	case errors.Is(err, sshx.ErrTooMany):
		return http.StatusTooManyRequests, CodeRateLimited
	default:
		// Everything else is "we could not get a shell on that host": DNS,
		// refused connection, timeout, no PTY.
		return http.StatusBadGateway, "SSH_UNREACHABLE"
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"type":"error","message":"encoding failed"}`)
	}
	return b
}
