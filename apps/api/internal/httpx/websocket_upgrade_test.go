package httpx

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/frix-me/pulse/api/internal/wsx"
)

// The SSH console upgrades to a WebSocket, which needs http.Hijacker to survive
// the middleware chain. The logging middleware wraps every ResponseWriter, so
// this test guards the wrapper's Hijack passthrough — without it the console
// fails at the handshake and nothing else in the suite would notice.
func TestWebSocketUpgradeSurvivesMiddleware(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /attach", func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsx.Accept(w, r)
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		defer conn.Close()
		op, data, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("ReadMessage: %v", err)
			return
		}
		if op != wsx.OpBinary {
			t.Errorf("opcode = %d, want OpBinary", op)
		}
		// Echo it back so the client can prove the full round trip works.
		if err := conn.WriteBinary(data); err != nil {
			t.Errorf("WriteBinary: %v", err)
		}
	})

	handler := Chain(mux,
		WithRequestID,
		Recover(slog.New(slog.NewTextHandler(discard{}, nil))),
		Logger(slog.New(slog.NewTextHandler(discard{}, nil))),
		SecurityHeaders,
	)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	const key = "dGhlIHNhbXBsZSBub25jZQ=="
	req := "GET /attach HTTP/1.1\r\n" +
		"Host: " + strings.TrimPrefix(ts.URL, "http://") + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read handshake response: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101 (the upgrade was swallowed)", resp.StatusCode)
	}
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if got, want := resp.Header.Get("Sec-WebSocket-Accept"), base64.StdEncoding.EncodeToString(sum[:]); got != want {
		t.Fatalf("Sec-WebSocket-Accept = %q, want %q", got, want)
	}

	// A masked client frame carrying "whoami", the way a browser sends keystrokes.
	payload := []byte("whoami")
	mask := [4]byte{0x11, 0x22, 0x33, 0x44}
	frame := []byte{0x80 | byte(wsx.OpBinary), 0x80 | byte(len(payload))}
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i&3])
	}
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	hdr := make([]byte, 2)
	if _, err := readFull(br, hdr); err != nil {
		t.Fatalf("read echo header: %v", err)
	}
	if hdr[0] != 0x80|byte(wsx.OpBinary) {
		t.Fatalf("echo opcode byte = %#x", hdr[0])
	}
	if hdr[1]&0x80 != 0 {
		t.Fatal("server frame is masked; it must not be")
	}
	echo := make([]byte, int(hdr[1]&0x7F))
	if _, err := readFull(br, echo); err != nil {
		t.Fatalf("read echo payload: %v", err)
	}
	if string(echo) != "whoami" {
		t.Fatalf("echo = %q, want %q", echo, "whoami")
	}
}

func readFull(r *bufio.Reader, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := r.Read(b[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
