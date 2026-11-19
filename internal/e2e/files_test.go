package e2e

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// raw sends a request with a raw body and returns the status and reply.
func (c *client) raw(method, path string, body []byte) (int, []byte, http.Header) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.base+path, bytes.NewReader(body))
	if body == nil {
		req.Body = nil
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, resp.Header
}

func TestFileManager(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	srv := "/api/servers/" + s.server
	q := url.QueryEscape

	// Upload a binary file larger than one websocket frame and read it back.
	payload := make([]byte, 3<<20)
	rand.Read(payload)
	if code, b, _ := s.admin.raw("PUT", srv+"/files/upload?path="+q("/mods/big.jar"), payload); code != 201 {
		t.Fatalf("upload: %d %s", code, b)
	}
	code, got, h := s.admin.raw("GET", srv+"/files/download?path="+q("/mods/big.jar"), nil)
	if code != 200 || !bytes.Equal(got, payload) {
		t.Fatalf("download: %d, %d bytes, same=%v", code, len(got), bytes.Equal(got, payload))
	}
	if !strings.Contains(h.Get("Content-Disposition"), `filename=big.jar`) || h.Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("download headers %v", h)
	}
	if code, _, _ := s.admin.raw("GET", srv+"/files/download?path="+q("/missing.txt"), nil); code != 404 {
		t.Fatalf("missing file: %d", code)
	}
	if code, _, _ := s.admin.raw("GET", srv+"/files/download?path="+q("/mods"), nil); code != 422 {
		t.Fatalf("download of a folder: %d", code)
	}

	// Folder, move, copy, chmod.
	s.admin.do("POST", srv+"/files/folder", map[string]string{"path": "/config/sub"}, 204)
	s.write("/server.properties", "motd=hi")
	s.admin.do("POST", srv+"/files/move", map[string]any{"moves": []map[string]string{{"from": "/server.properties", "to": "/config/server.properties"}}}, 204)
	if c, _ := s.read("/config/server.properties"); c != "motd=hi" {
		t.Fatalf("moved file %q", c)
	}
	s.write("/config/other.yml", "x")
	s.admin.do("POST", srv+"/files/move", map[string]any{"moves": []map[string]string{{"from": "/config/other.yml", "to": "/config/server.properties"}}}, 409)
	var cp struct{ Path string }
	json.Unmarshal(s.admin.do("POST", srv+"/files/copy", map[string]string{"path": "/config/server.properties"}, 201), &cp)
	if cp.Path != "/config/server copy.properties" {
		t.Fatalf("copy at %s", cp.Path)
	}
	s.admin.do("POST", srv+"/files/chmod", map[string]any{"paths": []string{"/config/server.properties"}, "mode": "640"}, 204)
	s.admin.do("POST", srv+"/files/chmod", map[string]any{"paths": []string{"/config/server.properties"}, "mode": "4777"}, 422)
	var list []struct {
		Name string
		Mode uint32
	}
	json.Unmarshal(s.admin.do("GET", srv+"/files?path=/config", nil, 200), &list)
	for _, f := range list {
		if f.Name == "server.properties" && f.Mode != 0o640 {
			t.Fatalf("mode %o", f.Mode)
		}
	}

	// Compress, delete, decompress.
	var arc struct{ Path string }
	json.Unmarshal(s.admin.do("POST", srv+"/files/compress", map[string]any{"root": "/", "paths": []string{"/config", "/mods"}, "format": "zip"}, 201), &arc)
	if !strings.HasSuffix(arc.Path, ".zip") {
		t.Fatalf("archive %s", arc.Path)
	}
	s.admin.do("POST", srv+"/files/delete", map[string]any{"paths": []string{"/config", "/mods"}}, 204)
	if _, code := s.read("/config/server.properties"); code != 404 {
		t.Fatal("delete left the file")
	}
	s.admin.do("POST", srv+"/files/delete", map[string]any{"paths": []string{"/"}}, 422)
	s.admin.do("POST", srv+"/files/decompress", map[string]string{"path": arc.Path}, 204)
	if c, _ := s.read("/config/server.properties"); c != "motd=hi" {
		t.Fatalf("extracted %q", c)
	}
	s.write("/notes.rar", "x")
	if b := s.admin.do("POST", srv+"/files/decompress", map[string]string{"path": "/notes.rar"}, 422); !strings.Contains(string(b), "unsupported archive") {
		t.Fatalf("rar: %s", b)
	}

	// Pull from URL: a local address is refused by default.
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("internal secret")) }))
	defer local.Close()
	if b := s.admin.do("POST", srv+"/files/pull", map[string]string{"url": local.URL + "/x.txt", "path": "/"}, 422); !strings.Contains(string(b), "private network") {
		t.Fatalf("pull from a private address: %s", b)
	}
	s.admin.do("POST", srv+"/files/pull", map[string]string{"url": "file:///etc/passwd", "path": "/"}, 422)

	// Backup download streams the archive.
	b := s.makeBackup(map[string]any{"name": "dl"})
	code, arch, _ := s.admin.raw("GET", srv+"/backups/"+b.UUID+"/download", nil)
	if code != 200 || len(arch) == 0 || arch[0] != 0x1f || arch[1] != 0x8b {
		t.Fatalf("backup download: %d, %d bytes", code, len(arch))
	}

	// A subuser who may only read files can download but not upload or change anything.
	s.st.CreateUser(s.ctx, "bob@example.com", "another-long-password", false)
	jar, _ := cookiejar.New(nil)
	bob := &client{t, s.admin.base, &http.Client{Jar: jar}}
	bob.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "another-long-password"}, 200)
	s.admin.do("POST", srv+"/users", map[string]any{"email": "bob@example.com", "permissions": []string{"files.read"}}, 201)
	if code, _, _ := bob.raw("GET", srv+"/files/download?path="+q("/config/server.properties"), nil); code != 200 {
		t.Fatalf("subuser download: %d", code)
	}
	if code, _, _ := bob.raw("PUT", srv+"/files/upload?path="+q("/evil.sh"), []byte("x")); code != 403 {
		t.Fatalf("subuser upload: %d", code)
	}
	if code, _, _ := bob.raw("GET", srv+"/backups/"+b.UUID+"/download", nil); code != 403 {
		t.Fatalf("subuser backup download: %d", code)
	}
	bob.do("POST", srv+"/files/delete", map[string]any{"paths": []string{"/config"}}, 403)
	// A stranger cannot even tell the server exists.
	s.admin.do("DELETE", srv+"/users/"+bobID(t, s), nil, 204)
	if code, _, _ := bob.raw("GET", srv+"/files/download?path="+q("/config/server.properties"), nil); code != 404 {
		t.Fatalf("stranger download: %d", code)
	}

	// Relay tickets are single use and bound to their node.
	if code, _, _ := s.admin.raw("GET", "/daemon/relay/whatever", nil); code != 401 {
		t.Fatalf("relay without a node token: %d", code)
	}
}

