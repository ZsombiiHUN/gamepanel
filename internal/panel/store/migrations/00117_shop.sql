-- +goose Up
-- What a shop (billing system) needs to sell servers through the API.
--
-- external_id: the shop's own id for a server or an account (an order or a
-- customer number). Unique, so a create the shop retries after a timeout
-- finds the first one instead of making a second.
ALTER TABLE servers ADD COLUMN external_id TEXT;
CREATE UNIQUE INDEX servers_external_id ON servers(external_id) WHERE external_id IS NOT NULL;
ALTER TABLE users ADD COLUMN external_id TEXT;
CREATE UNIQUE INDEX users_external_id ON users(external_id) WHERE external_id IS NOT NULL;

-- suspended: the server is stopped and cannot be started or used by its
-- owner (an unpaid invoice), but nothing is deleted.
ALTER TABLE servers ADD COLUMN suspended INTEGER NOT NULL DEFAULT 0;

-- install_status: installing | installed | failed, kept from the states the
-- node reports, so it survives a panel restart and a shop can poll it.
-- Existing servers were installed long ago.
ALTER TABLE servers ADD COLUMN install_status TEXT NOT NULL DEFAULT 'installed';

-- Webhooks: the panel POSTs signed events to these URLs.
CREATE TABLE webhooks (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  url         TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  secret      TEXT NOT NULL,            -- signs each delivery (HMAC-SHA256)
  events      TEXT NOT NULL,            -- comma separated, or *
  enabled     INTEGER NOT NULL DEFAULT 1,
  created_at  INTEGER NOT NULL
);

-- Each event for each webhook, until it is delivered or given up on.
CREATE TABLE webhook_deliveries (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  webhook_id   INTEGER NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
  event        TEXT NOT NULL,
  payload      TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'pending', -- pending | delivered | failed
  attempts     INTEGER NOT NULL DEFAULT 0,
  next_at      INTEGER NOT NULL,
  last_code    INTEGER NOT NULL DEFAULT 0,
  last_error   TEXT NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL,
  delivered_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX webhook_deliveries_due ON webhook_deliveries(status, next_at);
CREATE INDEX webhook_deliveries_hook ON webhook_deliveries(webhook_id, id);

-- +goose Down
DROP TABLE webhook_deliveries;
DROP TABLE webhooks;
ALTER TABLE servers DROP COLUMN install_status;
ALTER TABLE servers DROP COLUMN suspended;
DROP INDEX users_external_id;
ALTER TABLE users DROP COLUMN external_id;
DROP INDEX servers_external_id;
ALTER TABLE servers DROP COLUMN external_id;
