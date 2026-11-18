-- +goose Up
-- Recovery codes made before this had 80 bits and were hashed with plain
-- SHA-256, which ASVS v5.0.0-6.5.2 only allows from 112 bits up. New codes
-- have 120 bits. The old ones keep working (taking them away could lock
-- people out) but are marked, so the account page asks for new ones.
ALTER TABLE recovery_codes ADD COLUMN legacy INTEGER NOT NULL DEFAULT 0;
UPDATE recovery_codes SET legacy = 1;

-- +goose Down
ALTER TABLE recovery_codes DROP COLUMN legacy;
