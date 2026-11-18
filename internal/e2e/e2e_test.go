package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon"
	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

const egg = `{"name":"Echo","docker_images":{"x":"echo:latest"},"startup":"run {{GREETING}}",
 "config":{"stop":"stop"},"variables":[{"name":"g","env_variable":"GREETING","default_value":"hi"}]}`

type client struct {
	t    *testing.T
	base string
	c    *http.Client
}

func (c *client) do(method, path string, body any, want int) []byte {
	c.t.Helper()
	var r io.Reader
	if s, ok := body.(string); ok {
		r = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, r)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s: got %d want %d: %s", method, path, resp.StatusCode, want, b)
	}
	return b
}

func TestFullFlow(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	st, err := store.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.CreateUser(ctx, "admin@example.com", "correct-horse-battery", true)
	st.CreateUser(ctx, "bob@example.com", "another-long-password", false)

	h := hub.New(log)
	ts := httptest.NewServer(api.New(api.Deps{Store: st, Hub: h, Log: log}))
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	admin := &client{t, ts.URL, &http.Client{Jar: jar}}

	// unauthenticated access is refused
	admin.do("GET", "/api/servers", nil, 401)
	// wrong password
	admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": "nope-nope-nope"}, 401)
	admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": "correct-horse-battery"}, 200)

	var nodeResp struct {
		Token string             `json:"token"`
		Node  struct{ ID int64 } `json:"node"`
	}
	json.Unmarshal(admin.do("POST", "/api/nodes", map[string]any{"name": "n1", "port_start": 25565, "port_end": 25570}, 201), &nodeResp)

	// start a daemon with the fake runtime against this panel
	dm, err := daemon.New(daemon.Config{
		PanelURL: "ws" + strings.TrimPrefix(ts.URL, "http") + "/daemon/connect",
		Token:    nodeResp.Token, DataDir: t.TempDir(),
	}, runtime.NewFake(), log)
	if err != nil {
		t.Fatal(err)
	}
	go dm.Run(ctx)
	waitFor(t, "node online", func() bool { return h.Online(nodeResp.Node.ID) })

	var eggResp struct{ ID int64 }
	json.Unmarshal(admin.do("POST", "/api/eggs/import", egg, 201), &eggResp)

	var srv struct {
		UUID string
		Port int
	}
	json.Unmarshal(admin.do("POST", "/api/servers", map[string]any{
		"name": "s1", "node_id": nodeResp.Node.ID, "egg_id": eggResp.ID, "memory_mb": 512,
	}, 201), &srv)
	if srv.Port != 25565 {
		t.Fatalf("expected first free port, got %d", srv.Port)
	}

	// a different, non-admin user must not see or touch the server
	jar2, _ := cookiejar.New(nil)
	bob := &client{t, ts.URL, &http.Client{Jar: jar2}}
	bob.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "another-long-password"}, 200)
	bob.do("GET", "/api/servers/"+srv.UUID, nil, 404)
	bob.do("POST", "/api/servers/"+srv.UUID+"/power", map[string]string{"action": "start"}, 404)
	bob.do("POST", "/api/nodes", map[string]any{"name": "evil"}, 403)

	// console websocket
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/servers/" + srv.UUID + "/ws"
	u, _ := http.NewRequest("GET", ts.URL, nil)
	var cookie string
	for _, c := range jar.Cookies(u.URL) {
		cookie += c.Name + "=" + c.Value
	}
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {cookie}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	admin.do("POST", "/api/servers/"+srv.UUID+"/power", map[string]string{"action": "start"}, 202)
	readUntil(t, ctx, conn, "[fake] ready")

	admin.do("POST", "/api/servers/"+srv.UUID+"/command", map[string]string{"command": "say hello"}, 204)
	readUntil(t, ctx, conn, "[fake] \\u003e say hello") // json escapes ">"

	// files via API
	admin.do("PUT", "/api/servers/"+srv.UUID+"/files/content", map[string]string{"path": "/cfg/a.yml", "content": "k: v"}, 204)
	var fc struct{ Content string }
	json.Unmarshal(admin.do("GET", "/api/servers/"+srv.UUID+"/files/content?path=/cfg/a.yml", nil, 200), &fc)
	if fc.Content != "k: v" {
		t.Fatalf("content %q", fc.Content)
	}

	// graceful stop reaches offline
	admin.do("POST", "/api/servers/"+srv.UUID+"/power", map[string]string{"action": "stop"}, 202)
	waitFor(t, "offline", func() bool { return h.State(srv.UUID) == "offline" })

	// delete removes the data from the node
	admin.do("DELETE", "/api/servers/"+srv.UUID, nil, 204)
	admin.do("GET", "/api/servers/"+srv.UUID, nil, 404)
}

func TestDaemonRejectsBadToken(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, _ := store.Open(filepath.Join(t.TempDir(), "p.db"))
	defer st.Close()
	ts := httptest.NewServer(api.New(api.Deps{Store: st, Hub: hub.New(log), Log: log}))
	defer ts.Close()
	_, resp, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(ts.URL, "http")+"/daemon/connect",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer wrong"}}})
	if err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %v %v", resp, err)
	}
}

