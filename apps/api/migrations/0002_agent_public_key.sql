-- Signed re-enrolment looks an agent up by the Ed25519 key it enrolled with,
-- so that lookup needs an index. See httpx.handleReenroll.
--
-- Deliberately NOT unique: an agent that is revoked and later reinstalled with
-- the same key leaves two rows, and the lookup prefers the live one.

BEGIN;

CREATE INDEX IF NOT EXISTS idx_agents_public_key ON agents(public_key);

COMMIT;
