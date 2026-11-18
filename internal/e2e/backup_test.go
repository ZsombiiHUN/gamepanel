package e2e

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon"
	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

type stack struct {
	t       *testing.T
	ctx     context.Context
	st      *store.Store
	admin   *client
	server  string
	sftp    string // host:port of the daemon's SFTP listener
	dataDir string // the daemon's data dir on disk
	hub     *hub.Hub
	node    int64
	fake    *runtime.Fake
	token   string // the node's token, as its daemon uses it
}

// newStack boots panel + daemon (fake runtime) and creates one server.
func newStack(t *testing.T) *stack { t.Helper(); return newStackWith(t, nil, nil) }

// newStackWith is newStack with changes to the panel's and the daemon's settings.
func newStackWith(t *testing.T, panel func(*api.Deps), nodeCfg func(*daemon.Config)) *stack {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.CreateUser(ctx, "admin@example.com", "correct-horse-battery", true)
	h := hub.New(log)
	deps := api.Deps{Store: st, Hub: h, Log: log, SchedulerCtx: ctx, SchedulerTick: 100 * time.Millisecond, AuthRecheck: 100 * time.Millisecond}
	if panel != nil {
		panel(&deps)
	}
	ts := httptest.NewServer(api.New(deps))
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	admin := &client{t, ts.URL, &http.Client{Jar: jar}}
	admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": "correct-horse-battery"}, 200)
	var node struct {
		Token string             `json:"token"`
		Node  struct{ ID int64 } `json:"node"`
	}
	json.Unmarshal(admin.do("POST", "/api/nodes", map[string]any{"name": "n1", "port_start": 25565, "port_end": 25570}, 201), &node)
	dataDir := t.TempDir()
	fake := runtime.NewFake()
	dcfg := daemon.Config{PanelURL: "ws" + strings.TrimPrefix(ts.URL, "http") + "/daemon/connect", Token: node.Token, DataDir: dataDir, StatsInterval: 150 * time.Millisecond, DiskCacheTTL: 100 * time.Millisecond, SFTPRecheck: 100 * time.Millisecond}
	if nodeCfg != nil {
		nodeCfg(&dcfg)
	}
	dm, err := daemon.New(dcfg, fake, log)
	if err != nil {
		t.Fatal(err)
	}
	sftpAddr, err := dm.StartSFTP(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go dm.Run(ctx)
	waitFor(t, "node online", func() bool { return h.Online(node.Node.ID) })
	var e struct{ ID int64 }
	json.Unmarshal(admin.do("POST", "/api/eggs/import", egg, 201), &e)
	var s struct{ UUID string }
	json.Unmarshal(admin.do("POST", "/api/servers", map[string]any{"name": "s", "node_id": node.Node.ID, "egg_id": e.ID, "memory_mb": 256}, 201), &s)
	return &stack{t, ctx, st, admin, s.UUID, sftpAddr, dataDir, h, node.Node.ID, fake, node.Token}
}

func (s *stack) write(path, content string) {
	s.t.Helper()
	s.admin.do("PUT", "/api/servers/"+s.server+"/files/content", map[string]string{"path": path, "content": content}, 204)
}

func (s *stack) read(path string) (string, int) {
	s.t.Helper()
	req, _ := http.NewRequest("GET", s.admin.base+"/api/servers/"+s.server+"/files/content?path="+url.QueryEscape(path), nil)
	resp, err := s.admin.c.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var v struct{ Content string }
	json.Unmarshal(b, &v)
	return v.Content, resp.StatusCode
}

func (s *stack) state() string {
	var v struct{ State string }
	json.Unmarshal(s.admin.do("GET", "/api/servers/"+s.server, nil, 200), &v)
	return v.State
}

func (s *stack) makeBackup(body map[string]any) store.Backup {
	s.t.Helper()
	var b store.Backup
	json.Unmarshal(s.admin.do("POST", "/api/servers/"+s.server+"/backups", body, 202), &b)
	waitFor(s.t, "backup finished", func() bool {
		var l []store.Backup
		json.Unmarshal(s.admin.do("GET", "/api/servers/"+s.server+"/backups", nil, 200), &l)
		for _, x := range l {
			if x.UUID == b.UUID && x.Status != store.BackupRunning {
				b = x
				return true
			}
		}
		return false
	})
	return b
}

// restore starts a restore (it runs in the background) and waits for its
// outcome, which is recorded on the backup.
func (s *stack) restore(id string, truncate bool) store.Backup {
	s.t.Helper()
	s.admin.do("POST", "/api/servers/"+s.server+"/backups/"+id+"/restore", map[string]any{"truncate": truncate}, 202)
	var b store.Backup
	waitFor(s.t, "restore finished", func() bool {
		var l []store.Backup
		json.Unmarshal(s.admin.do("GET", "/api/servers/"+s.server+"/backups", nil, 200), &l)
		for _, x := range l {
			if x.UUID == id && x.RestoreStatus != store.BackupRunning {
				b = x
				return true
			}
		}
		return false
	})
	return b
}

func TestBackupRestoreLifecycle(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	base := "/api/servers/" + s.server + "/backups/"
	s.write("world/level.dat", "v1")
	s.write("logs/latest.log", "noise")
	b := s.makeBackup(map[string]any{"name": "first", "ignore": "logs"})
	if b.Status != store.BackupOK || b.Size == 0 || len(b.SHA256) != 64 {
		t.Fatalf("backup not ok: %+v", b)
	}

	s.write("world/level.dat", "v2-corrupted")
	s.write("extra.txt", "added later")
	// plain restore overwrites changed files but keeps unrelated ones
	if r := s.restore(b.UUID, false); r.RestoreStatus != store.BackupOK || r.RestoredAt == 0 {
		t.Fatalf("restore not recorded as ok: %+v", r)
	}
	if got, _ := s.read("world/level.dat"); got != "v1" {
		t.Fatalf("restore did not revert: %q", got)
	}
	if _, code := s.read("extra.txt"); code != 200 {
		t.Fatal("non-truncating restore removed a file")
	}
	// truncating restore removes files that were not in the backup, and ignored ones
	s.restore(b.UUID, true)
	if _, code := s.read("extra.txt"); code == 200 {
		t.Fatal("truncating restore left extra.txt")
	}
	if _, code := s.read("logs/latest.log"); code == 200 {
		t.Fatal("ignored path was in the backup")
	}

	// restoring under a running server is refused
	s.admin.do("POST", "/api/servers/"+s.server+"/power", map[string]string{"action": "start"}, 202)
	waitFor(t, "running", func() bool { return s.state() == "running" })
	s.admin.do("POST", base+b.UUID+"/restore", map[string]any{}, 409)
	s.admin.do("POST", "/api/servers/"+s.server+"/power", map[string]string{"action": "stop"}, 202)
	waitFor(t, "stopped", func() bool { return s.state() == "offline" })

	// locked backups cannot be deleted
	s.admin.do("PUT", base+b.UUID+"/lock", map[string]bool{"locked": true}, 204)
	s.admin.do("DELETE", base+b.UUID, nil, 409)
	s.admin.do("PUT", base+b.UUID+"/lock", map[string]bool{"locked": false}, 204)
	s.admin.do("DELETE", base+b.UUID, nil, 204)
	s.admin.do("POST", base+b.UUID+"/restore", map[string]any{}, 404)
}

func TestScheduleRunsCommandAndRespectsOnlyWhenOnline(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	base := "/api/servers/" + s.server + "/schedules"
	// validation
	s.admin.do("POST", base, map[string]any{"name": "bad", "cron": "not a cron", "enabled": true, "tasks": []any{}}, 422)
	s.admin.do("POST", base, map[string]any{"name": "bad", "cron": "@daily", "enabled": true, "tasks": []any{}}, 422)
	s.admin.do("POST", base, map[string]any{"name": "bad", "cron": "* * * * *", "enabled": true,
		"tasks": []map[string]any{{"action": "power", "payload": "explode"}}}, 422)

	var sc store.Schedule
	json.Unmarshal(s.admin.do("POST", base, map[string]any{
		"name": "ping", "cron": "*/5 * * * *", "enabled": true, "only_when_online": true,
		"tasks": []map[string]any{{"action": "command", "payload": "ping"}},
	}, 201), &sc)
	if sc.NextRun <= time.Now().Unix() || sc.NextRun%60 != 0 {
		t.Fatalf("next_run should be a future minute boundary, got %d", sc.NextRun)
	}
	status := func() string {
		var l []store.Schedule
		json.Unmarshal(s.admin.do("GET", base, nil, 200), &l)
		return l[0].LastStatus
	}
	due := func() {
		s.st.DB.Exec(`UPDATE schedules SET next_run=? WHERE id=?`, time.Now().Add(-time.Minute).Unix(), sc.ID)
	}

	// server stopped: the run is skipped, not failed
	due()
	waitFor(t, "skipped", func() bool { return strings.Contains(status(), "skipped") })

	// server running: the command is delivered to the console
	s.admin.do("POST", "/api/servers/"+s.server+"/power", map[string]string{"action": "start"}, 202)
	waitFor(t, "running", func() bool { return s.state() == "running" })
	due()
	waitFor(t, "ran ok", func() bool { return strings.Contains(status(), " ok") })
	var l []store.Schedule
	json.Unmarshal(s.admin.do("GET", base, nil, 200), &l)
	if l[0].NextRun <= time.Now().Unix() {
		t.Fatal("next_run was not advanced after the run")
	}

	// a failing task stops the run unless marked continue_on_failure
	json.Unmarshal(s.admin.do("PUT", base+"/"+itoa(sc.ID), map[string]any{
		"name": "ping", "cron": "*/5 * * * *", "enabled": true,
		"tasks": []map[string]any{{"action": "power", "payload": "start"}, {"action": "command", "payload": "after"}},
	}, 200), &sc)
	s.admin.do("POST", base+"/"+itoa(sc.ID)+"/run", nil, 202) // start while running -> daemon error
	waitFor(t, "failed", func() bool { return strings.Contains(status(), "failed") })

	s.admin.do("DELETE", base+"/"+itoa(sc.ID), nil, 204)
	s.admin.do("POST", base+"/"+itoa(sc.ID)+"/run", nil, 404)
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
