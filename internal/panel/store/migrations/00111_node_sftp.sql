-- +goose Up
ALTER TABLE nodes ADD COLUMN sftp_host TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN sftp_port INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE nodes DROP COLUMN sftp_port;
ALTER TABLE nodes DROP COLUMN sftp_host;
