# Development

## Layout

```text
agent/            Go agent + discovery + `pulse` CLI  (go.mod)
apps/api/         Go control-plane API                (go.mod)
apps/dashboard/   Vue 3 + TS + Vite dashboard         (package.json)
packages/         Shared protocol + TS types
monitoring/       Prometheus/Grafana/exporters
installer/        Safe installer + uninstall
tests/            Non-destructive + integration tests
```

## Prerequisites

- Go ≥ 1.22
- Node ≥ 20 + pnpm (or npm)
- Docker + Docker Compose
- `make`

## Common tasks

```bash
make help            # list targets
make dev             # run API + dashboard + monitoring locally
make build           # build agent, api, dashboard
make test            # unit + integration tests
make lint            # golangci-lint + eslint + typecheck
make agent           # build the agent binary into ./bin
make dashboard       # build the SPA into apps/dashboard/dist
```

### Agent

```bash
cd agent
go run ./cmd/pulse discover      # print a discovery snapshot (read-only)
go run ./cmd/pulse doctor
go test ./...
```

### API

```bash
cd apps/api
go run ./cmd/pulse-api           # needs DATABASE_URL + METRICS_URL (see .env)
go test ./...
```

### Dashboard

```bash
cd apps/dashboard
pnpm install
pnpm dev                         # Vite dev server, proxied to the API
pnpm build
pnpm typecheck && pnpm lint
```

## Coding standards

- **Safety first.** For every change ask: *"Could this unexpectedly modify or break
  an existing production VPS?"* If yes, redesign / make read-only / isolate /
  require authorization / add backup+rollback / remove it.
- No raw secrets anywhere; run everything user-facing through the redaction layer.
- No shell string construction; use structured APIs / arg arrays / allowlists.
- Parameterised SQL only. Canonical path resolution only. SSRF guard on all
  server-side fetches.
- Structured JSON logs with request/correlation IDs. Never log secrets.
- Tests accompany discovery logic, safety-critical code, and API handlers.

## Local monitoring stack

```bash
docker compose -f monitoring/docker-compose.monitoring.yml up -d
```

See [MONITORING.md](./MONITORING.md).
