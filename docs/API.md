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
| GET | `/auth/session` | current principal + permissions |
| POST | `/auth/mfa/verify` | MFA-ready (TOTP) |

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
Off unless `PULSE_SSH_CONSOLE=true` **and** the binary was built with
`-tags ssh`; requires the `ssh.exec` permission (owner/admin — never viewer).

- `GET /ssh/capabilities` — `{enabled, reason, default_port, can_use}`. The
  dashboard uses `reason` to explain what an operator has to change.
- `POST /servers/{id}/ssh/sessions` — dial the host and park a live session.
  Body: `host`, `port`, `username`, `auth_method` (`password`|`key`), the
  matching secret, optional `known_fingerprint`, `cols`, `rows`. Returns
  `session_id` and the host key `fingerprint`. Credentials are used once and
  never stored, logged or audited. A changed host key returns `409
  SSH_HOST_KEY_MISMATCH` with the fingerprint actually offered.
- `GET /servers/{id}/ssh/sessions/{sid}/attach` — WebSocket upgrade, authenticated
  by the session cookie. Binary frames carry terminal bytes verbatim; text
  frames carry `resize` and lifecycle messages.
- `DELETE /servers/{id}/ssh/sessions/{sid}` — end the session.

`ssh.connect` and `ssh.disconnect` are audited. See
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
