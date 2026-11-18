-- +goose Up
ALTER TABLE servers ADD COLUMN crash_restart INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE servers DROP COLUMN crash_restart;
