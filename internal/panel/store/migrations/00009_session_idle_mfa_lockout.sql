-- +goose Up
-- Idle timeout: a session not used for a while ends even before it expires.
-- Sessions that exist at upgrade time count as used now, so nobody is signed
-- out by the upgrade itself.
ALTER TABLE sessions ADD COLUMN last_seen_at INTEGER NOT NULL DEFAULT 0;
UPDATE sessions SET last_seen_at = CAST(strftime('%s','now') AS INTEGER);

-- Wrong authenticator codes in a row, kept across restarts, and when the
-- account may next try one (escalating delay, see store.mfaLockFor).
ALTER TABLE users ADD COLUMN mfa_fail_count   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN mfa_locked_until INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE users DROP COLUMN mfa_locked_until;
ALTER TABLE users DROP COLUMN mfa_fail_count;
ALTER TABLE sessions DROP COLUMN last_seen_at;
