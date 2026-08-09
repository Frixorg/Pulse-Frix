# Pulse Cloud Mode

In cloud mode your VPS connects **outbound** to `pulse.frix.me` and appears in your
central Pulse dashboard alongside your other servers. **No inbound management port
is ever opened** on the VPS — this dramatically reduces attack surface.

## Flow

```text
User Dashboard ──generate enrollment token──▶ TOKEN
                                                 │
VPS installer ──────────────────────────────────┘
      │
      ▼
   Agent ──outbound HTTPS/WebSocket──▶ Pulse API ──▶ secure registration
```

1. **Login** to the dashboard.
2. **Generate an enrollment token** (short-lived, single-use, org-scoped).
3. **Connect the VPS:**

   ```bash
   ./installer/install.sh --mode cloud --enrollment-token pst_xxx
   ```

4. **Discovery** runs; the server appears in your dashboard.
5. **Monitor** centrally.

## Security model

- The cloud **never** has unrestricted shell access to your VPS. The agent honours
  only a fixed read-only capability set (`READ_DOCKER`, `READ_SYSTEM`,
  `READ_NETWORK`, `READ_NGINX`, `READ_METRICS`) — there is no `exec` path.
- Communication is TLS with an authenticated agent identity, short-lived
  credentials, rotation, replay protection, and request signing. See
  [AGENT_PROTOCOL.md](./AGENT_PROTOCOL.md).
- Each VPS gets cryptographically-secure identifiers (`server_id`, `agent_id`,
  `installation_id`).
- Enrollment tokens are short-lived, single-use, rate-limited, and bound to the
  server fingerprint on first use.

## Multi-tenancy

Users never see another user's data. Every query is tenant-scoped at the API,
service, and database layers. See [DATA_MODEL.md](./DATA_MODEL.md#multi-tenancy).

## Offline resilience

If Pulse Cloud is unreachable, the agent keeps monitoring locally, buffers a
bounded amount of metrics, and retries with exponential backoff. **Cloud outages
never affect your VPS applications** (golden rule #13).

## What is (and isn't) sent

Metrics + metadata only. Pulse never sends environment variables, application
secrets, private keys, file contents, or database contents. See
[PRIVACY.md](./PRIVACY.md).

## Revocation

Revoke a server's agent identity from the dashboard at any time; the agent must
re-enroll with a fresh token to reconnect.
