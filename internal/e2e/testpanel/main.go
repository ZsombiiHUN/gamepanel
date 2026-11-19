// Command testpanel runs a throwaway panel with an in-process fake daemon and
// seeded data, for the browser tests in web/e2e. It listens on a free local port
// and prints one JSON line ("ready") describing what it created, then serves
// until it receives SIGINT or SIGTERM. It is for tests only and is not part of
// the release build.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon"
	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/modrinth"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/web"
)

// Fixed test accounts on a throwaway panel; the browser tests sign in with them.
const (
	adminEmail = "admin@example.com"
	adminPass  = "e2e-admin-password-1" //nolint:gosec // G101: see above
	userEmail  = "bob@example.com"
	userPass   = "e2e-user-password-1" //nolint:gosec // G101: see above
)

type ready struct {
	URL      string      `json:"url"`
	Admin    credentials `json:"admin"`
	User     credentials `json:"user"`
	NodeID   int64       `json:"node_id"`
	Servers  []seeded    `json:"servers"`
	DataDir  string      `json:"data_dir"`
	SharedTo string      `json:"shared_to"` // name of the server the regular user may access
}
type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type seeded struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	Egg  string `json:"egg"`
	Port int    `json:"port"`
}

func main() {
	n := flag.Int("servers", 26, "how many servers to create (at least 6)")
	dir := flag.String("dir", "", "working directory (default: a new temp dir, removed on exit)")
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	readyFile := flag.String("ready", "", "also write the ready JSON to this file, once seeding is done")
	flag.Parse()
	if err := run(*n, *dir, *addr, *readyFile); err != nil {
		fmt.Fprintln(os.Stderr, "testpanel:", err)
		os.Exit(1)
	}
}

func run(nServers int, dir, addr, readyFile string) (err error) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if dir == "" {
		d, err := os.MkdirTemp("", "gamepanel-e2e-")
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, os.RemoveAll(d)) }()
		dir = d
	}
	st, err := openStore(ctx, filepath.Join(dir, "panel.db"))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, st.Close()) }()

	h := hub.New(log)
	// Passkeys need a domain: browser tests that use them open the panel on localhost.
	public := ""
	if _, port, err := net.SplitHostPort(addr); err == nil && port != "0" {
		public = "http://localhost:" + port
	}
	modrinthAPI, err := fakeModrinth(ctx)
	if err != nil {
		return err
	}
	handler := api.New(api.Deps{Store: st, Hub: h, Log: log, Web: web.Handler(), SchedulerCtx: ctx, PublicURL: public, Modrinth: modrinthAPI})
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go serve(srv, ln)
	defer func() { err = errors.Join(err, srv.Close()) }()
	base := "http://" + ln.Addr().String()

	node, token, err := st.CreateNode(ctx, "eu-west-1", 25565, 25565+nServers+20)
	if err != nil {
		return err
	}
	dm, err := daemon.New(daemon.Config{PanelURL: "ws" + strings.TrimPrefix(base, "http") + "/daemon/connect", Token: token, DataDir: filepath.Join(dir, "daemon"), StatsInterval: 500 * time.Millisecond}, runtime.NewFake(), log)
	if err != nil {
		return err
	}
	go dm.Run(ctx)
	for i := 0; !h.Online(node.ID); i++ {
		if i > 200 {
			return errors.New("daemon never connected")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Give the node plenty of room so tests never hit capacity limits by accident.
	if _, err := st.DB.ExecContext(ctx, `UPDATE nodes SET memory_mb=?, disk_mb=?`, 1<<20, 1<<22); err != nil {
		return err
	}

	out := ready{URL: base, Admin: credentials{adminEmail, adminPass}, User: credentials{userEmail, userPass}, NodeID: node.ID, DataDir: filepath.Join(dir, "daemon"), SharedTo: "Creative Plots"}
	if out.Servers, err = seed(ctx, newClient(base), node.ID, nServers, out.SharedTo); err != nil {
		return err
	}
	line, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if readyFile != "" {
		// Write then rename so a reader never sees half a file.
		tmp := readyFile + ".tmp"
		if err := os.WriteFile(tmp, line, 0o600); err != nil {
			return err
		}
		if err := os.Rename(tmp, readyFile); err != nil {
			return err
		}
		defer func() { err = errors.Join(err, os.Remove(readyFile)) }()
	}
	fmt.Println(string(line))
	<-ctx.Done()
	return nil
}

// serve runs srv until it is closed.
func serve(srv *http.Server, ln net.Listener) {
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "testpanel: serve:", err)
	}
}

// openStore opens a fresh database with the test accounts.
func openStore(ctx context.Context, path string) (*store.Store, error) {
	st, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	rand.Read(key)
	if err := st.SetSecretKey(key); err != nil {
		return nil, errors.Join(err, st.Close())
	}
	for _, u := range []struct {
		email, pw string
		admin     bool
	}{{adminEmail, adminPass, true}, {userEmail, userPass, false}} {
		if _, err := st.CreateUser(ctx, u.email, u.pw, u.admin); err != nil {
			return nil, errors.Join(err, st.Close())
		}
	}
	return st, nil
}

// client calls the panel's API as the signed-in admin.
type client struct {
	base string
	http *http.Client
}

func newClient(base string) *client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		panic(err) // cookiejar.New never fails without options
	}
	return &client{base: base, http: &http.Client{Jar: jar}}
}

