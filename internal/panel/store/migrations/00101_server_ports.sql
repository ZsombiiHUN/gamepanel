-- +goose Up
-- Every port a server has on its node; servers.port stays the primary one
-- (SERVER_PORT). Ports are unique per node.
CREATE TABLE server_ports (
  node_id     INTEGER NOT NULL REFERENCES nodes(id),
  port        INTEGER NOT NULL,
  server_uuid TEXT NOT NULL REFERENCES servers(uuid) ON DELETE CASCADE,
  notes       TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (node_id, port)
);
CREATE INDEX server_ports_server ON server_ports(server_uuid);
INSERT INTO server_ports(node_id, port, server_uuid, created_at)
  SELECT node_id, port, uuid, created_at FROM servers;

-- How many ports beyond the primary one the owner may add.
ALTER TABLE servers ADD COLUMN port_limit INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE servers DROP COLUMN port_limit;
DROP TABLE server_ports;
