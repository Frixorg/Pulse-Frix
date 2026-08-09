# Privacy & Telemetry

Pulse collects only what is necessary to monitor your infrastructure. This document
states **exactly** what is collected, especially in cloud mode.

## What Pulse collects

In both modes, the agent gathers:

- CPU metrics
- memory metrics
- disk metrics
- network metrics
- service metadata (names, images, ports, status)
- container metadata
- health information

In **self-hosted** mode this data never leaves your VPS.

In **cloud** mode this data is transmitted (over TLS, redacted) to `pulse.frix.me`
and shown in your dashboard.

## What Pulse does NOT collect or send

- environment variables
- application secrets
- private keys
- file contents
- database contents

These are never transmitted unless a **future** feature is explicitly designed and
authorized for it — and even then, behind opt-in flags with audit and redaction.

## Secret redaction

A mandatory redaction layer runs **in the agent**, before anything is logged,
stored, transmitted, or displayed. For example:

```text
DATABASE_URL=postgres://user:password@host/db
        ↓
DATABASE_URL=postgres://user:***REDACTED***@host/db
```

Redaction covers connection strings, secret-like env names, PEM blocks, JWTs, cloud
credentials, and common token formats.

## Data control

- **Self-hosted:** you own all data; retention is configurable; `pulse uninstall`
  removes it.
- **Cloud:** you can delete a server (and its data) from your dashboard and revoke
  its agent identity.

## Logs

Logs are structured JSON and **never contain secrets**. Logs shown in the UI are
redacted and escaped (never rendered as raw HTML).
