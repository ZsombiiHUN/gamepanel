package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// A new server can be started as soon as it is installed.
func TestStartOnCompletion(t *testing.T) {
	rt := runtime.NewFake()
	d := newTestDaemon(t, rt)
	id := uuidN(1)
	if err := d.create(context.Background(), proto.ServerSpec{UUID: id, Image: "img", Startup: "run", MemoryMB: 128, StartOnCompletion: true,
		Install: &proto.InstallSpec{Image: "inst", Entrypoint: "bash", Script: "echo hi\n"}}); err != nil {
		t.Fatal(err)
	}
	if !eventually(5*time.Second, func() bool { st, _ := rt.State(context.Background(), id); return st == runtime.StateRunning }) {
		t.Fatal("not started after the install")
	}
}

// Suspending stops a running server, and nothing starts it again: not a
// start request, not a crash restart, not the end of an install. The
// suspension applies even while an install runs.
func TestASuspendedServerStaysStopped(t *testing.T) {
	rt := &slowRuntime{Fake: runtime.NewFake()}
	d := newTestDaemon(t, rt)
	id := uuidN(1)
	ctx := context.Background()
	spec := proto.ServerSpec{UUID: id, Image: "img", Startup: "run", MemoryMB: 128}
	if err := d.create(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if !eventually(5*time.Second, provisioned(d, id)) {
		t.Fatal("never provisioned")
	}
	if _, err := d.dispatch(ctx, req(proto.ReqServerPower, proto.Power{UUID: id, Action: "start"})); err != nil {
		t.Fatal(err)
	}
	sus := spec
	sus.Suspended = true
	if _, err := d.dispatch(ctx, req(proto.ReqServerUpdate, sus)); err != nil {
		t.Fatal(err)
	}
	if !eventually(5*time.Second, func() bool { st, _ := rt.State(ctx, id); return st == runtime.StateOffline }) {
		t.Fatal("still running after the suspension")
	}
	_, err := d.dispatch(ctx, req(proto.ReqServerPower, proto.Power{UUID: id, Action: "start"}))
	if proto.CodeOf(err) != proto.CodeConflict {
		t.Fatalf("start while suspended: %v", err)
	}
	// It survives a daemon restart.
	d2, err := New(d.cfg, rt, d.log)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d2.dispatch(ctx, req(proto.ReqServerPower, proto.Power{UUID: id, Action: "start"})); proto.CodeOf(err) != proto.CodeConflict {
		t.Fatalf("start after a restart: %v", err)
	}

	// A new server suspended while it is still being built is not started at
	// the end, though it was asked to be.
	id2 := uuidN(2)
	rt.set(func(r *slowRuntime) { r.gate = make(chan struct{}) })
	spec2 := proto.ServerSpec{UUID: id2, Image: "img", Startup: "run", MemoryMB: 128, StartOnCompletion: true}
	if err := d.create(ctx, spec2); err != nil {
		t.Fatal(err)
	}
	spec2.Suspended = true
	if _, err := d.dispatch(ctx, req(proto.ReqServerUpdate, spec2)); err != nil {
		t.Fatalf("suspend while installing: %v", err)
	}
	close(rt.gate)
	if !eventually(5*time.Second, provisioned(d, id2)) {
		t.Fatal("never provisioned")
	}
	time.Sleep(100 * time.Millisecond)
	if st, _ := rt.State(ctx, id2); st == runtime.StateRunning {
		t.Fatal("a suspended server was started at the end of its install")
	}
}
