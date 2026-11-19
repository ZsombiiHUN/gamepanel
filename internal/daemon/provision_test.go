package daemon

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// slowRuntime builds containers only when told to, or fails to.
type slowRuntime struct {
	*runtime.Fake
	mu      sync.Mutex
	gate    chan struct{} // Create waits on it when set
	failing bool          // Create fails, as with an unreachable registry
	creates int
}

func (r *slowRuntime) Create(ctx context.Context, s runtime.Spec) error {
	r.mu.Lock()
	gate, failing := r.gate, r.failing
	r.creates++
	r.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if failing {
		return errors.New("pull access denied for img")
	}
	return r.Fake.Create(ctx, s)
}

func (r *slowRuntime) set(f func(*slowRuntime)) { r.mu.Lock(); f(r); r.mu.Unlock() }

func provisioned(d *Daemon, id string) func() bool {
	return func() bool { st, _ := d.installState(id); return st == "" }
}

// A create answers at once even while the image is still being pulled, so
// the panel never gives up on a server the node goes on to build. Until the
// build ends the server counts as installing and nothing else runs on it.
func TestCreateRepliesBeforeTheImageIsPulled(t *testing.T) {
	t.Parallel()
	rt := &slowRuntime{Fake: runtime.NewFake(), gate: make(chan struct{})}
	d := newTestDaemon(t, rt)
	id := uuidN(1)
	done := make(chan error, 1)
	go func() {
		done <- d.create(context.Background(), proto.ServerSpec{UUID: id, Image: "img", Startup: "run", MemoryMB: 128})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("create waited for the image pull")
	}
	if st, _ := d.installState(id); st != StateInstalling {
		t.Fatalf("while pulling: state %q, want %q", st, StateInstalling)
	}
	if _, err := d.begin(id, opStart); proto.CodeOf(err) != proto.CodeConflict {
		t.Fatalf("start while pulling: %v, want a conflict", err)
	}
	close(rt.gate)
	if !eventually(5*time.Second, provisioned(d, id)) {
		t.Fatal("never finished provisioning")
	}
	if d.isStale(id) {
		t.Fatal("still stale after the container was built")
	}
	if _, err := os.Stat(d.stalePath(id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale marker left behind: %v", err)
	}
	if _, ok := rt.SpecOf(id); !ok {
		t.Fatal("no container")
	}
}

// When the container cannot be built the server is still created, says why on
// its console, and builds the container at its next start, also after the
// daemon restarts in between.
func TestAFailedImagePullIsRetriedAtStart(t *testing.T) {
	t.Parallel()
	rt := &slowRuntime{Fake: runtime.NewFake(), failing: true}
	d := newTestDaemon(t, rt)
	var mu sync.Mutex
	var sent []proto.Envelope
	d.sendHook = func(e proto.Envelope) { mu.Lock(); sent = append(sent, e); mu.Unlock() }
	id := uuidN(1)
	if err := d.create(context.Background(), proto.ServerSpec{UUID: id, Image: "img", Startup: "run", MemoryMB: 128}); err != nil {
		t.Fatal(err)
	}
	if !eventually(5*time.Second, provisioned(d, id)) {
		t.Fatal("never finished provisioning")
	}
	if !d.isStale(id) {
		t.Fatal("a server without a container must be stale")
	}
	told := func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, e := range sent {
			if e.Type == proto.EvtConsole && strings.Contains(string(e.Data), "pull access denied") {
				return true
			}
		}
		return false
	}
	if !eventually(5*time.Second, told) {
		t.Fatal("the console was not told why")
	}

	// A daemon restart remembers that the container still has to be built.
	d2, err := New(d.cfg, rt, d.log)
	if err != nil {
		t.Fatal(err)
	}
	if !d2.isStale(id) {
		t.Fatal("stale lost across a restart")
	}
	// Still failing: the start fails with the reason and the server stays offline.
	if _, err := d2.dispatch(context.Background(), req(proto.ReqServerPower, proto.Power{UUID: id, Action: "start"})); err == nil {
		t.Fatal("start without a container succeeded")
	}
	// The registry is back: the start builds the container and runs it.
	rt.set(func(r *slowRuntime) { r.failing = false })
	if _, err := d2.dispatch(context.Background(), req(proto.ReqServerPower, proto.Power{UUID: id, Action: "start"})); err != nil {
		t.Fatal(err)
	}
	if d2.isStale(id) {
		t.Fatal("still stale after a successful start")
	}
	if st, _ := rt.State(context.Background(), id); st != runtime.StateRunning {
		t.Fatalf("state %q after start", st)
	}
}

// The install script still runs when the server's own image could not be
// pulled (the installer has its own image), and a server with no container
// yet can be reinstalled: nothing of it is running.
func TestInstallAndReinstallWithoutAContainer(t *testing.T) {
	t.Parallel()
	rt := &slowRuntime{Fake: runtime.NewFake(), failing: true}
	d := newTestDaemon(t, rt)
	id := uuidN(1)
	spec := proto.ServerSpec{
		UUID: id, Image: "img", Startup: "run", MemoryMB: 128,
		Install: &proto.InstallSpec{Image: "inst", Entrypoint: "bash", Script: "echo hi\n"},
	}
	if err := d.create(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if !eventually(5*time.Second, provisioned(d, id)) {
		t.Fatal("install never finished")
	}
	if _, err := os.Stat(d.installedPath(id)); err != nil {
		t.Fatalf("install not recorded: %v", err)
	}
	if _, err := d.dispatch(context.Background(), req(proto.ReqServerReinstall, proto.UUIDOnly{UUID: id})); err != nil {
		t.Fatalf("reinstall without a container: %v", err)
	}
	if !eventually(5*time.Second, provisioned(d, id)) {
		t.Fatal("reinstall never finished")
	}
}

// A create that fails before replying leaves nothing on the node, since the
// panel rolls the server back.
func TestAFailedCreateLeavesNothingBehind(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	// A directory where the state file goes makes writing it fail.
	if err := os.Mkdir(d.statePath(id), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := d.create(context.Background(), proto.ServerSpec{UUID: id, Image: "img", Startup: "run", MemoryMB: 128}); err == nil {
		t.Fatal("create succeeded without its state file")
	}
	for _, p := range []string{d.serverDir(id), d.stalePath(id)} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s left behind: %v", p, err)
		}
	}
	if _, ok := d.installState(id); ok {
		t.Error("the daemon still lists the server")
	}
}
