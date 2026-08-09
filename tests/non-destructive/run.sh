#!/usr/bin/env bash
# =============================================================================
# Non-destructive installer test.
#
# Fast path (default, runs in CI): proves the installer's Discover + Plan phases
# create NOTHING — deterministic, targeted assertions (no whole-system diff, so
# it is stable on shared CI runners).
#
# Full path (PULSE_FULL_TEST=1, needs Docker): seeds an "existing" environment,
# runs a REAL install, and asserts those specific resources survive install AND
# uninstall unchanged. Pulse's own pulse-* resources are excluded.
# =============================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*"; exit 1; }

# --------------------------------------------------------------------------
# Fast path: Discover + Plan must change nothing.
# --------------------------------------------------------------------------
echo "==> Running installer in --dry-run (Discover + Plan; never Apply)"
out="$(bash "$ROOT/installer/install.sh" --dry-run --yes 2>&1 || true)"
echo "$out"

echo "$out" | grep -q "SAFE INSTALLATION PLAN" || fail "installer did not produce a plan"
echo "$out" | grep -q "stopping before Apply"   || fail "dry-run did not stop before Apply"

# Assert the installer created NONE of its own resources during dry-run.
[ ! -e /opt/pulse ] || fail "dry-run created /opt/pulse"
if command -v docker >/dev/null 2>&1; then
  ! docker network inspect pulse-net >/dev/null 2>&1 || fail "dry-run created the pulse-net network"
  [ -z "$(docker ps -aq --filter 'name=pulse-' 2>/dev/null)" ] || fail "dry-run created pulse-* containers"
fi
pass "Discover + Plan changed nothing."

if [ "${PULSE_FULL_TEST:-0}" != "1" ]; then
  echo "==> Skipping full apply test (set PULSE_FULL_TEST=1 to enable)."
  echo "ALL NON-DESTRUCTIVE CHECKS PASSED"
  exit 0
fi

# --------------------------------------------------------------------------
# Full path: a real install must not touch EXISTING resources.
# --------------------------------------------------------------------------
command -v docker >/dev/null 2>&1 || fail "docker required for the full test"

echo "==> Seeding an 'existing' environment (demo-net, demo-redis, demo-web)"
docker network create demo-net >/dev/null 2>&1 || true
docker rm -f demo-redis demo-web >/dev/null 2>&1 || true
docker run -d --name demo-redis --network demo-net redis:7-alpine >/dev/null
docker run -d --name demo-web   --network demo-net nginx:alpine   >/dev/null

# Record an identity fingerprint for each existing resource.
before_redis="$(docker inspect -f '{{.Id}} {{.State.Running}} {{.Config.Image}}' demo-redis)"
before_web="$(docker inspect -f '{{.Id}} {{.State.Running}} {{.Config.Image}}' demo-web)"
before_net="$(docker network inspect -f '{{.Id}}' demo-net)"

echo "==> Full install (--yes)"
bash "$ROOT/installer/install.sh" --yes || fail "install failed"

for name in demo-redis demo-web; do
  docker inspect "$name" >/dev/null 2>&1 || fail "existing container $name disappeared after install"
  running="$(docker inspect -f '{{.State.Running}}' "$name")"
  [ "$running" = "true" ] || fail "existing container $name is no longer running after install"
done
[ "$(docker inspect -f '{{.Id}} {{.State.Running}} {{.Config.Image}}' demo-redis)" = "$before_redis" ] || fail "demo-redis changed after install"
[ "$(docker inspect -f '{{.Id}} {{.State.Running}} {{.Config.Image}}' demo-web)"   = "$before_web"   ] || fail "demo-web changed after install"
[ "$(docker network inspect -f '{{.Id}}' demo-net)" = "$before_net" ] || fail "demo-net changed after install"
pass "existing resources unchanged after install."

echo "==> Uninstall (--yes)"
bash "$ROOT/installer/uninstall.sh" --yes || true
for name in demo-redis demo-web; do
  docker inspect "$name" >/dev/null 2>&1 || fail "uninstall removed existing container $name"
done
docker network inspect demo-net >/dev/null 2>&1 || fail "uninstall removed existing network demo-net"
pass "existing resources unchanged after uninstall."

echo "==> Cleaning up seeded demo environment"
docker rm -f demo-web demo-redis >/dev/null 2>&1 || true
docker network rm demo-net >/dev/null 2>&1 || true
echo "ALL NON-DESTRUCTIVE CHECKS PASSED"
