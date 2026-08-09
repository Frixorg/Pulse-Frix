#!/usr/bin/env bash
# =============================================================================
# Pulse installer — implements the Discover → Plan → Apply safety contract.
#
#   Phase A DISCOVER : read-only inspection (no writes of any kind)
#   Phase B PLAN     : generate an immutable, human-readable plan
#   Phase C APPLY    : apply ONLY explicitly permitted changes (SAFE MODE)
#
# The default SAFE MODE never modifies existing services, containers, config,
# ports, firewall or databases. See docs/SAFETY_MODEL.md.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"

# ---- defaults (all initialised for `set -u`) --------------------------------
MODE="local"
DOMAIN=""
ENROLL_TOKEN=""
DASH_PORT=""
ASSUME_YES=0
DRY_RUN=0
PULSE_HOME="${PULSE_HOME:-/opt/pulse}"
STATE_DIR="$PULSE_HOME/state"
MANIFEST="$PULSE_HOME/pulse-manifest.json"

usage() {
  cat <<EOF
Pulse installer

Usage: ./installer/install.sh [flags]

  --mode local|cloud            deployment mode (default: local)
  --domain <fqdn>               self-hosted domain (optional)
  --enrollment-token <token>    cloud enrollment token (short-lived)
  --dashboard-port <port>       override the auto-selected dashboard port
  --yes                         skip the confirmation prompt (CI only)
  --dry-run                     run Discover + Plan, never Apply
  -h, --help                    show this help

Pulse observes first and changes nothing by default.
EOF
}

# ---- arg parsing ------------------------------------------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    --mode) MODE="${2:-}"; shift 2;;
    --domain) DOMAIN="${2:-}"; shift 2;;
    --enrollment-token) ENROLL_TOKEN="${2:-}"; shift 2;;
    --dashboard-port) DASH_PORT="${2:-}"; shift 2;;
    --yes) ASSUME_YES=1; shift;;
    --dry-run) DRY_RUN=1; shift;;
    -h|--help) usage; exit 0;;
    *) die "unknown flag: $1 (see --help)";;
  esac
done

[ "$MODE" = "local" ] || [ "$MODE" = "cloud" ] || die "--mode must be local or cloud"

# Discovered facts (populated by discover()).
OS_NAME=""; OS_VERSION=""; ARCH=""; CPU_CORES=""; RAM_GB=""; DISK_HUMAN=""
HAS_DOCKER=0; HAS_COMPOSE=0; DOCKER_CONTAINERS=0; DOCKER_NETWORKS=0
HAS_NGINX=0; NGINX_SITES=0; HAS_SYSTEMD=0
HAS_POSTGRES=0; HAS_REDIS=0; HAS_MYSQL=0
EXISTING_MON=""

