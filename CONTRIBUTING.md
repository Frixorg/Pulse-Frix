# Contributing to Pulse

Thanks for helping build a safe, non-destructive observability platform.

## Ground rules

Pulse's promise is that it **observes first and changes nothing by default**. Every
contribution is judged against that promise. Before opening a PR, ask:

> **"Could this unexpectedly modify or break an existing production VPS?"**

If the answer is yes, the change must be redesigned to be read-only, isolated,
explicitly authorized, backed-up, and reversible — or it does not merge. The
[Golden Rules](./docs/SAFETY_MODEL.md#golden-rules) are non-negotiable.

## Workflow

1. Fork and branch from `main` (`feat/…`, `fix/…`, `docs/…`).
2. Read [DEVELOPMENT.md](./docs/DEVELOPMENT.md) and set up locally.
3. Make focused changes with tests.
4. Run `make lint && make test` (and `make test-nondestructive` for
   installer/discovery changes).
5. Update docs (including [THREAT_MODEL.md](./docs/THREAT_MODEL.md) if you change
   the attack surface).
6. Open a PR describing *what* changed and *why it is safe*.

## Commit style

Conventional Commits: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`,
`security:`. Keep the subject ≤ 50 chars.

## Code standards

- No raw secrets; route user-facing data through the redaction layer.
- Parameterised SQL; no shell string construction; canonical path resolution;
  SSRF guard on server-side fetches.
- Structured JSON logs, never logging secrets.
- Tests for discovery logic, safety-critical paths, and API handlers.
- Pin dependencies; keep lockfiles updated; no unpinned remote scripts.

## Security

Never commit keys/tokens/passwords — CI secret-scanning will fail the build.
Report vulnerabilities privately to **security@frix.me** (see
[SECURITY.md](./SECURITY.md)); do not open a public issue.

## Reviews

PRs touching the installer, agent capabilities, the Docker socket path, or the
protocol require review from a maintainer and a passing non-destructive test run.
