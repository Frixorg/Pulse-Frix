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
├── Systemd Detector            unit list + active state
├── Nginx Detector              vhosts, upstreams, TLS, listen ports
├── Apache Detector             vhosts, modules
├── Caddy Detector              sites, automatic TLS
├── Traefik Detector            routers, services, entrypoints
├── PostgreSQL Detector         reachability (ro), version
├── MySQL/MariaDB Detector      reachability (ro)
├── Redis Detector              PING, INFO (ro)
├── MongoDB Detector            reachability (ro)
├── Node.js / Python / Java / Go / PHP Detectors  runtime + framework heuristics
├── Process Detector            top CPU/mem, zombies, failed services
├── Port Detector               listening sockets + free-port finder
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
