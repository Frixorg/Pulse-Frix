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

# Start the local monitoring agent only if a token is configured.
if grep -Eq '^AGENT_ENROLLMENT_TOKEN=.+' "$ENV_FILE"; then
  info "Enrollment token found — starting the local agent..."
  "${DC[@]}" --profile agent up -d pulse-agent
fi

echo
"${DC[@]}" ps
echo
ok "Deploy complete."
domain="$(grep -E '^DASHBOARD_DOMAIN=' "$ENV_FILE" | cut -d= -f2-)"
[ -n "${domain:-}" ] && ok "Dashboard: https://${domain}   (landing at https://${domain}/ , app at /app)"