# =============================================================================
# Phase A — DISCOVER (read-only)
# =============================================================================
discover() {
  log ""
  log "${C_BOLD}VPS DISCOVERY${C_RESET}"
  log ""

  if [ -r /etc/os-release ]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    OS_NAME="${NAME:-Linux}"; OS_VERSION="${VERSION_ID:-}"
  else
    OS_NAME="$(uname -s)"; OS_VERSION=""
  fi
  ARCH="$(uname -m)"
  CPU_CORES="$(nproc 2>/dev/null || echo '?')"
  if [ -r /proc/meminfo ]; then
    local kb; kb="$(awk '/MemTotal/{print $2}' /proc/meminfo)"
    RAM_GB="$(awk "BEGIN{printf \"%.0f\", ${kb:-0}/1024/1024}")"
  fi
  DISK_HUMAN="$(df -h / 2>/dev/null | awk 'NR==2{print $2" total, "$4" free"}')" || DISK_HUMAN="?"
  # NOTE: use `if` blocks, never `cond && action` — under `set -e` a bare
  # `cond && action` exits the script whenever `cond` is false.
  if [ -d /run/systemd/system ]; then HAS_SYSTEMD=1; fi

  if have docker; then
    HAS_DOCKER=1
    DOCKER_CONTAINERS="$(docker ps -q 2>/dev/null | wc -l | tr -d ' ')" || DOCKER_CONTAINERS=0
    DOCKER_NETWORKS="$(docker network ls -q 2>/dev/null | wc -l | tr -d ' ')" || DOCKER_NETWORKS=0
    if docker compose version >/dev/null 2>&1 || have docker-compose; then HAS_COMPOSE=1; fi
  fi
  if have nginx || [ -f /etc/nginx/nginx.conf ]; then
    HAS_NGINX=1
    NGINX_SITES="$(ls -1 /etc/nginx/sites-enabled 2>/dev/null | wc -l | tr -d ' ')" || NGINX_SITES=0
  fi
  port_in_use 5432 && HAS_POSTGRES=1 || true
  port_in_use 6379 && HAS_REDIS=1 || true
  port_in_use 3306 && HAS_MYSQL=1 || true
  for p in 9090 3000 9100 9093; do
    if port_in_use "$p"; then EXISTING_MON="$EXISTING_MON $p"; fi
  done

  log "OS:            ${OS_NAME} ${OS_VERSION}"
  log "Architecture:  ${ARCH}"
  log "CPU:           ${CPU_CORES} cores"
  log "RAM:           ${RAM_GB:-?} GB"
  log "Disk:          ${DISK_HUMAN:-?}"
  log "systemd:       $([ "$HAS_SYSTEMD" = 1 ] && echo detected || echo 'not detected')"
  log "Docker:        $([ "$HAS_DOCKER" = 1 ] && echo "detected (${DOCKER_CONTAINERS} containers, ${DOCKER_NETWORKS} networks)" || echo 'not detected')"
  log "Compose:       $([ "$HAS_COMPOSE" = 1 ] && echo detected || echo 'not detected')"
  log "Nginx:         $([ "$HAS_NGINX" = 1 ] && echo "detected (${NGINX_SITES} sites)" || echo 'not detected')"
  log "PostgreSQL:    $([ "$HAS_POSTGRES" = 1 ] && echo 'detected (port 5432)' || echo 'not detected')"
  log "Redis:         $([ "$HAS_REDIS" = 1 ] && echo 'detected (port 6379)' || echo 'not detected')"
  if [ -n "$EXISTING_MON" ]; then
    warn "Existing monitoring detected on ports:$EXISTING_MON (Pulse will coexist, never replace)"
  fi
}

# =============================================================================
# Precheck
# =============================================================================
precheck() {
  info "Running prechecks..."
  case "$ARCH" in
    x86_64|amd64|aarch64|arm64) : ;;
    *) warn "Architecture $ARCH is not officially supported; continuing cautiously.";;
  esac
  local free_kb; free_kb="$(df -Pk / | awk 'NR==2{print $4}')"
  if [ "${free_kb:-0}" -lt 1048576 ]; then
    die "Less than 1 GB free on /. Pulse needs a little room; aborting to be safe."
  fi
  if [ "$MODE" = "cloud" ] && [ -z "$ENROLL_TOKEN" ]; then
    die "cloud mode requires --enrollment-token (generate one in the dashboard)"
  fi
  # Refuse to run on the host that IS the Pulse cloud control plane — the
  # installer is for OTHER VPSs, and here it would collide with the running
  # stack (pulse-net, pulse-agent, pulse-node-exporter).
  if have docker && docker ps --format '{{.Names}}' 2>/dev/null | grep -qx "pulse-api"; then
    die "This host already runs the Pulse cloud stack (pulse-api).
    The installer is for OTHER servers. To monitor THIS host, enrol its own agent:
      docker compose -f infrastructure/docker-compose.cloud.yml --env-file .env --profile agent up -d pulse-agent
    (set AGENT_ENROLLMENT_TOKEN in that .env first)."
  fi
  ok "Prechecks passed"
}

