#!/usr/bin/env bash
# =============================================================================
# Non-destructive installer test. Proves the installer does not modify EXISTING
# system state. The default (fast) path runs the installer in --dry-run (Discover
# + Plan only) and asserts a byte-identical before/after snapshot.
#
# Set PULSE_FULL_TEST=1 to additionally run a full apply against a seeded
# "existing" Docker environment and verify those resources survive install AND
# uninstall unchanged. (Requires Docker; slower.)
# =============================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SNAP="$ROOT/tests/non-destructive/snapshot.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "==> Non-destructive test: capturing BEFORE snapshot"
bash "$SNAP" > "$TMP/before.txt"

echo "==> Running installer in --dry-run (Discover + Plan; never Apply)"
bash "$ROOT/installer/install.sh" --dry-run --yes > "$TMP/plan.txt" 2>&1 || true
grep -q "SAFE INSTALLATION PLAN" "$TMP/plan.txt" || { echo "FAIL: plan not produced"; cat "$TMP/plan.txt"; exit 1; }
grep -q "stopping before Apply" "$TMP/plan.txt" || { echo "FAIL: dry-run applied changes"; exit 1; }

echo "==> Capturing AFTER snapshot"
bash "$SNAP" > "$TMP/after.txt"

if ! diff -u "$TMP/before.txt" "$TMP/after.txt"; then
  echo "FAIL: system state changed during Discover/Plan (must be read-only)"
  exit 1
fi
echo "PASS: Discover + Plan changed nothing."

if [ "${PULSE_FULL_TEST:-0}" != "1" ]; then
  echo "==> Skipping full apply test (set PULSE_FULL_TEST=1 to enable)."
  exit 0
fi

command -v docker >/dev/null 2>&1 || { echo "docker required for full test"; exit 1; }

echo "==> Seeding an 'existing' environment"
docker network create demo-net >/dev/null 2>&1 || true
docker run -d --name demo-redis --network demo-net redis:7-alpine >/dev/null
docker run -d --name demo-web --network demo-net nginx:alpine >/dev/null

bash "$SNAP" > "$TMP/before_full.txt"

echo "==> Full install (--yes)"
bash "$ROOT/installer/install.sh" --yes || { echo "install failed"; exit 1; }

bash "$SNAP" > "$TMP/after_install.txt"
if ! diff -u "$TMP/before_full.txt" "$TMP/after_install.txt"; then
  echo "FAIL: existing (non-pulse) resources changed during install"
  exit 1
fi
echo "PASS: existing resources unchanged after install."

echo "==> Uninstall (--yes)"
bash "$ROOT/installer/uninstall.sh" --yes || true
bash "$SNAP" > "$TMP/after_uninstall.txt"
if ! diff -u "$TMP/before_full.txt" "$TMP/after_uninstall.txt"; then
  echo "FAIL: existing resources changed after uninstall"
  exit 1
fi
echo "PASS: existing resources unchanged after uninstall."

# cleanup seeded env
docker rm -f demo-web demo-redis >/dev/null 2>&1 || true
docker network rm demo-net >/dev/null 2>&1 || true
echo "ALL NON-DESTRUCTIVE CHECKS PASSED"
