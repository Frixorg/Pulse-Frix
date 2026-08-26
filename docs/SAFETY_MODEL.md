# Safety Model

Pulse is an **observability platform, not a VPS management tool**. Its default
behaviour is to *inspect, detect, classify, monitor, measure, report and alert* —
never to modify the system it observes. This document is the authoritative
description of that guarantee.

We deliberately do **not** claim *"installation can never affect your VPS."* That
claim is impossible to honour. Instead we implement a **formal safety contract**
with phases, an enforced SAFE MODE, and a set of non-negotiable golden rules.

---

## The Safety Contract

Every installation and every configuration operation runs through three phases:

```text
Phase A — DISCOVER   read-only inspection, no writes of any kind
        ↓
Phase B — PLAN       generate an immutable, human-readable change plan
        ↓
Phase C — APPLY      apply ONLY the explicitly permitted changes
```

The plan produced in Phase B is **immutable**: Phase C applies exactly that plan or
nothing. There is no path from Discover directly to a mutation.

### Phase A — Discover

Pure read-only inspection. The discovery engine opens Docker's read-only API,
reads (never writes) config files, lists listening ports, samples `/proc`, and
queries health endpoints. It writes nothing outside Pulse's own scratch area.

### Phase B — Plan

The installer emits an explicit plan, e.g.:

```text
SAFE INSTALLATION PLAN

The installer WILL:
  ✓ create an isolated monitoring network (pulse-net)
  ✓ create monitoring containers (namespaced pulse-*)
  ✓ create dedicated data directories under /opt/pulse
  ✓ expose the dashboard on an unused port (auto-selected: 3210)
  ✓ preserve existing Nginx configuration
  ✓ preserve existing Docker workloads

The installer will NOT:
  ✓ modify existing application containers
  ✓ modify application configuration
  ✓ modify firewall rules
  ✓ replace Nginx
  ✓ restart existing applications
```

The plan is written to `state/plan.json` and shown to the user, who must confirm.

### Phase C — Apply

