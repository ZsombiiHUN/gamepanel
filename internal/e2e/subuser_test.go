package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

func TestSubuserPermissions(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	bobUser, err := s.st.CreateUser(s.ctx, "bob@example.com", "another-long-password", false)
	if err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	bob := &client{t, s.admin.base, &http.Client{Jar: jar}}
	bob.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "another-long-password"}, 200)
	srv := "/api/servers/" + s.server

	// a stranger cannot even tell the server exists
	bob.do("GET", srv, nil, 404)
	bob.do("GET", srv+"/files", nil, 404)
	if l := string(bob.do("GET", "/api/servers", nil, 200)); strings.Contains(l, s.server) {
		t.Fatal("stranger sees the server in their list")
	}

	// validation on grant
	add := func(email string, perms []string, want int) []byte {
		return s.admin.do("POST", srv+"/users", map[string]any{"email": email, "permissions": perms}, want)
	}
	add("bob@example.com", []string{"root"}, 422)
	add("nobody@example.com", []string{"files.read"}, 422)
	add("admin@example.com", []string{"files.read"}, 422) // the owner
	add("bob@example.com", []string{"console.read", "files.read"}, 201)
	add("bob@example.com", []string{"files.read"}, 422) // duplicate

	// what bob got: visible, read-only, no secrets, no owner powers
	var v struct {
		Permissions []string
		Owner       bool
		Env         map[string]string
	}
	json.Unmarshal(bob.do("GET", srv, nil, 200), &v)
	if v.Owner || len(v.Env) != 0 || strings.Join(v.Permissions, ",") != "console.read,files.read" {
		t.Fatalf("bob's view: %+v", v)
	}
	if l := string(bob.do("GET", "/api/servers", nil, 200)); !strings.Contains(l, s.server) {
		t.Fatal("shared server missing from bob's list")
	}
	bob.do("GET", srv+"/files", nil, 200)
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"PUT", "/files/content", map[string]string{"path": "x", "content": "y"}},
		{"DELETE", "/files?path=x", nil},
		{"POST", "/power", map[string]string{"action": "start"}},
		{"POST", "/command", map[string]string{"command": "op bob"}},
		{"POST", "/reinstall", nil},
		{"GET", "/backups", nil},
		{"POST", "/backups", map[string]any{}},
		{"GET", "/schedules", nil},
		{"GET", "/users", nil},
		{"POST", "/users", map[string]any{"email": "bob@example.com", "permissions": []string{"power"}}},
		{"PUT", "/users/" + itoa(bobUser.ID), map[string]any{"permissions": []string{"power"}}},
	} {
		bob.do(c.method, srv+c.path, c.body, 403)
	}
	bob.do("DELETE", srv, nil, 403) // only admins delete servers

	// granting more takes effect immediately
	s.admin.do("PUT", srv+"/users/"+itoa(bobUser.ID), map[string]any{"permissions": []string{"files.write", "schedules.manage"}}, 204)
	bob.do("PUT", srv+"/files/content", map[string]string{"path": "x", "content": "y"}, 204)
	bob.do("GET", srv+"/files", nil, 403) // replaced, not merged

	// a schedule must not be a way around missing permissions
	sched := func(action, payload string, want int) {
		bob.do("POST", srv+"/schedules", map[string]any{
			"name": "n", "cron": "0 4 * * *", "enabled": true,
			"tasks": []map[string]any{{"action": action, "payload": payload}},
		}, want)
	}
	sched("command", "op bob", 403)
	sched("power", "restart", 403)
	sched("backup", "", 403)
	s.admin.do("PUT", srv+"/users/"+itoa(bobUser.ID), map[string]any{"permissions": []string{"schedules.manage", "console.send"}}, 204)
	sched("command", "say hi", 201)
	sched("power", "restart", 403)

	// listing is visible to the owner
	var subs []store.Subuser
	json.Unmarshal(s.admin.do("GET", srv+"/users", nil, 200), &subs)
	if len(subs) != 1 || subs[0].Email != "bob@example.com" {
		t.Fatalf("subusers: %+v", subs)
	}

	// revoking cuts access at once
	s.admin.do("DELETE", srv+"/users/"+itoa(bobUser.ID), nil, 204)
	bob.do("GET", srv, nil, 404)
	s.admin.do("DELETE", srv+"/users/"+itoa(bobUser.ID), nil, 404)
}
