-- +goose Up
-- Usage history. Minute buckets (res 60) are kept for two days, then rolled
-- up into quarter-hour buckets (res 900) kept for 30 days.
CREATE TABLE server_metrics (
  server_uuid TEXT NOT NULL REFERENCES servers(uuid) ON DELETE CASCADE,
  res         INTEGER NOT NULL,          -- bucket length in seconds
  at          INTEGER NOT NULL,          -- bucket start, unix seconds
  cpu_avg     REAL NOT NULL,             -- percent of one core
  cpu_max     REAL NOT NULL,
  mem_avg     INTEGER NOT NULL,          -- bytes
  mem_max     INTEGER NOT NULL,
  disk        INTEGER NOT NULL,          -- bytes, at the end of the bucket
  samples     INTEGER NOT NULL,          -- readings while running
  PRIMARY KEY (server_uuid, res, at)
);

-- +goose Down
DROP TABLE server_metrics;
