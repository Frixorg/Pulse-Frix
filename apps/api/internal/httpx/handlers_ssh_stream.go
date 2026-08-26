package httpx

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// A WebSocket-free transport for the SSH console.
//
// Plenty of deployments sit behind a reverse proxy whose Content-Security-Policy
// looks permissive but has no `connect-src`, e.g.
//
//	default-src https: data: blob: 'unsafe-inline' 'unsafe-eval'
//
// The `https:` scheme source does not match `wss:`, so the browser refuses the
// WebSocket before it ever leaves the machine — and every CSP header on a
// response is enforced, so Pulse cannot override one set upstream. Asking every
// operator to edit their proxy is not a fix we can ship.
//
// Server-sent events and ordinary POSTs are plain https, so they pass that same
// policy. The dashboard uses the WebSocket when it can and drops to this when it
// cannot; both speak to the same live session.
//
//	GET  …/stream   text/event-stream, terminal output as base64 `data` events
//	POST …/input    {"type":"data","data":"<base64>"} or {"type":"resize",…}

// sseHeartbeat keeps intermediate proxies from closing an idle stream. Their
// read timeouts are commonly 60s.
const sseHeartbeat = 20 * time.Second

// handleSSHStream streams terminal output over SSE. Like the WebSocket handler
// it claims the session, so only one transport can be attached at a time.
func (s *Server) handleSSHStream(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	sess, ok := s.ssh.Get(p.OrgID, p.UserID, r.PathValue("sid"))
	if !ok {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "ssh session not found or already closed")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "streaming is not supported here")
		return
	}
	if err := s.ssh.Attach(sess); err != nil {
		Fail(w, r, http.StatusConflict, CodeValidation, err.Error())
		return
	}
	// Recorded however the stream ends: the shell exited, the browser navigated
	// away, or the connection dropped.
	defer func() {
		s.ssh.CloseSession(sess)
		s.audit.Record(p.OrgID, p.Email, "ssh.disconnect", "success", clientIP(r), map[string]any{
			"session": sess.ID, "host": sess.Host, "port": sess.Port, "username": sess.User,
			"transport": "http", "duration_sec": int(time.Since(sess.CreatedAt).Seconds()),
		})
	}()

	// A terminal outlives the server's per-request write deadline.
	if rc := http.NewResponseController(w); rc != nil {
		_ = rc.SetWriteDeadline(time.Time{})
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-store, no-transform")
	h.Set("Connection", "keep-alive")
	// nginx buffers proxied responses by default, which would hold keystrokes
	// back until the buffer fills. This header switches that off per-response,
	// including on proxies Pulse does not control.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	writeEvent(w, flusher, "status", mustJSON(map[string]any{
		"state": "connected", "host": sess.Host, "port": sess.Port,
		"username": sess.User, "fingerprint": sess.Fingerprint, "transport": "http",
	}))

	ctx := r.Context()
	chunks := make(chan []byte, 16)

	// One reader goroutine; this handler is the only writer to w.
	go func() {
		defer close(chunks)
		buf := make([]byte, 32<<10)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				select {
				case chunks <- chunk:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(sseHeartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done(): // the browser navigated away or closed the stream
			return
		case chunk, open := <-chunks:
			if !open {
				writeEvent(w, flusher, "exit", []byte(`{"message":"session ended"}`))
				return
			}
			// Terminal output is arbitrary bytes; SSE is a line protocol.
			writeEvent(w, flusher, "data", []byte(base64.StdEncoding.EncodeToString(chunk)))
		case <-ticker.C:
			// A comment line: valid SSE, ignored by the client, keeps proxies awake.
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleSSHInput carries keystrokes and window sizes the other way.
func (s *Server) handleSSHInput(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	sess, ok := s.ssh.Get(p.OrgID, p.UserID, r.PathValue("sid"))
	if !ok {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "ssh session not found or already closed")
		return
	}

	var msg struct {
		Type string `json:"type"`
		Data string `json:"data"` // base64
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&msg); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "invalid request body")
		return
	}

	switch msg.Type {
	case "resize":
		if err := sess.Resize(msg.Cols, msg.Rows); err != nil {
			Fail(w, r, http.StatusGone, CodeValidation, "the ssh session has ended")
			return
		}
	default:
		raw, err := base64.StdEncoding.DecodeString(msg.Data)
		if err != nil {
			Fail(w, r, http.StatusBadRequest, CodeValidation, "data must be base64")
			return
		}
		if len(raw) > 0 {
			if _, err := sess.Write(raw); err != nil {
				Fail(w, r, http.StatusGone, CodeValidation, "the ssh session has ended")
				return
			}
		}
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writeEvent emits one SSE event. Payloads are single-line (base64 or compact
// JSON), so no `data:` continuation handling is needed.
func writeEvent(w http.ResponseWriter, f http.Flusher, event string, payload []byte) {
	if _, err := io.WriteString(w, "event: "+event+"\ndata: "); err != nil {
		return
	}
	if _, err := w.Write(payload); err != nil {
		return
	}
	if _, err := io.WriteString(w, "\n\n"); err != nil {
		return
	}
	f.Flush()
}
