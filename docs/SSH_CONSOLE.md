# SSH Console

The **SSH** tab gives you a real terminal on your server inside the dashboard —
like PuTTY, without leaving Pulse.

It works out of the box — no build flags, no environment variables. It is the
one part of Pulse that can change a server, so it is built as a separate path
that does not weaken the read-only guarantees documented in
[SAFETY_MODEL.md](./SAFETY_MODEL.md): it is gated on the `ssh.exec` permission,
it cannot do anything without SSH credentials you type in, and the agent is
never involved.

---

## What it is — and what it is not

```text
  Browser ──WebSocket──► pulse-api ──SSH (outbound, port 22)──► your VPS
                             │
  pulse-agent ────ingest────►┘        the agent is NOT in this path
```

- The console is an ordinary **SSH client that happens to run inside the API**.
- The **agent is not involved**. It stays read-only and still never accepts a
  command from the control plane. Golden Rule 7 ("never execute arbitrary
  cloud-provided shell commands") is about the agent obeying the control plane —
  that remains true. Here *you* are the one typing, over SSH, authenticating
  with your own credentials.
- Turning the console off changes nothing else. Every other page keeps working.

---

## Turning it off

Nothing is needed to turn it **on**. The SSH client (`golang.org/x/crypto`, the
control plane's only direct dependency) is in the default build, and
`PULSE_SSH_CONSOLE` defaults to `true`.

To remove the terminal from a deployment:

```bash
PULSE_SSH_CONSOLE=false     # hides the console; the tab explains why
```

To remove the SSH client from the binary entirely — a pure standard-library
control plane:

```bash
docker compose build --build-arg TAGS=nosshconsole pulse-api
# or: go build -tags nosshconsole ./cmd/pulse-api
```

Tags combine with the PostgreSQL adapter: `--build-arg TAGS="pgx nosshconsole"`.
Either way the SSH tab says exactly what was switched off, instead of offering a
terminal that cannot connect. Live sessions close when the API shuts down.

---

## Who can use it

The console requires the `ssh.exec` permission:

| Role   | `ssh.exec` |
|--------|------------|
| owner  | yes        |
| admin  | yes        |
| viewer | **no**     |

Viewers keep full read-only access to every other page. There is no
configuration that grants a viewer a shell.

---

## One-click setup ("Set up SSH on my VPS")

The SSH page has a **Set up SSH on my VPS** button. Give it one working login
and it makes every later session password-free — you never run anything on the
server yourself.

What it does, over that one SSH connection:

1. creates `~/.ssh` (mode 700) and `authorized_keys` (600) if they are missing;
2. generates an ed25519 key and authorises its public half;
3. restores the SELinux context where that applies;
4. signs in again with the new key to prove it actually works;
5. reports the `sshd` settings that decide whether key logins are possible.

The private key is returned to your browser once and stored there (opt-out at
setup time). It is never written to the Pulse database or logs. A different
browser simply runs setup again.

**What it will not do.** It never edits `sshd_config`, firewall rules, or any
other file — a remote `sshd` rewrite is the classic way to lock yourself out, so
Pulse reports those settings and leaves the decision to you.

**Re-running it is safe.** Setup replaces the key Pulse installed previously,
matched on its exact comment (`pulse-console@<hostname>`), so repeated runs
never litter the file. Keys you or your team added are never touched. The
rewrite goes through a temp file and a rename, so a failure part-way leaves
`authorized_keys` exactly as it was.

**It needs one login to start.** Pulse is an SSH client here, not an agent
command — the agent is read-only and never executes anything. If you cannot log
in at all yet, use your provider's rescue console once to enable SSH, then come
back.

---

## Credentials

You type them per session. Pulse:

- uses them to open **one** connection and then drops them;
- **never** writes them to the database, the logs, or an audit record;
- remembers only the host, port and username on your device (localStorage), and
  only if you tick *Remember host & username* — never a password or key. Those
  details are saved as you type, so a connection that fails does not cost you
  the retyping; unticking the box deletes what was stored.

The host box accepts what you are likely to paste — `root@host`, `host:2222`,
`ssh://root@host:2222/` — and splits it into the right fields.

Both password and private-key (OpenSSH format, with optional passphrase)
authentication are supported. If `sshd` answers with keyboard-interactive
instead of plain password auth, the same password is used for its prompts.

Because credentials travel from your browser to the API, **run the dashboard
over HTTPS**. Pulse Cloud and the Caddy-based self-hosted setup already do.

---

## Host keys

The console pins host keys per `host:port` in your browser:

1. **First connection** — trust on first use. The fingerprint is shown and
   pinned, so you can verify it out of band:
   ```bash
   ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub
   ```
2. **Later connections** — a different key is **refused**. The UI shows the
   trusted and the offered fingerprint side by side. If the change is legitimate
   (a rebuild or reinstall), click *Forget the old key and try again*.

---

## Limits

| Limit | Value |
|-------|-------|
| Concurrent sessions per user | 4 |
| Concurrent sessions per API | 64 |
| Dial attempts | ~5, then 1 per 5s (per IP) |
| Time to attach after dialling | 60s, then the session is reaped |
| Maximum session lifetime | 8 hours |
| One WebSocket per session | yes — a second tab is refused |

Switching servers in the dashboard ends the session: a shell pointed at the
wrong host is a hazard, not a convenience.

Moving between **pages** does not. The console is kept alive while you are
signed in, so you can check a metric or a container and come back to the same
shell with its scrollback intact. It closes when you sign out, leave the
dashboard, switch servers, or click Disconnect.

---

## Audit trail

Every connection writes an audit entry — who, which host, which port, which
username, when, and the result. Disconnects record the session duration.
Credentials never appear.

```json
{ "action": "ssh.connect", "result": "success",
  "metadata": { "host": "203.0.113.10", "port": 22, "username": "root",
                "fingerprint": "SHA256:…" } }
```

---

## API

| Method | Path | Permission |
|--------|------|-----------|
| `GET` | `/api/v1/ssh/capabilities` | `server.read` |
| `POST` | `/api/v1/servers/{id}/ssh/sessions` | `ssh.exec` |
| `POST` | `/api/v1/servers/{id}/ssh/setup` | `ssh.exec` |
| `GET` | `/api/v1/servers/{id}/ssh/sessions/{sid}/attach` | `ssh.exec` |
| `DELETE` | `/api/v1/servers/{id}/ssh/sessions/{sid}` | `ssh.exec` |

`setup` takes the same body as `sessions` and returns the steps it performed,
the `sshd` settings it observed, the new public key, and the private key (once).
`ssh.setup` is audited with the public key installed and whether verification
passed.

`attach` is a WebSocket upgrade authenticated by the ordinary session cookie —
no token ever appears in a URL. The wire protocol is deliberately tiny:

| Direction | Frame | Meaning |
|-----------|-------|---------|
| browser → API | binary | keystrokes, verbatim |
| browser → API | text `{"type":"resize","cols":N,"rows":M}` | window changed |
| API → browser | binary | terminal output, verbatim |
| API → browser | text `{"type":"status"\|"exit",…}` | session lifecycle |

When a WebSocket is impossible the dashboard uses the same session over two
plain HTTP endpoints instead:

| Method | Path | Carries |
|--------|------|---------|
| `GET` | `…/ssh/sessions/{sid}/stream` | terminal output as base64 SSE `data` events |
| `POST` | `…/ssh/sessions/{sid}/input` | `{"type":"data","data":"<base64>"}` or `{"type":"resize","cols":N,"rows":M}` |

Both need the `ssh.exec` permission, and `stream` claims the session exactly as
`attach` does — one transport at a time. The stream sets `X-Accel-Buffering: no`
and sends a comment heartbeat every 20s so proxies neither buffer nor time it
out.

---

## Reverse proxies

The console holds a WebSocket open for as long as the terminal is on screen.
Pulse's own nginx config (`apps/dashboard/nginx.conf`) already forwards the
upgrade, raises `proxy_read_timeout` to 1h, and sets a `connect-src` that
permits the socket. Caddy needs no extra configuration.

If you front Pulse with **your own** proxy (Nginx Proxy Manager, Cloudflare, a
hand-rolled nginx), two things there can break the terminal.

### 1. Content-Security-Policy

This is the common one, and the console **handles it on its own** — read this
only if you want the faster transport back. A CSP like

```text
default-src https: data: blob: 'unsafe-inline' 'unsafe-eval'
```

looks permissive but blocks the console, because `connect-src` falls back to
`default-src` and the `https:` scheme does **not** match `wss:`. The browser
console shows:

```text
Connecting to 'wss://pulse.example.com/api/v1/servers/…/attach' violates the
following Content Security Policy directive: "default-src https: …".
Note that 'connect-src' was not explicitly set, so 'default-src' is used as a
fallback.
```

Every CSP header on the response is enforced, so a permissive one from Pulse
cannot rescue a restrictive one from your proxy.

**Pulse falls back automatically.** When the browser refuses the WebSocket, the
console reconnects over server-sent events plus ordinary POSTs — plain https,
which that same policy allows — and the terminal works normally. The toolbar
shows an `http fallback` chip so the switch is never silent, and the choice is
remembered per browser so later sessions do not retry a socket that cannot open.

The fallback costs a little latency and one request per burst of keystrokes, so
it is still worth restoring the WebSocket. Fix it at your proxy by adding an
explicit `connect-src`:

```text
add_header Content-Security-Policy "default-src https: data: blob: 'unsafe-inline' 'unsafe-eval'; connect-src 'self' wss://pulse.example.com" always;
```

or, if you would rather not manage a CSP there at all, remove the header and let
Pulse's own (already correct) one apply. Clear `pulse-ssh-transport` from the
browser's local storage — or just use a different browser profile — to make the
console try the WebSocket again.

### 2. Upgrade headers and timeouts

The proxy must forward the upgrade and not time the socket out:

```nginx
proxy_http_version 1.1;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection $connection_upgrade;   # "upgrade" only when requested
proxy_read_timeout 1h;
proxy_send_timeout 1h;
```

