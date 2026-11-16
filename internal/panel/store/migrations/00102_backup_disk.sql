-- +goose Up
-- Where a backup's archive is: "local" (on the node) or "s3".
ALTER TABLE backups ADD COLUMN disk TEXT NOT NULL DEFAULT 'local';

-- +goose Down
ALTER TABLE backups DROP COLUMN disk;
