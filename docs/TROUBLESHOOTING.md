# Troubleshooting

Start with:

```bash
pulse doctor        # environment + health checks
pulse status        # what Pulse is running
pulse logs          # recent, redacted Pulse logs
```

## Docker monitoring unavailable

```text
Pulse could not access the Docker API.
Possible reason: Docker socket permissions.
Your existing Docker services are unaffected.
```

Fixes: ensure Docker is running (`systemctl status docker`); the Pulse agent user
must be in the `docker` group or have read access to the socket (or the read-only
socket proxy must be running). System/Nginx/DB monitoring keep working regardless.

## Port collision

The installer detects occupied ports and picks a free one. To force a port:

```bash
./installer/install.sh --dashboard-port 3211
```

## Dashboard not reachable

- Check the chosen port: `pulse status` shows the dashboard URL/port.
- If using a domain, confirm DNS resolves to this VPS: `pulse doctor` reports it.
- If Nginx fronts the dashboard, confirm the (Pulse-generated, confirmed) server
  block is enabled and `nginx -t` passes.

## Agent not connected (cloud)

- Enrollment tokens are short-lived and single-use — generate a fresh one.
- The VPS needs outbound HTTPS to `pulse.frix.me` (no inbound port needed).
- Check `pulse logs` for `enroll`/`heartbeat` errors.

## Disk pressure

Pulse caps its own metric storage (`METRICS_RETENTION`, `METRICS_MAX_DISK`) and
cleans up automatically. If the *host* disk is full, that is an infrastructure
condition Pulse reports (and correlates as a root cause) but never "fixes" by
deleting your data.

## Installation failed

State `FAILED` includes exact recovery info. Roll back what this run created:

```bash
pulse rollback
```

Rollback only removes resources in the installation manifest; existing
infrastructure is untouched.

## Still stuck?

Open an issue with `pulse doctor` output (already redacted). For **security**
issues, email security@frix.me instead — see [SECURITY.md](../SECURITY.md).
