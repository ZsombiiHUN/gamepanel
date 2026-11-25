package daemon

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

type fakeWings struct {
	mu      sync.Mutex
	running map[string]bool
	stopped []string // "<uuid> <stop command>"
}

func (f *fakeWings) WingsRunning(_ context.Context, uuid string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running[uuid], nil
}

func (f *fakeWings) StopWings(_ context.Context, uuid, stop string, _ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running[uuid] = false
	f.stopped = append(f.stopped, uuid+" "+stop)
	return nil
}

func (f *fakeWings) stops() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.stopped)
}

// wingsHost is the fake runtime on a node where Wings' containers also run.
type wingsHost struct {
	*runtime.Fake
	*fakeWings
}

// wingsNode lays out /var/lib/pterodactyl as Wings leaves it: a volume per
// server, and states.json.
func wingsNode(t *testing.T, ids ...string) (volumes string) {
	t.Helper()
	root := t.TempDir()
	volumes = filepath.Join(root, "volumes")
	for _, id := range ids {
		dir := filepath.Join(volumes, id)
		if err := os.MkdirAll(filepath.Join(dir, "world"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte("server-port=25565\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "world", "level.dat"), []byte("world of "+id), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(volumes, ".sftp"), 0o755); err != nil { // not a server
		t.Fatal(err)
	}
	states := `{"` + ids[0] + `":"running"`
	for _, id := range ids[1:] {
		states += `,"` + id + `":"offline"`
	}
	if err := os.WriteFile(filepath.Join(root, "states.json"), []byte(states+"}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return volumes
}

// Taking over a Wings node, then the panel creating the imported servers:
// the files stay, nothing is reinstalled, and the server Wings had running
// starts again.
func TestAdoptWings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a, b := uuidN(1), uuidN(2)
	volumes := wingsNode(t, a, b)
	data := t.TempDir()
	dk := &fakeWings{running: map[string]bool{a: true}}
	opts := AdoptOptions{From: volumes, DataDir: data, States: filepath.Join(filepath.Dir(volumes), "states.json"), Docker: dk}

	// a still runs under Wings. On one file system its folder moves anyway:
	// the container's mount follows it.
	res, err := AdoptWings(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Moved, []string{a, b}) || len(dk.stops()) != 0 {
		t.Fatalf("result %+v, stopped %v", res, dk.stops())
	}
	if _, err := os.Stat(filepath.Join(volumes, a)); err == nil {
		t.Fatal("the Wings folder is still there after a move")
	}

	// The daemon, then the panel's create (with the egg's install script).
	rt := wingsHost{runtime.NewFake(), dk}
	d, err := New(Config{DataDir: data, DiskCacheTTL: time.Minute}, rt, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{a, b} {
		spec := proto.ServerSpec{
			UUID: id, Image: "img", Startup: "run", Stop: "stop", MemoryMB: 128,
			Install: &proto.InstallSpec{Image: "installer", Entrypoint: "bash", Script: "download the game"},
		}
		if err := d.create(ctx, spec); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{a, b} {
		if !eventually(5*time.Second, func() bool { st, ok := d.installState(id); return ok && st == "" }) {
			t.Fatalf("%s never finished provisioning", id)
		}
		dir := d.serverDir(id)
		if got, err := os.ReadFile(filepath.Join(dir, "world", "level.dat")); err != nil || string(got) != "world of "+id {
			t.Errorf("%s: files not kept: %q %v", id, got, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "installed.txt")); err == nil {
			t.Errorf("%s: the install script ran over adopted files", id)
		}
		if _, err := os.Stat(d.installedPath(id)); err != nil {
			t.Errorf("%s: not recorded as installed", id)
		}
		if _, err := os.Stat(d.adoptedPath(id)); err == nil {
			t.Errorf("%s: adopted marker left behind", id)
		}
	}
	// a was still running under Wings: stopped there with the egg's stop
	// command, then started here. b stays offline.
	if !eventually(5*time.Second, stateIs(rt, a, runtime.StateRunning)) {
		t.Error("the server Wings had running did not start")
	}
	if got := dk.stops(); !slices.Equal(got, []string{a + " stop"}) {
		t.Errorf("Wings' container was stopped with %v, want the egg's stop command", got)
	}
	if _, err := os.Stat(d.handoverPath(a)); err == nil {
		t.Error("handover marker left behind")
	}
	if st, _ := rt.State(ctx, b); st == runtime.StateRunning {
		t.Error("a server Wings had stopped was started")
	}

	// A second run finds nothing left to take over.
	if _, err := AdoptWings(ctx, opts); err == nil || !strings.Contains(err.Error(), "no server folders") {
		t.Fatalf("second run: %v", err)
	}
}

// A server the daemon already set up (it connected before adopt-wings ran,
// so the panel's create installed it from scratch) is not overwritten.
func TestAdoptRefusesAServerTheDaemonHas(t *testing.T) {
	t.Parallel()
	a := uuidN(1)
	volumes := wingsNode(t, a)
	data := t.TempDir()
	if err := os.MkdirAll(filepath.Join(data, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "state", a+".json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := AdoptWings(context.Background(), AdoptOptions{From: volumes, DataDir: data})
	if err == nil || !strings.Contains(err.Error(), "already set this server up") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(volumes, a, "server.properties")); err != nil {
		t.Fatal("the Wings files were touched")
	}
}

func TestCopyTreeKeepsLinksModesAndTimes(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(filepath.Join(src, "a", "b"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a", "b", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a/b/run.sh", filepath.Join(src, "start")); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(src, "a", "b", "run.sh"), old, old); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "dst")
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dst, "a", "b", "run.sh"))
	if err != nil || fi.Mode().Perm() != 0o755 || !fi.ModTime().Equal(old) {
		t.Fatalf("file: %v %v", fi, err)
	}
	if di, err := os.Stat(filepath.Join(dst, "a")); err != nil || di.Mode().Perm() != 0o750 {
		t.Fatalf("dir mode: %v %v", di, err)
	}
	if target, err := os.Readlink(filepath.Join(dst, "start")); err != nil || target != "a/b/run.sh" {
		t.Fatalf("link: %q %v", target, err)
	}
}
