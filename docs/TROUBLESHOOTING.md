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

## Dashboard says "No servers yet" but the agent is running

The agent holds a credential the control plane no longer accepts. `docker logs
pulse-agent` shows repeated `metrics send failed` with an `unknown or revoked
agent` message.

The agent now repairs this itself: it treats a rejected credential as a
credential failure rather than a transient one, and re-binds over the signed
`/agents/reenroll` endpoint using the keypair it already holds. Give it a
minute; the buffered metrics flush once it is back.

If the log instead says **"the control plane no longer recognises this agent and
refused re-enrolment"**, the control plane holds no record of the key at all and
a human has to act:

```bash
# On the control plane: is it keeping data at all?
docker logs pulse-api 2>&1 | grep -i "in-memory store"
```

If that matches, the API was built without the pgx tag and forgets every agent
on restart. Rebuild it with `API_TAGS=pgx` (the default since this fix) and
re-run the install command on the monitored host with a fresh tracking key.

## SSH console: "server sent: publickey"

Password login is not disabled by Pulse — Pulse never edits `sshd_config`. The
host is refusing passwords for that user, usually from a hardening drop-in.

Two OpenSSH rules decide this, and both surprise people:

- `Include /etc/ssh/sshd_config.d/*.conf` sits at the **top** of `sshd_config` on
  Debian and Ubuntu, so the drop-ins are parsed **first**;
- the **first** value sshd reads for a keyword wins, so a later
  `PasswordAuthentication yes` in `sshd_config` changes nothing.

`PermitRootLogin prohibit-password` blocks passwords for root specifically,
whatever `PasswordAuthentication` says. See the effective values:

```bash
sudo sshd -T | grep -Ei 'permitrootlogin|passwordauthentication|pubkeyauthentication'
```

Connect with a private key, or as a non-root user the host still allows a
password. Pulse's inventory reports the resolved values along with the file each
one came from, so you know which file to edit.

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