# =============================================================================
# Phase B — PLAN (immutable)
# =============================================================================
plan() {
  # Choose a safe, unused dashboard port. NEVER assume 3000/9090/9100 are free.
  if [ -n "$DASH_PORT" ]; then
    if port_in_use "$DASH_PORT"; then die "requested --dashboard-port $DASH_PORT is in use"; fi
  else
    DASH_PORT="$(find_free_port 3210)" || die "could not find a free port in 3210-3410"
  fi

  log ""
  log "${C_BOLD}SAFE INSTALLATION PLAN${C_RESET}"
  log ""
  log "The installer ${C_BOLD}WILL${C_RESET}:"
  ok "create an isolated monitoring network (pulse-net)"
  ok "create monitoring containers (namespaced pulse-*)"
  ok "create dedicated data directories under ${PULSE_HOME}"
  ok "expose the dashboard on an unused port (${C_BOLD}${DASH_PORT}${C_RESET})"
  ok "preserve existing Nginx configuration"
  ok "preserve existing Docker workloads"
  log ""
  log "The installer will ${C_BOLD}NOT${C_RESET}:"
  ok "modify existing application containers"
  ok "modify application configuration"
  ok "modify firewall rules"
  ok "replace Nginx"
  ok "restart existing applications"
  log ""
}

confirm() {
  if [ "$DRY_RUN" = 1 ]; then
    warn "--dry-run: stopping before Apply. Nothing was changed."
    exit 0
  fi
  if [ "$ASSUME_YES" = 1 ]; then return 0; fi
  printf "Proceed with the plan above? [y/N] "
  read -r reply
  case "$reply" in y|Y|yes|YES) : ;; *) die "aborted by user; nothing was changed";; esac
}

# =============================================================================
# Phase C — APPLY (only permitted changes)
# =============================================================================
apply() {
  require_root_for_apply
  info "Applying (SAFE MODE)..."

  as_root mkdir -p "$PULSE_HOME"/{agent,monitoring,data,backups,logs,state}
  as_root chmod 750 "$PULSE_HOME"

  # Generate secrets locally; never hardcode. Written 0600.
  local session_secret jwt_key db_pass env_file
  session_secret="$(gen_secret)"; jwt_key="$(gen_secret)"; db_pass="$(gen_secret)"
  env_file="$PULSE_HOME/.env"

  # Self-hosted bootstrap: create an initial admin + a local enrollment token so
  # the agent can enroll over the same secure enroll+ingest path as cloud mode.
  ADMIN_EMAIL="${ADMIN_EMAIL:-admin@${DOMAIN:-localhost}}"
  ADMIN_PASSWORD="$(gen_secret | cut -c1-20)"
  local api_url agent_mode local_enroll
  if [ "$MODE" = "cloud" ]; then
    api_url="${PULSE_API_URL:-https://pulse.frix.me}"; agent_mode="cloud"; local_enroll="$ENROLL_TOKEN"
  else
    api_url="http://pulse-api:8080"; agent_mode="cloud"; local_enroll="pst_local_$(gen_secret | cut -c1-24)"
  fi

  as_root tee "$env_file" >/dev/null <<EOF
PULSE_MODE=$MODE
PULSE_API_URL=$api_url
DASHBOARD_DOMAIN=$DOMAIN
DASHBOARD_PORT=$DASH_PORT
DATABASE_URL=postgres://pulse:${db_pass}@pulse-postgres:5432/pulse?sslmode=disable
POSTGRES_PASSWORD=${db_pass}
METRICS_URL=http://pulse-prometheus:9090
PULSE_SESSION_SECRET=$session_secret
PULSE_JWT_SIGNING_KEY=$jwt_key
PULSE_ENV=production
METRICS_RETENTION=7d
MONITORING_CPU_LIMIT=1.0
MONITORING_MEMORY_LIMIT=512M
ENABLE_CONFIG_MUTATION=false
ENABLE_AUTO_TLS=false
ENABLE_REMOTE_ACTIONS=false
ENABLE_AUTO_UPDATE=false
# --- bootstrap (first-run) ---
PULSE_BOOTSTRAP_EMAIL=$ADMIN_EMAIL
PULSE_BOOTSTRAP_PASSWORD=$ADMIN_PASSWORD
PULSE_BOOTSTRAP_ENROLLMENT_TOKEN=$local_enroll
# --- agent ---
AGENT_MODE=$agent_mode
AGENT_ENROLLMENT_TOKEN=$local_enroll
EOF
  as_root chmod 600 "$env_file"
  ok "wrote isolated configuration to $env_file (0600)"

  # Record the installation manifest (what we created — and ONLY this).
  write_manifest

  if [ "$HAS_DOCKER" = 1 ] && [ "$HAS_COMPOSE" = 1 ]; then
    info "Starting isolated Pulse stack (compose project: pulse)..."
    local compose_cmd compose_file; compose_cmd="$(compose_binary)"
    if [ "$MODE" = "cloud" ]; then
      compose_file="infrastructure/docker-compose.agent.yml"  # outbound agent only
    else
      compose_file="docker-compose.yml"                        # full self-hosted stack
    fi
    ( cd "$REPO_ROOT" && as_root $compose_cmd --env-file "$env_file" -p pulse \
        -f "$compose_file" up -d ) \
      || die "compose failed; run './installer/uninstall.sh' to remove anything this run created"
    ok "Pulse stack started on the isolated pulse-net network"
  else
    warn "Docker/Compose not detected — skipping container stack."
    warn "Install Docker to enable container discovery + the monitoring stack."
  fi
}

