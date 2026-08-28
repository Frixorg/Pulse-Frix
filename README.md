<div align="center">

# PulseFrix

### Observe everything. Change nothing.

**Non-destructive VPS auto-discovery, monitoring & observability.**
Point PulseFrix at any server and, in seconds, see everything running on it — containers, databases, reverse proxies, domains, TLS, ports, processes, disks and traffic — monitored continuously, read-only, without touching a thing.

[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](./LICENSE)
[![Made with Go](https://img.shields.io/badge/agent%20%2B%20api-Go-00ADD8.svg)](https://go.dev)
[![Dashboard](https://img.shields.io/badge/dashboard-Vue%203%20%2B%20TS-42b883.svg)](https://vuejs.org)
[![Safety](https://img.shields.io/badge/safety-observe--first-c7f542.svg)](./docs/SAFETY_MODEL.md)

</div>

---

## What is PulseFrix?

PulseFrix is an **observability platform for a single VPS or a fleet of them**. You run one command on a server; a tiny Go **agent** inspects it read-only and streams what it finds to a dashboard. There is no manual configuration of what to watch — it's **auto-discovered**.

It runs in two modes that share the same agent and codebase:

| Mode | Where the dashboard lives | Best for |
|------|---------------------------|----------|
| **PulseFrix Cloud** | Central dashboard; each VPS dials **out** | Many servers, zero inbound ports, sign in with Google/Telegram |
| **Self-hosted** | The full stack on your own VPS + domain | One machine, full control, your data never leaves it |

The agent is **outbound-only** in cloud mode — it opens **no inbound port** and every message it sends is signed with a per-agent key.

---

## What PulseFrix actually *does* — and doesn't

Everything PulseFrix does is **read-only inspection**. It never starts, stops, edits or reconfigures your workloads.

**It reads:**

- **Docker** (socket, read-only): container list, state, image, live CPU/mem/net stats, ports, mounts, and — for the security view — `Privileged`, `IpcMode` and password-shaped env-var *names*.
- **The OS** (`/proc`, `/sys`): CPU, load, memory, swap, disk usage (via the host rootfs), network interfaces and counters, uptime, processes and listening ports.
- **Nginx** config (read-only): virtual hosts, `server_name`, listen/TLS, `proxy_pass` upstreams, and security directives (`add_header`, `server_tokens`, `limit_req`).
- **TLS certs**: issuer, expiry and days-left for discovered domains.
- **SSH** (`sshd_config`): `PermitRootLogin`, `PasswordAuthentication`, `PermitEmptyPasswords`, weak ciphers/MACs/KEX, and reused `authorized_keys`.
- **Container logs** (read-only): the last lines per running container, to stream in the dashboard.

**It never:** modify a container, write to the Docker socket, edit Nginx/app config, open a firewall port, change SSH, or touch your databases. The install is **non-destructive by default** — see the [Safety Model](./docs/SAFETY_MODEL.md).

The one exception is the opt-in **[SSH console](./docs/SSH_CONSOLE.md)**, where *you* type commands in a real terminal. Even then the agent is not involved: it stays read-only and still never accepts a command from the control plane.

---

## What you get

| Page | What it shows |
|------|---------------|
| **Dashboard** | "Is my machine healthy?" — health, CPU/RAM/disk/network/uptime, service & container counts, live CPU/memory charts, recent events |
| **Containers** | Every container (running or not), image, state, live CPU/mem, ports, detected engine |
| **Domains** | Vhosts from your reverse proxy — proxy target, TLS state + expiry, clickable to open |
| **Databases** | Postgres, MySQL/MariaDB, Redis, Mongo, and more — detected by port **and container image** |
| **Network** | Throughput chart, totals, primary interface, per-interface counters (noise filtered) |
| **Storage** | Capacity donut, used/free, per-filesystem usage bars and inodes |
| **Inventory** | Everything running on the box in one list — host services and containers side by side, with every listening port attributed to the process, unit or container that owns it. Sockets nobody could be found for are shown, not hidden |
| **Service Audit** | What talks to what, and what nothing seems to need — proxy routes, shared networks and Compose links, plus flags for stopped containers still on disk, services nothing routes to, idle workloads and orphaned volumes. Every flag shows its evidence and its confidence, core infrastructure is exempt, and **nothing is ever removed** |
| **Metrics** | CPU, memory, disk, network, load — **all at once**, live, drag-to-reorder, 1h→30d ranges, history |
| **Logs** | Pick a container and **watch its logs live**; search, copy, and export — always escaped, never raw HTML |
| **Alerts** | Define rules (metric threshold or container-down) with a for-duration + severity; live **pop-up** when one fires |
| **SSH** | A real terminal on the server, in the browser — like PuTTY, with your own credentials. One click sets up key access for you. The agent is not involved and stays read-only. See [SSH Console](./docs/SSH_CONSOLE.md) |
| **Security** | A full read-only audit: exposure, TLS, base images, resource limits, **SSH hardening, privileged flag, blank/default credentials, shared SSH keys, shared memory, cipher suites, security headers, information leakage, rate limiting** — each with the exact resource, why it matters, and a fix. Re-run all or one check, filter by any mix of severities, and export the result as Markdown, CSV or JSON. |

---

## Quick start

### PulseFrix Cloud

1. Sign in at your PulseFrix Cloud dashboard (Google or Telegram).
2. Click **Add server** → generate a short-lived key.
3. On the VPS you want to watch:

```bash
git clone https://github.com/Frixorg/Pulse-Frix.git pulse && cd pulse
sudo PULSE_API_URL=https://<your-cloud> bash installer/install.sh \
  --mode cloud --enrollment-token pst_xxx
```

The agent dials out, enrolls, and your server appears automatically. No inbound port is opened.

### Self-hosted

```bash
git clone https://github.com/Frixorg/Pulse-Frix.git pulse && cd pulse
sudo bash installer/install.sh --mode local --domain pulse.example.com
```

The installer discovers your infrastructure read-only, shows a plan, then brings up the full stack (dashboard, API, PostgreSQL, monitoring) on an **isolated** Docker network — nothing existing is touched. It prints an admin email + password once; sign in at `https://pulse.example.com`.

---

## Architecture

```text
   Vue 3 + TS dashboard  ──REST──▶  Pulse API (Go)  ──signed protocol──▶  Pulse Agent (Go)
        (ECharts)                    auth · RBAC ·                          read-only discovery
                                     multi-tenant · alerts                  + metrics + logs
                                          │                                        │
                                     PostgreSQL                         Docker · OS · Nginx · SSH
                                     Prometheus-compatible
```

| Layer | Tech |
|-------|------|
| Agent & API | **Go** (single static binaries) |
| Dashboard | **Vue 3 + TypeScript + Vite + Tailwind + ECharts** |
| Control-plane DB | **PostgreSQL** |
| Deployment | **Docker Compose** |

Repository layout: `agent/` (Go agent + detectors), `apps/api/` (Go control plane), `apps/dashboard/` (Vue), `installer/` (safe install/uninstall), `infrastructure/` (compose + `deploy.sh`), `docs/`.

---

## Safety model (read this first)

Every install runs **Discover → Plan → Apply**, and the default **SAFE MODE** never modifies an existing service, container, config file, port, firewall rule or database. Removing a monitored server never touches the VPS; a self-contained cleanup command removes only the agent. See the [Safety Model & Golden Rules](./docs/SAFETY_MODEL.md).

---

## Contributing

Contributions are welcome. Good first steps:

1. Read [CONTRIBUTING.md](./CONTRIBUTING.md) and [DEVELOPMENT.md](./docs/DEVELOPMENT.md).
2. Run the stack locally with Docker Compose (`infrastructure/`).
3. The agent is stdlib-only Go (no external deps) so it always builds; the API uses `-tags pgx` for PostgreSQL.
4. Open an issue describing the change before large PRs.

## License

[GPL-3.0](./LICENSE). PulseFrix is open source and intended for public deployment.
