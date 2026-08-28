# Discovery Engine

The discovery engine is a registry of independent **detectors** that inspect the
VPS **read-only** and emit structured inventory. It is the heart of "discover
first." No detector ever writes to the system it inspects.

Reference implementation: [`agent/internal/discovery`](../agent/internal/discovery).

---

## Detector interface

Every detector implements a common interface (Go):

```go
type Detector interface {
    ID() string                 // stable id, e.g. "docker"
    Name() string               // human name
    Version() string            // detector version
    Available(ctx) Availability // is this detector usable here?
    Detect(ctx) ([]Resource, error) // read-only inventory
    Health(ctx) HealthReport    // health of what it found
}
```

- `Available` lets a detector opt out cleanly (e.g. Docker socket absent) so the
  platform **degrades gracefully** instead of failing.
- `Detect` returns typed `Resource`s.
- `Health` maps discovered things to `HEALTHY | DEGRADED | DOWN | UNKNOWN`.

The engine runs detectors concurrently, bounds each with a timeout, isolates
panics, and merges results. One failing detector never fails the run.

---

## Detectors (v1)

```text
Discovery Engine
├── OS Detector                 os-release, kernel, arch, uptime, hostname
├── Docker Detector             daemon reachability, version, info
├── Container Detector          containers + stats (ro)
├── Compose Detector            compose files → project→service map (read-only)
├── Systemd Detector            running + failed units (systemctl, else cgroups)
├── SysV Init Detector          /etc/init.d scripts + matching live process
├── Nginx Detector              vhosts, upstreams, TLS, listen ports
├── Reverse Proxy Detector      Apache, Caddy, Traefik, HAProxy, /etc/hosts
├── PostgreSQL Detector         reachability (ro), version
├── MySQL/MariaDB Detector      reachability (ro)
├── Redis Detector              PING, INFO (ro)
├── MongoDB Detector            reachability (ro)
├── Node.js / Python / Java / Go / PHP Detectors  runtime + framework heuristics
├── SQLite Detector             database files held open by a live process
├── Process Detector            top CPU/mem, zombies, host vs container split
├── Port Detector               listening sockets + owning PID/unit/container
├── Network Detector            interfaces, connections, states
├── Filesystem Detector         mounts, capacity, inodes, IO
├── SSL Detector                cert chains, expiry
├── GPU Detector                presence + utilisation where available
└── Existing Monitoring Detector  Prometheus/Grafana/exporters already present
```

---

## Structured results

Results are typed structured data (already redacted). Example — a Docker container:

```json
{
  "type": "docker_container",
  "id": "abc123",
  "name": "my-api",
  "image": "example/api:latest",
  "status": "running",
  "ports": [{ "host": 8080, "container": 8080, "protocol": "tcp" }],
  "networks": ["app-network"],
  "volumes": ["/srv/app/data:/data"],
  "restart_policy": "unless-stopped"
}
```

Application detection is **heuristic** and never modifies application files.
Signals: running process, Docker image, container labels, open ports, package
manifests, systemd unit, reverse-proxy upstream, executable names. Environment
metadata is used only after redaction — **secrets are never surfaced**.

---

## Docker discovery

Docker is a first-class integration. Detected per container: status, uptime,
CPU/mem, network RX/TX, block IO, process count, restarts, health, image, ports,
networks, volumes, dependencies. Pulse prefers Docker's read-only APIs and **never
modifies a user's containers to monitor them**.

Compose files (`docker-compose.yml|yaml`, `compose.yml|yaml`) are parsed to map
project→service relationships and drive the topology graph — **never edited**.

---

## Nginx discovery

Nginx is treated as an existing critical service. Pulse reads and analyses
`/etc/nginx/nginx.conf`, `sites-enabled/*`, `sites-available/*` (never overwriting
them) to detect vhosts, domains, upstreams, proxy targets, TLS certs + expiry,
listen ports, HTTP/2, compression, caching, redirects. If richer instrumentation
would require modifying Nginx, Pulse does **not** do it automatically — it offers a
previewed, validated, backed-up, reversible change behind explicit confirmation.
See [SAFETY_MODEL.md](./SAFETY_MODEL.md).

---

## Other reverse proxies

Nginx has its own detector; **Apache, Caddy, Traefik and HAProxy** share the
Reverse Proxy Detector, and every one of them is parse-only — no config file is
rewritten and no proxy is ever reloaded.

