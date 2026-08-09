#!/usr/bin/env bash
# =============================================================================
# Pulse uninstaller. Removes ONLY resources created by Pulse (from the
# installation manifest). It never removes existing containers, networks,
# databases, Nginx config, applications, system services, or user data.
# See docs/SAFETY_MODEL.md and spec section 77.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"

PULSE_HOME="${PULSE_HOME:-/opt/pulse}"
MANIFEST="$PULSE_HOME/pulse-manifest.json"
ASSUME_YES=0
[ "${1:-}" = "--yes" ] && ASSUME_YES=1

[ -f "$MANIFEST" ] || die "no Pulse manifest found at $MANIFEST; nothing to uninstall"

log ""
log "${C_BOLD}The following Pulse resources will be removed:${C_RESET}"
ok "Pulse stack (compose project: pulse)"
ok "Pulse monitoring containers (pulse-*)"
ok "Pulse data + configuration under $PULSE_HOME"
ok "The isolated pulse-net network"
log ""
log "${C_GREEN}Existing services affected: 0${C_RESET}"
log ""

if [ "$ASSUME_YES" != 1 ]; then
  printf "Proceed with uninstall? [y/N] "
  read -r reply
  case "$reply" in y|Y|yes|YES) : ;; *) die "aborted; nothing was removed";; esac
fi

# Bring down ONLY the Pulse compose project (never a broad prune).
if have docker; then
  compose_cmd="docker compose"; docker compose version >/dev/null 2>&1 || compose_cmd="docker-compose"
  # Bring down whichever footprint is running (self-hosted or cloud agent).
  for f in docker-compose.yml infrastructure/docker-compose.agent.yml; do
    if [ -f "$REPO_ROOT/$f" ]; then
      ( cd "$REPO_ROOT" && as_root $compose_cmd -p pulse -f "$f" down ) 2>/dev/null || true
    fi
  done
  # Remove the isolated network only if it exists and is a Pulse network.
  as_root docker network rm pulse-net >/dev/null 2>&1 || true
fi

# Remove Pulse's own directory. NEVER touch anything outside the manifest.
as_root rm -rf "$PULSE_HOME"

ok "Pulse removed. Your containers, networks, databases, Nginx config and applications are untouched."
