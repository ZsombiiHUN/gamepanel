package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
)

type listedServer struct {
	UUID     string `json:"uuid"`
	Name     string `json:"name"`
	EggName  string `json:"egg_name"`
	NodeName string `json:"node_name"`
	Port     int    `json:"port"`
}

func TestServerViewsCarryEggAndNodeNames(t *testing.T) {
	t.Parallel()
	s := newStack(t)

	var list []listedServer
	json.Unmarshal(s.admin.do("GET", "/api/servers", nil, 200), &list)
	if len(list) != 1 || list[0].EggName != "Echo" || list[0].NodeName != "n1" {
		t.Fatalf("list view lacks names: %+v", list)
	}
	var one listedServer
	json.Unmarshal(s.admin.do("GET", "/api/servers/"+s.server, nil, 200), &one)
	if one.EggName != "Echo" || one.NodeName != "n1" {
		t.Fatalf("detail view lacks names: %+v", one)
	}
}

func TestAuditSearchEndpoint(t *testing.T) {
	t.Parallel()
	s := newStack(t) // logs in (login.ok) and creates a node, egg and server

	get := func(q string) []struct {
		Action string `json:"action"`
	} {
		var es []struct {
			Action string `json:"action"`
		}
		json.Unmarshal(s.admin.do("GET", "/api/audit?q="+url.QueryEscape(q), nil, 200), &es)
		return es
	}
	if all := get(""); len(all) < 4 {
		t.Fatalf("expected login, node, egg and server events, got %v", all)
	}
	if got := get("server.create"); len(got) != 1 || got[0].Action != "server.create" {
		t.Fatalf("q=server.create: %v", got)
	}
	if got := get("no-such-event"); len(got) != 0 {
		t.Fatalf("q=no-such-event: %v", got)
	}
	// Over-long queries are rejected rather than scanned.
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	s.admin.do("GET", "/api/audit?q="+string(long), nil, 422)
}

func TestAuditSearchIsAdminOnly(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	s.st.CreateUser(s.ctx, "bob@example.com", "another-long-password", false)
	jar, _ := cookiejar.New(nil)
	bob := &client{t, s.admin.base, &http.Client{Jar: jar}}
	bob.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "another-long-password"}, 200)
	bob.do("GET", "/api/audit?q=login", nil, 403)
}
