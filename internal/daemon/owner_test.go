package daemon

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

type outdatedRuntime struct {
	*runtime.Fake
	ids []string
}

func (o outdatedRuntime) Outdated(context.Context) ([]string, error) { return o.ids, nil }

func TestMarkOutdated(t *testing.T) {
	old, fresh := uuidN(1), uuidN(2)
	rt := &outdatedRuntime{Fake: runtime.NewFake()}
	d := newTestDaemon(t, rt)
	addServer(t, d, old, 0)
	addServer(t, d, fresh, 0)
	rt.ids = []string{old, "not-a-uuid", uuidN(9)} // unknown ids are ignored
	d.markOutdated(context.Background())
	if !d.isStale(old) || d.isStale(fresh) {
		t.Fatalf("stale: old=%v fresh=%v", d.isStale(old), d.isStale(fresh))
	}
	if _, err := os.Stat(d.stalePath(old)); err != nil {
		t.Fatal("the stale marker was not written, so a daemon restart would forget it")
	}
	// The next start rebuilds it.
	if err := d.start(context.Background(), proto0(d, old)); err != nil {
		t.Fatal(err)
	}
	if d.isStale(old) {
		t.Fatal("the start did not rebuild the container")
	}
}

func TestContainerIDs(t *testing.T) {
	d := &Daemon{}
	if u, g := d.containerIDs(); u != LegacyContainerID || g != LegacyContainerID {
		t.Fatalf("default: %d:%d", u, g)
	}
	d.cfg.ContainerUID = 2500
	if u, g := d.containerIDs(); u != 2500 || g != 2500 {
		t.Fatalf("uid only: %d:%d", u, g)
	}
	d.cfg.ContainerGID = 2600
	if u, g := d.containerIDs(); u != 2500 || g != 2600 {
		t.Fatalf("uid:gid: %d:%d", u, g)
	}
}

// New servers' folders go to the configured user, and the files of servers
// made before it was set move from the legacy user to it. Needs root.
func TestRootDaemonMigrateOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("chown needs root")
	}
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 0)
	dir := d.serverDir(id)
	other := filepath.Join(dir, "other")
	for _, p := range []string{filepath.Join(dir, "world"), filepath.Join(dir, "world", "level.dat"), other} {
		if filepath.Ext(p) == "" {
			_ = os.Mkdir(p, 0o755)
		} else {
			_ = os.WriteFile(p, nil, 0o644)
		}
		_ = os.Lchown(p, LegacyContainerID, LegacyContainerID)
	}
	_ = os.Lchown(other, 4242, 4242)
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	_ = os.Lchown(filepath.Join(dir, "link"), LegacyContainerID, LegacyContainerID)

	d.cfg.ContainerUID, d.cfg.ContainerGID = 2500, 2600
	d.migrateOwner(id)
	owner := func(p string) (uint32, uint32) {
		fi, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		st := fi.Sys().(*syscall.Stat_t)
		return st.Uid, st.Gid
	}
	for _, p := range []string{dir, filepath.Join(dir, "world"), filepath.Join(dir, "world", "level.dat"), filepath.Join(dir, "link")} {
		if u, g := owner(p); u != 2500 || g != 2600 {
			t.Errorf("%s: %d:%d", p, u, g)
		}
	}
	if u, _ := owner(other); u != 4242 {
		t.Errorf("a file of another user was taken: %d", u)
	}
	if u, _ := owner("/etc/passwd"); u != 0 {
		t.Fatal("the symlink was followed")
	}

	// New servers start with the configured owner.
	id2 := uuidN(2)
	addServer(t, d, id2, 0)
	if u, g := owner(d.serverDir(id2)); u != 2500 || g != 2600 {
		t.Errorf("new server: %d:%d", u, g)
	}
}

// proto0 is a server's current spec.
func proto0(d *Daemon, id string) proto.ServerSpec {
	s, _ := d.spec(id)
	return s
}
