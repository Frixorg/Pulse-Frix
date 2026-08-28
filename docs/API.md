# REST API

Base path: `/api/v1`. All responses are JSON. All timestamps are RFC 3339 UTC.
Auth is via session cookie (dashboard) or `Authorization: Bearer <api_token>`
(programmatic). Every endpoint enforces authentication, authorization, input
validation, rate limiting where appropriate, audit logging, and safe error
responses. **Frontend authorization is never trusted on its own.**

---

## Conventions

### Errors

Structured, never a stack trace:

```json
{
  "error": {
    "code": "SERVER_NOT_FOUND",
    "message": "Server does not exist",
    "request_id": "req_01H..."
  }
}
```

Error codes include: `AUTH_ERROR`, `PERMISSION_ERROR`, `VALIDATION_ERROR`,
`SERVER_NOT_FOUND`, `RATE_LIMITED`, `DOCKER_UNAVAILABLE`, `CONFIGURATION_ERROR`,
`SSRF_BLOCKED`, `INTERNAL_ERROR`.

### Pagination

List endpoints accept `?limit=` (default 50, max 200) and `?cursor=` and return
`{ "data": [...], "next_cursor": "..." }`. Large datasets are never dumped whole.

### Versioning

The path is versioned (`/api/v1`). Breaking changes bump the version.

---

## Endpoints

### Auth — `/api/v1/auth`

| Method | Path | Notes |
|--------|------|-------|
| POST | `/auth/login` | email+password → session; login rate-limited |
| POST | `/auth/logout` | invalidate session |
| GET | `/auth/session` | current principal + permissions + `has_password` |
| POST | `/auth/mfa/verify` | MFA-ready (TOTP) |

### First-boot provisioning — `/api/v1/setup`

Public by necessity, and closed permanently once any account exists. Used only
when `ADMIN_EMAIL`/`ADMIN_PASSWORD` were not supplied to the installer, and
only in self-hosted mode — cloud is multi-tenant and provisions through an
identity provider, so the wizard is never offered there.

| Method | Path | Notes |
|--------|------|-------|
| GET | `/setup/status` | `{needs_setup, mode, self_hosted, min_password_length}` |
| POST | `/setup` | creates the first owner and signs it in; 409 once provisioned; rate-limited |

### Account self-service — `/api/v1/account`

Any authenticated role manages its OWN credentials; these are not admin
capabilities. Both endpoints re-verify the current password, so a hijacked
session alone cannot change them.

They apply only to accounts that HAVE a password. An identity-provider account —
the normal case on Pulse Cloud, where people sign in with Google or Telegram —
has nothing to verify against, so both endpoints return `409` and the dashboard
hides the forms entirely. `has_password` on the session payload is what it keys
on, so a self-hosted password account still gets them.

| Method | Path | Notes |
|--------|------|-------|
| POST | `/account/email` | `{current_password, email}` → updated session payload |
| POST | `/account/password` | `{current_password, new_password}`; revokes every session the user holds and issues a fresh cookie to this browser |

Passwords are hashed with PBKDF2-HMAC-SHA256 (210,000 iterations, 16-byte
per-password salt) and must be at least 12 characters. Changes are audited as
`account.email_change` / `account.password_change`.

### Users & orgs — `/api/v1/users`, `/api/v1/organizations`

CRUD scoped to the caller's org; `user.manage` / `org.manage` required for writes.

### Servers — `/api/v1/servers`

| Method | Path | Permission |
|--------|------|-----------|
| GET | `/servers` | `server.read` |
| GET | `/servers/{id}` | `server.read` |
| GET | `/servers/{id}/summary` | `server.read` — health/CPU/RAM/disk/net/uptime |
| DELETE | `/servers/{id}` | `server.manage` — removes from Pulse only |

### Agents & enrollment — `/api/v1/agents`

| Method | Path | Notes |
|--------|------|-------|
| POST | `/agents/enrollment-tokens` | `server.manage`; returns short-lived token |
| POST | `/agents/enroll` | called by the agent with a token (rate-limited) |
| GET | `/agents/{id}` | status, protocol version, last seen |
| POST | `/agents/{id}/revoke` | `server.manage`; revokes identity |

### Discovery — `/api/v1/discovery`

| Method | Path | Notes |
|--------|------|-------|
| GET | `/servers/{id}/discovery` | latest redacted inventory snapshot |
| GET | `/servers/{id}/topology` | generated graph (nodes+edges) from real data |

### Services / containers / applications / databases

| Method | Path |
|--------|------|
| GET | `/servers/{id}/services` |
| GET | `/servers/{id}/containers` |
| GET | `/servers/{id}/containers/{cid}` (metrics, mounts, networks, ports, **redacted** env, health) |
| GET | `/servers/{id}/applications` |
| GET | `/servers/{id}/databases` |
| GET | `/servers/{id}/inventory` |
| GET | `/servers/{id}/service-audit` |

#### Service audit

`GET /servers/{id}/service-audit` answers "what talks to what, and what is
running that nothing needs?".

The `relations` graph is built only from evidence already in the snapshot — a
reverse-proxy upstream, a shared user-defined Docker network, a Compose project
or `depends_on`, a socket attributed to a process. It never invents an edge.

`findings` are HEURISTICS and are presented as such. Each carries the `evidence`
behind it, a `confidence`, and a `reclaimable` estimate; the response also ships
its own blind spots in `limitations`, because acting on a finding means removing
something from a live server.

