-- Pulse control-plane schema (PostgreSQL). See docs/DATA_MODEL.md.
-- Time-series metrics do NOT live here — they live in Prometheus.
--
-- Multi-tenancy: every tenant-owned row carries org_id and composite lookups
-- are (org_id, id). Optional row-level security policies are included at the
-- bottom for defence in depth.

BEGIN;

CREATE TABLE IF NOT EXISTS organizations (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    email         TEXT UNIQUE,              -- nullable: some OIDC logins share no email
    name          TEXT,
    password_hash TEXT NOT NULL DEFAULT '',
    mfa_secret    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Federated identities (Google, Telegram, ...) linked to a user.
CREATE TABLE IF NOT EXISTS oidc_identities (
    provider   TEXT NOT NULL,
    subject    TEXT NOT NULL,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject)
);

CREATE TABLE IF NOT EXISTS memberships (
    org_id  TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role    TEXT NOT NULL CHECK (role IN ('owner','admin','viewer')),
    PRIMARY KEY (org_id, user_id)
);

CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,               -- hash of the cookie secret
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id      TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    csrf_secret TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS servers (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    server_id    TEXT NOT NULL,                 -- public opaque id
    hostname     TEXT,
    mode         TEXT NOT NULL DEFAULT 'local',
    status       TEXT NOT NULL DEFAULT 'UNKNOWN',
    last_seen_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, server_id)
);
CREATE INDEX IF NOT EXISTS idx_servers_org ON servers(org_id);

CREATE TABLE IF NOT EXISTS agents (
    id               TEXT PRIMARY KEY,
    org_id           TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    server_id        TEXT NOT NULL,
    agent_id         TEXT NOT NULL UNIQUE,
    public_key       TEXT NOT NULL,
    protocol_version TEXT NOT NULL,
    revoked_at       TIMESTAMPTZ,
    last_seen_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_agents_org ON agents(org_id);

CREATE TABLE IF NOT EXISTS enrollment_tokens (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    org_id      TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    fingerprint TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Latest discovery snapshot + latest metrics sample per server (JSONB, redacted).
CREATE TABLE IF NOT EXISTS discovery_snapshots (
    org_id     TEXT NOT NULL,
    server_id  TEXT NOT NULL,
    snapshot   JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, server_id)
);

CREATE TABLE IF NOT EXISTS metric_samples (
    org_id     TEXT NOT NULL,
    server_id  TEXT NOT NULL,
    sample     JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, server_id)
);

CREATE TABLE IF NOT EXISTS alerts (
    id               TEXT PRIMARY KEY,
    org_id           TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    expr             TEXT NOT NULL,
    severity         TEXT NOT NULL CHECK (severity IN ('INFO','WARNING','CRITICAL')),
    for_seconds      INTEGER NOT NULL DEFAULT 0,
    cooldown_seconds INTEGER NOT NULL DEFAULT 0,
    enabled          BOOLEAN NOT NULL DEFAULT true
);
CREATE INDEX IF NOT EXISTS idx_alerts_org ON alerts(org_id);

CREATE TABLE IF NOT EXISTS alert_instances (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    alert_id    TEXT NOT NULL,
    name        TEXT NOT NULL,
    severity    TEXT NOT NULL,
    server_id   TEXT NOT NULL,
    state       TEXT NOT NULL CHECK (state IN ('firing','resolved')),
    dedup_key   TEXT NOT NULL,
    root_cause  TEXT,
    affected    JSONB,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_instances_org_state ON alert_instances(org_id, state);
CREATE UNIQUE INDEX IF NOT EXISTS idx_instances_dedup ON alert_instances(org_id, dedup_key) WHERE state = 'firing';

CREATE TABLE IF NOT EXISTS events (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    server_id  TEXT NOT NULL,
    severity   TEXT NOT NULL,
    source     TEXT NOT NULL,
    event      TEXT NOT NULL,
    prev_state TEXT,
    new_state  TEXT,
    ts         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_events_org_ts ON events(org_id, ts DESC);

CREATE TABLE IF NOT EXISTS audit_log (
    id        TEXT PRIMARY KEY,
    org_id    TEXT NOT NULL,
    actor     TEXT NOT NULL,
    action    TEXT NOT NULL,
    result    TEXT NOT NULL,
    ip        TEXT,
    metadata  JSONB,
    ts        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_org_ts ON audit_log(org_id, ts DESC);

-- Self-healing: bring an older/partial `users` table up to date so identity
-- (Google/Telegram) logins work. Safe to re-run on any schema version.
ALTER TABLE users ADD COLUMN IF NOT EXISTS name TEXT;
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;
ALTER TABLE users ALTER COLUMN password_hash SET DEFAULT '';

COMMIT;

-- ---------------------------------------------------------------------------
-- Optional: Row-Level Security policies (defence in depth). Enable if the app
-- sets `SET app.current_org = '<org_id>'` per connection. Application-layer
-- scoping is always enforced regardless.
-- ---------------------------------------------------------------------------
-- ALTER TABLE servers ENABLE ROW LEVEL SECURITY;
-- CREATE POLICY servers_tenant_isolation ON servers
--   USING (org_id = current_setting('app.current_org', true));
