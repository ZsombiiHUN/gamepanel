-- +goose Up
-- What a person needs to recognise a session in their account's session list
-- (ASVS v5.0.0-7.5.2). public_id names a session in the API without exposing
-- its token hash; sessions from before the upgrade get one now and show their
-- start time as unknown (0).
ALTER TABLE sessions ADD COLUMN public_id  TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN ip         TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN last_ip    TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN method     TEXT NOT NULL DEFAULT '';
UPDATE sessions SET public_id = lower(hex(randomblob(16)));
CREATE UNIQUE INDEX sessions_public_id ON sessions(public_id);
CREATE INDEX sessions_user ON sessions(user_id);

-- +goose Down
DROP INDEX sessions_user;
DROP INDEX sessions_public_id;
ALTER TABLE sessions DROP COLUMN method;
ALTER TABLE sessions DROP COLUMN user_agent;
ALTER TABLE sessions DROP COLUMN last_ip;
ALTER TABLE sessions DROP COLUMN ip;
ALTER TABLE sessions DROP COLUMN created_at;
ALTER TABLE sessions DROP COLUMN public_id;
