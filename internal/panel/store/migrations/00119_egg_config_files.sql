-- +goose Up
-- An egg's config.files: config files the node edits before each start, such
-- as server-port in Minecraft's server.properties. JSON, see EggConfigFile.
ALTER TABLE eggs ADD COLUMN config_files TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE eggs DROP COLUMN config_files;
