#!/usr/bin/env bash
# =============================================================================
# Pulse Cloud one-shot deploy/update for the control-plane host (pulse.frix.me).
# Pulls the latest code, (re)builds and starts the stack, and — if an agent
# enrollment token is present in .env — (re)starts the local agent too.
#
# Usage:
#   ./deploy.sh            # pull + build + up
#   ./deploy.sh --no-pull  # skip git pull (deploy local changes)
# =============================================================================
set -euo pipefail
cd "$(dirname "$0")"

COMPOSE_FILE="infrastructure/docker-compose.cloud.yml"
ENV_FILE=".env"
DC=(docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE")

c_green='\033[32m'; c_blue='\033[34m'; c_red='\033[31m'; c_reset='\033[0m'
info() { printf "${c_blue}==>${c_reset} %s\n" "$*"; }
ok()   { printf "${c_green}✓${c_reset} %s\n" "$*"; }
die()  { printf "${c_red}✗ %s${c_reset}\n" "$*" >&2; exit 1; }

[ -f "$ENV_FILE" ] || die "$ENV_FILE not found. Create it first (see docs/DEPLOYMENT.md)."
command -v docker >/dev/null 2>&1 || die "docker is not installed."

PULL=1
[ "${1:-}" = "--no-pull" ] && PULL=0

if [ "$PULL" = 1 ] && [ -d .git ]; then
  info "Pulling latest code..."
  git pull --ff-only || info "git pull skipped (local changes or detached HEAD)"
fi

info "Building and starting the Pulse Cloud stack..."
"${DC[@]}" up -d --build

# Apply the database schema (idempotent: every statement is IF NOT EXISTS, plus
# self-healing ALTERs). Runs on every deploy so new tables/columns land without
# a manual psql step.
info "Applying database schema..."
# shellcheck disable=SC1090
set -a; . "$ENV_FILE"; set +a
for _ in $(seq 1 15); do
  docker exec pulse-postgres pg_isready -U pulse >/dev/null 2>&1 && break
  sleep 1
done
if docker run --rm -i --network pulse-net -e PGPASSWORD="${POSTGRES_PASSWORD:-}" postgres:16-alpine \
     psql -h pulse-postgres -U pulse -d pulse < apps/api/migrations/0001_init.sql >/dev/null 2>&1; then
  ok "Schema applied."
  info "Restarting the API so it picks up the schema..."
  "${DC[@]}" restart pulse-api >/dev/null
else
  info "Schema step skipped or already up to date."
fi

# Start the local monitoring agent only if a token is configured.
if grep -Eq '^AGENT_ENROLLMENT_TOKEN=.+' "$ENV_FILE"; then
  info "Enrollment token found — starting the local agent..."
  "${DC[@]}" --profile agent up -d pulse-agent
fi

# Rebuild the installer-managed agent (compose project 'pulse') if it's running,
# so agent-side changes (new detectors) land on ./deploy.sh without re-running
# the installer. The agent keeps its identity, so it won't re-enroll.
if [ -f /opt/pulse/.env ] && docker ps --format '{{.Names}}' 2>/dev/null | grep -qx pulse-agent; then
  info "Rebuilding the local monitoring agent (new detectors)…"
  docker compose -p pulse --env-file /opt/pulse/.env \
    -f infrastructure/docker-compose.agent.yml up -d --build \
    || info "agent rebuild skipped"
fi

echo
"${DC[@]}" ps
echo
ok "Deploy complete."
domain="$(grep -E '^DASHBOARD_DOMAIN=' "$ENV_FILE" | cut -d= -f2-)"
[ -n "${domain:-}" ] && ok "Dashboard: https://${domain}   (landing at https://${domain}/ , app at /app)"
