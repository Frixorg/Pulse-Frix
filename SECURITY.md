# Security Policy

Pulse is designed to be deployed on internet-facing production VPSs. Security is a
first-class product concern, not an afterthought. This document describes our
posture and how to report issues.

## Reporting a vulnerability

**Do not open a public issue for security vulnerabilities.**

Email **security@frix.me** with:

- a description of the issue and its impact,
- steps to reproduce (a proof-of-concept if possible),
- affected versions/commit,
- any suggested remediation.

We aim to acknowledge within 72 hours and to provide a remediation timeline after
triage. We support coordinated disclosure and will credit reporters who wish to be
credited.

## Supported versions

Until 1.0, the latest `main` and the most recent tagged release receive security
fixes.

## Security posture (summary)

- **No cloud → shell → VPS path.** The agent exposes only a fixed, read-only
  capability set (`READ_DOCKER`, `READ_SYSTEM`, `READ_NETWORK`, `READ_NGINX`,
  `READ_METRICS`). It never executes arbitrary commands from the control plane.
- **Non-destructive by default.** See [SAFETY_MODEL.md](./docs/SAFETY_MODEL.md).
- **Secret redaction** before logs, API responses, telemetry, storage, and UI.
- **Outbound-only agent.** No inbound management port required.
- **Multi-tenant isolation** enforced at API, service, and DB layers.
- **SSRF, SQLi, command-injection, and path-traversal** guards on all
  user-controlled input. See [THREAT_MODEL.md](./docs/THREAT_MODEL.md).
- **Least privilege.** The dashboard does not run as root. Privileged discovery is
  isolated from the unprivileged application.

## The Docker socket (please read)

Access to `/var/run/docker.sock` is **root-equivalent control over the host**.
Pulse:

- accesses Docker **read-only** (or through a read-only socket proxy that allowlists
  only the inspection endpoints Pulse actually uses — `/containers/json`,
  `/containers/*/stats`, `/networks`, `/volumes`, `/images/json`, `/version`,
  `/info`);
- runs the socket-facing component as a dedicated, unprivileged user in the
  `docker` group rather than root;
- **never** mounts the raw socket into arbitrary application containers;
- makes Docker access **opt-in** and clearly explains the implication during
  installation.

If you do not want to grant Docker access, Pulse still monitors system, network,
Nginx, databases and domains — Docker discovery is simply reported as unavailable.

## Handling secrets

- Secrets are generated at install time (`openssl rand`), never hardcoded, never
  committed. `.env` is git-ignored.
- The redaction layer is mandatory and tested.
- CI runs secret scanning; committing keys/tokens/passwords fails the build.

## Supply chain

Pinned dependencies + lockfiles, dependency/container scanning, SBOM generation,
signed & checksummed releases, and provenance. See
[THREAT_MODEL.md](./docs/THREAT_MODEL.md#t10-supply-chain).
