package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"sort"
	"testing"
)

type netResp struct {
	Ports []struct {
		Port    int
		Notes   string
		Primary bool
	}
	Limit     int64
	CanManage bool `json:"can_manage"`
	Pending   bool
}

func TestPorts(t *testing.T) {
	t.Parallel()
	s := newStack(t) // node range 25565-25570; the server has 25565
	srv := "/api/servers/" + s.server
	containerPorts := func() []int {
		sp, _ := s.fake.SpecOf(s.server)
		seen := map[int]bool{}
		var out []int
		for _, p := range sp.Ports {
			if !seen[p.Host] {
				seen[p.Host] = true
				out = append(out, p.Host)
			}
		}
		sort.Ints(out)
		return out
	}
	var n netResp
	json.Unmarshal(s.admin.do("GET", srv+"/network", nil, 200), &n)
	if len(n.Ports) != 1 || n.Ports[0].Port != 25565 || !n.Ports[0].Primary || n.Limit != 0 {
		t.Fatalf("initial %+v", n)
	}

	// The owner (a subuser here) gets nothing beyond the limit; an admin may assign any free port.
	s.st.CreateUser(s.ctx, "bob@example.com", "another-long-password", false)
	jar, _ := cookiejar.New(nil)
	bob := &client{t, s.admin.base, &http.Client{Jar: jar}}
	bob.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "another-long-password"}, 200)
	s.admin.do("POST", srv+"/users", map[string]any{"email": "bob@example.com", "permissions": []string{"network.read", "network.manage"}}, 201)
	bob.do("POST", srv+"/network", map[string]any{}, 409) // limit 0
	s.setLimits(map[string]any{"port_limit": 2}, 204)
	json.Unmarshal(bob.do("POST", srv+"/network", map[string]any{"notes": "query"}, 201), &n)
	bob.do("POST", srv+"/network", map[string]any{"port": 25570}, 403) // only admins pick
	json.Unmarshal(bob.do("POST", srv+"/network", map[string]any{}, 201), &n)
	bob.do("POST", srv+"/network", map[string]any{}, 409) // 2 extra: full
	if got := containerPorts(); len(got) != 3 || got[1] != 25566 || got[2] != 25567 {
		t.Fatalf("container ports %v", got)
	}
	s.admin.do("POST", srv+"/network", map[string]any{"port": 25570, "notes": "rcon"}, 201) // admins are not limited
	s.admin.do("POST", srv+"/network", map[string]any{"port": 25570}, 422)                  // taken
	s.admin.do("POST", srv+"/network", map[string]any{"port": 30000}, 422)                  // outside the range

	// Primary can move; the old primary can then be removed, the new one cannot.
	json.Unmarshal(bob.do("PUT", srv+"/network/25566", map[string]any{"primary": true}, 200), &n)
	if n.Ports[0].Port != 25566 || !n.Ports[0].Primary || n.Ports[0].Notes != "query" {
		t.Fatalf("after primary change %+v", n.Ports)
	}
	sp, _ := s.fake.SpecOf(s.server)
	if sp.Env["SERVER_PORT"] != "25566" {
		t.Fatalf("SERVER_PORT %s", sp.Env["SERVER_PORT"])
	}
	bob.do("DELETE", srv+"/network/25566", nil, 409)
	bob.do("DELETE", srv+"/network/25565", nil, 200)
	bob.do("DELETE", srv+"/network/25565", nil, 404)
	if got := containerPorts(); len(got) != 3 {
		t.Fatalf("container ports after removal %v", got)
	}
	bob.do("PUT", srv+"/network/25567", map[string]any{"notes": "voice"}, 200)

	// A freed port can be handed out again, to another server too.
	var other struct{ UUID string }
	json.Unmarshal(s.admin.do("POST", "/api/servers", map[string]any{"name": "other", "node_id": s.node, "egg_id": 1, "memory_mb": 128}, 201), &other)
	var o netResp
	json.Unmarshal(s.admin.do("GET", "/api/servers/"+other.UUID+"/network", nil, 200), &o)
	if o.Ports[0].Port != 25565 {
		t.Fatalf("freed port not reused: %+v", o.Ports)
	}

	// Without network.manage: read only.
	s.admin.do("PUT", srv+"/users/"+bobID(t, s), map[string]any{"permissions": []string{"network.read"}}, 204)
	json.Unmarshal(bob.do("GET", srv+"/network", nil, 200), &n)
	if n.CanManage {
		t.Fatal("read-only subuser can manage")
	}
	bob.do("POST", srv+"/network", map[string]any{}, 403)
	bob.do("DELETE", srv+"/network/25567", nil, 403)

	// Deleting a server frees its ports.
	s.admin.do("DELETE", "/api/servers/"+other.UUID, nil, 204)
	var count int
	s.st.DB.QueryRow(`SELECT COUNT(*) FROM server_ports WHERE port=25565`).Scan(&count)
	if count != 0 {
		t.Fatal("a deleted server kept its port")
	}
}
