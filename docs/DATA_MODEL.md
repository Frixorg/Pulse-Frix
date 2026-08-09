# Data Model

Pulse separates the **control plane** (PostgreSQL) from **metrics** (Prometheus
TSDB) and **logs** (bounded, redacted). This document describes the control-plane
entities. The authoritative schema lives in
[`apps/api/migrations`](../apps/api/migrations).

---

## Entities

```text
Organization
    ├── User            (membership + role)
    ├── Server          (a monitored VPS)
    │     ├── Agent     (the binary running on the server)
    │     ├── Service   (discovered service: docker/system/nginx/db/app…)
    │     ├── Domain    (discovered domain + TLS)
    │     └── Event     (state changes / discoveries)
    ├── EnrollmentToken (short-lived, single-use)
    ├── Alert           (rule) → AlertInstance (firing/resolved)
    ├── ApiToken        (programmatic access, scoped)
    ├── AuditLogEntry   (sensitive operations)
    └── Setting         (org/server scoped)
```

In **self-hosted (single-tenant)** mode there is exactly one implicit
organization; the same schema and code run, so behaviour is identical.

---

## Multi-tenancy

Every tenant-owned row carries an `org_id`. The rule:

> `org_id` is always taken from the authenticated principal, **never** from a
> request parameter or body.

Isolation is enforced at three layers:

1. **API** — middleware derives `org_id` from the session/token.
2. **Service** — every store call requires an `org_id` argument (no "get by id
   without scope" method exists).
3. **Database** — composite lookups are `(org_id, id)`; foreign keys cascade within
   an org. (Optional Postgres RLS policies are provided for defence in depth.)

---

## Roles & permissions (RBAC)

Roles: `owner`, `admin`, `viewer`.

| Permission | viewer | admin | owner |
|------------|:------:|:-----:|:-----:|
| `server.read` | ✓ | ✓ | ✓ |
| `alert.read` | ✓ | ✓ | ✓ |
| `event.read` | ✓ | ✓ | ✓ |
| `server.manage` |  | ✓ | ✓ |
| `alert.manage` |  | ✓ | ✓ |
| `integration.manage` |  | ✓ | ✓ |
| `user.manage` |  |  | ✓ |
| `org.manage` |  |  | ✓ |

Permissions are explicit strings checked at the API layer; roles are just bundles
of permissions.

---

## Key tables (summary)

| Table | Purpose | Notable columns |
|-------|---------|-----------------|
| `organizations` | tenant root | `id`, `name`, `created_at` |
| `users` | accounts | `id`, `email`, `password_hash`, `mfa_secret` (nullable) |
| `memberships` | user↔org + role | `org_id`, `user_id`, `role` |
| `servers` | monitored VPS | `id`, `org_id`, `server_id` (public), `hostname`, `status`, `last_seen_at` |
| `agents` | agent identity | `id`, `org_id`, `server_id`, `agent_id`, `public_key`, `protocol_version`, `revoked_at` |
| `enrollment_tokens` | short-lived enroll | `token_hash`, `org_id`, `expires_at`, `used_at`, `fingerprint` |
| `services` | discovered services | `id`, `org_id`, `server_id`, `type`, `name`, `status`, `metadata` (jsonb, redacted) |
| `domains` | discovered domains | `id`, `org_id`, `server_id`, `fqdn`, `tls_expires_at`, `http_status`, `latency_ms` |
| `alerts` | alert rules | `id`, `org_id`, `name`, `expr`, `severity`, `for_seconds`, `cooldown_seconds`, `enabled` |
| `alert_instances` | firing/resolved | `id`, `org_id`, `alert_id`, `server_id`, `state`, `started_at`, `resolved_at`, `dedup_key` |
| `events` | state changes | `id`, `org_id`, `server_id`, `severity`, `source`, `event`, `prev_state`, `new_state`, `ts` |
| `api_tokens` | programmatic | `id`, `org_id`, `token_hash`, `scopes`, `expires_at` |
| `audit_log` | sensitive ops | `id`, `org_id`, `actor`, `action`, `result`, `ip`, `metadata`, `ts` |
| `settings` | config | `id`, `org_id`, `server_id` (nullable), `key`, `value` |
| `sessions` | web sessions | `id`, `user_id`, `org_id`, `expires_at`, `csrf_secret` |

`metadata` JSONB columns are **redacted before storage** — no raw secrets ever land
in the database.

---

## Metrics storage

Time-series live in Prometheus, **not** Postgres. The API queries Prometheus
(PromQL) and returns already-shaped series to the dashboard. Retention is
conservative and disk-pressure aware. See [MONITORING.md](./MONITORING.md).

Service identity is stable across restarts so metrics correlate to the same
logical service even as containers churn (identity derived from
image+name+compose-project+labels, never the ephemeral container id alone).

---

## Events

Every important state change is recorded with `timestamp, severity, source, event,
previous_state, new_state`. Examples: *container stopped/restarted, service became
unhealthy, disk threshold exceeded, SSL nearing expiry, VPS restarted, new service
discovered, service disappeared, monitoring configuration changed.*

---

## Audit log

Any sensitive operation produces an audit record. For cloud mode we additionally
record `user, organization, server, agent, operation, timestamp, IP, result`.
Sensitive credentials are never logged. See
[`apps/api/internal/audit`](../apps/api/internal/audit).
