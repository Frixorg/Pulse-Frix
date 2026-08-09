# Tests

Pulse's promise — *observe first, change nothing by default* — is verified, not
asserted. Tests live with their components plus a repo-level non-destructive
suite that gates CI.

## Layout

```text
agent/internal/**/**_test.go     Go unit tests (discovery engine, redaction, cache)
apps/api/internal/**/**_test.go  Go unit + HTTP tests (auth, tenant isolation, SSRF)
tests/non-destructive/           The install-safety suite (this directory)
```

## Unit tests

```bash
cd agent && go test ./...
cd apps/api && go test ./...
```

Highlights:

- **redaction** — secrets in connection strings / env / JWT / PEM are redacted
  before leaving the agent (`agent/internal/redact`).
- **graceful degradation** — a panicking or unavailable detector never fails the
  discovery run (`agent/internal/discovery`).
- **offline backpressure** — the bounded buffer drops old low-priority data and
  protects critical items (`agent/internal/cache`).
- **tenant isolation** — no cross-org read/delete is possible
  (`apps/api/internal/store`).
- **SSRF** — loopback/link-local/RFC1918/metadata/Docker addresses are blocked
  (`apps/api/internal/ssrf`).
- **auth flow** — unauthenticated access is rejected; login → session works
  (`apps/api/internal/httpx`).

## Non-destructive suite (the most important CI test)

`tests/non-destructive/run.sh` proves the installer does not modify existing
system state.

- **Fast path (default, always in CI):** snapshots the system, runs the installer
  in `--dry-run` (Discover + Plan only), snapshots again, and fails on ANY diff.
  This proves Discover/Plan are truly read-only.
- **Full path (`PULSE_FULL_TEST=1`, requires Docker):** seeds an *existing*
  environment (nginx, redis, a network), runs a full install, and asserts those
  resources are byte-identical afterwards — then uninstalls and re-checks.
  Pulse's own `pulse-*` resources are excluded, so the test measures impact on
  *existing* infrastructure only.

```bash
bash tests/non-destructive/run.sh              # fast
PULSE_FULL_TEST=1 bash tests/non-destructive/run.sh   # full (Docker)
```

The snapshot (`snapshot.sh`) covers listening ports, docker
containers/networks/images/volumes, failed systemd units, and hashes of
`/etc/nginx`. Any unexpected change fails CI (spec sections 61–62).