// post sends body as JSON, expects status want and decodes the reply into
// out unless it is nil.
func (c *client) post(ctx context.Context, path string, body any, want int, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	reply, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != want {
		return fmt.Errorf("POST %s: got %d want %d: %s", path, resp.StatusCode, want, reply)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(reply, out)
}

// seed signs in as the admin and creates the eggs and nServers servers the
// browser tests expect, sharing the one named sharedTo with the regular user.
func seed(ctx context.Context, c *client, nodeID int64, nServers int, sharedTo string) ([]seeded, error) {
	if err := c.post(ctx, "/api/auth/login", map[string]string{"email": adminEmail, "password": adminPass}, 200, nil); err != nil {
		return nil, err
	}
	eggs := map[string]int64{}
	for _, e := range []struct{ name, image, startup string }{
		{"Paper Minecraft", "ghcr.io/pterodactyl/yolks:java_21", "java -Xms128M -jar server.jar"},
		{"Valheim", "ghcr.io/parkervcp/steamcmd:debian", "./valheim_server.x86_64 -name {{SRV}}"},
		{"Rust", "ghcr.io/parkervcp/games:rust", "./RustDedicated"},
		{"Terraria", "ghcr.io/parkervcp/yolks:dotnet_6", "./TerrariaServer"},
	} {
		body := map[string]any{
			"name": e.name, "docker_images": map[string]string{"default": e.image}, "startup": e.startup, "config": map[string]string{"stop": "stop"},
			"variables": []map[string]string{{"name": "Name", "env_variable": "SRV", "default_value": "My Server"}},
		}
		var egg struct{ ID int64 }
		if err := c.post(ctx, "/api/eggs/import", body, 201, &egg); err != nil {
			return nil, err
		}
		eggs[e.name] = egg.ID
	}
	var servers []seeded
	for _, p := range serverPlan(nServers) {
		var s struct {
			UUID string `json:"uuid"`
			Port int    `json:"port"`
		}
		body := map[string]any{"name": p.name, "node_id": nodeID, "egg_id": eggs[p.egg], "memory_mb": 256, "disk_mb": 1024}
		if err := c.post(ctx, "/api/servers", body, 201, &s); err != nil {
			return nil, err
		}
		servers = append(servers, seeded{UUID: s.UUID, Name: p.name, Egg: p.egg, Port: s.Port})
	}
	// Let the regular user see one server, with read-only console and files.
	for _, s := range servers {
		if s.Name == sharedTo {
			grant := map[string]any{"email": userEmail, "permissions": []string{"console.read", "files.read"}}
			if err := c.post(ctx, "/api/servers/"+s.UUID+"/users", grant, 201, nil); err != nil {
				return nil, err
			}
		}
	}
	return servers, nil
}

type plannedServer struct{ name, egg string }

// serverPlan names n servers. The first six are fixed so tests can rely on
// them; the rest pad the list out.
func serverPlan(n int) []plannedServer {
	plan := []plannedServer{
		{"Survival SMP", "Paper Minecraft"},
		{"Creative Plots", "Paper Minecraft"},
		{"Skyblock", "Paper Minecraft"},
		{"Valheim Friends", "Valheim"},
		{"Rust Wipe Friday", "Rust"},
		{"Terraria Hardmode", "Terraria"},
	}
	// Names chosen to exercise sorting ("Lobby 2" before "Lobby 10"), shared words and several games.
	extra := []string{"Lobby 2", "Lobby 10", "Lobby 1", "Vanilla Survival", "Modded Create", "Pixelmon", "Hub Lobby", "BedWars", "SkyWars", "Factions", "Prison", "Hardcore", "Anarchy", "RPG Realm", "Tekkit", "Ark Island", "Rust Modded", "Rust Vanilla 2x", "Valheim Hardcore", "Terraria Calamity", "Creative Build", "Towny", "Kitpvp"}
	eggNames := []string{"Paper Minecraft", "Valheim", "Rust", "Terraria"}
	for i := 0; len(plan) < n; i++ {
		name := fmt.Sprintf("Extra %d", i)
		if i < len(extra) {
			name = extra[i]
		}
		plan = append(plan, plannedServer{name, eggNames[i%len(eggNames)]})
	}
	return plan
}

// fakeModrinth answers the mod manager with canned results, so browser tests
// never depend on the real Modrinth.
func fakeModrinth(ctx context.Context) (*modrinth.Client, error) {
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) {
		_ = json.NewEncoder(w).Encode(v) //nolint:errchkjson // canned data; a failed write means the test stopped
	}
	mux.HandleFunc("GET /search", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, map[string]any{"total_hits": 2, "hits": []any{
			map[string]any{"project_id": "Vebnzrzj", "slug": "luckperms", "title": "LuckPerms", "author": "Luck", "description": "A permissions plugin.", "downloads": 2400000, "project_type": "plugin"},
			map[string]any{"project_id": "fALzjamp", "slug": "chunky", "title": "Chunky", "author": "pop4959", "description": "Pre-generates chunks.", "downloads": 870000, "project_type": "plugin"},
		}})
	})
	mux.HandleFunc("GET /project/{id}/version", func(w http.ResponseWriter, r *http.Request) {
		reply(w, []any{map[string]any{"id": "abc123", "project_id": r.PathValue("id"), "version_number": "5.4.0", "version_type": "release", "game_versions": []string{"1.21.1"}, "loaders": []string{"paper"}, "files": []any{}}})
	})
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	go serve(&http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}, ln)
	c := modrinth.New()
	c.BaseURL = "http://" + ln.Addr().String()
	return c, nil
}
