# SSH Console

The **SSH** tab gives you a real terminal on your server inside the dashboard —
like PuTTY, without leaving Pulse.

It is the one part of Pulse that can change a server, so it is built as a
separate, opt-in path that is **off by default** and does not weaken the
read-only guarantees documented in [SAFETY_MODEL.md](./SAFETY_MODEL.md).

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

## Enabling it

Two independent switches, both required.

### 1. Build an API that contains an SSH client

The default API build is **standard-library only** — it has no SSH client
compiled in at all, so there is nothing to enable. Build with the `ssh` tag to
include one (this is the only dependency it adds: `golang.org/x/crypto`).

```bash
# Docker
docker compose build --build-arg TAGS=ssh pulse-api

# From source
cd apps/api && go get golang.org/x/crypto && go build -tags ssh ./cmd/pulse-api
```

Tags combine with the PostgreSQL adapter: `--build-arg TAGS="pgx ssh"`.

### 2. Turn the console on

```bash
PULSE_SSH_CONSOLE=true
```

Restart the API. Until both are true, the SSH tab explains which step is
missing instead of offering a terminal that cannot connect.

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

## Credentials

You type them per session. Pulse:

- uses them to open **one** connection and then drops them;
- **never** writes them to the database, the logs, or an audit record;
- remembers only the host, port and username on your device (localStorage), and
  only if you tick *Remember host & username* — never a password or key.

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
| `GET` | `/api/v1/servers/{id}/ssh/sessions/{sid}/attach` | `ssh.exec` |
| `DELETE` | `/api/v1/servers/{id}/ssh/sessions/{sid}` | `ssh.exec` |

`attach` is a WebSocket upgrade authenticated by the ordinary session cookie —
no token ever appears in a URL. The wire protocol is deliberately tiny:

| Direction | Frame | Meaning |
|-----------|-------|---------|
| browser → API | binary | keystrokes, verbatim |
| browser → API | text `{"type":"resize","cols":N,"rows":M}` | window changed |
| API → browser | binary | terminal output, verbatim |
| API → browser | text `{"type":"status"\|"exit",…}` | session lifecycle |

---

## Reverse proxies

The console holds a WebSocket open for as long as the terminal is on screen.
Pulse's own nginx config (`apps/dashboard/nginx.conf`) already forwards the
upgrade and raises `proxy_read_timeout` to 1h. Caddy needs no configuration. If
you front Pulse with your own proxy, do the same.

---

## Turning it off

Set `PULSE_SSH_CONSOLE=false` (or rebuild without `-tags ssh`) and restart. Live
sessions are closed when the API shuts down.
