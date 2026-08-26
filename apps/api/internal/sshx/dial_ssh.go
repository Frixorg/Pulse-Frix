//go:build ssh

package sshx

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// Compiled reports that this binary contains an SSH client.
const Compiled = true

const dialTimeout = 20 * time.Second

// sshTerminal adapts a golang.org/x/crypto/ssh shell session to Terminal.
type sshTerminal struct {
	client  *gossh.Client
	session *gossh.Session
	stdin   io.WriteCloser
	out     *io.PipeReader
	once    sync.Once
}

func (t *sshTerminal) Read(p []byte) (int, error)  { return t.out.Read(p) }
func (t *sshTerminal) Write(p []byte) (int, error) { return t.stdin.Write(p) }

// Resize forwards a window change. x/crypto takes (rows, cols).
func (t *sshTerminal) Resize(cols, rows int) error { return t.session.WindowChange(rows, cols) }

func (t *sshTerminal) Close() error {
	t.once.Do(func() {
		_ = t.stdin.Close()
		_ = t.session.Close()
		_ = t.client.Close()
		_ = t.out.Close()
	})
	return nil
}

// Dial opens an interactive shell with a PTY and returns it alongside the
// remote host key fingerprint ("SHA256:..."), which the caller shows the
// operator and the browser pins for subsequent connections.
func Dial(ctx context.Context, c Credentials, cols, rows int) (Terminal, string, error) {
	auths, err := authMethods(c)
	if err != nil {
		return nil, "", err
	}

	// The fingerprint is captured during the handshake so it can be reported
	// even when the handshake itself fails.
	var (
		fingerprint string
		mismatch    bool
	)
	hostKey := func(_ string, _ net.Addr, key gossh.PublicKey) error {
		sum := sha256.Sum256(key.Marshal())
		fingerprint = "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:]), "=")
		if c.KnownFingerprint == "" {
			return nil // trust on first use; the operator is shown what we saw
		}
		if subtle.ConstantTimeCompare([]byte(c.KnownFingerprint), []byte(fingerprint)) != 1 {
			mismatch = true
			return ErrHostKeyMismatch
		}
		return nil
	}

	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, "", fmt.Errorf("could not reach %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(dialTimeout))

	clientCfg := &gossh.ClientConfig{
		User:            c.User,
		Auth:            auths,
		HostKeyCallback: hostKey,
		Timeout:         dialTimeout,
		ClientVersion:   "SSH-2.0-PulseFrix",
	}
	sc, chans, reqs, err := gossh.NewClientConn(conn, addr, clientCfg)
	if err != nil {
		_ = conn.Close()
		if mismatch {
			return nil, fingerprint, ErrHostKeyMismatch
		}
		return nil, fingerprint, fmt.Errorf("%w (%v)", ErrAuth, err)
	}
	_ = conn.SetDeadline(time.Time{}) // the session is long-lived from here on

	client := gossh.NewClient(sc, chans, reqs)
	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, fingerprint, fmt.Errorf("could not open a shell: %w", err)
	}

	modes := gossh.TerminalModes{
		gossh.ECHO:          1,
		gossh.TTY_OP_ISPEED: 38400,
		gossh.TTY_OP_OSPEED: 38400,
	}
	if err := session.RequestPty("xterm-256color", clamp(rows, 24), clamp(cols, 80), modes); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, fingerprint, fmt.Errorf("the server refused a PTY: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, fingerprint, err
	}
	// A real terminal merges stdout and stderr; one pipe keeps the ordering the
	// remote PTY intended.
	pr, pw := io.Pipe()
	session.Stdout = pw
	session.Stderr = pw

	if err := session.Shell(); err != nil {
		_ = stdin.Close()
		_ = session.Close()
		_ = client.Close()
		_ = pw.Close()
		return nil, fingerprint, fmt.Errorf("could not start a shell: %w", err)
	}

	term := &sshTerminal{client: client, session: session, stdin: stdin, out: pr}
	// When the remote shell exits, closing the write end surfaces io.EOF to the
	// reader, which ends the WebSocket pump and the session with it.
	go func() {
		_ = session.Wait()
		_ = pw.Close()
	}()
	return term, fingerprint, nil
}

func authMethods(c Credentials) ([]gossh.AuthMethod, error) {
	var auths []gossh.AuthMethod
	if key := strings.TrimSpace(c.PrivateKey); key != "" {
		var (
			signer gossh.Signer
			err    error
		)
		if c.Passphrase != "" {
			signer, err = gossh.ParsePrivateKeyWithPassphrase([]byte(key), []byte(c.Passphrase))
		} else {
			signer, err = gossh.ParsePrivateKey([]byte(key))
		}
		if err != nil {
			return nil, fmt.Errorf("%w (%v)", ErrBadKey, err)
		}
		auths = append(auths, gossh.PublicKeys(signer))
	}
	if c.Password != "" {
		pw := c.Password
		auths = append(auths,
			gossh.Password(pw),
			// Many sshd configs answer with keyboard-interactive rather than
			// plain password auth; reuse the same secret for its prompts.
			gossh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = pw
				}
				return answers, nil
			}),
		)
	}
	if len(auths) == 0 {
		return nil, ErrNoAuth
	}
	return auths, nil
}

func clamp(v, def int) int {
	if v < 1 || v > 1000 {
		return def
	}
	return v
}