func TestUploadRespectsDiskLimit(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	srv := "/api/servers/" + s.server
	s.setLimits(map[string]any{"disk_mb": 1}, 204)
	if code, b, _ := s.admin.raw("PUT", srv+"/files/upload?path=/big.bin", make([]byte, 2<<20)); code != 507 {
		t.Fatalf("over-quota upload: %d %s", code, b)
	}
	if _, code := s.read("/big.bin"); code != 404 {
		t.Fatal("the refused upload left a file")
	}
}

// The editor goes through the relay, not the control connection: it still
// refuses files over 10 MB, and saving keeps a script's permissions.
func TestEditorThroughRelay(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	srv := "/api/servers/" + s.server
	q := url.QueryEscape

	big := make([]byte, 10<<20+1)
	if code, b, _ := s.admin.raw("PUT", srv+"/files/upload?path="+q("/big.log"), big); code != 201 {
		t.Fatalf("upload: %d %s", code, b)
	}
	if code, b, _ := s.admin.raw("GET", srv+"/files/content?path="+q("/big.log"), nil); code != 422 || !strings.Contains(string(b), "too large") {
		t.Fatalf("opening a big file: %d %s", code, b)
	}
	if code, _, _ := s.admin.raw("GET", srv+"/files/content?path="+q("/mods"), nil); code != 404 && code != 422 {
		t.Fatalf("opening a folder: %d", code)
	}

	s.write("/start.sh", "#!/bin/sh\necho one")
	s.admin.do("POST", srv+"/files/chmod", map[string]any{"paths": []string{"/start.sh"}, "mode": "755"}, 204)
	s.write("/start.sh", "#!/bin/sh\necho two")
	if c, _ := s.read("/start.sh"); c != "#!/bin/sh\necho two" {
		t.Fatalf("saved %q", c)
	}
	var list []struct {
		Name string
		Mode uint32
	}
	json.Unmarshal(s.admin.do("GET", srv+"/files?path=/", nil, 200), &list)
	for _, f := range list {
		if f.Name == "start.sh" && f.Mode != 0o755 {
			t.Fatalf("mode after save %o", f.Mode)
		}
	}
	s.write("/empty.txt", "")
	if c, _ := s.read("/empty.txt"); c != "" {
		t.Fatalf("empty file read %q", c)
	}
}
