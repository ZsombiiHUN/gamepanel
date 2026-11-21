-- +goose Up
-- features: the egg's Pterodactyl "features" list (e.g. ["eula",
-- "java_version"]), which the UI uses to offer game-specific help.
-- done: console lines that mean the server has finished starting
-- (config.startup.done); until one is printed the server reports "starting".
ALTER TABLE eggs ADD COLUMN features TEXT NOT NULL DEFAULT '[]';
ALTER TABLE eggs ADD COLUMN done TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE eggs DROP COLUMN done;
ALTER TABLE eggs DROP COLUMN features;
