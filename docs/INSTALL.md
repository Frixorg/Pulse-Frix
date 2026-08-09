# Installation

Pulse installs as a **transaction** with the Discover → Plan → Apply safety
contract. It never blindly executes privileged operations.

## Prerequisites

- Linux (Ubuntu/Debian/RHEL family), amd64 or arm64
- `bash`, `curl`, `openssl`
- Docker + Docker Compose (recommended)
- root or sudo for the *apply* phase only (discovery is unprivileged)

## Option A — clone (recommended)

```bash
git clone https://github.com/frix-me/pulse.git
cd pulse
./installer/install.sh
```

## Option B — hosted convenience installer

```bash
curl -fsSL https://install.frix.me/install.sh | bash
```

The hosted script is checksum-pinned and still performs discovery, prints a plan,
and asks for confirmation. It never runs privileged operations without your OK.

## What happens

```text
DISCOVERY → PRECHECK → PLAN → BACKUP → INSTALL → VERIFY → COMPLETE
```

1. **Discover** — read-only inspection of OS, Docker, Nginx, ports, disks, etc.
2. **Precheck** — validate OS, arch, permissions, disk, memory, ports, deps.
3. **Plan** — print exactly what will and will not happen; wait for confirmation.
4. **Backup** — snapshot the system state for verification/rollback.
5. **Install** — create isolated resources under `/opt/pulse` on a free port.
6. **Verify** — post-install health checks.
7. **Complete** — print the report (ending in `Existing services modified: 0`).

## Flags

```text
--mode local|cloud            deployment mode (default: local)
--domain <fqdn>               self-hosted domain (optional)
--enrollment-token <token>    cloud enrollment (short-lived)
--dashboard-port <port>       override the auto-selected port
--yes                         skip the confirmation prompt (CI only)
--dry-run                     run Discover + Plan, never Apply
```

## Verifying

```bash
pulse doctor
```

```text
✓ OS supported     ✓ Docker available   ✓ systemd available
✓ network available ✓ DNS available     ✓ disk space sufficient
✓ required ports available ✓ monitoring healthy ✓ agent connected
```

## Failure & recovery

If any step fails, installation state becomes `FAILED` with exact recovery
information, and rollback removes only what this run created (from the manifest).
See [TROUBLESHOOTING.md](./TROUBLESHOOTING.md).
