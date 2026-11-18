package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// gatedStopRuntime is the fake runtime whose Stop waits on a gate, so a stop
// or a restart can be caught while it holds the server.
type gatedStopRuntime struct {
	*runtime.Fake
	gate chan struct{}
}

func (r *gatedStopRuntime) Stop(ctx context.Context, uuid string, timeout int) error {
	<-r.gate
	return r.Fake.Stop(ctx, uuid, timeout)
}

func power(d *Daemon, id, action string) error {
	_, err := d.dispatch(context.Background(), req(proto.ReqServerPower, proto.Power{UUID: id, Action: action}))
	return err
}

func opOf(d *Daemon, id string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if m := d.servers[id]; m != nil {
		return m.op
	}
	return ""
}

func stateIs(rt runtime.Runtime, id, want string) func() bool {
	return func() bool { st, _ := rt.State(context.Background(), id); return st == want }
}

// A stop or a kill that lands while a start is under way is turned away, not
// run and then undone by the start reporting the server as running.
func TestStopAndKillAreRefusedDuringAStart(t *testing.T) {
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 0)
	end, _ := d.begin(id, opStart)
	defer end()
	for _, a := range []string{"stop", "kill", "restart"} {
		if err := power(d, id, a); proto.CodeOf(err) != proto.CodeConflict {
			t.Errorf("%s during a start: %v", a, err)
		}
	}
}

// A restart holds the server from its stop through its start, so a second
// restart, a start or a stop in between is refused. A kill goes through, and
// ends the restart instead of being undone by its start.
func TestRestartHoldsTheServerAndAKillEndsIt(t *testing.T) {
	rt := &gatedStopRuntime{Fake: runtime.NewFake(), gate: make(chan struct{})}
	d := newTestDaemon(t, rt)
	id := uuidN(1)
	addServer(t, d, id, 0)
	if err := power(d, id, "start"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- power(d, id, "restart") }()
	if !eventually(5*time.Second, func() bool { return opOf(d, id) == opRestart }) {
		t.Fatal("restart never claimed the server")
	}
	for _, a := range []string{"restart", "start", "stop"} {
		if err := power(d, id, a); proto.CodeOf(err) != proto.CodeConflict {
			t.Errorf("%s during a restart: %v", a, err)
		}
	}
	if err := power(d, id, "kill"); err != nil {
		t.Fatalf("kill during a restart: %v", err)
	}
	close(rt.gate)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("restart: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("restart never returned")
	}
	time.Sleep(100 * time.Millisecond)
	if st, _ := rt.State(context.Background(), id); st != runtime.StateOffline {
		t.Fatalf("the restart started the killed server again: %s", st)
	}
	if opOf(d, id) != "" {
		t.Fatal("the restart did not release the server")
	}
}

// Without a kill, a restart stops the server and starts it again.
func TestRestartStartsTheServerAgain(t *testing.T) {
	rt := runtime.NewFake()
	d := newTestDaemon(t, rt)
	id := uuidN(1)
	addServer(t, d, id, 0)
	if err := power(d, id, "start"); err != nil {
		t.Fatal(err)
	}
	if err := power(d, id, "restart"); err != nil {
		t.Fatal(err)
	}
	if st, _ := rt.State(context.Background(), id); st != runtime.StateRunning {
		t.Fatalf("after a restart: %s", st)
	}
}

// A kill on a server that is not running has nothing to do and is no error.
func TestKillingAStoppedServerIsANoOp(t *testing.T) {
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 0)
	if err := power(d, id, "kill"); err != nil {
		t.Fatalf("kill while offline: %v", err)
	}
	if opOf(d, id) != "" {
		t.Fatal("the kill did not release the server")
	}
}

// A kill cuts a hung stop short.
func TestKillCutsAStopShort(t *testing.T) {
	rt := &gatedStopRuntime{Fake: runtime.NewFake(), gate: make(chan struct{})}
	d := newTestDaemon(t, rt)
	id := uuidN(1)
	addServer(t, d, id, 0)
	if err := power(d, id, "start"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- power(d, id, "stop") }()
	if !eventually(5*time.Second, func() bool { return opOf(d, id) == opStop }) {
		t.Fatal("stop never claimed the server")
	}
	if err := power(d, id, "kill"); err != nil {
		t.Fatalf("kill during a stop: %v", err)
	}
	if !eventually(5*time.Second, stateIs(rt, id, runtime.StateOffline)) {
		t.Fatal("still running after the kill")
	}
	close(rt.gate)
	if err := <-done; err != nil {
		t.Fatalf("stop: %v", err)
	}
}

// A suspension that lands while a start holds the server still stops it,
// once the start is done.
func TestASuspensionStopWaitsForAStart(t *testing.T) {
	rt := runtime.NewFake()
	d := newTestDaemon(t, rt)
	id := uuidN(1)
	ctx := context.Background()
	spec := proto.ServerSpec{UUID: id, Image: "img", Startup: "run", Stop: "^C", MemoryMB: 128}
	if err := d.create(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if !eventually(5*time.Second, provisioned(d, id)) {
		t.Fatal("never provisioned")
	}
	if err := power(d, id, "start"); err != nil {
		t.Fatal(err)
	}
	end, err := d.begin(id, opStart)
	if err != nil {
		t.Fatal(err)
	}
	spec.Suspended = true
	if _, err := d.dispatch(ctx, req(proto.ReqServerUpdate, spec)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if st, _ := rt.State(ctx, id); st != runtime.StateRunning {
		t.Fatalf("stopped while the start still held the server: %s", st)
	}
	end()
	if !eventually(5*time.Second, stateIs(rt, id, runtime.StateOffline)) {
		t.Fatal("never stopped after the start let go")
	}
}

// A start sent just as a stop finishes waits for it instead of being turned
// away; one sent while a stop is still under way is refused.
func TestAStartWaitsForAStopThatIsEnding(t *testing.T) {
	rt := runtime.NewFake()
	d := newTestDaemon(t, rt)
	id := uuidN(1)
	addServer(t, d, id, 0)
	end, _ := d.begin(id, opStop)
	go func() { time.Sleep(300 * time.Millisecond); end() }()
	if err := power(d, id, "start"); err != nil {
		t.Fatalf("start as a stop ends: %v", err)
	}
	if st, _ := rt.State(context.Background(), id); st != runtime.StateRunning {
		t.Fatalf("after the start: %s", st)
	}
	end2, _ := d.begin(id, opStop)
	defer end2()
	if err := power(d, id, "start"); proto.CodeOf(err) != proto.CodeConflict {
		t.Fatalf("start during a long stop: %v", err)
	}
}