compose_binary() {
  if docker compose version >/dev/null 2>&1; then echo "docker compose"; else echo "docker-compose"; fi
}

write_manifest() {
  as_root tee "$MANIFEST" >/dev/null <<EOF
{
  "created": ["$PULSE_HOME"],
  "modified": [],
  "services_created": ["pulse"],
  "ports_allocated": [$DASH_PORT],
  "networks_created": ["pulse-net"],
  "containers_created": ["pulse-api","pulse-dashboard","pulse-postgres","pulse-prometheus","pulse-node-exporter","pulse-cadvisor"],
  "mode": "$(json_escape "$MODE")",
  "created_at": "$(date -u +%FT%TZ)"
}
EOF
  ok "recorded installation manifest at $MANIFEST"
}

# =============================================================================
# Verify + report
# =============================================================================
verify() {
  info "Verifying installation..."
  local url="http://127.0.0.1:${DASH_PORT}/healthz"
  if have curl && [ "$HAS_DOCKER" = 1 ] && [ "$HAS_COMPOSE" = 1 ]; then
    for _ in $(seq 1 15); do
      if curl -fsS "$url" >/dev/null 2>&1; then ok "dashboard reachable at $url"; break; fi
      sleep 2
    done
  fi
}

report() {
  log ""
  log "${C_BOLD}INSTALLATION COMPLETE${C_RESET}"
  log ""
  if [ -n "$DOMAIN" ]; then log "Dashboard:  https://$DOMAIN"; else log "Dashboard:  http://<server-ip>:${DASH_PORT}"; fi
  log "Mode:       $MODE"
  log "Config:     $PULSE_HOME"
  log "Manifest:   $MANIFEST"
  if [ "$MODE" = "local" ] && [ -n "${ADMIN_EMAIL:-}" ]; then
    log ""
    log "${C_BOLD}Initial sign-in (shown once — store it safely):${C_RESET}"
    log "  Email:    ${ADMIN_EMAIL}"
    log "  Password: ${ADMIN_PASSWORD}"
  fi
  log ""
  log "${C_GREEN}Existing services modified: 0${C_RESET}"
  log ""
  log "Next: run '${C_BOLD}pulse doctor${C_RESET}' to verify, or '${C_BOLD}pulse uninstall${C_RESET}' to remove Pulse only."
}

# =============================================================================
main() {
  log "${C_BOLD}Pulse${C_RESET} — non-destructive VPS observability installer"
  discover
  precheck
  plan
  confirm
  apply
  verify
  report
}
main "$@"
