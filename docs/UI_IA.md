# UI Information Architecture

The dashboard is a **first-class product**, not a generic admin panel. It should
feel professional, modern, clean, technical, and fast — information-dense without
being overwhelming. An operator should understand infrastructure state in **~5
seconds**.

Prioritize: **status · health · trend · action**. Avoid excessive gradients,
meaningless animation, giant cards, clutter, and decorative graphics.

Reference implementation: [`apps/dashboard`](../apps/dashboard).

---

## Navigation

```text
Dashboard · Servers · Services · Containers · Runtimes · Domains ·
Network · Storage · Databases · Logs · SSH · Alerts · Metrics ·
Infrastructure · Security · Integrations · Settings
```

---

## Main dashboard (overview)

Answers "Is my infrastructure healthy?" immediately. Top cards: **VPS Health, CPU,
RAM, Disk, Network, Uptime**. Then rollups:

```text
Services   17 healthy · 2 degraded · 1 down
Containers 21 running · 1 unhealthy
Domains    8 online · 1 SSL expiring
Alerts     3 critical · 5 warning
```

Then live graphs (CPU, Memory, Network, Disk, Load) and a recent-events feed.

## Server page

OS/CPU/RAM/disk summary; health badge; usage bars; RX/TX. Time ranges: `1h, 6h,
24h, 7d, 30d, custom`.

## Service page

Per-service page with only the metrics that actually exist (never invented).
Example (PostgreSQL): status, uptime, connections, queries/sec, cache hit ratio;
CPU/memory/connections/latency/errors graphs.

## Container page

Status, image, uptime, CPU, memory, network RX/TX, restarts, health — plus logs,
**redacted** environment, mounts, networks, ports, health checks, dependencies.

## Domains

Per domain: online state, HTTP status, latency, TLS validity, days-to-expiry.

## Service topology

A graph generated from real discovery data (never invented):

```text
Internet → Nginx → { Frontend, API → { PostgreSQL, Redis } }
```

Dependency-aware: shows when a service is down while its dependencies are healthy.

## Security view

Read-only findings (SSH exposed, HTTP-without-HTTPS, expired/weak TLS, public
DB/Redis port, Docker exposure, unexpected listeners) with recommendations —
**never auto-changes** anything.

---

## First-run experience

```text
Welcome to Pulse — How would you like to use Pulse?
  [ Connect to Pulse Cloud ]   [ Self-host Pulse ]
```

- **Cloud:** login → generate enrollment token → connect VPS → discovery →
  dashboard.
- **Self-host:** enter domain → check domain → continue → discovery → dashboard.

Discovery progress, then:

```text
What would you like to monitor?
  ☑ System  ☑ Docker  ☑ Nginx  ☑ Domains  ☑ Databases  ☑ Applications
```

Everything defaults to **safe** monitoring.

### Permission transparency

Before requesting privileged access the UI explains what Pulse will inspect
(Docker metadata, system metrics, service status, network info) and what it will
**not** do (modify apps, access secrets, modify databases, change firewall). The
copy must accurately reflect the implementation.

---

## Error UX

Never show `ERROR 500`. Show actionable messages, e.g.:

```text
Docker monitoring unavailable
Pulse could not access the Docker API.
Possible reason: Docker socket permissions.
Your existing Docker services are unaffected.
[View diagnostics]
```

---

## Live data, responsiveness & performance

- Live updates via WebSocket/SSE; update individual components (`CPU 72% → 74%`),
  never full-page reloads.
- Responsive for Desktop (primary), Laptop, Tablet, Mobile. Mobile still shows
  server health, service health, alerts, and critical metrics.
- Stays responsive with 100+ services / 500+ containers via pagination,
  aggregation, sampling, lazy loading, virtualized lists, and caching. Never ship
  massive datasets to the browser.

---

## Design tokens

Consistent tokens (color, spacing, typography, radius, elevation) live in
`apps/dashboard/src/styles/tokens.css` and drive a dark, technical theme. Status
colors: healthy (green), degraded (amber), down (red), unknown (grey).
