package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func (s *stack) limits(extra map[string]any) map[string]any {
	l := map[string]any{"memory_mb": 256, "swap_mb": 0, "cpu_pct": 0, "cpuset": "", "pids_limit": 1024, "io_weight": 0, "disk_mb": 0, "backup_limit": 20}
	for k, v := range extra {
		l[k] = v
	}
	return l
}

func (s *stack) setLimits(extra map[string]any, want int) {
	s.t.Helper()
	s.admin.do("PUT", "/api/servers/"+s.server+"/limits", s.limits(extra), want)
}

type serverLimitsView struct {
	MemoryMB    int64  `json:"memory_mb"`
	SwapMB      int64  `json:"swap_mb"`
	CPUPct      int64  `json:"cpu_pct"`
	CPUSet      string `json:"cpuset"`
	PidsLimit   int64  `json:"pids_limit"`
	IOWeight    int64  `json:"io_weight"`
	DiskMB      int64  `json:"disk_mb"`
	BackupLimit int64  `json:"backup_limit"`
	Stats       *struct {
		DiskBytes int64 `json:"disk_bytes"`
	} `json:"stats"`
}

func (s *stack) view() (v serverLimitsView) {
	json.Unmarshal(s.admin.do("GET", "/api/servers/"+s.server, nil, 200), &v)
	return
}

func TestEditLimitsAndNodeCapacity(t *testing.T) {
	s := newStack(t)
	srv := "/api/servers/" + s.server

	v := s.view()
	if v.PidsLimit != 1024 || v.BackupLimit != 20 || v.SwapMB != 0 || v.DiskMB != 0 {
		t.Fatalf("defaults: %+v", v)
	}
	s.setLimits(map[string]any{"memory_mb": 512, "swap_mb": 256, "cpu_pct": 150, "cpuset": "0-1", "pids_limit": 300, "io_weight": 200, "disk_mb": 2048, "backup_limit": 3}, 204)
	if v = s.view(); v.MemoryMB != 512 || v.SwapMB != 256 || v.CPUPct != 150 || v.CPUSet != "0-1" || v.PidsLimit != 300 || v.IOWeight != 200 || v.DiskMB != 2048 || v.BackupLimit != 3 {
		t.Fatalf("limits not saved: %+v", v)
	}
	// bad values are refused with a reason, nothing changes
	s.setLimits(map[string]any{"cpuset": "0-3; reboot"}, 422)
	s.setLimits(map[string]any{"memory_mb": 8}, 422)
	s.setLimits(map[string]any{"io_weight": 5}, 422)
	if v = s.view(); v.MemoryMB != 512 {
		t.Fatal("a rejected update changed the server")
	}

	// a backup limit of 3 is honoured
	for i := 0; i < 3; i++ {
		s.makeBackup(map[string]any{"name": "b"})
	}
	s.admin.do("POST", srv+"/backups", map[string]any{}, 409)

	// not while the server runs
	s.admin.do("POST", srv+"/power", map[string]string{"action": "start"}, 202)
	waitFor(t, "running", func() bool { return s.state() == "running" })
	s.setLimits(map[string]any{"memory_mb": 1024}, 409)
	s.admin.do("POST", srv+"/power", map[string]string{"action": "stop"}, 202)
	waitFor(t, "stopped", func() bool { return s.state() == "offline" })

	// only admins change limits (an owner/subuser cannot raise their own quota)
	s.st.CreateUser(s.ctx, "bob@example.com", "another-long-password", false)
	jar, _ := cookiejar.New(nil)
	bob := &client{t, s.admin.base, &http.Client{Jar: jar}}
	bob.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "another-long-password"}, 200)
	s.admin.do("POST", srv+"/users", map[string]any{"email": "bob@example.com", "permissions": []string{"power"}}, 201)
	bob.do("PUT", srv+"/limits", s.limits(map[string]any{"memory_mb": 99999}), 403)

	// node capacity: allocated so far is 512 MB
	s.admin.do("PUT", "/api/nodes/1/capacity", map[string]any{"memory_mb": 1000, "disk_mb": 0, "overallocate_pct": 0}, 204)
	var nodes []struct {
		Allocated int64 `json:"allocated_memory_mb"`
		Capacity  int64 `json:"memory_mb"`
	}
	json.Unmarshal(s.admin.do("GET", "/api/nodes", nil, 200), &nodes)
	if nodes[0].Allocated != 512 || nodes[0].Capacity != 1000 {
		t.Fatalf("node accounting: %+v", nodes[0])
	}
	s.setLimits(map[string]any{"memory_mb": 1001}, 409) // exceeds node
	s.setLimits(map[string]any{"memory_mb": 1000}, 204) // exact fit
	body := map[string]any{"name": "second", "node_id": 1, "egg_id": 1, "memory_mb": 64}
	if got := string(s.admin.do("POST", "/api/servers", body, 422)); !strings.Contains(got, "memory left") {
		t.Fatalf("expected a capacity message, got %s", got)
	}
	s.admin.do("PUT", "/api/nodes/1/capacity", map[string]any{"memory_mb": 1000, "disk_mb": 0, "overallocate_pct": 100}, 204)
	s.admin.do("POST", "/api/servers", body, 201)
	s.admin.do("PUT", "/api/nodes/999/capacity", map[string]any{"memory_mb": 1, "disk_mb": 0, "overallocate_pct": 0}, 404)
}

