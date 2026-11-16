-- +goose Up
-- MySQL/MariaDB servers the panel creates databases on. The password is
-- that of an account allowed to create databases and users; it is sealed
-- with the panel's secret key.
CREATE TABLE database_hosts (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  name          TEXT NOT NULL UNIQUE,
  host          TEXT NOT NULL,
  port          INTEGER NOT NULL DEFAULT 3306,
  username      TEXT NOT NULL,
  password_enc  TEXT NOT NULL,
  max_databases INTEGER NOT NULL DEFAULT 0, -- 0 = no limit
  node_id       INTEGER REFERENCES nodes(id) ON DELETE SET NULL, -- preferred for servers on this node
  created_at    INTEGER NOT NULL
);

CREATE TABLE server_databases (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  server_uuid  TEXT NOT NULL REFERENCES servers(uuid) ON DELETE CASCADE,
  host_id      INTEGER NOT NULL REFERENCES database_hosts(id),
  name         TEXT NOT NULL,
  username     TEXT NOT NULL,
  password_enc TEXT NOT NULL,
  remote       TEXT NOT NULL DEFAULT '%', -- where connections may come from
  created_at   INTEGER NOT NULL,
  UNIQUE(host_id, name),
  UNIQUE(host_id, username)
);
CREATE INDEX server_databases_server ON server_databases(server_uuid);

ALTER TABLE servers ADD COLUMN database_limit INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE servers DROP COLUMN database_limit;
DROP TABLE server_databases;
DROP TABLE database_hosts;