| Engine | Read from | Extracted |
|--------|-----------|-----------|
| Apache | `/etc/apache2/{sites-enabled,sites-available,conf.d}`, `/etc/httpd/{conf.d,sites-enabled,vhosts.d}`, the main `httpd.conf` | `ServerName`, `ServerAlias`, `DocumentRoot`, `SSLEngine`, `SSLCertificateFile`, `ProxyPass` |
| Caddy | `/etc/caddy/Caddyfile`, `conf.d/*`, `sites-enabled/*` | site addresses, `reverse_proxy` upstreams, `root` |
| Traefik | `/etc/traefik/*.{yml,yaml,toml}`, `dynamic/*`, `conf.d/*` | every host in a ``Host(`…`)`` matcher |
| HAProxy | `/etc/haproxy/haproxy.cfg`, `conf.d/*` | frontends, `bind` (incl. TLS), `hdr(host)` ACLs, backends and their servers |
| hosts | `/etc/hosts` | static name → address mappings, minus the stock localhost entries |

Traefik is most often configured entirely through **Docker labels**, which never
reach a file the agent could parse. Those routers are derived in the API from
the container inventory instead, so a label-only Traefik still populates the
Domains view.

Every path is read under `PULSE_ROOTFS`, so a containerised agent with
`/:/host:ro` sees the operator's real configuration rather than its own image.

---

## Port → process attribution

Listening sockets come from the `/proc/net/*` tables. Attributing each one to
the process behind it — the correlation `ss -tulpn` and `lsof -i` perform — is
done by matching socket inodes against `/proc/<pid>/fd`, so it needs no binary
on the host and no shell.

From the owning process's cgroup the agent also learns its **systemd unit** and
**container id**, which is what separates host-native workloads from
containerised ones throughout the dashboard.

Two deployment details decide how much of this works:

- `/proc/net/*` is always the *caller's* network namespace. The agent therefore
  reads `/proc/1/net/*` when it is readable: with `pid: host` that is the host's
  namespace, and without it PID 1 is the same namespace anyway.
- Reading another process's descriptors needs root or `CAP_SYS_PTRACE`. Without
  it, ports are still discovered — they are reported as **unattributed** in the
  inventory rather than silently dropped.

For a host-installed agent running unprivileged, setting `PULSE_USE_SUDO=true`
lets exactly two read-only commands retry through `sudo -n`
(`systemctl list-units …` and `ss -tulpnH`), with a fixed argument vector and a
least-privilege rule in
[`infrastructure/pulse-discovery.sudoers`](../infrastructure/pulse-discovery.sudoers).
This is the only place in the agent that may escalate, it is off by default, and
the direct call is always tried first.

---

## Databases

Engines are found two ways:

- **By listening port**, correlated with the owning process, so a Postgres on
  5432 is reported together with the PID, unit or container behind it.
- **By open file**, for SQLite — which has no port at all. Candidate paths come
  from the descriptors live processes hold open, and each one is confirmed by
  reading its 16-byte `SQLite format 3` header. The database is never opened as
  a database and nothing is written.

Reachability is a read-only TCP connect. No application credentials are ever
required or used.

---

## Unified inventory

Each detector answers one question well; operators ask a different one. The API
correlates the snapshot into a single host-vs-container workload list at
`GET /servers/{id}/inventory` — see [API.md](./API.md#inventory).

---

## Service relationships and unused workloads

`GET /servers/{id}/service-audit` goes one step further: it derives which
services are connected to which, and flags the ones nothing appears to need.

The relationship graph uses only evidence the snapshot already contains — a
reverse-proxy upstream resolved to the container publishing that port, a shared
user-defined Docker network, a Compose project or `depends_on`, a socket
attributed to a process. Same rule as the topology graph: **no edge without
evidence**.

The "unused" half is explicitly heuristic. A stopped container holding disk is a
fact; "nothing routes here" is a lead. So every finding ships the observations
behind it, a confidence, and a reclaimable estimate, and the response states its
own blind spots — a single CPU sample, cumulative container counters, and
traffic over channels Pulse cannot see. Core infrastructure and the Pulse stack
itself are exempt from every rule.

Nothing in this path proposes or performs a change. It flags; the operator
decides. See [API.md](./API.md#service-audit) for the rule table and
[SAFETY_MODEL.md](./SAFETY_MODEL.md) for why it stops there.

---

## Topology & dependencies

The engine derives an infrastructure graph from **real** discovery data — Nginx
upstreams, Docker networks, Compose, ports, reverse-proxy config, health checks. It
**never invents relationships**. The graph powers dependency-aware health ("API is
DOWN; its dependency PostgreSQL is HEALTHY") and alert correlation ("root cause:
disk full → affected: Postgres, API, Nginx"). See
[UI_IA.md](./UI_IA.md#service-topology).

---

## Secret redaction

The redaction layer (`agent/internal/redact`) runs on all discovery output before
it is logged, stored, transmitted, or displayed. Patterns cover connection strings,
`KEY`/`SECRET`/`TOKEN`/`PASSWORD` env names, PEM blocks, JWTs, cloud credentials,
and common token formats. Redaction is applied at the source, in the agent.
