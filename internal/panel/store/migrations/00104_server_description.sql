-- +goose Up
ALTER TABLE servers ADD COLUMN description TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE servers DROP COLUMN description;
