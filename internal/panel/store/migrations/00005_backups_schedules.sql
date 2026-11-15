-- +goose Up
CREATE TABLE backups (
  uuid         TEXT PRIMARY KEY,
  server_uuid  TEXT NOT NULL REFERENCES servers(uuid) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  status       TEXT NOT NULL,            -- running | ok | failed
  error        TEXT NOT NULL DEFAULT '',
  size         INTEGER NOT NULL DEFAULT 0,
  sha256       TEXT NOT NULL DEFAULT '',
  ignore       TEXT NOT NULL DEFAULT '', -- newline separated patterns
  locked       INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL,
  completed_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX backups_server ON backups(server_uuid, created_at DESC);

CREATE TABLE schedules (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  server_uuid      TEXT NOT NULL REFERENCES servers(uuid) ON DELETE CASCADE,
  name             TEXT NOT NULL,
  cron             TEXT NOT NULL,        -- 5 fields: minute hour day-of-month month day-of-week
  enabled          INTEGER NOT NULL DEFAULT 1,
  only_when_online INTEGER NOT NULL DEFAULT 0,
  next_run         INTEGER NOT NULL DEFAULT 0, -- unix seconds, 0 = never
  last_run         INTEGER NOT NULL DEFAULT 0,
  last_status      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX schedules_due ON schedules(enabled, next_run);

CREATE TABLE schedule_tasks (
  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
  schedule_id         INTEGER NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
  seq                 INTEGER NOT NULL,
  action              TEXT NOT NULL,     -- command | power | backup
  payload             TEXT NOT NULL DEFAULT '',
  offset_seconds      INTEGER NOT NULL DEFAULT 0,
  continue_on_failure INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX schedule_tasks_sched ON schedule_tasks(schedule_id, seq);

-- +goose Down
DROP TABLE schedule_tasks;
DROP TABLE schedules;
DROP TABLE backups;
