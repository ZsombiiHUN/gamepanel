package daemon

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
)

// reboot simulates the node going down: every container stops without the
// daemon seeing it (it was stopped first), then a new daemon starts on the
// same data directory and the same engine.
func reboot(t *testing.T, d *Daemon, rt *runtime.Fake, cfg func(*Config)) *Daemon {
	t.Helper()
	ctx := context.Background()
	ids, _ := rt.List(ctx)
	for _, id := range ids {
		if err := rt.Stop(ctx, id, 0); err != nil {
			t.Fatal(err)
		}
		// The fake's kill returns before the process has gone.
		if !eventually(5*time.Second, stateIs(rt, id, runtime.StateOffline)) {
			t.Fatalf("server %s never stopped", id)
		}
	}
	c := d.cfg
	if cfg != nil {
		cfg(&c)
	}
	d2, err := New(c, rt, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return d2
}

// After a reboot, the servers that were running start again; those someone
// stopped stay stopped.
func TestServersThatWereRunningStartAfterAReboot(t *testing.T) {
	t.Parallel()
	rt := runtime.NewFake()
	d := newTestDaemon(t, rt)
	running, stopped, never := uuidN(1), uuidN(2), uuidN(3)
	for _, id := range []string{running, stopped, never} {
		addServer(t, d, id, 0)
	}
	for _, id := range []string{running, stopped} {
		if err := power(d, id, "start"); err != nil {
			t.Fatal(err)
		}
	}
	if err := power(d, stopped, "stop"); err != nil {
		t.Fatal(err)
	}

	d2 := reboot(t, d, rt, nil)
	if n := d2.resume(context.Background()); n != 1 {
		t.Fatalf("resume started %d servers, want 1", n)
	}
	for id, want := range map[string]string{running: runtime.StateRunning, stopped: runtime.StateOffline, never: runtime.StateOffline} {
		if st, _ := rt.State(context.Background(), id); st != want {
			t.Errorf("server %s after the reboot: %s, want %s", id[len(id)-1:], st, want)
		}
	}
	// A daemon restart with the server still running starts nothing twice.
	if n := d2.resume(context.Background()); n != 0 {
		t.Errorf("a second resume started %d servers", n)
	}
}

// A kill counts as a stop: the server stays down after a reboot.
func TestAKilledServerStaysDownAfterAReboot(t *testing.T) {
	t.Parallel()
	rt := runtime.NewFake()
	d := newTestDaemon(t, rt)
	id := uuidN(1)
	addServer(t, d, id, 0)
	if err := power(d, id, "start"); err != nil {
		t.Fatal(err)
	}
	if err := power(d, id, "kill"); err != nil {
		t.Fatal(err)
	}
	if n := reboot(t, d, rt, nil).resume(context.Background()); n != 0 {
		t.Errorf("resume started %d servers", n)
	}
}

// A server that stops itself cleanly (exit code 0, e.g. "stop" typed in its
// console) or crashes and is left stopped is not started again.
func TestServersThatStoppedThemselvesStayDown(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := newTestDaemon(t, runtime.NewFake())
	clean, crashed := uuidN(1), uuidN(2)
	for _, id := range []string{clean, crashed} {
		addServer(t, d, id, 0)
		if err := power(d, id, "start"); err != nil {
			t.Fatal(err)
		}
	}
	d.exited(ctx, clean, runtime.Exit{Code: 0})
	d.mu.Lock()
	d.servers[crashed].spec.NoCrashRestart = true
	d.mu.Unlock()
	if !d.exited(ctx, crashed, runtime.Exit{Code: 1}) {
		t.Fatal("exit code 1 was not taken for a crash")
	}
	for _, id := range []string{clean, crashed} {
		if d.shouldRun(id) {
			t.Errorf("server %s is still marked to run", id[len(id)-1:])
		}
	}
}

// An exit seen while the daemon shuts down (Docker stopping everything with
// the machine) does not forget that the server should run.
func TestExitsDuringShutdownKeepTheMarker(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 0)
	if err := power(d, id, "start"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d.exited(ctx, id, runtime.Exit{Code: 0})
	if !d.shouldRun(id) {
		t.Error("the marker was cleared by an exit during shutdown")
	}
}

// GAMEPANEL_RESUME=off (Config.NoResume), a suspended server and a deleted
// one are never started.
func TestResumeSkips(t *testing.T) {
	t.Parallel()
	rt := runtime.NewFake()
	d := newTestDaemon(t, rt)
	a, b := uuidN(1), uuidN(2)
	for _, id := range []string{a, b} {
		addServer(t, d, id, 0)
		if err := power(d, id, "start"); err != nil {
			t.Fatal(err)
		}
	}
	if n := reboot(t, d, rt, func(c *Config) { c.NoResume = true }).resume(context.Background()); n != 0 {
		t.Errorf("with NoResume, resume started %d servers", n)
	}

	d2 := reboot(t, d, rt, nil)
	d2.mu.Lock()
	d2.servers[a].spec.Suspended = true
	d2.mu.Unlock()
	if err := d2.remove(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if d2.shouldRun(b) {
		t.Error("a deleted server kept its marker")
	}
	if n := d2.resume(context.Background()); n != 0 {
		t.Errorf("resume started %d servers, want none", n)
	}
}
