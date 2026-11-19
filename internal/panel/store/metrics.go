package store

import (
	"context"
	"time"
)

// Metric is one bucket of a server's usage history.
type Metric struct {
	At      int64   `json:"at" doc:"bucket start, unix seconds"`
	CPUAvg  float64 `json:"cpu_avg"`
	CPUMax  float64 `json:"cpu_max"`
	MemAvg  int64   `json:"mem_avg"`
	MemMax  int64   `json:"mem_max"`
	Disk    int64   `json:"disk"`
	Running bool    `json:"running" doc:"the server ran during the bucket"`
}

// Resolutions and how long each is kept.
const (
	MetricMinute    = 60
	MetricQuarter   = 900
	keepMinuteRows  = 48 * time.Hour
	keepQuarterRows = 30 * 24 * time.Hour
)

// MetricBucket is what the panel collected for one server in one minute.
type MetricBucket struct {
	Server         string
	At             int64
	CPUSum, CPUMax float64
	MemSum, MemMax int64
	Disk           int64
	Samples        int64 // readings while running
}

// SaveMetrics writes minute buckets (a bucket written twice is merged).
func (s *Store) SaveMetrics(ctx context.Context, bs []MetricBucket) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, b := range bs {
		avgCPU, avgMem := 0.0, int64(0)
		if b.Samples > 0 {
			avgCPU, avgMem = b.CPUSum/float64(b.Samples), b.MemSum/b.Samples
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO server_metrics(server_uuid,res,at,cpu_avg,cpu_max,mem_avg,mem_max,disk,samples)
			SELECT ?,?,?,?,?,?,?,?,? WHERE EXISTS (SELECT 1 FROM servers WHERE uuid=?)
			ON CONFLICT(server_uuid,res,at) DO UPDATE SET
			  cpu_avg=(cpu_avg*samples + excluded.cpu_avg*excluded.samples)/MAX(samples+excluded.samples,1),
			  mem_avg=(mem_avg*samples + excluded.mem_avg*excluded.samples)/MAX(samples+excluded.samples,1),
			  cpu_max=MAX(cpu_max,excluded.cpu_max), mem_max=MAX(mem_max,excluded.mem_max),
			  disk=excluded.disk, samples=samples+excluded.samples`,
			b.Server, MetricMinute, b.At, avgCPU, b.CPUMax, avgMem, b.MemMax, b.Disk, b.Samples, b.Server); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RollupMetrics folds minute buckets older than two days into quarter-hour
// buckets and drops what is past its keeping time.
func (s *Store) RollupMetrics(ctx context.Context, now time.Time) error {
	cut := now.Add(-keepMinuteRows).Unix()
	cut -= cut % MetricQuarter // whole quarters only
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO server_metrics(server_uuid,res,at,cpu_avg,cpu_max,mem_avg,mem_max,disk,samples)
		SELECT server_uuid, ?, at - at % ?,
		  COALESCE(SUM(cpu_avg*samples)/NULLIF(SUM(samples),0),0), MAX(cpu_max),
		  COALESCE(SUM(mem_avg*samples)/NULLIF(SUM(samples),0),0), MAX(mem_max),
		  MAX(disk), SUM(samples)
		FROM server_metrics WHERE res=? AND at<?
		GROUP BY server_uuid, at - at % ?
		ON CONFLICT(server_uuid,res,at) DO NOTHING`, MetricQuarter, MetricQuarter, MetricMinute, cut, MetricQuarter); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM server_metrics WHERE res=? AND at<?`, MetricMinute, cut); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM server_metrics WHERE res=? AND at<?`, MetricQuarter, now.Add(-keepQuarterRows).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// Metrics returns a server's history since a time, oldest first, in
// buckets of step seconds (a multiple of the stored resolution). Long ranges
// read the quarter-hour buckets; the most recent two days come from minutes.
func (s *Store) Metrics(ctx context.Context, server string, since time.Time, step int64) ([]Metric, error) {
	if step < MetricMinute {
		step = MetricMinute
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT at - at % ?,
		  COALESCE(SUM(cpu_avg*samples)/NULLIF(SUM(samples),0),0), MAX(cpu_max),
		  COALESCE(SUM(mem_avg*samples)/NULLIF(SUM(samples),0),0), MAX(mem_max),
		  MAX(disk), SUM(samples)
		FROM server_metrics WHERE server_uuid=? AND at>=? AND (res=? OR (res=? AND ? >= ?))
		GROUP BY at - at % ? ORDER BY 1`,
		step, server, since.Unix(), MetricMinute, MetricQuarter, step, MetricQuarter, step)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Metric{}
	for rows.Next() {
		var m Metric
		var mem float64
		var samples int64
		if err := rows.Scan(&m.At, &m.CPUAvg, &m.CPUMax, &mem, &m.MemMax, &m.Disk, &samples); err != nil {
			return nil, err
		}
		m.MemAvg, m.Running = int64(mem), samples > 0
		out = append(out, m)
	}
	return out, rows.Err()
}
