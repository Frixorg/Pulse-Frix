# Roadmap

Pulse is delivered in phases. **Future features must never compromise the safety
architecture** (SAFE MODE, the golden rules, or the capability model).

The central workflow is always:

```text
CLONE → DOCTOR → DISCOVER → UNDERSTAND → PLAN → CONFIRM → INSTALL MONITORING → VERIFY → MONITOR
```

never `CLONE → INSTALL EVERYTHING → HOPE NOTHING BREAKS`.

---

## Phase 1 — Foundation (MVP)

- Go agent (single binary) + discovery engine
- System discovery + metrics (CPU, memory, disk, network, processes, system)
- Docker discovery + container metrics
- Nginx discovery (read-only)
- Secret redaction layer
- Safe installer (Discover → Plan → Apply, SAFE MODE, port detection, manifest)
- `pulse` CLI (`doctor`, `discover`, `status`, …)
- Basic dashboard (overview, servers, containers)
- Monitoring stack (Prometheus, node-exporter, cAdvisor) isolated on `pulse-net`

## Phase 2 — Health, domains, alerts

- Service health engine (`HEALTHY/DEGRADED/DOWN/UNKNOWN`)
- Domain monitoring (DNS, HTTP(S), latency, TLS + expiry) behind the SSRF guard
- Alert engine (debounce, cooldown, grouping, dedup, recovery) + Alertmanager
- Grafana integration (internal)
- Database metrics via read-only exporters

## Phase 3 — Cloud & multi-tenant

- Pulse Cloud (`pulse.frix.me`) with outbound agent enrollment
- Authentication (email/password → OAuth/OIDC-ready, MFA-ready)
- Multi-server, multi-user
- RBAC (owner/admin/viewer) enforced at API/service/DB
- Multi-tenant isolation + audit log

## Phase 4 — Topology & self-hosted domain automation

- Service topology graph from real discovery data
- Dependency mapping + dependency-aware alert correlation
- Self-hosted domain automation (previewed, validated, reversible reverse-proxy
  changes behind `ENABLE_AUTO_TLS` / `ENABLE_CONFIG_MUTATION`)
- Advanced integrations

## Phase 5 — Advanced insights

- Advanced security insights (beyond the v1 "obvious risks" view)
- Advanced log analytics
- Advanced (still safe, still opt-in) automation

---

## Definition of done

Each phase must keep passing the [acceptance criteria](./SAFETY_MODEL.md#acceptance-criteria)
and the non-destructive CI test that snapshots the system before install and diffs
it after. A feature that cannot pass those does not ship.
