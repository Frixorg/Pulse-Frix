//go:build ssh

package sshx

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/pem"
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

// dialClient authenticates to the host and returns a live client alongside the
// host key fingerprint ("SHA256:..."), which is reported even when the
// handshake fails so the caller can show what was offered.
func dialClient(ctx context.Context, c Credentials) (*gossh.Client, string, error) {
	auths, err := authMethods(c)
	if err != nil {
		return nil, "", err
	}

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
	_ = conn.SetDeadline(time.Time{}) // sessions on this client are long-lived
	return gossh.NewClient(sc, chans, reqs), fingerprint, nil
}

// Dial opens an interactive shell with a PTY and returns it alongside the
// remote host key fingerprint, which the caller shows the operator and the
// browser pins for subsequent connections.
func Dial(ctx context.Context, c Credentials, cols, rows int) (Terminal, string, error) {
	client, fingerprint, err := dialClient(ctx, c)
	if err != nil {
		return nil, fingerprint, err
	}

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

// setupScript is run once on the host, over the connection the operator just
// authenticated. It is deliberately conservative:
//
//   - it only ever APPENDS one line to the user's own authorized_keys, and is a
//     no-op if that exact key is already there;
//   - it never edits sshd_config. Rewriting a remote sshd config is the classic
//     way to lock yourself out of a box, so Pulse reports those settings and
//     lets a human decide;
//   - every action prints a PULSE-STEP line so the UI can show what happened
//     instead of "done".
//
// The two %s are the public key and its comment, as single-quoted shell literals.
const setupScript = `
umask 077
KEY=%s
COMMENT=%s
HOME_DIR="${HOME:-}"
if [ -z "$HOME_DIR" ]; then HOME_DIR=$(cd ~ 2>/dev/null && pwd); fi
if [ -z "$HOME_DIR" ]; then
  echo "PULSE-STEP:home:error:could not determine the home directory for this user"
  exit 1
fi

if mkdir -p "$HOME_DIR/.ssh" 2>/dev/null; then
  chmod 700 "$HOME_DIR/.ssh" 2>/dev/null
  echo "PULSE-STEP:dir:ok:$HOME_DIR/.ssh ready (mode 700)"
else
  echo "PULSE-STEP:dir:error:could not create $HOME_DIR/.ssh"
  exit 1
fi

AK="$HOME_DIR/.ssh/authorized_keys"
if [ ! -f "$AK" ]; then : > "$AK" 2>/dev/null; fi
chmod 600 "$AK" 2>/dev/null
if grep -qF "$KEY" "$AK" 2>/dev/null; then
  echo "PULSE-STEP:key:skipped:this key was already authorised — nothing changed"
else
  # Running setup twice must not litter the file. Drop any key Pulse itself
  # wrote earlier — matched on its exact comment, so nobody else's keys are
  # touched — then append the new one. The rewrite goes through a temp file and
  # a rename, so a failure part-way leaves authorized_keys exactly as it was.
  PREV=$(awk -v c="$COMMENT" '$NF == c' "$AK" 2>/dev/null | wc -l | tr -d " ")
  TMP="$AK.pulse.$$"
  awk -v c="$COMMENT" '$NF != c' "$AK" > "$TMP" 2>/dev/null
  printf '%%s\n' "$KEY" >> "$TMP" 2>/dev/null
  chmod 600 "$TMP" 2>/dev/null
  if mv "$TMP" "$AK" 2>/dev/null; then
    if [ "${PREV:-0}" -gt 0 ] 2>/dev/null; then
      echo "PULSE-STEP:key:ok:replaced the previous Pulse key in $AK — your other keys are untouched"
    else
      echo "PULSE-STEP:key:ok:public key appended to $AK"
    fi
  else
    rm -f "$TMP" 2>/dev/null
    echo "PULSE-STEP:key:error:could not write to $AK"
    exit 1
  fi
fi

# SELinux-labelled systems ignore authorized_keys with the wrong context.
if command -v restorecon >/dev/null 2>&1; then
  if restorecon -R "$HOME_DIR/.ssh" >/dev/null 2>&1; then
    echo "PULSE-STEP:selinux:ok:restored the SELinux context on $HOME_DIR/.ssh"
  fi
fi

# Read-only: report the sshd settings that decide whether key login works.
CFG=$(sshd -T 2>/dev/null || cat /etc/ssh/sshd_config 2>/dev/null || true)
echo "PULSE-INFO:pubkey_authentication:$(printf '%%s\n' "$CFG" | grep -i '^[[:space:]]*pubkeyauthentication' | tail -1 | awk '{print tolower($2)}')"
echo "PULSE-INFO:password_authentication:$(printf '%%s\n' "$CFG" | grep -i '^[[:space:]]*passwordauthentication' | tail -1 | awk '{print tolower($2)}')"
echo "PULSE-INFO:permit_root_login:$(printf '%%s\n' "$CFG" | grep -i '^[[:space:]]*permitrootlogin' | tail -1 | awk '{print tolower($2)}')"
echo "PULSE-INFO:port:$(printf '%%s\n' "$CFG" | grep -i '^[[:space:]]*port[[:space:]]' | tail -1 | awk '{print $2}')"
echo "PULSE-INFO:user:$(id -un 2>/dev/null || whoami 2>/dev/null)"
echo "PULSE-STEP:done:ok:setup finished"
`

// InstallKey generates a fresh key, authorises it on the host, and proves it
// works by logging in again with it. The private key is returned to the caller
// (and on to the browser) exactly once; nothing is stored here.
func InstallKey(ctx context.Context, c Credentials, label string) (*SetupResult, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("could not generate a key: %w", err)
	}
	sshPub, err := gossh.NewPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("could not encode the key: %w", err)
	}
	comment := "pulse-console" + sanitiseLabel(label)
	authorizedKey := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(sshPub))) + " " + comment
	// x/crypto requires a POINTER for ed25519 keys here.
	block, err := gossh.MarshalPrivateKey(&priv, comment)
	if err != nil {
		return nil, fmt.Errorf("could not encode the private key: %w", err)
	}
	privatePEM := string(pem.EncodeToMemory(block))

	client, fingerprint, err := dialClient(ctx, c)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("could not open a session: %w", err)
	}
	script := fmt.Sprintf(setupScript, shellQuote(authorizedKey), shellQuote(comment))
	session.Stdin = strings.NewReader(script)
	// Run through /bin/sh explicitly: the login shell may be csh or fish.
	out, runErr := session.CombinedOutput("/bin/sh -s")
	_ = session.Close()

	res := &SetupResult{
		PublicKey:   authorizedKey,
		PrivateKey:  privatePEM,
		Fingerprint: fingerprint,
		Info:        map[string]string{},
	}
	parseSetupOutput(string(out), res)

	if runErr != nil && !hasStep(res, "done") {
		detail := strings.TrimSpace(lastLine(string(out)))
		if detail == "" {
			detail = runErr.Error()
		}
		res.Steps = append(res.Steps, SetupStep{Name: "run", Status: "error", Detail: detail})
		return res, fmt.Errorf("setup could not run on the host: %s", detail)
	}

	// Prove it: a brand-new connection using only the key we just installed.
	verifyCreds := Credentials{
		Host: c.Host, Port: c.Port, User: c.User,
		PrivateKey:       privatePEM,
		KnownFingerprint: fingerprint,
	}
	vctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	if vc, _, verr := dialClient(vctx, verifyCreds); verr == nil {
		_ = vc.Close()
		res.Verified = true
		res.Steps = append(res.Steps, SetupStep{
			Name: "verify", Status: "ok",
			Detail: "signed in again using the new key — no password needed",
		})
	} else {
		res.Steps = append(res.Steps, SetupStep{
			Name: "verify", Status: "warn",
			Detail: "the key was installed but a test login with it did not succeed",
		})
		res.Warnings = append(res.Warnings, verifyHint(res))
	}
	return res, nil
}