func TestDiskLimitEnforcement(t *testing.T) {
	s := newStack(t)
	srv := "/api/servers/" + s.server
	dir := filepath.Join(s.dataDir, "servers", s.server)
	s.setLimits(map[string]any{"disk_mb": 1}, 204)
	big := strings.Repeat("a", 700_000)

	// writes through the panel stop at the limit
	s.write("one.bin", big)
	s.admin.do("PUT", srv+"/files/content", map[string]string{"path": "two.bin", "content": big}, 507)
	if _, code := s.read("two.bin"); code == 200 {
		t.Fatal("the rejected write left a file behind")
	}
	// deleting makes room again
	s.admin.do("DELETE", srv+"/files?path=one.bin", nil, 204)
	waitFor(t, "room again", func() bool {
		code, _ := s.admin.c.Do(mustReq("PUT", s.admin.base+srv+"/files/content", `{"path":"three.bin","content":"`+big+`"}`))
		if code != nil {
			code.Body.Close()
			return code.StatusCode == 204
		}
		return false
	})
	s.admin.do("DELETE", srv+"/files?path=three.bin", nil, 204)

	// something that bypasses the panel (a game writing its world) goes over the limit:
	// usage is reported, and the server will not start
	os.WriteFile(filepath.Join(dir, "world.dat"), []byte(strings.Repeat("w", 3<<20)), 0o644)
	waitFor(t, "disk usage reported", func() bool {
		st := s.view().Stats
		return st != nil && st.DiskBytes >= 3<<20
	})
	s.admin.do("POST", srv+"/power", map[string]string{"action": "start"}, 202)
	time.Sleep(600 * time.Millisecond)
	if st := s.state(); st != "offline" {
		t.Fatalf("a server over its disk limit started (state %s)", st)
	}

	// back under the limit it starts; growing past it while running stops it
	os.Remove(filepath.Join(dir, "world.dat"))
	waitFor(t, "usage drops", func() bool { st := s.view().Stats; return st != nil && st.DiskBytes < 1<<20 })
	s.admin.do("POST", srv+"/power", map[string]string{"action": "start"}, 202)
	waitFor(t, "running", func() bool { return s.state() == "running" })
	os.WriteFile(filepath.Join(dir, "world.dat"), []byte(strings.Repeat("w", 3<<20)), 0o644)
	waitFor(t, "stopped for exceeding the disk limit", func() bool { return s.state() == "offline" })
	os.Remove(filepath.Join(dir, "world.dat"))
	waitFor(t, "usage drops again", func() bool { st := s.view().Stats; return st != nil && st.DiskBytes < 1<<20 })

	// a restore that would not fit is refused instead of filling the disk
	s.write("keep.bin", strings.Repeat("k", 600_000))
	b := s.makeBackup(map[string]any{"name": "fits"})
	os.WriteFile(filepath.Join(dir, "extra.bin"), []byte(strings.Repeat("e", 700_000)), 0o644)
	waitFor(t, "usage includes the extra file", func() bool { st := s.view().Stats; return st != nil && st.DiskBytes > 1<<20 })
	if got := string(s.admin.do("POST", srv+"/backups/"+b.UUID+"/restore", map[string]any{}, 409)); !strings.Contains(got, "disk limit") {
		t.Fatalf("restore error should mention the disk limit: %s", got)
	}
	// ... but a truncating restore replaces everything, so it fits
	s.admin.do("POST", srv+"/backups/"+b.UUID+"/restore", map[string]any{"truncate": true}, 204)
}

func mustReq(method, url, body string) *http.Request {
	r, _ := http.NewRequest(method, url, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// The web form always sends every limit, with 0 meaning "default/unlimited".
func TestCreateServerWithExplicitDefaultLimits(t *testing.T) {
	s := newStack(t)
	var out struct{ UUID string }
	json.Unmarshal(s.admin.do("POST", "/api/servers", map[string]any{
		"name": "form", "node_id": 1, "egg_id": 1, "memory_mb": 128, "swap_mb": 0, "cpu_pct": 0, "cpuset": "",
		"pids_limit": 1024, "io_weight": 0, "disk_mb": 0, "backup_limit": 20,
	}, 201), &out)
	if out.UUID == "" {
		t.Fatal("no server created")
	}
	// ... and the same with real values
	s.admin.do("POST", "/api/servers", map[string]any{
		"name": "full", "node_id": 1, "egg_id": 1, "memory_mb": 128, "swap_mb": -1, "cpu_pct": 200, "cpuset": "0-1",
		"pids_limit": 500, "io_weight": 300, "disk_mb": 4096, "backup_limit": 5,
	}, 201)
	s.admin.do("POST", "/api/servers", map[string]any{"name": "bad", "node_id": 1, "egg_id": 1, "memory_mb": 128, "io_weight": 5}, 422)
}
