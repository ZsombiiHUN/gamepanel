package daemon

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// One tick is one message covering every server, built from the watchers'
// samples rather than a container inspect and stats call per server.
func TestStatsAreBatchedFromWatchers(t *testing.T) {
	t.Parallel()
	fake := runtime.NewFake()
	d := newTestDaemon(t, fake)
	var mu sync.Mutex
	var batches []proto.StatsBatch
	d.sendHook = func(env proto.Envelope) {
		if env.Type == proto.EvtStats {
			t.Error("a single-server stats message was sent")
		}
		if env.Type != proto.EvtStatsBatch {
			return
		}
		var b proto.StatsBatch
		json.Unmarshal(env.Data, &b)
		mu.Lock()
		batches = append(batches, b)
		mu.Unlock()
	}
	ctx := context.Background()
	on, off := uuidN(1), uuidN(2)
	addServer(t, d, on, 0)
	addServer(t, d, off, 0)
	if err := d.power(ctx, proto.Power{UUID: on, Action: "start"}); err != nil {
		t.Fatal(err)
	}
	tick := func() map[string]proto.Stats {
		mu.Lock()
		batches = nil
		mu.Unlock()
		d.sendStats(ctx)
		mu.Lock()
		defer mu.Unlock()
		if len(batches) != 1 {
			t.Fatalf("%d messages for one tick", len(batches))
		}
		out := map[string]proto.Stats{}
		for _, s := range batches[0].Stats {
			out[s.UUID] = s
		}
		return out
	}
	deadline := time.Now().Add(3 * time.Second)
	for tick()[on].MemBytes == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no sample from the watcher")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := tick(); len(got) != 2 || got[off].MemBytes != 0 {
		t.Fatalf("batch %+v", got)
	}

	// A stopped server reports no usage, and its watcher is gone.
	if err := fake.Kill(ctx, on); err != nil {
		t.Fatal(err)
	}
	d.emitState(ctx, on, runtime.StateOffline) // what the event watcher does
	if s := tick()[on]; s.MemBytes != 0 || s.CPUPct != 0 {
		t.Fatalf("stopped server still reports %+v", s)
	}
	d.mu.Lock()
	watching := d.servers[on].stopWatch != nil
	d.mu.Unlock()
	if watching {
		t.Fatal("stats watcher still running after stop")
	}
}