func TestCrossOriginConsoleBlocked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, _ := store.Open(filepath.Join(t.TempDir(), "p.db"))
	defer st.Close()
	u, _ := st.CreateUser(ctx, "a@example.com", "correct-horse-battery", true)
	n, _, _ := st.CreateNode(ctx, "n", 25565, 25570)
	e, _ := st.CreateEgg(ctx, store.Egg{Name: "e", DockerImage: "i", Startup: "s", Stop: "^C"})
	srv, err := st.CreateServer(ctx, store.Server{UUID: "11111111-2222-4333-8444-555555555555", Name: "s", OwnerID: u.ID, NodeID: n.ID, EggID: e.ID, MemoryMB: 128})
	if err != nil {
		t.Fatal(err)
	}
	tok, _ := st.NewSession(ctx, u.ID, store.SessionMeta{})
	ts := httptest.NewServer(api.New(api.Deps{Store: st, Hub: hub.New(log), Log: log}))
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/servers/" + srv.UUID + "/ws"
	hdr := func(origin string) http.Header {
		h := http.Header{"Cookie": {"gp_session=" + tok}}
		if origin != "" {
			h.Set("Origin", origin)
		}
		return h
	}
	// A page on another site that rides the user's cookie must be refused.
	if c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: hdr("https://evil.example")}); err == nil {
		c.CloseNow()
		t.Fatal("cross-origin websocket was accepted")
	}
	// Same origin works.
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: hdr(ts.URL)})
	if err != nil {
		t.Fatalf("same-origin websocket rejected: %v", err)
	}
	c.CloseNow()
}

func waitFor(t *testing.T, what string, f func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if f() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func readUntil(t *testing.T, ctx context.Context, c *websocket.Conn, sub string) {
	t.Helper()
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		_, b, err := c.Read(rctx)
		if err != nil {
			t.Fatalf("waiting for %q: %v", sub, err)
		}
		if strings.Contains(string(b), sub) {
			return
		}
	}
}

// The panel keeps server state only in memory. After it restarts, the node
// must report stopped servers too, or they show as unknown.
func TestStoppedServerStateSurvivesPanelRestart(t *testing.T) {
	// Not parallel: the panel comes back on the port it let go of, which a test
	// running alongside could take in between.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	st, err := store.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.CreateUser(ctx, "admin@example.com", "correct-horse-battery", true)

	h := hub.New(log)
	ts := httptest.NewUnstartedServer(api.New(api.Deps{Store: st, Hub: h, Log: log}))
	var (
		mu       sync.Mutex
		hijacked []net.Conn // the node's websocket, which Close leaves open
	)
	ts.Config.ConnState = func(c net.Conn, s http.ConnState) {
		if s == http.StateHijacked {
			mu.Lock()
			hijacked = append(hijacked, c)
			mu.Unlock()
		}
	}
	ts.Start()
	defer func() { ts.Close() }()

	jar, _ := cookiejar.New(nil)
	admin := &client{t, ts.URL, &http.Client{Jar: jar}}
	admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": "correct-horse-battery"}, 200)
	var nodeResp struct {
		Token string             `json:"token"`
		Node  struct{ ID int64 } `json:"node"`
	}
	json.Unmarshal(admin.do("POST", "/api/nodes", map[string]any{"name": "n1", "port_start": 25565, "port_end": 25570}, 201), &nodeResp)
	dm, err := daemon.New(daemon.Config{
		PanelURL: "ws" + strings.TrimPrefix(ts.URL, "http") + "/daemon/connect",
		Token:    nodeResp.Token, DataDir: t.TempDir(),
	}, runtime.NewFake(), log)
	if err != nil {
		t.Fatal(err)
	}
	go dm.Run(ctx)
	waitFor(t, "node online", func() bool { return h.Online(nodeResp.Node.ID) })

	var eggResp struct{ ID int64 }
	json.Unmarshal(admin.do("POST", "/api/eggs/import", egg, 201), &eggResp)
	var srv struct{ UUID string }
	json.Unmarshal(admin.do("POST", "/api/servers", map[string]any{
		"name": "s1", "node_id": nodeResp.Node.ID, "egg_id": eggResp.ID, "memory_mb": 512,
	}, 201), &srv)
	admin.do("POST", "/api/servers/"+srv.UUID+"/power", map[string]string{"action": "start"}, 202)
	waitFor(t, "running", func() bool { return h.State(srv.UUID) == "running" })
	admin.do("POST", "/api/servers/"+srv.UUID+"/power", map[string]string{"action": "stop"}, 202)
	waitFor(t, "offline", func() bool { return h.State(srv.UUID) == "offline" })

	// Restart the panel on the same address with a fresh hub.
	addr := ts.Listener.Addr().String()
	ts.Close()
	mu.Lock()
	for _, c := range hijacked {
		c.Close()
	}
	mu.Unlock()
	h2 := hub.New(log)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	ts = httptest.NewUnstartedServer(api.New(api.Deps{Store: st, Hub: h2, Log: log}))
	ts.Listener.Close()
	ts.Listener = ln
	ts.Start()
	waitFor(t, "node back online", func() bool { return h2.Online(nodeResp.Node.ID) })
	waitFor(t, "offline after the restart", func() bool { return h2.State(srv.UUID) == "offline" })
}
