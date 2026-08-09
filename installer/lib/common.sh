#!/usr/bin/env bash
# Shared installer helpers. Sourced by install.sh / uninstall.sh.
# No secrets are ever hardcoded here; secrets are generated at apply time.

# Colors (disabled if not a TTY).
if [ -t 1 ]; then
  C_RESET="\033[0m"; C_DIM="\033[2m"; C_GREEN="\033[32m"; C_YELLOW="\033[33m"
  C_RED="\033[31m"; C_BOLD="\033[1m"; C_BLUE="\033[34m"
else
  C_RESET=""; C_DIM=""; C_GREEN=""; C_YELLOW=""; C_RED=""; C_BOLD=""; C_BLUE=""
fi

log()   { printf "%b\n" "$*"; }
info()  { printf "%b\n" "${C_BLUE}•${C_RESET} $*"; }
ok()    { printf "%b\n" "${C_GREEN}✓${C_RESET} $*"; }
warn()  { printf "%b\n" "${C_YELLOW}⚠${C_RESET} $*" >&2; }
err()   { printf "%b\n" "${C_RED}✗${C_RESET} $*" >&2; }
die()   { err "$*"; exit 1; }

have()  { command -v "$1" >/dev/null 2>&1; }

# require_root_for_apply ensures we can write privileged paths ONLY in the apply
# phase. Discovery is unprivileged.
require_root_for_apply() {
  if [ "$(id -u)" -ne 0 ] && ! have sudo; then
    die "The apply phase needs root (or sudo). Discovery is read-only and does not."
  fi
}

as_root() {
  if [ "$(id -u)" -eq 0 ]; then "$@"; else sudo "$@"; fi
}

# gen_secret prints a cryptographically-random hex string.
gen_secret() {
  if have openssl; then openssl rand -hex 32
  else head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'
  fi
}

# port_in_use returns 0 if a TCP port is currently listening.
port_in_use() {
  local port="$1"
  if have ss; then ss -ltn 2>/dev/null | awk '{print $4}' | grep -Eq "[:.]$port\$"
  elif have netstat; then netstat -ltn 2>/dev/null | awk '{print $4}' | grep -Eq "[:.]$port\$"
  else
    # Fallback: try to bind with bash /dev/tcp (best-effort).
    ! (echo >"/dev/tcp/127.0.0.1/$port") 2>/dev/null
  fi
}

# find_free_port scans upward from a base port for a free one.
find_free_port() {
  local base="${1:-3210}" p
  for p in $(seq "$base" $((base + 200))); do
    if ! port_in_use "$p"; then echo "$p"; return 0; fi
  done
  return 1
}

# json_escape escapes a string for embedding in JSON.
json_escape() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'; }
