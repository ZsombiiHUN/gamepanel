-- +goose Up
CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  is_admin      INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL
);
CREATE TABLE sessions (
  token_hash TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL
);
CREATE TABLE nodes (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT NOT NULL UNIQUE,
  token_hash TEXT NOT NULL UNIQUE,
  port_start INTEGER NOT NULL DEFAULT 25565,
  port_end   INTEGER NOT NULL DEFAULT 25665,
  created_at INTEGER NOT NULL
);
CREATE TABLE eggs (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  name         TEXT NOT NULL,
  description  TEXT NOT NULL DEFAULT '',
  docker_image TEXT NOT NULL,
  startup      TEXT NOT NULL,
  stop         TEXT NOT NULL DEFAULT '^C',
  variables    TEXT NOT NULL DEFAULT '[]', -- JSON: [{env,name,default,description}]
  created_at   INTEGER NOT NULL
);
CREATE TABLE servers (
  uuid       TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  owner_id   INTEGER NOT NULL REFERENCES users(id),
  node_id    INTEGER NOT NULL REFERENCES nodes(id),
  egg_id     INTEGER NOT NULL REFERENCES eggs(id),
  memory_mb  INTEGER NOT NULL,
  cpu_pct    INTEGER NOT NULL DEFAULT 0,
  port       INTEGER NOT NULL,
  env        TEXT NOT NULL DEFAULT '{}', -- JSON map
  created_at INTEGER NOT NULL,
  UNIQUE(node_id, port)
);

-- +goose Down
DROP TABLE servers; DROP TABLE eggs; DROP TABLE nodes; DROP TABLE sessions; DROP TABLE users;
