// Package wsx implements the server half of RFC 6455 (WebSocket) — just the
// parts Pulse needs to stream an interactive terminal.
//
// It is deliberately small and dependency-free: the control plane's default
// build uses only the Go standard library (see go.mod), so pulling in a
// general-purpose WebSocket library would be a supply-chain cost we do not
// need. Scope: a server-side connection, unfragmented writes, transparent
// ping/pong and close handling, and a hard cap on inbound message size.
package wsx

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// magicGUID is the constant every RFC 6455 handshake mixes into the key.
const magicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// Frame opcodes.
const (
	OpContinuation byte = 0x0
	OpText         byte = 0x1
	OpBinary       byte = 0x2
	OpClose        byte = 0x8
	OpPing         byte = 0x9
	OpPong         byte = 0xA
)

// Close status codes (subset).
const (
	CloseNormal      uint16 = 1000
	CloseGoingAway   uint16 = 1001
	CloseProtocolErr uint16 = 1002
	CloseTooLarge    uint16 = 1009
	CloseInternal    uint16 = 1011
)

// MaxMessage bounds a single inbound message. Terminal keystrokes and resize
// notices are tiny; anything larger is a client bug or an attack.
const MaxMessage = 1 << 20 // 1 MiB

// Errors returned by this package.
var (
	ErrClosed      = errors.New("wsx: connection closed")
	ErrNotUpgrade  = errors.New("wsx: not a websocket upgrade request")
	ErrNoHijack    = errors.New("wsx: response writer does not support hijacking")
	ErrProtocol    = errors.New("wsx: protocol error")
	ErrMessageSize = errors.New("wsx: message too large")
)

// Conn is a server-side WebSocket connection. Write methods are safe for
// concurrent use; ReadMessage must be called from a single goroutine.
type Conn struct {
	conn net.Conn
	br   *bufio.Reader

	wmu       sync.Mutex
	closed    atomic.Bool
	closeOnce sync.Once
}

// IsUpgrade reports whether r is a well-formed WebSocket upgrade request.
// Callers should check this before Accept so they can still write a normal
// HTTP error response — after Accept hijacks the connection, they cannot.
func IsUpgrade(r *http.Request) bool {
	return r.Method == http.MethodGet &&
		strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		headerHasToken(r.Header, "Connection", "upgrade") &&
		r.Header.Get("Sec-WebSocket-Version") == "13" &&
		r.Header.Get("Sec-WebSocket-Key") != ""
}

// Accept completes the handshake and takes ownership of the connection. Once
// it returns successfully the ResponseWriter must not be used again.
func Accept(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !IsUpgrade(r) {
		return nil, ErrNotUpgrade
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, ErrNoHijack
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	// http.Server applied ReadTimeout/WriteTimeout deadlines before the handler
	// ran. A terminal session outlives them, so clear them here.
	_ = conn.SetDeadline(time.Time{})

	sum := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + magicGUID))
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n"
	if _, err := conn.Write([]byte(resp)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	// brw.Reader may already hold bytes the http server buffered — keep using it.
	return &Conn{conn: conn, br: brw.Reader}, nil
}

// RemoteAddr returns the peer address.
func (c *Conn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

// SetReadDeadline bounds how long the next ReadMessage may block.
func (c *Conn) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }

// ReadMessage returns the next complete data message and its opcode (OpText or
// OpBinary). Ping frames are answered transparently; a close frame surfaces as
// ErrClosed.
func (c *Conn) ReadMessage() (byte, []byte, error) {
	var (
		buf   []byte
		msgOp byte
	)
	for {
		fin, op, data, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch op {
		case OpPing:
			if err := c.writeFrame(OpPong, data); err != nil {
				return 0, nil, err
			}
			continue
		case OpPong:
			continue
		case OpClose:
			_ = c.writeFrame(OpClose, data)
			_ = c.Close()
			return 0, nil, ErrClosed
		case OpText, OpBinary:
			if msgOp != 0 {
				return 0, nil, ErrProtocol // a new data frame inside a fragmented message
			}
			msgOp = op
			buf = append(buf, data...)
		case OpContinuation:
			if msgOp == 0 {
				return 0, nil, ErrProtocol
			}
			buf = append(buf, data...)
		default:
			return 0, nil, ErrProtocol
		}
		if len(buf) > MaxMessage {
			return 0, nil, ErrMessageSize
		}
		if fin {
			return msgOp, buf, nil
		}
	}
}

// WriteText sends one text frame.
func (c *Conn) WriteText(b []byte) error { return c.writeFrame(OpText, b) }

// WriteBinary sends one binary frame.
func (c *Conn) WriteBinary(b []byte) error { return c.writeFrame(OpBinary, b) }

// WritePing sends a ping frame (an idle keepalive for intermediate proxies).
func (c *Conn) WritePing() error { return c.writeFrame(OpPing, nil) }

// CloseWith sends a close frame with a status code, then closes the socket.
func (c *Conn) CloseWith(code uint16, reason string) {
	if c.closed.Load() {
		return
	}
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload, code)
	copy(payload[2:], reason)
	_ = c.writeFrame(OpClose, payload)
	_ = c.Close()
}

// Close tears down the underlying connection. It is safe to call repeatedly.
func (c *Conn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		err = c.conn.Close()
	})
	return err
}

// --- framing ---

func (c *Conn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	var hdr [2]byte
	if _, err = io.ReadFull(c.br, hdr[:]); err != nil {
		return false, 0, nil, err
	}
	fin = hdr[0]&0x80 != 0
	if hdr[0]&0x70 != 0 { // RSV1-3 must be zero: no extensions were negotiated
		return false, 0, nil, ErrProtocol
	}
	opcode = hdr[0] & 0x0F
	masked := hdr[1]&0x80 != 0
	n := uint64(hdr[1] & 0x7F)
	switch n {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return false, 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return false, 0, nil, err
		}
		n = binary.BigEndian.Uint64(ext[:])
	}
	if opcode >= OpClose && (n > 125 || !fin) {
		return false, 0, nil, ErrProtocol // control frames are short and never fragmented
	}
	if n > MaxMessage {
		return false, 0, nil, ErrMessageSize
	}
	// RFC 6455 section 5.1: every client-to-server frame must be masked.
	if !masked {
		return false, 0, nil, ErrProtocol
	}
	var mask [4]byte
	if _, err = io.ReadFull(c.br, mask[:]); err != nil {
		return false, 0, nil, err
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return false, 0, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i&3]
	}
	return fin, opcode, payload, nil
}

func (c *Conn) writeFrame(opcode byte, payload []byte) error {
	if c.closed.Load() {
		return ErrClosed
	}
	n := len(payload)
	var head int
	switch {
	case n <= 125:
		head = 2
	case n <= 0xFFFF:
		head = 4
	default:
		head = 10
	}
	// One buffer, one Write: a partial frame would desynchronise the peer.
	frame := make([]byte, head+n)
	frame[0] = 0x80 | opcode // FIN set; server frames are never masked
	switch head {
	case 2:
		frame[1] = byte(n)
	case 4:
		frame[1] = 126
		binary.BigEndian.PutUint16(frame[2:4], uint16(n))
	default:
		frame[1] = 127
		binary.BigEndian.PutUint64(frame[2:10], uint64(n))
	}
	copy(frame[head:], payload)

	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed.Load() {
		return ErrClosed
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
	_, err := c.conn.Write(frame)
	_ = c.conn.SetWriteDeadline(time.Time{})
	return err
}

func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}
