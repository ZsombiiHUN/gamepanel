-- +goose Up
ALTER TABLE servers ADD COLUMN bandwidth_mbit INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE servers DROP COLUMN bandwidth_mbit;
