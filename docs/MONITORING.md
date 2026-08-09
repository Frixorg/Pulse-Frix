# Monitoring Stack

Pulse uses **battle-tested open-source observability components** rather than
implementing a metrics database from scratch — but abstracts them so users never
need to understand Prometheus or Grafana, and so the storage engine can evolve.

Reference: [`monitoring/`](../monitoring).

---

## Components

```text
monitoring/                         (isolated on the pulse-net network)
├── prometheus       metrics storage + scraping + alert rules
├── alertmanager     debounce / group / route alerts
├── node-exporter    host system metrics
├── cadvisor         per-container metrics
└── grafana          internal visualization engine (not user-facing)
```

All monitoring containers are namespaced `pulse-*` and attached only to
`pulse-net`. Existing application networks are untouched.

---

## Isolation & resource limits

Monitoring must never become the VPS's performance problem:

```yaml
resources:
  cpu_limit: "1.0"
  memory_limit: "512M"
```

Limits adapt to VPS capacity (a 1 GB VPS gets a smaller budget). The stack has a
small footprint by design.

---

## Retention & disk safety

Metrics have configurable retention: `24h`, `7d`, `30d`, `90d`. Small VPSs default
to a conservative period (`7d`, capped by `METRICS_MAX_DISK`). Pulse implements:

- storage monitoring,
- retention limits,
- disk-pressure detection,
- automatic metric cleanup.

> Pulse never consumes the user's entire disk.

---

## Abstraction

The dashboard talks only to the Pulse API (`/api/v1/metrics`). The API translates
requests into PromQL and returns already-shaped series. Grafana remains an
**internal implementation component** (useful for power users and debugging, but
not required). This lets the metrics backend change later without touching the
frontend.

The API **only returns metrics that actually exist** and never invents metrics for
a service that doesn't expose them.

---

## Database metrics

Where possible Pulse uses official exporters/integrations, read-only:

- **PostgreSQL:** connections, transactions, queries, locks, cache hit ratio,
  replication, database size.
- **Redis:** memory, commands, connections, hit rate, evictions.
- **MySQL/MariaDB:** connections, queries, buffer pool, locks, replication.

Pulse **never modifies database configuration** and does not require application
credentials where a read-only check suffices.

---

## Alerting

The alert engine (in the API, backed by Alertmanager) supports debounce, cooldown,
grouping, severity, deduplication, and recovery notifications. Severities:
`INFO | WARNING | CRITICAL`. Representative rules:

```text
CPU > 90% for 10m                 WARNING
RAM > 90% for 5m                  WARNING
Disk > 85% / > 95%                WARNING / CRITICAL
Service DOWN                      CRITICAL
Container unhealthy / restarting  WARNING
HTTP 5xx spike / latency spike    WARNING
TLS cert < 30d / < 7d             WARNING / CRITICAL
Filesystem inode exhaustion       CRITICAL
Server unreachable                CRITICAL
```

**Alert correlation** is dependency-aware: when disk fills and Postgres, API, and
Nginx all fail, Pulse surfaces a single *root cause* ("disk 100%") with an
*affected* list instead of four unrelated pages.

---

## Self-observability

Pulse monitors itself: agent CPU/memory/network, API latency & errors, DB latency,
queue depth, metric ingestion & loss, and WebSocket connection counts. These are
exposed as internal metrics and visible in the *Infrastructure → Pulse* view.

---

## Failure handling

If Docker is unavailable, container metrics are marked unavailable while system,
network, Nginx, and database monitoring continue:

```text
Docker monitoring: unavailable
System monitoring: healthy
Nginx monitoring:  healthy
```

One integration being down never fails the whole platform.
