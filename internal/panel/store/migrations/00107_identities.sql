-- +goose Up
-- Single sign-on identities linked to accounts: one account may have
-- several, an identity belongs to one account.
CREATE TABLE user_identities (
  provider   TEXT NOT NULL,
  subject    TEXT NOT NULL,           -- the provider's stable user id
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  email      TEXT NOT NULL DEFAULT '', -- as the provider reported it, for display
  created_at INTEGER NOT NULL,
  PRIMARY KEY (provider, subject)
);
CREATE INDEX user_identities_user ON user_identities(user_id);

-- Accounts made through single sign-on have no password until they set one.
ALTER TABLE users ADD COLUMN has_password INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE users DROP COLUMN has_password;
DROP TABLE user_identities;
