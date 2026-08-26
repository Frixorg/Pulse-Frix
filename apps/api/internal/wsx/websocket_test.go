package wsx

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// memConn is a net.Conn whose writes land in a buffer and whose reads are
// empty. Framing is a pure function of bytes, so this is all the tests need.
type memConn struct{ out bytes.Buffer }

func (m *memConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (m *memConn) Write(b []byte) (int, error)      { return m.out.Write(b) }
func (m *memConn) Close() error                     { return nil }
func (m *memConn) LocalAddr() net.Addr              { return memAddr{} }
func (m *memConn) RemoteAddr() net.Addr             { return memAddr{} }
func (m *memConn) SetDeadline(time.Time) error      { return nil }
func (m *memConn) SetReadDeadline(time.Time) error  { return nil }
func (m *memConn) SetWriteDeadline(time.Time) error { return nil }

type memAddr struct{}

func (memAddr) Network() string { return "mem" }
func (memAddr) String() string  { return "mem" }

func newConn(inbound []byte) (*Conn, *memConn) {
	mc := &memConn{}
	return &Conn{conn: mc, br: bufio.NewReader(bytes.NewReader(inbound))}, mc
}

// clientFrame builds a masked client-to-server frame, as a browser would.
func clientFrame(fin bool, opcode byte, payload []byte) []byte {
	var buf bytes.Buffer
	b0 := opcode
	if fin {
		b0 |= 0x80
	}
	buf.WriteByte(b0)
	n := len(payload)
	switch {
	case n <= 125:
		buf.WriteByte(0x80 | byte(n))
	case n <= 0xFFFF:
		buf.WriteByte(0x80 | 126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(n))
		buf.Write(ext[:])
	default:
		buf.WriteByte(0x80 | 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		buf.Write(ext[:])
	}
	mask := [4]byte{0x37, 0xFA, 0x21, 0x3D}
	buf.Write(mask[:])
	masked := make([]byte, n)
	for i := range payload {
		masked[i] = payload[i] ^ mask[i&3]
	}
	buf.Write(masked)
	return buf.Bytes()
}

func TestReadMessageUnmasksPayload(t *testing.T) {
	c, _ := newConn(clientFrame(true, OpText, []byte("ls -la")))
	op, data, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if op != OpText {
		t.Fatalf("opcode = %d, want OpText", op)
	}
	if string(data) != "ls -la" {
		t.Fatalf("payload = %q, want %q", data, "ls -la")
	}
}

func TestReadMessageReassemblesFragments(t *testing.T) {
	var in []byte
	in = append(in, clientFrame(false, OpBinary, []byte("abc"))...)
	in = append(in, clientFrame(false, OpContinuation, []byte("def"))...)
	in = append(in, clientFrame(true, OpContinuation, []byte("ghi"))...)
	c, _ := newConn(in)

	op, data, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if op != OpBinary || string(data) != "abcdefghi" {
		t.Fatalf("got op=%d data=%q, want OpBinary %q", op, data, "abcdefghi")
	}
}

func TestReadMessageAnswersPing(t *testing.T) {
	var in []byte
	in = append(in, clientFrame(true, OpPing, []byte("hi"))...)
	in = append(in, clientFrame(true, OpText, []byte("ok"))...)
	c, mc := newConn(in)

	_, data, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if string(data) != "ok" {
		t.Fatalf("ping leaked into the data stream: %q", data)
	}
	out := mc.out.Bytes()
	if len(out) < 2 || out[0] != 0x80|OpPong {
		t.Fatalf("no pong written, got % x", out)
	}
	if string(out[2:]) != "hi" {
		t.Fatalf("pong payload = %q, want %q", out[2:], "hi")
	}
}

func TestReadMessageRejectsUnmaskedClientFrame(t *testing.T) {
	// A well-formed frame except for the mask bit: RFC 6455 requires clients
	// to mask, and accepting unmasked frames enables cache-poisoning attacks.
	in := []byte{0x80 | OpText, 0x02, 'h', 'i'}
	c, _ := newConn(in)
	if _, _, err := c.ReadMessage(); !errors.Is(err, ErrProtocol) {
		t.Fatalf("err = %v, want ErrProtocol", err)
	}
}

func TestReadMessageRejectsOversizedFrame(t *testing.T) {
	in := []byte{0x80 | OpBinary, 0x80 | 127}
	var ext [8]byte
	binary.BigEndian.PutUint64(ext[:], MaxMessage+1)
	in = append(in, ext[:]...)
	c, _ := newConn(in)
	if _, _, err := c.ReadMessage(); !errors.Is(err, ErrMessageSize) {
		t.Fatalf("err = %v, want ErrMessageSize", err)
	}
}

func TestReadMessageRejectsFragmentedControlFrame(t *testing.T) {
	c, _ := newConn(clientFrame(false, OpPing, []byte("x")))
	if _, _, err := c.ReadMessage(); !errors.Is(err, ErrProtocol) {
		t.Fatalf("err = %v, want ErrProtocol", err)
	}
}

func TestWriteFrameHeaders(t *testing.T) {
	cases := []struct {
		name    string
		size    int
		wantLen byte
		hdrLen  int
	}{
		{"short", 5, 5, 2},
		{"extended16", 300, 126, 4},
		{"extended64", 70000, 127, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, mc := newConn(nil)
			if err := c.WriteBinary(bytes.Repeat([]byte{'z'}, tc.size)); err != nil {
				t.Fatalf("WriteBinary: %v", err)
			}
			out := mc.out.Bytes()
			if len(out) != tc.hdrLen+tc.size {
				t.Fatalf("wrote %d bytes, want %d", len(out), tc.hdrLen+tc.size)
			}
			if out[0] != 0x80|OpBinary {
				t.Fatalf("first byte = %#x, want FIN+binary", out[0])
			}
			if out[1] != tc.wantLen {
				t.Fatalf("length byte = %d, want %d", out[1], tc.wantLen)
			}
			// Server frames must never set the mask bit.
			if out[1]&0x80 != 0 {
				t.Fatal("server frame is masked")
			}
		})
	}
}

func TestIsUpgrade(t *testing.T) {
	valid := httptest.NewRequest(http.MethodGet, "/attach", nil)
	valid.Header.Set("Upgrade", "websocket")
	valid.Header.Set("Connection", "keep-alive, Upgrade")
	valid.Header.Set("Sec-WebSocket-Version", "13")
	valid.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	if !IsUpgrade(valid) {
		t.Fatal("a valid upgrade request was rejected")
	}

	plain := httptest.NewRequest(http.MethodGet, "/attach", nil)
	if IsUpgrade(plain) {
		t.Fatal("a plain GET was treated as an upgrade")
	}

	wrongVersion := valid.Clone(valid.Context())
	wrongVersion.Header.Set("Sec-WebSocket-Version", "8")
	if IsUpgrade(wrongVersion) {
		t.Fatal("an unsupported websocket version was accepted")
	}
}
