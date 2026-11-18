-- +goose Up
-- Servers deleted while their node was offline: the node removes their
-- container and files the next time it connects.
CREATE TABLE node_cleanup (
  node_id     INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  server_uuid TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (node_id, server_uuid)
);

-- +goose Down
DROP TABLE node_cleanup;
