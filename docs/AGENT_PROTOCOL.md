# Agent Protocol

The agent protocol governs how the Pulse agent communicates with the control plane
(self-hosted or cloud). It is versioned, authenticated, TLS-only, redaction-aware,
and **capability-restricted**. The canonical message schemas live in
[`packages/protocol`](../packages/protocol).

Design goals: outbound-only, offline-tolerant, replay-resistant, and impossible to
use as a remote shell.

---

## Identity

Each installation generates three cryptographically-secure identifiers (never
derived from hostname / IP / MAC alone):

```text
installation_id   uuid v4, per install
server_id         opaque public id shown in the dashboard
agent_id          identity used for authentication (bound to a keypair)
```

On first run the agent generates an Ed25519 keypair. The **private key never
leaves the VPS**. The public key is registered during enrollment.

---

## Transport

- **Cloud mode:** the agent dials **out** over HTTPS/WebSocket to
  `PULSE_API_URL`. No inbound port is opened on the VPS.
- **Self-hosted mode:** the agent connects to the local API over the loopback
  interface or the internal Docker network.

All connections are TLS. In cloud mode the agent pins the expected server
certificate/authority.

---

## Authentication & anti-replay

Every agent→server request carries:

```text
X-Pulse-Agent-Id:   <agent_id>
X-Pulse-Timestamp:  <unix millis>          (rejected outside ±60s)
X-Pulse-Nonce:      <random 128-bit>       (single-use, cached window)
X-Pulse-Signature:  Ed25519( method || path || timestamp || nonce || sha256(body) )
```

The server verifies the signature against the registered public key, checks the
timestamp window, and rejects replayed nonces. Credentials are short-lived and
rotated; a revoked `agent_id` is rejected immediately.

---

## Version negotiation

On connect the agent and server negotiate a protocol version:

```text
agent protocol: 1.2
server supports: 1.0 – 1.3   →   compatible: yes (use 1.2)
```

Incompatible versions fail *closed* with a clear message and never silently break
monitoring. The current version is `PROTOCOL_VERSION` in `packages/protocol`.

---

## Message types

### 1. `enroll` (agent → server)

```json
{
  "type": "enroll",
  "enrollment_token": "pst_...",
  "installation_id": "…",
  "public_key": "ed25519:…",
  "fingerprint": { "os": "linux", "arch": "amd64", "boot_id": "…" },
  "protocol_version": "1.2"
}
```

Response returns the assigned `server_id`, `agent_id`, and a short-lived session
credential. Enrollment tokens are single-use and expire quickly.

### 2. `hello` / `heartbeat` (agent → server)

Liveness + protocol negotiation + agent self-metrics (CPU/mem/net of the agent
itself). Missing heartbeats mark the server `UNKNOWN`/unreachable.

### 3. `discovery` (agent → server)

A structured discovery snapshot — the full inventory from the discovery engine
(already **redacted**). See [DISCOVERY.md](./DISCOVERY.md) for the shape. Diffed
server-side to emit `new service discovered` / `service disappeared` events.

### 4. `metrics` (agent → server)

Batched, compressed metric samples. Subject to backpressure: on failure the agent
buffers, batches, retries with exponential backoff, and drops **old low-priority**
samples first. Critical health state outranks historical samples.

### 5. `health` (agent → server)

Per-service health results: `HEALTHY | DEGRADED | DOWN | UNKNOWN` plus the check
that produced it.

### 6. `command` (server → agent) — **capability-restricted**

The only server→agent messages the agent will act on are read capabilities:

```text
READ_DOCKER · READ_SYSTEM · READ_NETWORK · READ_NGINX · READ_METRICS
```

There is **no** `exec` / `shell` / `run` command in the protocol or the dispatcher.
Requests for anything outside the granted capability set are rejected and audited.
Future write capabilities must be *disabled by default, explicitly authorized,
scoped, audited, revocable* — and still never include arbitrary shell.

---

## Offline behaviour

If the control plane is unreachable the agent:

- continues local discovery, metrics, and health checks,
- buffers a **bounded** amount of metrics (size + time capped),
- retries with exponential backoff + jitter,
- **never** stops, restarts, or disables anything because the cloud is down.

---

## Data minimization

The agent sends metrics + metadata only. It never sends environment variables,
application secrets, private keys, file contents, or database contents. See
[PRIVACY.md](./PRIVACY.md). Redaction is applied *before* anything leaves the agent.
