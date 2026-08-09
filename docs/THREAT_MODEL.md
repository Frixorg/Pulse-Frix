# Threat Model

Pulse is internet-facing, handles multiple tenants (cloud mode), and runs on
production VPSs with root-equivalent visibility (Docker socket). This document
enumerates actors, threats, and mitigations. It is a living document; changes to
the attack surface must update it.

Methodology: STRIDE per component + explicit trust boundaries.

---

## Trust boundaries

```text
                ┌────────────────── Untrusted ──────────────────┐
   Internet ──▶ │ Browser  ·  Public DNS  ·  User-supplied URLs   │
                └───────────────────────┬───────────────────────┘
                              (B1) authn/authz, input validation, CSP
                ┌───────────────────────▼───────────────────────┐
                │ Pulse API / Dashboard (semi-trusted, hardened) │
                └───────────────────────┬───────────────────────┘
                              (B2) agent identity, capability model
                ┌───────────────────────▼───────────────────────┐
                │ Pulse Agent on the VPS (trusted, least-priv)   │
                └───────────────────────┬───────────────────────┘
                              (B3) read-only Docker/OS access
                ┌───────────────────────▼───────────────────────┐
                │ Host: Docker socket, /proc, config files       │
                └────────────────────────────────────────────────┘
```

---

## Threat actors

| Actor | Capability assumed |
|-------|--------------------|
| Internet attacker | Unauthenticated network access to public endpoints |
| Malicious authenticated user | Valid account, tries to exceed their scope |
| Compromised VPS | Attacker has code exec on a monitored VPS |
| Compromised agent | Agent binary/keys stolen or tampered |
| Malicious container | Hostile workload on the same Docker host |
| Malicious Docker image | Supply-chain of a monitored workload |
| Supply-chain attacker | Targets Pulse's own dependencies/build |
| Stolen enrollment token | Short-lived token intercepted |

---

## Threats & mitigations

### T1. Remote command execution via cloud → VPS
**Risk:** the cloud instructs the agent to run arbitrary commands.
**Mitigation:** there is **no** `cloud → shell → VPS` path. The agent exposes a
fixed capability set (`READ_*`) and refuses anything else. Write capabilities are
disabled by default, explicitly authorized, scoped, audited, revocable. Enforced in
`agent/internal/protocol` (the command dispatcher has no `exec` handler).

### T2. Cross-tenant data access (cloud)
**Risk:** user A sees user B's servers/metrics.
**Mitigation:** every query is tenant-scoped at the **API, service, and database**
layers — not just the frontend. `org_id` is derived from the authenticated session,
never from a request parameter. Row-level scoping in every store method; integration
tests assert isolation. See [DATA_MODEL.md](./DATA_MODEL.md#multi-tenancy).

### T3. Credential / secret leakage
**Risk:** DB passwords, API keys, TLS private keys reach logs/UI/telemetry.
**Mitigation:** a mandatory **redaction layer** (`agent/internal/redact`) runs
before logs, API responses, telemetry, DB storage, frontend, and debug output.
`DATABASE_URL=postgres://user:password@h/db` → `...user:***REDACTED***@h/db`.
Secrets are never sent to the dashboard in cloud mode. Secret-scanning in CI.

### T4. Agent takeover / key theft
**Risk:** stolen agent identity used to impersonate a server.
**Mitigation:** short-lived credentials with rotation; replay protection
(nonce + timestamp window); request signing; revocable agent identities; anomaly
rate-limiting on ingestion. A stolen key is revoked from the dashboard and the
agent must re-enroll.

### T5. SSRF (the platform fetches URLs for domain/health checks)
**Risk:** attacker makes the server fetch `169.254.169.254`, `localhost`, internal
services, `file://`.
**Mitigation:** a strict URL/IP validation layer (`apps/api/internal/ssrf`) blocks
loopback, link-local (incl. `169.254.169.254`), RFC1918, IPv6 ULA/link-local,
Docker bridge ranges, unix sockets, and non-`http(s)` schemes. DNS is resolved and
the **resolved IP** is re-checked (no TOCTOU via DNS rebinding); redirects are
re-validated per hop.

### T6. Privilege escalation / Docker escape
**Risk:** Docker socket access is root-equivalent; a bug becomes host compromise.
**Mitigation:** the agent runs unprivileged where possible, splitting *privileged
discovery* from the *unprivileged application*. Docker access is read-only (or via a
read-only socket proxy with an explicit endpoint allowlist). The socket is never
mounted into arbitrary application containers. Documented in [SECURITY.md](../SECURITY.md).

### T7. Injection (SQL, command, path traversal)
**Mitigation:** parameterised SQL only; no shell string construction — structured
APIs / safe argument arrays / allowlists; canonical path resolution with a
restricted root and symlink-escape refusal. Fuzz + unit tests for each.

### T8. Web attacks (XSS, CSRF)
**Mitigation:** CSP, secure/HttpOnly/SameSite cookies, output escaping (logs and
config are never rendered as raw HTML), CSRF tokens on state-changing requests,
dependency auditing.

### T9. Metrics poisoning / ingestion abuse
**Risk:** a compromised agent floods or poisons metrics.
**Mitigation:** per-agent ingestion rate limits and quotas; schema validation of
samples; bounded cardinality; drop + alert on anomalies. A compromised agent cannot
overwhelm the backend.

### T10. Supply-chain
**Mitigation:** pinned versions + lockfiles, dependency & container scanning, SBOM
generation, signed releases, checksum verification, reproducible builds where
possible, release provenance. The installer never executes remote scripts from
untrusted sources; the hosted installer is checksum-pinned.

### T11. Stolen enrollment token
**Mitigation:** enrollment tokens are short-lived, single-use, org-scoped, and
bound to an expected server fingerprint on first use. Enrollment endpoints are rate
limited. A used/expired token is rejected.

### T12. Denial of self (monitoring harms the VPS)
**Mitigation:** resource limits on the monitoring stack (CPU/memory), conservative
metric retention, disk-pressure detection with automatic metric cleanup. Monitoring
must never become the VPS's performance problem.

---

## Non-goals (v1)

- Full vulnerability scanning (the Security view flags *obvious* risks only).
- Centralized log analytics at scale.
- Automated remediation / config mutation (behind disabled feature flags).

---

## Residual risks

- Read-only Docker socket access still exposes substantial host metadata; documented
  and opt-in.
- Self-hosted operators are responsible for securing their own reverse proxy and TLS.

Report vulnerabilities per [SECURITY.md](../SECURITY.md).
