-- +goose Up
ALTER TABLE servers ADD COLUMN swap_mb      INTEGER NOT NULL DEFAULT 0;    -- 0 = none, -1 = unlimited
ALTER TABLE servers ADD COLUMN cpuset       TEXT    NOT NULL DEFAULT '';   -- e.g. "0-3", empty = any core
ALTER TABLE servers ADD COLUMN pids_limit   INTEGER NOT NULL DEFAULT 1024;
ALTER TABLE servers ADD COLUMN io_weight    INTEGER NOT NULL DEFAULT 0;    -- 10..1000, 0 = engine default
ALTER TABLE servers ADD COLUMN disk_mb      INTEGER NOT NULL DEFAULT 0;    -- 0 = unlimited
ALTER TABLE servers ADD COLUMN backup_limit INTEGER NOT NULL DEFAULT 20;

ALTER TABLE nodes ADD COLUMN memory_mb       INTEGER NOT NULL DEFAULT 0;   -- capacity, 0 = not tracked
ALTER TABLE nodes ADD COLUMN disk_mb         INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN overallocate_pct INTEGER NOT NULL DEFAULT 0;  -- allow sum of limits up to capacity * (1 + pct/100)

-- +goose Down
ALTER TABLE nodes DROP COLUMN overallocate_pct;
ALTER TABLE nodes DROP COLUMN disk_mb;
ALTER TABLE nodes DROP COLUMN memory_mb;
ALTER TABLE servers DROP COLUMN backup_limit;
ALTER TABLE servers DROP COLUMN disk_mb;
ALTER TABLE servers DROP COLUMN io_weight;
ALTER TABLE servers DROP COLUMN pids_limit;
ALTER TABLE servers DROP COLUMN cpuset;
ALTER TABLE servers DROP COLUMN swap_mb;
