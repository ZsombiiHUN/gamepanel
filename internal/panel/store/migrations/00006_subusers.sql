-- +goose Up
CREATE TABLE server_users (
  server_uuid TEXT NOT NULL REFERENCES servers(uuid) ON DELETE CASCADE,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  permissions TEXT NOT NULL,           -- comma separated, validated against AllPermissions
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (server_uuid, user_id)
);
CREATE INDEX server_users_user ON server_users(user_id);

-- +goose Down
DROP TABLE server_users;
