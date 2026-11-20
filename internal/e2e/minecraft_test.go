package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/minecraft"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
)

// A server made from a bundled Minecraft egg carries its features, lists the
// versions its project offers, and looks up players.
func TestMinecraftEndpoints(t *testing.T) {
	t.Parallel()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fill/paper/versions":
			_, _ = io.WriteString(w, `{"versions":[{"version":{"id":"1.21.11"}},{"version":{"id":"26.3"}}]}`)
		case "/profiles/Notch":
			_, _ = io.WriteString(w, `{"id":"069a79f444e94726a5befca90e38aaf5","name":"Notch"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer up.Close()
	mc := minecraft.NewClient(minecraft.Sources{Fill: up.URL + "/fill", Profiles: up.URL + "/profiles"})
	s := newStackWith(t, func(d *api.Deps) { d.Minecraft = mc }, nil)

	egg, err := os.ReadFile("../../eggs/minecraft/paper.json")
	if err != nil {
		t.Fatal(err)
	}
	var e struct {
		ID       int64
		Features []string
		Done     []string
	}
	json.Unmarshal(s.admin.do("POST", "/api/eggs/import", json.RawMessage(egg), 201), &e)
	if !slices.Contains(e.Features, "minecraft") || len(e.Done) != 1 {
		t.Fatalf("imported egg %+v", e)
	}
	var created struct{ UUID string }
	json.Unmarshal(s.admin.do("POST", "/api/servers", map[string]any{
		"name": "mc", "node_id": s.node, "egg_id": e.ID, "memory_mb": 1024, "env": map[string]string{"MINECRAFT_VERSION": "1.21.11"},
	}, 201), &created)
	srv := "/api/servers/" + created.UUID

	var view struct{ Features []string }
	json.Unmarshal(s.admin.do("GET", srv, nil, 200), &view)
	if !slices.Contains(view.Features, "eula") {
		t.Fatalf("server features %q", view.Features)
	}

	var v struct {
		Flavor, Variable, Current string
		Reset                     map[string]string
		Versions                  []string
	}
	json.Unmarshal(s.admin.do("GET", srv+"/minecraft/versions", nil, 200), &v)
	if v.Flavor != "paper" || v.Variable != "MINECRAFT_VERSION" || v.Current != "1.21.11" ||
		strings.Join(v.Versions, " ") != "26.3 1.21.11" || v.Reset["BUILD_NUMBER"] != "latest" {
		t.Fatalf("versions %+v", v)
	}

	var p minecraft.PlayerProfile
	json.Unmarshal(s.admin.do("GET", srv+"/minecraft/players/Notch", nil, 200), &p)
	if p.UUID != "069a79f4-44e9-4726-a5be-fca90e38aaf5" || p.Offline {
		t.Fatalf("profile %+v", p)
	}
	json.Unmarshal(s.admin.do("GET", srv+"/minecraft/players/Nobody_Here", nil, 200), &p)
	if p.UUID != minecraft.OfflineUUID("Nobody_Here") || !p.Offline {
		t.Fatalf("offline profile %+v", p)
	}
	json.Unmarshal(s.admin.do("GET", srv+"/minecraft/players/Notch?offline=true", nil, 200), &p)
	if p.UUID != minecraft.OfflineUUID("Notch") || !p.Offline {
		t.Fatalf("forced offline profile %+v", p)
	}
	s.admin.do("GET", srv+"/minecraft/players/bad%20name", nil, 422)

	// A stopped server cannot be pinged.
	if b := string(s.admin.do("GET", srv+"/minecraft/status", nil, 409)); !strings.Contains(b, "not running") {
		t.Fatalf("status of a stopped server: %s", b)
	}
}
