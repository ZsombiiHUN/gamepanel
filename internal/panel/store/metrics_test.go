package store

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestMetricsSaveRollupAndRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, srv := testServer(t)
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	minuteAgo := func(ago time.Duration) int64 { t := now.Add(-ago).Unix(); return t - t%60 }

	// Two halves of the same minute merge into one bucket with a fair average.
	s.SaveMetrics(ctx, []MetricBucket{{Server: srv, At: minuteAgo(time.Minute), CPUSum: 30, CPUMax: 20, MemSum: 300, MemMax: 200, Disk: 5, Samples: 3}})
	s.SaveMetrics(ctx, []MetricBucket{{Server: srv, At: minuteAgo(time.Minute), CPUSum: 90, CPUMax: 90, MemSum: 100, MemMax: 100, Disk: 6, Samples: 1}})
	pts, err := s.Metrics(ctx, srv, now.Add(-time.Hour), 60)
	if err != nil || len(pts) != 1 {
		t.Fatalf("%v %+v", err, pts)
	}
	p := pts[0]
	if p.CPUAvg != 30 || p.CPUMax != 90 || p.MemAvg != 100 || p.MemMax != 200 || p.Disk != 6 || !p.Running {
		t.Fatalf("merged %+v", p)
	}
	// A minute with no readings (server stopped) is not running and does not drag averages.
	s.SaveMetrics(ctx, []MetricBucket{{Server: srv, At: minuteAgo(2 * time.Minute), Disk: 6}})
	pts, _ = s.Metrics(ctx, srv, now.Add(-time.Hour), 600)
	if len(pts) != 1 || pts[0].CPUAvg != 30 {
		t.Fatalf("stopped minute counted: %+v", pts)
	}
	// Unknown servers are ignored rather than failing the batch.
	if err := s.SaveMetrics(ctx, []MetricBucket{{Server: "nope", At: minuteAgo(0), Samples: 1}}); err != nil {
		t.Fatal(err)
	}

	// Three days of one reading every hour: the old part rolls up into quarters.
	var bs []MetricBucket
	for h := 1; h <= 72; h++ {
		bs = append(bs, MetricBucket{Server: srv, At: minuteAgo(time.Duration(h) * time.Hour), CPUSum: float64(h), CPUMax: float64(h), MemSum: 1, MemMax: 1, Samples: 1})
	}
	s.SaveMetrics(ctx, bs)
	if err := s.RollupMetrics(ctx, now); err != nil {
		t.Fatal(err)
	}
	var minutes, quarters int
	s.DB.QueryRow(`SELECT COUNT(*) FROM server_metrics WHERE res=60`).Scan(&minutes)
	s.DB.QueryRow(`SELECT COUNT(*) FROM server_metrics WHERE res=900`).Scan(&quarters)
	if quarters == 0 || minutes > 50 {
		t.Fatalf("after rollup: %d minutes, %d quarters", minutes, quarters)
	}
	week, _ := s.Metrics(ctx, srv, now.Add(-7*24*time.Hour), 3600)
	if len(week) < 70 {
		t.Fatalf("a week reads both resolutions: %d points", len(week))
	}
	var total float64
	for _, p := range week {
		total += p.CPUMax
	}
	// Hour buckets keep the peak: the reading of an hour ago (1) shares its
	// bucket with the one of a minute ago (90), so 90 stands for both.
	if want := float64(72*73/2) - 1 + 90; math.Abs(total-want) > 1e-6 {
		t.Fatalf("readings lost or doubled in the rollup: %v, want %v", total, want)
	}
	// Past 30 days everything goes.
	s.RollupMetrics(ctx, now.Add(40*24*time.Hour))
	var left int
	s.DB.QueryRow(`SELECT COUNT(*) FROM server_metrics`).Scan(&left)
	if left != 0 {
		t.Fatalf("%d buckets outlived their keeping time", left)
	}
}
