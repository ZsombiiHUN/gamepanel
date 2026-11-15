-- +goose Up
CREATE TABLE audit_log (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  at          INTEGER NOT NULL,
  actor_id    INTEGER,
  actor_email TEXT NOT NULL DEFAULT '',
  action      TEXT NOT NULL,
  target      TEXT NOT NULL DEFAULT '',
  ip          TEXT NOT NULL DEFAULT '',
  detail      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_log_at ON audit_log(at DESC);
