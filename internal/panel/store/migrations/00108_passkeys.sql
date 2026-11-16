-- +goose Up
-- Passkeys (WebAuthn credentials). data holds the credential record as the
-- WebAuthn library stores it (public key, sign count, flags, transports).
CREATE TABLE passkeys (
  id           TEXT PRIMARY KEY,      -- credential id, base64url
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  data         BLOB NOT NULL,
  created_at   INTEGER NOT NULL,
  last_used_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX passkeys_user ON passkeys(user_id);

-- The account's WebAuthn user handle: random, so it reveals nothing.
ALTER TABLE users ADD COLUMN webauthn_id BLOB;

-- +goose Down
ALTER TABLE users DROP COLUMN webauthn_id;
DROP TABLE passkeys;
