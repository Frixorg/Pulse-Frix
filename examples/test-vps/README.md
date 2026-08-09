# Reference "existing VPS" environment

This mimics a VPS that already has real workloads: Nginx, a Node API, PostgreSQL,
Redis, a Python service, and two Docker networks. Use it to validate Pulse's
non-destructive guarantee end to end.

```bash
# 1. Bring up the pretend "existing" infrastructure
docker compose -f examples/test-vps/docker-compose.yml up -d

# 2. Run the full non-destructive test (installs Pulse, verifies zero changes,
#    then uninstalls and re-verifies)
PULSE_FULL_TEST=1 bash tests/non-destructive/run.sh

# 3. Tear the demo down when finished
docker compose -f examples/test-vps/docker-compose.yml down -v
```

Pulse should discover all of the above, monitor them, and leave every container,
network, volume, port and config byte-for-byte unchanged.