Applies only what the confirmed plan describes. Each created resource is recorded
in the [installation manifest](#rollback--manifest) so it — and only it — can be
removed later.

---

## SAFE MODE (the default)

In SAFE MODE the following are **guaranteed** and asserted in code + CI:

- existing application containers are never modified
- existing Docker Compose files are never modified
- existing Nginx files are never overwritten
- existing systemd units are never modified
- existing firewall rules are never changed
- existing ports are not repurposed
- existing application directories are not modified
- existing databases are not touched
- existing services are not restarted

> **If monitoring requires a change, Pulse creates a new isolated monitoring
> component instead of touching an existing one.**

---

## When a change *is* explicitly requested

Config mutation is gated behind `ENABLE_CONFIG_MUTATION=false` (default) and, even
when enabled, follows the safe modification pipeline:

```text
BACKUP → GENERATE → DIFF → VALIDATE → APPLY → HEALTH CHECK → SUCCESS
                                                   │
                                                   └─ on any failure → ROLLBACK
```

- **Backup** — copy the current file/artifact before any write.
- **Generate** — produce proposed content with structured manipulation (never
  blind `sed -i` on production config).
- **Diff** — show the user a unified diff.
- **Validate** — syntax check (e.g. `nginx -t`) must pass *before* reload.
- **Apply** — atomic write: `temp file → fsync → atomic rename`. Never truncate a
  production file and then write.
- **Health check** — verify the service is still healthy after apply.
- **Rollback** — automatically restore the backup if validation or health fails.

See [File safety](#file-safety) below.

---

## File safety

Before modifying any file Pulse:

1. Resolves the real path (canonicalises, no `..` escape).
2. Checks ownership.
3. Checks permissions.
4. Checks symlinks (refuses symlink escape).
5. Checks whether the file is managed by another tool.
6. Creates a backup.
7. Generates proposed content.
8. Calculates a diff.
9. Validates.
10. Writes atomically (`temp → fsync → rename`).
11. Verifies.
12. Records the operation in the audit log.

---

## Port management

Ports are never assumed free. Before binding:

```text
Check existing listeners → detect conflict → choose a safe unused port
```

`3000`, `9090`, `9100` are treated as *probably occupied*. The installer presents
detected/occupied vs. available ports and lets the user override.

---

## Docker safety

Pulse never automatically runs `docker compose down`, `docker stop`, `docker rm`,
`docker system prune`, or `docker volume prune`. It never removes unused volumes,
and never pulls a different image for an existing application. Monitoring
containers live on an **isolated** `pulse-net` network under a `pulse-*` namespace.

The Docker socket is treated as **root-equivalent**. The host agent accesses it
read-only (or via a read-only socket proxy with an explicit endpoint allowlist).
See [THREAT_MODEL.md](./THREAT_MODEL.md) and [SECURITY.md](../SECURITY.md).

---

## Rollback & manifest

Every install writes an immutable manifest of what it created:

```json
{
  "created": ["/opt/pulse-agent", "/opt/pulse-monitoring"],
  "modified": [],
  "services_created": ["pulse-agent"],
  "ports_allocated": [3210],
  "networks_created": ["pulse-net"],
  "containers_created": ["pulse-prometheus", "pulse-grafana"]
}
```

Rollback and `pulse uninstall` remove **only** resources in this manifest. Pulse
never performs broad cleanup and never deletes existing infrastructure.

---

## Installation as a transaction

```text
DISCOVERY → PRECHECK → PLAN → BACKUP → INSTALL → VERIFY → COMPLETE
```

On failure the state becomes `FAILED` with exact recovery information. The VPS is
never left half-configured — the transaction either completes or rolls back to the
manifest.

---

## The SSH console — the one explicit exception

The dashboard's **SSH** tab opens a real terminal on a server. It is the only
feature in Pulse that can change one, so it is fenced off rather than folded
into the observability path:

- It is **inert without credentials**. Pulse has no standing access to any
  host: every session is opened with an SSH password or key the operator types
  in at that moment, and nothing is stored afterwards. A deployment that wants
  the terminal gone sets `PULSE_SSH_CONSOLE=false`, or builds with
  `-tags nosshconsole` to leave the SSH client out of the binary altogether.
- It requires the `ssh.exec` permission — owners and admins only. No
  configuration gives a viewer a shell.
- **The agent is not involved.** The API dials the host over SSH like any other
  client. Agents remain read-only and still never accept a command from the
  control plane, so Golden Rule 7 below is untouched: it forbids the *agent*
  executing what the cloud tells it to, not an operator typing into their own
  terminal.
- Credentials are typed per session, used for one dial, and never written to the
  database, the logs or an audit record. The audit trail records who connected,
  to which host, and when.
- Host keys are pinned per host in the browser; a changed key stops the
  connection until the operator explicitly accepts the new one.

See [SSH_CONSOLE.md](./SSH_CONSOLE.md) for the full description.

---

## Golden Rules

These are non-negotiable and enforced in code review and CI:

1. Never modify existing services unless explicitly authorized.
2. Never delete existing user data.
3. Never automatically stop production services.
4. Never automatically restart production services.
5. Never overwrite configuration blindly.
6. Never expose secrets.
7. Never execute arbitrary cloud-provided shell commands.
8. Never trust user-controlled paths or URLs.
9. Never assume a port is free.
10. Never assume Docker is configured normally.
11. Never assume Nginx configuration is standard.
12. Never require an inbound management port when outbound communication is possible.
13. Never make cloud availability a dependency for application availability.
14. Never silently modify firewall rules.
15. Never silently change DNS.
16. Never automatically upgrade user applications.
17. Never remove Docker volumes automatically.
18. Never log secrets.
19. Never trust frontend authorization alone.
20. Every mutation must be auditable and reversible.

---

## Acceptance criteria

Pulse is **not** considered complete until all of the following hold:

- **Install** — fresh VPS → clone → install → dashboard.
- **Existing Docker** — containers keep running; Compose projects unchanged.
- **Existing Nginx** — remains functional; config unchanged unless authorized.
- **Existing database** — untouched.
- **Port collision** — installer detects it and chooses a safe alternative.
- **Monitoring failure** — does not stop application services.
- **Cloud failure** — Pulse Cloud outage does not affect VPS applications.
- **Agent failure** — does not affect application services.
- **Uninstall** — removes Pulse without deleting existing infrastructure.
- **Rollback** — every Pulse-created change can be reverted.

The [non-destructive test suite](../tests/README.md) asserts these in CI by taking
a full system snapshot before install and diffing it after.

---

## The design question

For every implementation decision we ask:

> **"Could this unexpectedly modify or break an existing production VPS?"**

If yes: redesign it, make it read-only, isolate it, require explicit
authorization, add backup/rollback, or remove the feature.

> **Observe first. Change nothing by default. Explain every change. Back up before
> changing. Validate before applying. Roll back when necessary.**
