# Deployment

This guide takes you from a fresh repo to (1) a smoke test, (2) a **self-hosted**
Pulse on your own VPS, and (3) the **Pulse Cloud** control plane at
`pulse.frix.me` with VPSs enrolled into it.

> **Do this first.** This codebase is new — get CI green and run the local smoke
> test before pointing it at production. Never run an unverified installer on a
> box you can't afford to disrupt (though SAFE MODE is designed exactly for that).

---

## 0. Prerequisites

- A Linux VPS (Ubuntu/Debian/RHEL), amd64 or arm64, with Docker + Docker Compose.
- `git`, `curl`, `openssl`.
- For cloud: a domain you control (`frix.me`) and the ability to add DNS records,
  plus a host for the control plane with ports 80 + 443 open.

Generate secrets with `openssl rand -hex 32` (the self-hosted installer does this
for you automatically).

---

## 1. Smoke test (5 minutes, throwaway box or laptop)

```bash
git clone https://github.com/frix-me/pulse.git
cd pulse

# Optional: spin up a fake "existing" environment to be discovered
docker compose -f examples/test-vps/docker-compose.yml up -d

# Prove the installer changes nothing during Discover + Plan
bash tests/non-destructive/run.sh
# Full proof (real install + verify existing survive + uninstall):
PULSE_FULL_TEST=1 bash tests/non-destructive/run.sh
```

Then a real self-hosted install (see §2). Open the dashboard, confirm it
discovers the demo containers, then `docker compose -f examples/test-vps/docker-compose.yml down -v`.

---

## 2. Self-hosted on your own VPS

On the VPS that runs your services:

```bash
git clone https://github.com/frix-me/pulse.git
cd pulse
sudo ./installer/install.sh --mode local            # or add: --domain monitor.example.com
```

The installer runs **Discover → Plan → Apply**, prints exactly what it will and
won't do, picks a free dashboard port, generates secrets into
`/opt/pulse/.env` (0600), and prints your **initial admin login once**.

Access the dashboard at `http://<vps-ip>:<port>` (the report shows the port). If
you passed `--domain`, front it with TLS (your existing reverse proxy, or add a
Caddy service like the cloud setup below). Pulse never edits your existing Nginx —
if you want it to, that's an explicit, previewed, reversible action.

Verify and manage:

```bash
/opt/pulse   # config, manifest, data live here
docker compose -p pulse ps          # the isolated pulse-* stack
./installer/uninstall.sh            # removes ONLY Pulse; your services untouched
```

**Persistence:** the API image is built with the pgx tag by default, so accounts,
servers and agent registrations live in `pulse-postgres` and survive a restart.
The schema is applied automatically at start-up from the embedded migrations —
there is no separate psql step.

Building without it (`API_TAGS=`) gives an in-memory store, which loses every
account and agent registration on restart. Because that is indistinguishable
from total data loss — an empty dashboard and every agent refused at ingest —
the API refuses to start that way under `PULSE_ENV=production` unless you set
`PULSE_ALLOW_EPHEMERAL_STORE=true`.

---

## 3. Pulse Cloud at `pulse.frix.me`

Run this on the host that will BE `pulse.frix.me` (a small dedicated VPS is ideal —
it is not one of your monitored boxes; monitored boxes only run the agent).

### 3a. DNS

Add an `A` (and `AAAA` if you have IPv6) record:

```text
pulse.frix.me  ->  <control-plane-host-public-ip>
```

Open ports **80** and **443** on that host.

### 3b. Configure

```bash
git clone https://github.com/frix-me/pulse.git
cd pulse

cat > .env <<EOF
DASHBOARD_DOMAIN=pulse.frix.me
POSTGRES_PASSWORD=$(openssl rand -hex 24)
PULSE_SESSION_SECRET=$(openssl rand -hex 32)
PULSE_JWT_SIGNING_KEY=$(openssl rand -hex 32)
PULSE_BOOTSTRAP_EMAIL=you@frix.me
PULSE_BOOTSTRAP_PASSWORD=$(openssl rand -hex 16)
METRICS_RETENTION=30d
EOF
chmod 600 .env
grep PULSE_BOOTSTRAP_PASSWORD .env   # note your first-login password
```

### 3c. Launch (Caddy handles TLS automatically)

```bash
docker compose -f infrastructure/docker-compose.cloud.yml --env-file .env up -d --build
# apply the schema (pgx build is durable):
docker run --rm --network pulse-net -e PGPASSWORD="$(grep POSTGRES_PASSWORD .env | cut -d= -f2)" \
  postgres:16-alpine psql -h pulse-postgres -U pulse -d pulse \
  -f - < apps/api/migrations/0001_init.sql
```

Visit `https://pulse.frix.me` and sign in with the bootstrap email/password.

### 3d. Enroll each VPS

1. In the dashboard: **Settings → Generate enrollment token** (short-lived, single-use).
2. On the VPS you want to monitor:

   ```bash
   git clone https://github.com/frix-me/pulse.git && cd pulse
   sudo ./installer/install.sh --mode cloud --enrollment-token pst_xxxxx
   ```

The agent dials **out** to `pulse.frix.me` over TLS (no inbound port), enrolls,
and the server appears in your dashboard within a minute. Repeat per VPS.

---

## 4. Testing your services are seen

After install/enroll, open the dashboard and check:

- **Dashboard** → VPS health, CPU/RAM/disk/network, uptime.
- **Containers / Services / Databases** → your real workloads, discovered read-only.
- **Domains** → domains parsed from your reverse proxy, with TLS expiry.
- **Infrastructure** → which detectors ran and what they found.
- **Security** → obvious risks (public DB ports, weak/expired TLS, Docker exposure).

From the CLI on the box (or `docker exec` into `pulse-agent`):

```bash
pulse doctor      # environment + health checks (read-only)
pulse discover    # full inventory snapshot (read-only)
pulse status      # what Pulse is running
```

---

## 5. Production hardening checklist

- Keep `API_TAGS=pgx` (the default) so storage is durable; never set `PULSE_ALLOW_EPHEMERAL_STORE`.
- Strong, unique `POSTGRES_PASSWORD`, `PULSE_SESSION_SECRET`, `PULSE_JWT_SIGNING_KEY`.
- Restrict the control-plane host's firewall to 80/443 (+ SSH).
- Back up the `pulse-postgres-data` volume.
- Prefer a **read-only Docker socket proxy** over mounting `docker.sock` (SECURITY.md).
- Set `METRICS_RETENTION` to fit disk; Pulse also self-limits and detects disk pressure.
- Keep the safety feature flags at their defaults (all `false`).
- Rotate enrollment tokens; revoke agents you decommission (Settings → Agents).

See [SECURITY.md](../SECURITY.md), [SAFETY_MODEL.md](./SAFETY_MODEL.md), and
[THREAT_MODEL.md](./THREAT_MODEL.md).
