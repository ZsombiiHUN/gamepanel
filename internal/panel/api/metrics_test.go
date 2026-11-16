package api

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

func TestCollectorBucketsReadingsByMinute(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	u, _ := st.CreateUser(ctx, "a@example.com", "correct-horse-battery", true)
	n, _, _ := st.CreateNode(ctx, "n", 25565, 25600)
	e, _ := st.CreateEgg(ctx, store.Egg{Name: "e", DockerImage: "i", Startup: "x", Stop: "^C"})
	srv, _ := st.CreateServer(ctx, store.Server{UUID: "11111111-1111-4111-8111-111111111111", Name: "s", OwnerID: u.ID, NodeID: n.ID, EggID: e.ID, Limits: store.Limits{MemoryMB: 128}})
	a := &App{Deps: Deps{Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}

	for _, s := range []proto.Stats{
		{UUID: srv.UUID, CPUPct: 10, MemBytes: 100, DiskBytes: 7},
		{UUID: srv.UUID, CPUPct: 30, MemBytes: 300, DiskBytes: 8},
		{UUID: srv.UUID, DiskBytes: 8}, // stopped: no reading
	} {
		a.collect(s)
	}
	// Nothing is written until the minute is over.
	a.flushMetrics(ctx, time.Now())
	if pts, _ := st.Metrics(ctx, srv.UUID, time.Now().Add(-time.Hour), 60); len(pts) != 0 {
		t.Fatalf("an unfinished minute was written: %+v", pts)
	}
	a.flushMetrics(ctx, time.Now().Add(time.Minute))
	pts, _ := st.Metrics(ctx, srv.UUID, time.Now().Add(-time.Hour), 60)
	if len(pts) != 1 || pts[0].CPUAvg != 20 || pts[0].CPUMax != 30 || pts[0].MemAvg != 200 || pts[0].Disk != 8 {
		t.Fatalf("bucket %+v", pts)
	}
}
