-- +goose Up
-- One-time links: password resets and invitations. Only a hash of each
-- token is stored, like sessions.
CREATE TABLE user_tokens (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  token_hash  TEXT NOT NULL UNIQUE,
  kind        TEXT NOT NULL,            -- reset | invite
  user_id     INTEGER REFERENCES users(id) ON DELETE CASCADE,   -- reset: whose
  email       TEXT NOT NULL DEFAULT '', -- invite: who is invited
  server_uuid TEXT REFERENCES servers(uuid) ON DELETE CASCADE,  -- invite: onto which server
  permissions TEXT NOT NULL DEFAULT '', -- invite: what they may do there
  is_admin    INTEGER NOT NULL DEFAULT 0,
  created_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
  expires_at  INTEGER NOT NULL,
  created_at  INTEGER NOT NULL
);
CREATE INDEX user_tokens_server ON user_tokens(server_uuid);

-- +goose Down
DROP TABLE user_tokens;
