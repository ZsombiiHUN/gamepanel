-- +goose Up
-- Numeric server ids for Pterodactyl's application API, which billing
-- modules use: servers are keyed by uuid here, so each gets a number when it
-- is created. AUTOINCREMENT never hands a number out twice, and the row stays
-- after the server is deleted, so a number a shop stored can never point at
-- someone else's server later.
CREATE TABLE server_ids (
  id   INTEGER PRIMARY KEY AUTOINCREMENT,
  uuid TEXT NOT NULL UNIQUE
);
INSERT INTO server_ids(uuid) SELECT uuid FROM servers ORDER BY created_at, uuid;
-- +goose StatementBegin
CREATE TRIGGER servers_number AFTER INSERT ON servers
BEGIN
  -- An import numbers its servers itself, before inserting them. (INSERT OR
  -- IGNORE would use up a number even when it inserts nothing.)
  INSERT INTO server_ids(uuid) SELECT NEW.uuid WHERE NOT EXISTS (SELECT 1 FROM server_ids WHERE uuid = NEW.uuid);
END;
-- +goose StatementEnd

-- Locations group nodes (a data centre, a region). A shop's product names
-- the locations its servers may go to.
CREATE TABLE locations (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  short      TEXT NOT NULL UNIQUE COLLATE NOCASE,
  long       TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
ALTER TABLE nodes ADD COLUMN location_id INTEGER REFERENCES locations(id);

-- +goose Down
ALTER TABLE nodes DROP COLUMN location_id;
DROP TABLE locations;
DROP TRIGGER servers_number;
DROP TABLE server_ids;
