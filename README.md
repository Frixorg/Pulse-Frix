<div align="center">

# Pulse

### Non-destructive VPS auto-discovery, monitoring & observability

**Install Pulse on any VPS and immediately understand everything running on it — without changing a thing.**

[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](./LICENSE)
[![CI](https://img.shields.io/badge/CI-github--actions-brightgreen.svg)](./.github/workflows/ci.yml)
[![Safety Model](https://img.shields.io/badge/safety-observe--first-success.svg)](./docs/SAFETY_MODEL.md)

</div>

---

## What is Pulse?

Pulse is an **observability platform**, not a VPS management tool. You clone the
repo onto an existing VPS, run one command, and receive a complete dashboard that
**automatically discovers** your infrastructure — services, containers, reverse
proxies, databases, processes, networking, storage, TLS certificates and domains —
then continuously monitors them **as an additional layer** that never disrupts what
is already running.

Pulse runs in two modes that share the same agent and the same code:

| Mode | Where the dashboard lives | Best for |
|------|---------------------------|----------|
| **Self-hosted** | On your own VPS, under your own domain | Full control, air-gapped, single machine |
| **Pulse Cloud** | `pulse.frix.me`, your VPS connects out | Fleet view, many servers, zero inbound ports |

---

## The safety model (read this first)

> **Pulse is designed as a non-destructive observability layer.**
>
> It discovers and monitors existing infrastructure without replacing or
> reconfiguring your applications. Existing configuration is **read-only by
> default**. Any configuration change requires **explicit authorization**, is
> **previewed as a diff**, **validated** before application, **backed up** before
> modification, and is **rollback-capable**.

We do **not** claim "installation can never affect your VPS." Instead we enforce a
formal [Safety Contract](./docs/SAFETY_MODEL.md): every install runs in three
phases — **Discover → Plan → Apply** — and the default `SAFE MODE` never modifies a
single existing service, container, config file, port, firewall rule or database.

The 20 [Golden Rules](./docs/SAFETY_MODEL.md#golden-rules) are non-negotiable and
enforced in code and CI.

---

## Quick start

### Self-hosted

```bash
git clone https://github.com/frix-me/pulse.git
cd pulse
./installer/install.sh                 # discovers, shows a plan, asks before applying
```

Or the hosted convenience installer (still runs discovery + plan + confirmation —
it never blindly executes privileged operations):

```bash
curl -fsSL https://install.frix.me/install.sh | bash
```

### Connect to Pulse Cloud

```bash
# 1. In the Pulse dashboard, generate a short-lived enrollment token
# 2. On the VPS:
./installer/install.sh --mode cloud --enrollment-token pst_xxx
```

The VPS makes an **outbound** TLS/WebSocket connection to the cloud. No inbound
management port is ever required or opened.

---

## What you get

```text
Dashboard   Overview: "Is my infrastructure healthy?" in ~5 seconds
Servers     CPU / RAM / disk / network / uptime, historical graphs
Services    Health engine: HEALTHY / DEGRADED / DOWN / UNKNOWN
Containers  Live CPU/mem/net/IO, restarts, health, ports, mounts, redacted env
Domains     DNS, HTTP(S) status, latency, TLS validity + expiry
Network      Interfaces, connections, topology graph from real discovery
Storage     Capacity, inodes, IOPS, retention, disk-pressure detection
Databases   PostgreSQL / MySQL / Redis / MongoDB metrics via read-only checks
Alerts      Debounced, deduplicated, dependency-aware, with recovery events
Security    SSH exposure, public DB ports, weak/expired TLS, Docker exposure
```

Under the hood Pulse uses a **battle-tested monitoring stack** (Prometheus,
node-exporter, cAdvisor, Alertmanager, Grafana) isolated on its own network — but
users never need to understand Prometheus or Grafana. See [MONITORING.md](./docs/MONITORING.md).

---

## Architecture at a glance

```text
                       ┌───────────────────────────────────────┐
                       │  Dashboard (Vue 3 + TS + Vite)         │
                       └───────────────┬───────────────────────┘
                                       │  REST + WebSocket
                       ┌───────────────▼───────────────────────┐
   Self-hosted ───────▶│  Pulse API (Go) — control plane        │
   Cloud ─── outbound ▶│  auth · RBAC · multi-tenancy · audit   │
                       └───────────────┬───────────────────────┘
                                       │  agent protocol (versioned, signed)
                       ┌───────────────▼───────────────────────┐
                       │  Pulse Agent (Go, single binary)       │
                       │  discovery · metrics · health · redact │
                       └───────────────┬───────────────────────┘
             read-only inspection ┌────┴────┬─────────┬─────────┐
                                  ▼         ▼         ▼         ▼
                               Docker      OS       Nginx    Databases
```

Full detail: [ARCHITECTURE.md](./docs/ARCHITECTURE.md).

---

## Technology

| Layer | Choice |
|-------|--------|
| Agent | **Go** — single static binary, low footprint |
| Backend API | **Go** — strongly typed control plane |
| Dashboard | **Vue 3 + TypeScript + Vite + Tailwind + ECharts** |
| Control-plane DB | **PostgreSQL** |
| Metrics | **Prometheus-compatible** (Prometheus + exporters) |
| Visualization engine | **Grafana-compatible** (internal component) |
| Deployment | **Docker Compose** |

---

## Repository layout

```text
pulse/
├── agent/           Go agent + discovery engine + `pulse` CLI
├── apps/
│   ├── api/         Go control-plane API
│   └── dashboard/   Vue 3 dashboard
├── packages/
│   ├── protocol/    Shared agent↔server protocol (JSON schema)
│   └── types/       Shared TypeScript types
├── discovery/       Discovery reference specs & fixtures
├── monitoring/      Prometheus / Grafana / exporters compose + configs
├── installer/       Safe installer (discover → plan → apply) + uninstall
├── infrastructure/  Dockerfiles, root compose, deploy helpers
├── tests/           Non-destructive test suite + snapshot verifier
├── docs/            Architecture, threat model, protocol, API, safety…
├── examples/        Example environments & configs
└── scripts/         Dev/build/release helper scripts
```

---

## Documentation

- [ARCHITECTURE.md](./docs/ARCHITECTURE.md) — system design
- [SAFETY_MODEL.md](./docs/SAFETY_MODEL.md) — the safety contract & golden rules
- [THREAT_MODEL.md](./docs/THREAT_MODEL.md) — actors, threats, mitigations
- [SECURITY.md](./SECURITY.md) — reporting & security posture
- [DISCOVERY.md](./docs/DISCOVERY.md) — how detection works
- [MONITORING.md](./docs/MONITORING.md) — the metrics stack
- [AGENT_PROTOCOL.md](./docs/AGENT_PROTOCOL.md) — agent ↔ server protocol
- [API.md](./docs/API.md) — REST API reference
- [DATA_MODEL.md](./docs/DATA_MODEL.md) — entities & database schema
- [SELF_HOSTING.md](./docs/SELF_HOSTING.md) · [CLOUD_MODE.md](./docs/CLOUD_MODE.md)
- [PRIVACY.md](./docs/PRIVACY.md) — exactly what data is collected
- [INSTALL.md](./docs/INSTALL.md) · [TROUBLESHOOTING.md](./docs/TROUBLESHOOTING.md)
- [DEVELOPMENT.md](./docs/DEVELOPMENT.md) · [CONTRIBUTING.md](./CONTRIBUTING.md)
- [ROADMAP.md](./docs/ROADMAP.md) — phased delivery plan

---

## Project status

Pulse is under active development. See [ROADMAP.md](./docs/ROADMAP.md) for the
phased plan and [the acceptance criteria](./docs/SAFETY_MODEL.md#acceptance-criteria)
that gate "done."

## License

[GPL-3.0](./LICENSE). Pulse is open source and intended for public deployment.
