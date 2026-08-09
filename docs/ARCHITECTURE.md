# Architecture

Pulse separates cleanly into **agent**, **discovery**, **monitoring**, **backend**,
**frontend**, **installer**, and **infrastructure**. The two deployment modes
(self-hosted and cloud) share the agent and as much backend code as possible; the
only real difference is *where the control plane runs* and *who owns the data*.

---

## 1. System context

```text
                          ┌──────────────────────────────────────────┐
                          │              Browser (operator)           │
                          └───────────────────┬──────────────────────┘
                                              │ HTTPS (REST + WebSocket/SSE)
                          ┌───────────────────▼──────────────────────┐
                          │        Pulse Dashboard (Vue 3 SPA)        │
                          └───────────────────┬──────────────────────┘
                                              │
              LOCAL MODE                      │                CLOUD MODE
   ┌──────────────────────────┐               │      ┌───────────────────────────┐
   │ Self-hosted Pulse API     │◀─────────────┴─────▶│ Pulse Cloud API (multi-    │
   │ (single tenant)           │                      │ tenant, pulse.frix.me)    │
   └───────────┬──────────────┘                       └───────────┬───────────────┘
               │ agent protocol (versioned, authenticated, TLS)   │
               │                                                   │  (VPS dials OUT)
   ┌───────────▼───────────────────────────────────────────────────▼──────────────┐
   │                        Pulse Agent (Go, single binary)                          │
   │   Discovery · Metrics collection · Health checks · Secret redaction · Cache     │
   └───────────┬─────────────┬─────────────┬─────────────┬──────────────┬───────────┘
     read-only │             │             │             │              │
               ▼             ▼             ▼             ▼              ▼
            Docker API      /proc,      Nginx/Apache   Databases     Prometheus
           (ro socket)    sysfs, OS    /etc configs   (ro checks)    exporters
```

- **Self-hosted (LOCAL):** browser → self-hosted dashboard → local Pulse API →
  local agent. Everything stays on the VPS.
- **Cloud:** browser → `pulse.frix.me` → Pulse Cloud API → **outbound** connection
  from the VPS agent. **No inbound management port is required.**

The agent is written once and works with both. The self-hosted API and the cloud
API are the same Go codebase compiled with a `tenancy` mode: single-tenant vs
multi-tenant. See [CLOUD_MODE.md](./CLOUD_MODE.md) and [SELF_HOSTING.md](./SELF_HOSTING.md).

---

## 2. Components

### 2.1 Agent (`/agent`, Go)

A lightweight single binary. Responsibilities: discovery, metrics collection,
health checks, secure outbound communication, local caching, configuration, and
(disabled by default) update management. **The agent never executes arbitrary
commands received from the control plane** — it exposes a fixed capability set.

Capabilities are explicit and read-only by default:

```text
READ_DOCKER · READ_SYSTEM · READ_NETWORK · READ_NGINX · READ_METRICS
```

Write capabilities (future) are disabled by default, explicitly authorized,
scoped, audited and revocable.

### 2.2 Discovery engine (`/agent/internal/discovery`)

A registry of **detectors** implementing a common interface. Each detector is
independent and degrades gracefully — if Docker is unavailable, Docker discovery is
marked unavailable and everything else keeps working. See [DISCOVERY.md](./DISCOVERY.md).

### 2.3 Monitoring stack (`/monitoring`)

Prometheus + node-exporter + cAdvisor + Alertmanager + Grafana, isolated on
`pulse-net`. The stack is an *internal implementation detail*: users never need to
learn Prometheus/Grafana. The API abstracts it behind `/api/v1/metrics` so the
backend can evolve the storage engine later. See [MONITORING.md](./MONITORING.md).

### 2.4 Backend / control plane (`/apps/api`, Go)

Serves the REST API and WebSocket/SSE stream, owns the control-plane database
(PostgreSQL), authenticates users and agents, enforces RBAC and multi-tenancy,
proxies metric queries to the monitoring backend, runs the alert engine, and writes
the audit log. See [API.md](./API.md) and [DATA_MODEL.md](./DATA_MODEL.md).

### 2.5 Frontend / dashboard (`/apps/dashboard`, Vue 3 + TS)

A professional infrastructure monitoring UI (not a generic admin panel). Live
updates via WebSocket; component-level refresh, never full-page reloads. See
[UI_IA.md](./UI_IA.md).

### 2.6 Installer (`/installer`)

Implements the Discover → Plan → Apply safety contract in POSIX shell with
`set -euo pipefail`, port detection, an installation manifest and rollback. See
[SAFETY_MODEL.md](./SAFETY_MODEL.md).

---

## 3. Data planes

Pulse separates three storage concerns and never conflates them:

| Plane | Store | Contents |
|-------|-------|----------|
| **Control plane** | PostgreSQL | users, orgs, servers, agents, enrollment, permissions, alerts, settings, audit |
| **Metrics** | Prometheus TSDB | time-series samples (system, container, service) |
| **Logs** | Docker/journald + bounded buffer | recent, redacted logs (no giant log platform in v1) |

> We do **not** put millions of time-series samples into PostgreSQL simply because
> Postgres already exists.

---

## 4. Communication & security

- Agent↔server uses **TLS**, an **authenticated agent identity**, **short-lived
  credentials**, token/cert rotation, replay protection, and request signing where
  appropriate.
- The agent prefers **outbound** connections. No inbound management port.
- The cloud never has unrestricted shell access to any VPS — only the fixed
  capability set. See [THREAT_MODEL.md](./THREAT_MODEL.md).

---

## 5. Identity

Every VPS has cryptographically-generated identifiers — `server_id`, `agent_id`,
`installation_id` — never derived from hostname / public IP / MAC alone. See
[AGENT_PROTOCOL.md](./AGENT_PROTOCOL.md#identity).

---

## 6. Resilience & degradation

- **Offline-first agent:** if the cloud is unreachable, the agent keeps monitoring
  locally, buffers a bounded amount of metrics, and retries with exponential
  backoff. It never stops/restarts/disables anything because the cloud is down.
- **Graceful degradation:** one failing integration never fails the platform.
  Errors are classified (`DISCOVERY_ERROR`, `DOCKER_UNAVAILABLE`, …) and surfaced
  as friendly, actionable messages — never `ERROR 500`.
- **Backpressure:** when the server can't receive metrics, the agent buffers,
  compresses, batches, retries, and drops *old low-priority* data first. Critical
  health state is prioritized over high-frequency historical samples.

---

## 7. Observability of Pulse itself

Pulse exposes its own metrics: agent CPU/memory/network, API latency & errors, DB
latency, queue depth, metric ingestion & loss, and WebSocket connection counts.
Structured JSON logs with correlation/request IDs everywhere. See
[MONITORING.md](./MONITORING.md#self-observability).

---

## 8. Directory ↔ component map

| Directory | Component |
|-----------|-----------|
| `agent/` | Agent + discovery + `pulse` CLI |
| `apps/api/` | Control-plane API |
| `apps/dashboard/` | Vue dashboard |
| `packages/protocol/` | Agent↔server protocol (shared) |
| `packages/types/` | Shared TS types |
| `monitoring/` | Prometheus/Grafana/exporters |
| `installer/` | Safe installer + uninstall |
| `infrastructure/` | Dockerfiles, compose, deploy |
| `tests/` | Non-destructive + integration tests |

---

## 9. Roadmap alignment

The architecture is intentionally modular so the phased [ROADMAP](./ROADMAP.md) can
be delivered without compromising the safety architecture. Nothing in later phases
is allowed to weaken SAFE MODE or the golden rules.