// parseSetupOutput turns the script's PULSE-STEP / PULSE-INFO lines into
// structured results, ignoring anything else the host printed (MOTDs, warnings).
func parseSetupOutput(out string, res *SetupResult) {
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "PULSE-STEP:"):
			parts := strings.SplitN(strings.TrimPrefix(line, "PULSE-STEP:"), ":", 3)
			if len(parts) < 3 {
				continue
			}
			if parts[0] == "done" {
				res.Steps = append(res.Steps, SetupStep{Name: "done", Status: "ok", Detail: parts[2]})
				continue
			}
			res.Steps = append(res.Steps, SetupStep{Name: parts[0], Status: parts[1], Detail: parts[2]})
		case strings.HasPrefix(line, "PULSE-INFO:"):
			parts := strings.SplitN(strings.TrimPrefix(line, "PULSE-INFO:"), ":", 2)
			if len(parts) == 2 && parts[1] != "" {
				res.Info[parts[0]] = parts[1]
			}
		}
	}
}

// verifyHint explains the usual reason a freshly installed key is still refused.
func verifyHint(res *SetupResult) string {
	if v := res.Info["pubkey_authentication"]; v == "no" {
		return "sshd has PubkeyAuthentication no — key logins are switched off on this host. " +
			"Enable it in /etc/ssh/sshd_config and reload sshd."
	}
	if v := res.Info["permit_root_login"]; v == "no" {
		return "sshd has PermitRootLogin no — sign in as a non-root user instead."
	}
	return "sshd did not accept the new key. Check the home directory permissions " +
		"(700 on ~/.ssh, 600 on authorized_keys) and /var/log/auth.log."
}

func hasStep(res *SetupResult, name string) bool {
	for _, s := range res.Steps {
		if s.Name == name {
			return true
		}
	}
	return false
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

// shellQuote wraps s in single quotes, which is safe because callers only pass
// base64 key material and a sanitised comment (neither can contain a quote).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sanitiseLabel keeps a key comment readable and shell-safe.
func sanitiseLabel(label string) string {
	var b strings.Builder
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		}
		if b.Len() >= 40 {
			break
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "@" + b.String()
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
