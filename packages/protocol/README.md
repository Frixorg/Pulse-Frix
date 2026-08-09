# @pulse/protocol

Language-neutral definition of the Pulse **agent ↔ control-plane** protocol.
Both the Go agent (`agent/internal/protocol`) and the Go API
(`apps/api/internal/http`) conform to this. See
[docs/AGENT_PROTOCOL.md](../../docs/AGENT_PROTOCOL.md) for the narrative.

- `version.json` — the current protocol version and supported range.
- `schema.json` — JSON Schema for the request envelope and message bodies.

## Rules the protocol enforces

- **Outbound-only.** The agent initiates every connection; no inbound port.
- **Signed.** Every request carries `X-Pulse-Agent-Id`, `X-Pulse-Timestamp`,
  `X-Pulse-Nonce`, `X-Pulse-Signature` (Ed25519 over
  `method|path|timestamp|nonce|sha256(body)`).
- **Replay-resistant.** Timestamp window ±60s; nonces are single-use.
- **No remote shell.** The only server→agent messages are read capabilities
  (`READ_DOCKER`, `READ_SYSTEM`, `READ_NETWORK`, `READ_NGINX`, `READ_METRICS`).
  There is no `exec` message type — by construction.
- **Redacted.** All bodies leave the agent already secret-redacted.
