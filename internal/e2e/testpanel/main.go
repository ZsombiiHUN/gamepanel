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
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/web"
)

const (
	adminEmail = "admin@example.com"
	adminPass  = "e2e-admin-password-1"
	userEmail  = "bob@example.com"
	userPass   = "e2e-user-password-1"
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

func run(nServers int, dir, addr, readyFile string) error {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if dir == "" {
		d, err := os.MkdirTemp("", "gamepanel-e2e-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(d)
		dir = d
	}
	st, err := store.Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	key := make([]byte, 32)
	rand.Read(key)
	if err := st.SetSecretKey(key); err != nil {
		return err
	}
	if _, err := st.CreateUser(ctx, adminEmail, adminPass, true); err != nil {
		return err
	}
	if _, err := st.CreateUser(ctx, userEmail, userPass, false); err != nil {
		return err
	}

	h := hub.New(log)
	// Passkeys need a domain: browser tests that use them open the panel on localhost.
	public := ""
	if _, port, err := net.SplitHostPort(addr); err == nil && port != "0" {
		public = "http://localhost:" + port
	}
	handler := api.New(api.Deps{Store: st, Hub: h, Log: log, Web: web.Handler(), SchedulerCtx: ctx, PublicURL: public})
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	defer srv.Close()
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

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	call := func(method, path string, body any, want int) ([]byte, error) {
		var r io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			r = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, base+path, r)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			return nil, fmt.Errorf("%s %s: got %d want %d: %s", method, path, resp.StatusCode, want, b)
		}
		return b, nil
	}
	if _, err := call("POST", "/api/auth/login", map[string]string{"email": adminEmail, "password": adminPass}, 200); err != nil {
		return err
	}

	eggs := map[string]int64{}
	for _, e := range []struct{ name, image, startup string }{
		{"Paper Minecraft", "ghcr.io/pterodactyl/yolks:java_21", "java -Xms128M -jar server.jar"},
		{"Valheim", "ghcr.io/parkervcp/steamcmd:debian", "./valheim_server.x86_64 -name {{SRV}}"},
		{"Rust", "ghcr.io/parkervcp/games:rust", "./RustDedicated"},
		{"Terraria", "ghcr.io/parkervcp/yolks:dotnet_6", "./TerrariaServer"},
	} {
		body := map[string]any{"name": e.name, "docker_images": map[string]string{"default": e.image}, "startup": e.startup, "config": map[string]string{"stop": "stop"},
			"variables": []map[string]string{{"name": "Name", "env_variable": "SRV", "default_value": "My Server"}}}
		b, err := call("POST", "/api/eggs/import", body, 201)
		if err != nil {
			return err
		}
		var out struct{ ID int64 }
		json.Unmarshal(b, &out)
		eggs[e.name] = out.ID
	}

	// The first six are fixed so tests can rely on them; the rest pad the list out.
	plan := []struct{ name, egg string }{
		{"Survival SMP", "Paper Minecraft"}, {"Creative Plots", "Paper Minecraft"}, {"Skyblock", "Paper Minecraft"},
		{"Valheim Friends", "Valheim"}, {"Rust Wipe Friday", "Rust"}, {"Terraria Hardmode", "Terraria"},
	}
	// Names chosen to exercise sorting ("Lobby 2" before "Lobby 10"), shared words and several games.
	extra := []string{"Lobby 2", "Lobby 10", "Lobby 1", "Vanilla Survival", "Modded Create", "Pixelmon", "Hub Lobby", "BedWars", "SkyWars", "Factions", "Prison", "Hardcore", "Anarchy", "RPG Realm", "Tekkit", "Ark Island", "Rust Modded", "Rust Vanilla 2x", "Valheim Hardcore", "Terraria Calamity", "Creative Build", "Towny", "Kitpvp"}
	eggNames := []string{"Paper Minecraft", "Valheim", "Rust", "Terraria"}
	for i := 0; len(plan) < nServers; i++ {
		name := fmt.Sprintf("Extra %d", i)
		if i < len(extra) {
			name = extra[i]
		}
		plan = append(plan, struct{ name, egg string }{name, eggNames[i%len(eggNames)]})
	}
	out := ready{URL: base, Admin: credentials{adminEmail, adminPass}, User: credentials{userEmail, userPass}, NodeID: node.ID, DataDir: filepath.Join(dir, "daemon"), SharedTo: "Creative Plots"}
	for _, p := range plan {
		b, err := call("POST", "/api/servers", map[string]any{"name": p.name, "node_id": node.ID, "egg_id": eggs[p.egg], "memory_mb": 256, "disk_mb": 1024}, 201)
		if err != nil {
			return err
		}
		var s struct {
			UUID string `json:"uuid"`
			Port int    `json:"port"`
		}
		json.Unmarshal(b, &s)
		out.Servers = append(out.Servers, seeded{UUID: s.UUID, Name: p.name, Egg: p.egg, Port: s.Port})
	}
	// Let the regular user see one server, with read-only console and files.
	for _, s := range out.Servers {
		if s.Name == out.SharedTo {
			if _, err := call("POST", "/api/servers/"+s.UUID+"/users", map[string]any{"email": userEmail, "permissions": []string{"console.read", "files.read"}}, 201); err != nil {
				return err
			}
		}
	}

	line, _ := json.Marshal(out)
	if readyFile != "" {
		// Write then rename so a reader never sees half a file.
		tmp := readyFile + ".tmp"
		if err := os.WriteFile(tmp, line, 0o600); err != nil {
			return err
		}
		if err := os.Rename(tmp, readyFile); err != nil {
			return err
		}
		defer os.Remove(readyFile)
	}
	fmt.Println(string(line))
	<-ctx.Done()
	return nil
}
