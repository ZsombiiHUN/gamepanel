-- +goose Up
ALTER TABLE eggs ADD COLUMN install_image TEXT NOT NULL DEFAULT '';
ALTER TABLE eggs ADD COLUMN install_entrypoint TEXT NOT NULL DEFAULT 'bash';
ALTER TABLE eggs ADD COLUMN install_script TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE eggs DROP COLUMN install_script;
ALTER TABLE eggs DROP COLUMN install_entrypoint;
ALTER TABLE eggs DROP COLUMN install_image;
