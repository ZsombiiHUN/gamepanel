package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// Creating a server checks what it is given the way later changes are
// checked: variables against the egg's rules, the owner, and the name. A
// refused create leaves nothing behind.
func TestCreateServerChecksItsInput(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	var e struct{ ID int64 }
	json.Unmarshal(s.admin.do("POST", "/api/eggs/import", startupEgg, 201), &e)
	body := func(extra map[string]any) map[string]any {
		b := map[string]any{"name": "paper", "node_id": s.node, "egg_id": e.ID, "memory_mb": 256}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	count := func() (n int) {
		s.st.DB.QueryRow(`SELECT COUNT(*) FROM servers`).Scan(&n)
		return
	}
	before := count()
	refused := func(extra map[string]any, want string) {
		t.Helper()
		got := string(s.admin.do("POST", "/api/servers", body(extra), 422))
		if !strings.Contains(got, want) {
			t.Errorf("%v: %s, want it to say %q", extra, got, want)
		}
	}
	refused(map[string]any{"env": map[string]string{"JAR": "; rm -rf /"}}, "Jar")
	refused(map[string]any{"env": map[string]string{"VERSION": ""}}, "Version: is required")
	refused(map[string]any{"env": map[string]string{"LD_PRELOAD": "/x.so"}}, "no variable LD_PRELOAD")
	refused(map[string]any{"env": map[string]string{"SERVER_PORT": "1"}}, "no variable SERVER_PORT")
	refused(map[string]any{"owner_id": 999}, "no account")
	refused(map[string]any{"name": "   "}, "name on one line")
	refused(map[string]any{"name": "a\nb"}, "name on one line")
	refused(map[string]any{"name": "a\x1b[31mb"}, "control or text-direction")
	refused(map[string]any{"name": "admin‮gnp.exe"}, "control or text-direction")
	if n := count(); n != before {
		t.Fatalf("refused creates left %d servers", n-before)
	}

	// Valid values pass, the name is trimmed, and defaults fill the rest.
	var created struct{ UUID, Name string }
	json.Unmarshal(s.admin.do("POST", "/api/servers", body(map[string]any{"name": "  Paper ✨  ", "env": map[string]string{"JAR": "paper-1.21.jar"}}), 201), &created)
	if created.Name != "Paper ✨" {
		t.Fatalf("name %q", created.Name)
	}
	waitFor(t, "container built", func() bool { _, ok := s.fake.SpecOf(created.UUID); return ok })
	sp, _ := s.fake.SpecOf(created.UUID)
	if sp.Env["JAR"] != "paper-1.21.jar" || sp.Env["VERSION"] != "latest" {
		t.Fatalf("env on the node: %v", sp.Env)
	}

	// Renaming applies the same name rules.
	s.admin.do("PUT", "/api/servers/"+created.UUID+"/details", map[string]any{"name": "x⁦y"}, 422)
}

// A node that cannot take the server (here: offline) rolls the create back.
func TestCreateOnAnOfflineNodeIsRolledBack(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	var n struct{ Node struct{ ID int64 } }
	json.Unmarshal(s.admin.do("POST", "/api/nodes", map[string]any{"name": "offline", "port_start": 30000, "port_end": 30001}, 201), &n)
	s.admin.do("POST", "/api/servers", map[string]any{"name": "x", "node_id": n.Node.ID, "egg_id": 1, "memory_mb": 128}, 503)
	var servers, ports int
	s.st.DB.QueryRow(`SELECT COUNT(*) FROM servers WHERE node_id=?`, n.Node.ID).Scan(&servers)
	s.st.DB.QueryRow(`SELECT COUNT(*) FROM server_ports WHERE node_id=?`, n.Node.ID).Scan(&ports)
	if servers != 0 || ports != 0 {
		t.Fatalf("left %d servers and %d ports on the offline node", servers, ports)
	}
}
