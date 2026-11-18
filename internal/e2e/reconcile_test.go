package e2e

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon"
	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// A node that comes back is brought in line with the panel: drifted settings
// are re-sent, servers deleted while it was away are removed, and servers
// the panel does not know are reported, not deleted.
func TestNodeReconcile(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	var node struct {
		Token string             `json:"token"`
		Node  struct{ ID int64 } `json:"node"`
	}
	json.Unmarshal(s.admin.do("POST", "/api/nodes", map[string]any{"name": "n2", "port_start": 26000, "port_end": 26010}, 201), &node)
	id := node.Node.ID
	fake := runtime.NewFake()
	dataDir := t.TempDir()
	base := strings.TrimSuffix(strings.Replace(s.admin.base, "http", "ws", 1), "/")
	boot := func() context.CancelFunc {
		ctx, cancel := context.WithCancel(s.ctx)
		dm, err := daemon.New(daemon.Config{PanelURL: base + "/daemon/connect", Token: node.Token, DataDir: dataDir}, fake, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		go dm.Run(ctx)
		waitFor(t, "node online", func() bool { return s.hub.Online(id) })
		return cancel
	}
	stop := boot()

	create := func(name string) string {
		var v struct{ UUID string }
		json.Unmarshal(s.admin.do("POST", "/api/servers", map[string]any{"name": name, "node_id": id, "egg_id": 1, "memory_mb": 256}, 201), &v)
		waitFor(t, name+" on the node", func() bool { _, ok := fake.SpecOf(v.UUID); return ok })
		return v.UUID
	}
	kept, gone := create("kept"), create("gone")
	stranger := "00000000-0000-4000-8000-0000000000aa"
	if err := fake.Create(s.ctx, runtime.Spec{ServerSpec: proto.ServerSpec{UUID: stranger}}); err != nil {
		t.Fatal(err)
	}

	stop()
	waitFor(t, "node offline", func() bool { return !s.hub.Online(id) })

	// The panel's copy changes while the node cannot hear about it.
	if _, err := s.st.DB.ExecContext(s.ctx, `UPDATE servers SET memory_mb=512 WHERE uuid=?`, kept); err != nil {
		t.Fatal(err)
	}
	// Deleting needs the node, unless forced.
	s.admin.do("DELETE", "/api/servers/"+gone, nil, 503)
	s.admin.do("DELETE", "/api/servers/"+gone+"?force=true", nil, 204)
	s.admin.do("GET", "/api/servers/"+gone, nil, 404)

	stop = boot()
	defer stop()
	waitFor(t, "force-deleted server removed from the node", func() bool { _, ok := fake.SpecOf(gone); return !ok })
	if _, err := os.Stat(filepath.Join(dataDir, "servers", gone)); !os.IsNotExist(err) {
		t.Fatalf("its files are still there: %v", err)
	}
	waitFor(t, "drifted settings re-sent", func() bool { sp, _ := fake.SpecOf(kept); return sp.MemoryMB == 512 })

	unknown := func() []string {
		var ns []struct {
			ID             int64
			UnknownServers []string `json:"unknown_servers"`
		}
		json.Unmarshal(s.admin.do("GET", "/api/nodes", nil, 200), &ns)
		for _, n := range ns {
			if n.ID == id {
				return n.UnknownServers
			}
		}
		return nil
	}
	waitFor(t, "unknown server reported", func() bool { return slices.Equal(unknown(), []string{stranger}) })
	if _, ok := fake.SpecOf(stranger); !ok {
		t.Fatal("an unknown server was deleted without anyone asking")
	}

	nodePath := "/api/nodes/" + strconv.FormatInt(id, 10) + "/unknown/"
	s.admin.do("DELETE", nodePath+kept, nil, 404) // not reported as unknown
	s.admin.do("DELETE", nodePath+stranger, nil, 204)
	if _, ok := fake.SpecOf(stranger); ok {
		t.Fatal("the unknown server is still on the node")
	}
	if u := unknown(); len(u) != 0 {
		t.Fatalf("still reported: %v", u)
	}
	if _, ok := fake.SpecOf(kept); !ok {
		t.Fatal("the kept server was removed")
	}
}
