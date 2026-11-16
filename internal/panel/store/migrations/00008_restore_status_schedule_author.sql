-- +goose Up
-- Restores run in the background; their outcome is kept on the backup.
ALTER TABLE backups ADD COLUMN restore_status TEXT    NOT NULL DEFAULT ''; -- '' | running | ok | failed
ALTER TABLE backups ADD COLUMN restore_error  TEXT    NOT NULL DEFAULT '';
ALTER TABLE backups ADD COLUMN restored_at    INTEGER NOT NULL DEFAULT 0;

-- Who last saved a schedule. Its tasks run with that person's permissions,
-- checked again at every run. NULL (schedules saved before this column
-- existed) runs with the server owner's.
ALTER TABLE schedules ADD COLUMN created_by INTEGER REFERENCES users(id);

CREATE INDEX sessions_expiry ON sessions(expires_at);

-- +goose Down
DROP INDEX sessions_expiry;
ALTER TABLE schedules DROP COLUMN created_by;
ALTER TABLE backups DROP COLUMN restored_at;
ALTER TABLE backups DROP COLUMN restore_error;
ALTER TABLE backups DROP COLUMN restore_status;