| Category | Fires when | Confidence |
|----------|-----------|------------|
| `stopped` | a container is not running but still holds its writable layer | high — "not running" is a fact |
| `unrouted` | running and listening, but no proxy route and no connected peer | medium; suppressed entirely on hosts with no reverse proxy |
| `idle` | near-zero CPU and almost no traffic since start, while holding memory | low, or medium when also unconnected |
| `unreferenced` | a database engine with no observed consumer | low — clients connect over sockets Pulse does not trace |
| `duplicate` | more than one instance of the same engine | medium |
| `orphaned` | Docker volumes whose project has no containers left | medium |

Core infrastructure (`sshd`, `systemd-*`, `docker`, the reverse proxy in use,
and every `pulse-*` component) is exempt from every rule, whatever the numbers
say. Nothing here proposes or performs a change — Pulse flags, the operator
decides. See [SAFETY_MODEL.md](./SAFETY_MODEL.md).

#### Inventory

`GET /servers/{id}/inventory` is the correlated answer to "what is running on
this box, and is it on the host or in a container?". It is derived server-side
from the same snapshot, so the dashboard needs one request.

Each item carries `kind` (`container` | `service` | `database` | `proxy`) and
`placement` (`host` | `container`), with every listening socket attached to
whatever owns it — a container id, a systemd unit, or a bare PID.

Sockets whose owner could not be read come back under `unattributed` rather
than being dropped: an unexplained open port is a finding, not an empty result.
It usually means the agent could not read `/proc/<pid>/fd`.

### Domains — `/api/v1/domains`

`GET /servers/{id}/domains` → DNS, HTTP(S) status, latency, TLS validity + expiry.
Domain checks pass through the [SSRF guard](./THREAT_MODEL.md#t5-ssrf).

### Metrics — `/api/v1/metrics`

`GET /servers/{id}/metrics?query=<name>&range=1h|6h|24h|7d|30d|custom` — the API
abstracts Prometheus; the dashboard never speaks PromQL. Only metrics that actually
exist are returned; the API never invents metrics.

### Alerts & events — `/api/v1/alerts`, `/api/v1/events`

| Method | Path | Permission |
|--------|------|-----------|
| GET | `/alerts` | `alert.read` |
| POST | `/alerts` | `alert.manage` |
| PUT | `/alerts/{id}` | `alert.manage` |
| GET | `/alerts/instances` | `alert.read` (firing/resolved, dedup, correlation) |
| GET | `/events` | `event.read` |

### Integrations & settings — `/api/v1/integrations`, `/api/v1/settings`

Manage exporters/notifiers and org/server settings; writes require
`integration.manage`. Any config-mutation integration is gated behind
`ENABLE_CONFIG_MUTATION` and the [safe modification pipeline](./SAFETY_MODEL.md).

### Security view — `/api/v1/servers/{id}/security`

Read-only findings (SSH exposed, public DB port, weak/expired TLS, Docker exposure,
unexpected listeners). **Reports only — never changes** security configuration.

### SSH console — `/api/v1/servers/{id}/ssh`

The only write path in the API, and the only one an agent is not involved in.
On by default and requires no configuration; it can do nothing without SSH
credentials supplied per request. Requires the `ssh.exec` permission
(owner/admin — never viewer). `PULSE_SSH_CONSOLE=false` removes it, and
`-tags nosshconsole` leaves the SSH client out of the binary.

- `GET /ssh/capabilities` — `{enabled, reason, default_port, can_use}`. The
  dashboard uses `reason` to explain what an operator has to change.
- `POST /servers/{id}/ssh/sessions` — dial the host and park a live session.
  Body: `host`, `port`, `username`, `auth_method` (`password`|`key`), the
  matching secret, optional `known_fingerprint`, `cols`, `rows`. Returns
  `session_id` and the host key `fingerprint`. Credentials are used once and
  never stored, logged or audited. A changed host key returns `409
  SSH_HOST_KEY_MISMATCH` with the fingerprint actually offered.
- `POST /servers/{id}/ssh/setup` — one-click setup. Using the same credentials,
  authorises a freshly generated key on the host so later sessions need no
  password. Returns the steps performed, the `sshd` settings observed, the
  public key, and the private key (returned once, never stored). It appends to
  the user's `authorized_keys` only, replacing any key Pulse installed before,
  and never edits `sshd_config`.
- `GET /servers/{id}/ssh/sessions/{sid}/attach` — WebSocket upgrade, authenticated
  by the session cookie. Binary frames carry terminal bytes verbatim; text
  frames carry `resize` and lifecycle messages.
- `GET /servers/{id}/ssh/sessions/{sid}/stream` and
  `POST /servers/{id}/ssh/sessions/{sid}/input` — the same session over plain
  HTTP (server-sent events + POSTs), used when a browser cannot open a
  WebSocket. The usual cause is a reverse-proxy CSP without `connect-src`,
  which blocks `wss:` while allowing https. `stream` claims the session exactly
  as `attach` does.
- `DELETE /servers/{id}/ssh/sessions/{sid}` — end the session.

`ssh.connect`, `ssh.disconnect` and `ssh.setup` are audited. See
[SSH_CONSOLE.md](./SSH_CONSOLE.md).

### Audit — `/api/v1/audit`

`GET /audit` (owner/admin) — sensitive operations with actor, action, result, IP.

---

## Realtime

`GET /api/v1/stream` (WebSocket; SSE fallback) pushes component-level updates
(`metric`, `health`, `event`, `alert`). The dashboard updates individual widgets —
never a full-page reload. Connection counts are themselves a Pulse metric.

---

## Rate limiting

Authentication, login, enrollment, agent ingestion and SSH console dials are
independently rate limited so a compromised agent — or a credential-stuffing
attempt against a customer's own sshd — cannot overwhelm the backend. `RATE_LIMITED`
responses include `Retry-After`.
