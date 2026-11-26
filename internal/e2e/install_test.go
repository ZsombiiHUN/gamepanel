package e2e

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon"
	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store/storetest"
)

func installEgg(script string) string {
	b, _ := json.Marshal(map[string]any{
		"name": "Installable", "docker_images": map[string]string{"x": "game:latest"},
		"startup": "run", "config": map[string]string{"stop": "stop"},
		"scripts": map[string]any{"installation": map[string]string{
			"script": script, "container": "alpine:3.20", "entrypoint": "ash",
		}},
	})
	return string(b)
}

func TestInstallScriptLifecycle(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	st, err := storetest.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.CreateUser(ctx, "admin@example.com", "correct-horse-battery", true)
	h := hub.New(log)
	ts := httptest.NewServer(api.New(api.Deps{Store: st, Hub: h, Log: log}))
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	admin := &client{t, ts.URL, &http.Client{Jar: jar}}
	admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": "correct-horse-battery"}, 200)

	var node struct {
		Token string             `json:"token"`
		Node  struct{ ID int64 } `json:"node"`
	}
	json.Unmarshal(admin.do("POST", "/api/nodes", map[string]any{"name": "n1", "port_start": 25565, "port_end": 25570}, 201), &node)
	dataDir := t.TempDir()
	dm, err := daemon.New(daemon.Config{
		PanelURL: "ws" + strings.TrimPrefix(ts.URL, "http") + "/daemon/connect",
		Token:    node.Token, DataDir: dataDir,
	}, runtime.NewFake(), log)
	if err != nil {
		t.Fatal(err)
	}
	go dm.Run(ctx)
	waitFor(t, "node online", func() bool { return h.Online(node.Node.ID) })

	newServer := func(script string) string {
		var e struct{ ID int64 }
		json.Unmarshal(admin.do("POST", "/api/eggs/import", installEgg(script), 201), &e)
		var s struct{ UUID string }
		json.Unmarshal(admin.do("POST", "/api/servers", map[string]any{
			"name": "s", "node_id": node.Node.ID, "egg_id": e.ID, "memory_mb": 256,
		}, 201), &s)
		return s.UUID
	}
	state := func(uuid string) string {
		var v struct{ State string }
		json.Unmarshal(admin.do("GET", "/api/servers/"+uuid, nil, 200), &v)
		return v.State
	}

	// success: server ends up offline with the script's output on disk, and starts
	ok := newServer("echo installing\n")
	waitFor(t, "install finished", func() bool { return state(ok) == "offline" })
	if _, err := os.Stat(filepath.Join(dataDir, "servers", ok, "installed.txt")); err != nil {
		t.Fatalf("install did not run: %v", err)
	}
	admin.do("POST", "/api/servers/"+ok+"/power", map[string]string{"action": "start"}, 202)
	waitFor(t, "running", func() bool { return state(ok) == "running" })
	// cannot reinstall while running
	admin.do("POST", "/api/servers/"+ok+"/reinstall", nil, 409)
	admin.do("POST", "/api/servers/"+ok+"/power", map[string]string{"action": "stop"}, 202)
	waitFor(t, "stopped", func() bool { return state(ok) == "offline" })
	admin.do("POST", "/api/servers/"+ok+"/reinstall", nil, 202)
	waitFor(t, "reinstalled", func() bool { return state(ok) == "offline" })
	// Stopping a stopped server changes nothing (it was left "stopping").
	admin.do("POST", "/api/servers/"+ok+"/power", map[string]string{"action": "stop"}, 202)
	time.Sleep(300 * time.Millisecond)
	if s := state(ok); s != "offline" {
		t.Fatalf("stopping a stopped server left it %s", s)
	}

	// While it installs, power is refused outright rather than accepted and
	// dropped by the node.
	slow := newServer("#slow\necho installing\n")
	waitFor(t, "installing", func() bool { return state(slow) == "installing" })
	for _, action := range []string{"start", "stop", "restart", "kill"} {
		admin.do("POST", "/api/servers/"+slow+"/power", map[string]string{"action": action}, 409)
	}
	waitFor(t, "slow install finished", func() bool { return state(slow) == "offline" })
	admin.do("POST", "/api/servers/"+slow+"/power", map[string]string{"action": "start"}, 202)
	waitFor(t, "slow server running", func() bool { return state(slow) == "running" })

	// failure: install_failed, and the server refuses to start, saying so
	bad := newServer("#fail\nexit 1\n")
	waitFor(t, "install failed", func() bool { return state(bad) == "install_failed" })
	admin.do("POST", "/api/servers/"+bad+"/power", map[string]string{"action": "start"}, 409)
	admin.do("POST", "/api/servers/"+bad+"/power", map[string]string{"action": "restart"}, 409)
	time.Sleep(300 * time.Millisecond)
	if s := state(bad); s != "install_failed" {
		t.Fatalf("failed install must not start, state=%s", s)
	}
}

func TestEggImportRejectsBadInstallEntrypoint(t *testing.T) {
	t.Parallel()
	b := installEgg("echo hi")
	b = strings.Replace(b, `"ash"`, `"ash; rm -rf /"`, 1)
	ctx := context.Background()
	st, _ := storetest.Open(filepath.Join(t.TempDir(), "p.db"))
	defer st.Close()
	st.CreateUser(ctx, "admin@example.com", "correct-horse-battery", true)
	ts := httptest.NewServer(api.New(api.Deps{Store: st, Hub: hub.New(slog.New(slog.NewTextHandler(io.Discard, nil))), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}))
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	c := &client{t, ts.URL, &http.Client{Jar: jar}}
	c.do("POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": "correct-horse-battery"}, 200)
	c.do("POST", "/api/eggs/import", b, 422)
}
