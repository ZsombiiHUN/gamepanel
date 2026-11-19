package e2e

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/modrinth"
)

func TestModManager(t *testing.T) {
	t.Parallel()
	jar := []byte("PK fake LuckPerms jar")
	sum := sha512.Sum512(jar)
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(jar) }))
	defer cdn.Close()
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("malware")) }))
	defer evil.Close()
	var lastSearch url.Values
	file := func(u, hash string) map[string]any {
		return map[string]any{"url": u, "filename": "LuckPerms-5.4.jar", "primary": true, "size": len(jar), "hashes": map[string]string{"sha512": hash}}
	}
	versions := map[string]map[string]any{
		"good":     {"id": "good", "project_id": "Vebnzrzj", "version_number": "5.4", "files": []any{file(cdn.URL+"/data/LuckPerms-5.4.jar", hex.EncodeToString(sum[:]))}},
		"tampered": {"id": "tampered", "project_id": "Vebnzrzj", "version_number": "5.3", "files": []any{file(cdn.URL+"/data/x.jar", strings.Repeat("ab", 64))}},
		"offsite":  {"id": "offsite", "project_id": "Vebnzrzj", "version_number": "5.2", "files": []any{file(evil.URL+"/x.jar", hex.EncodeToString(sum[:]))}},
	}
	mr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch {
		case r.URL.Path == "/search":
			lastSearch = r.URL.Query()
			json.NewEncoder(w).Encode(map[string]any{"hits": []any{map[string]any{"project_id": "Vebnzrzj", "slug": "luckperms", "title": "LuckPerms", "project_type": "plugin"}}, "total_hits": 1})
		case r.URL.Path == "/project/Vebnzrzj/version":
			json.NewEncoder(w).Encode([]any{versions["good"]})
		case strings.HasPrefix(r.URL.Path, "/version/"):
			v, ok := versions[strings.TrimPrefix(r.URL.Path, "/version/")]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(v)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mr.Close()
	cdnHost, _ := url.Parse(cdn.URL)
	client := modrinth.New()
	client.BaseURL, client.FileHosts, client.AllowHTTP = mr.URL, []string{cdnHost.Host}, true
	s := newStackWith(t, func(d *api.Deps) { d.Modrinth = client }, func(c *daemon.Config) { c.PullAllowPrivate = true })
	srv := "/api/servers/" + s.server

	var res struct {
		Hits []struct{ Title string }
	}
	json.Unmarshal(s.admin.do("GET", srv+"/mods/search?q=perms&type=plugin&loader=paper&version=1.21.1", nil, 200), &res)
	if len(res.Hits) != 1 || res.Hits[0].Title != "LuckPerms" {
		t.Fatalf("search %+v", res)
	}
	if f := lastSearch.Get("facets"); !strings.Contains(f, "project_type:plugin") || !strings.Contains(f, "categories:paper") || !strings.Contains(f, "versions:1.21.1") {
		t.Fatalf("facets %s", f)
	}
	s.admin.do("GET", srv+"/mods/Vebnzrzj/versions?loader=paper", nil, 200)

	var out struct{ Path string }
	json.Unmarshal(s.admin.do("POST", srv+"/mods", map[string]string{"version_id": "good", "type": "plugin"}, 201), &out)
	if out.Path != "/plugins/LuckPerms-5.4.jar" {
		t.Fatalf("installed at %s", out.Path)
	}
	if c, _ := s.read("/plugins/LuckPerms-5.4.jar"); c != string(jar) {
		t.Fatalf("content %q", c)
	}
	// A file that does not match Modrinth's checksum is not kept.
	if b := string(s.admin.do("POST", srv+"/mods", map[string]string{"version_id": "tampered", "type": "plugin"}, 422)); !strings.Contains(b, "checksum") {
		t.Fatalf("tampered: %s", b)
	}
	if _, code := s.read("/plugins/x.jar"); code != 404 {
		t.Fatal("a tampered file was kept")
	}
	// A download hosted anywhere else is refused before the node fetches it.
	if b := string(s.admin.do("POST", srv+"/mods", map[string]string{"version_id": "offsite", "type": "plugin"}, 422)); !strings.Contains(b, "Modrinth's servers") {
		t.Fatalf("offsite: %s", b)
	}
	s.admin.do("POST", srv+"/mods", map[string]string{"version_id": "missing", "type": "mod"}, 404)
	s.admin.do("POST", srv+"/mods", map[string]string{"version_id": "../../x", "type": "mod"}, 422)
}
