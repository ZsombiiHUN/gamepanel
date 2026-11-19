package api

import (
	"context"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// Usage history: nodes report every few seconds; the panel folds the
// readings into one-minute buckets in memory and writes each finished minute
// to the database, so the charts can look back hours and weeks.

type metricsCollector struct {
	mu  sync.Mutex
	cur map[string]*store.MetricBucket // server -> the minute being filled
	out []store.MetricBucket           // finished minutes not yet written
}

func minuteOf(t time.Time) int64 { return t.Unix() - t.Unix()%store.MetricMinute }

// collect takes one reading (called by the hub; must not block).
func (a *App) collect(s proto.Stats) {
	c := &a.metrics
	at := minuteOf(time.Now())
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cur == nil {
		c.cur = map[string]*store.MetricBucket{}
	}
	b := c.cur[s.UUID]
	if b != nil && b.At != at {
		c.out = append(c.out, *b)
		b = nil
	}
	if b == nil {
		b = &store.MetricBucket{Server: s.UUID, At: at}
		c.cur[s.UUID] = b
	}
	b.Disk = s.DiskBytes
	// A stopped server reports no memory in use: that is not a reading.
	if s.MemBytes > 0 {
		b.Samples++
		b.CPUSum += s.CPUPct
		b.MemSum += int64(s.MemBytes)
		b.CPUMax = max(b.CPUMax, s.CPUPct)
		b.MemMax = max(b.MemMax, int64(s.MemBytes))
	}
}

// flushMetrics writes every minute that has ended.
func (a *App) flushMetrics(ctx context.Context, now time.Time) {
	c := &a.metrics
	at := minuteOf(now)
	c.mu.Lock()
	for id, b := range c.cur {
		if b.At < at {
			c.out = append(c.out, *b)
			delete(c.cur, id)
		}
	}
	out := c.out
	c.out = nil
	c.mu.Unlock()
	if len(out) == 0 {
		return
	}
	if err := a.Store.SaveMetrics(ctx, out); err != nil {
		a.Log.Warn("could not save usage history", "err", err)
	}
}

// runMetrics writes finished minutes and rolls up old ones until ctx ends.
func (a *App) runMetrics(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	lastRollup := time.Time{}
	for {
		select {
		case <-ctx.Done():
			a.flushMetrics(context.Background(), time.Now().Add(time.Minute)) // keep the last partial minute
			return
		case now := <-t.C:
			a.flushMetrics(ctx, now)
			if now.Sub(lastRollup) >= time.Hour {
				if err := a.Store.RollupMetrics(ctx, now); err != nil {
					a.Log.Warn("could not roll up usage history", "err", err)
				}
				lastRollup = now
			}
		}
	}
}

// Ranges the charts offer, and the bucket size that gives each about
// 150-200 points.
var metricRanges = map[string]struct {
	span time.Duration
	step int64
}{
	"1h":  {time.Hour, 60},
	"6h":  {6 * time.Hour, 120},
	"24h": {24 * time.Hour, 600},
	"7d":  {7 * 24 * time.Hour, 3600},
	"30d": {30 * 24 * time.Hour, 14400},
}

type metricsIn struct {
	UUID  string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Range string `query:"range" enum:"1h,6h,24h,7d,30d" default:"1h"`
}
type metricsOut struct {
	Body struct {
		Step   int64          `json:"step" doc:"seconds per point"`
		Points []store.Metric `json:"points" nullable:"false"`
	}
}

func (a *App) registerMetrics(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "serverMetrics", Method: "GET", Path: "/servers/{uuid}/metrics",
		Summary: "Usage history", Description: "CPU, memory and disk over time. Minutes are kept for two days, quarter hours for 30 days.",
	},
		func(ctx context.Context, in *metricsIn) (*metricsOut, error) {
			_, s, err := a.authorizeP(ctx, in.UUID, PermConsoleRead)
			if err != nil {
				return nil, err
			}
			r := metricRanges[in.Range]
			pts, err := a.Store.Metrics(ctx, s.UUID, time.Now().Add(-r.span), r.step)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			out := &metricsOut{}
			out.Body.Step, out.Body.Points = r.step, pts
			return out, nil
		})
}
