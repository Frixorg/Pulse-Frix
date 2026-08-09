#!/usr/bin/env bash
# Produce a normalized snapshot of EXISTING system state so before/after can be
# diffed to prove Pulse changed nothing it shouldn't. Pulse's own resources
# (pulse-*) are excluded so the test measures impact on *existing* infra only.
# See docs (tests/README.md) and spec sections 61-62.
set -euo pipefail

section() { echo "### $1"; }

section "listening_ports"
if command -v ss >/dev/null 2>&1; then
  ss -Hltn 2>/dev/null | awk '{print $4}' | sed 's/.*://' | sort -n | uniq
elif command -v netstat >/dev/null 2>&1; then
  netstat -ltn 2>/dev/null | awk 'NR>2{print $4}' | sed 's/.*://' | sort -n | uniq
fi

section "docker_containers"
if command -v docker >/dev/null 2>&1; then
  docker ps -a --format '{{.Names}} {{.Image}} {{.Status}}' 2>/dev/null \
    | grep -v '^pulse-' | sort || true
fi

section "docker_networks"
if command -v docker >/dev/null 2>&1; then
  docker network ls --format '{{.Name}}' 2>/dev/null | grep -v '^pulse-net$' | sort || true
fi

section "docker_images"
if command -v docker >/dev/null 2>&1; then
  docker images --format '{{.Repository}}:{{.Tag}}' 2>/dev/null | grep -v '^pulse' | sort || true
fi

section "docker_volumes"
if command -v docker >/dev/null 2>&1; then
  docker volume ls --format '{{.Name}}' 2>/dev/null | grep -v '^pulse' | sort || true
fi

section "systemd_failed_units"
if command -v systemctl >/dev/null 2>&1; then
  systemctl list-units --type=service --state=failed --no-legend --no-pager --plain 2>/dev/null \
    | awk '{print $1}' | sort || true
fi

section "nginx_config_hash"
if [ -d /etc/nginx ]; then
  find /etc/nginx -type f -exec sha256sum {} + 2>/dev/null | sort || true
fi
