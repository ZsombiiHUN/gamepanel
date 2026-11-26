package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

// The Pterodactyl application API, driven the way the billing modules
// drive it (WHMCS's pterodactyl.php, Paymenter's Pterodactyl extension,
// Blesta's module): the same paths, bodies and status checks.

type appServer struct {
	ID          int64
	ExternalID  *string `json:"external_id"`
	UUID        string
	Identifier  string
	Name        string
	Description string
	Status      *string
	Suspended   bool
	User        int64
	Node        int64
	Allocation  int64
	Egg         int64
	Limits      struct {
		Memory, Swap, Disk, IO, CPU int64
		Threads                     *string
	}
	FeatureLimits struct{ Databases, Allocations, Backups int64 } `json:"feature_limits"`
	Container     struct {
		StartupCommand string `json:"startup_command"`
		Image          string
		Installed      int
		Environment    map[string]string
	}
	Relationships struct {
		Allocations struct {
			Data []struct{ Attributes appAlloc }
		}
	}
}

type appAlloc struct {
	ID       int64
	IP       string
	Port     int
	Assigned bool
}

// appObj decodes {"object": ..., "attributes": ...}.
func appObj[T any](t *testing.T, b []byte) T {
	t.Helper()
	var v struct {
		Object     string
		Attributes T
	}
	if err := json.Unmarshal(b, &v); err != nil || v.Object == "" {
		t.Fatalf("not a Pterodactyl object (%v): %s", err, b)
	}
	return v.Attributes
}

type appListOf[T any] struct {
	Object string
	Data   []struct{ Attributes T }
	Meta   struct {
		Pagination struct {
			Total, Count int
			PerPage      int `json:"per_page"`
			CurrentPage  int `json:"current_page"`
			TotalPages   int `json:"total_pages"`
		}
	}
}

func appList[T any](t *testing.T, b []byte) appListOf[T] {
	t.Helper()
	var v appListOf[T]
	if err := json.Unmarshal(b, &v); err != nil || v.Object != "list" {
		t.Fatalf("not a Pterodactyl list (%v): %s", err, b)
	}
	return v
}

// appErr decodes Pterodactyl's error body.
func appErr(t *testing.T, b []byte) (code, status, field string) {
	t.Helper()
	var v struct {
		Errors []struct {
			Code, Status, Detail string
			Meta                 struct {
				SourceField string `json:"source_field"`
			}
		}
	}
	if err := json.Unmarshal(b, &v); err != nil || len(v.Errors) != 1 || v.Errors[0].Detail == "" {
		t.Fatalf("not a Pterodactyl error (%v): %s", err, b)
	}
	e := v.Errors[0]
	return e.Code, e.Status, e.Meta.SourceField
}

// appStack is a stack with an admin API key, and a location holding its node.
func appStack(t *testing.T) (*stack, ptero, int64, int64) {
	t.Helper()
	s := newStack(t)
	p := ptero{t, s.admin.base, s.apiKey(s.admin, "correct-horse-battery")}
	loc := appObj[struct{ ID int64 }](t, p.do("POST", "/api/application/locations", map[string]string{"short": "eu", "long": "Frankfurt"}, 201))
	s.admin.do("PUT", fmt.Sprintf("/api/nodes/%d/location", s.node), map[string]any{"location_id": loc.ID}, 204)
	var e []struct{ ID int64 }
	json.Unmarshal(s.admin.do("GET", "/api/eggs", nil, 200), &e)
	return s, p, loc.ID, e[0].ID
}

func TestApplicationAPIWHMCS(t *testing.T) {
	t.Parallel()
	s, p, loc, eggID := appStack(t)

	// Test connection.
	if l := appList[struct{ ID int64 }](t, p.do("GET", "/api/application/nodes", nil, 200)); l.Meta.Pagination.Count != 1 || l.Data[0].Attributes.ID != s.node {
		t.Fatalf("nodes %+v", l)
	}

	// The egg, with its variables.
	type eggVar struct {
		EnvVariable  string `json:"env_variable"`
		DefaultValue string `json:"default_value"`
		Name         string
	}
	eg := appObj[struct {
		DockerImage   string `json:"docker_image"`
		Startup       string
		Relationships struct {
			Variables struct{ Data []struct{ Attributes eggVar } }
		}
	}](t, p.do("GET", fmt.Sprintf("/api/application/eggs/%d?include=variables", eggID), nil, 200))
	if eg.DockerImage != "echo:latest" || eg.Startup != "run {{GREETING}}" || len(eg.Relationships.Variables.Data) != 1 || eg.Relationships.Variables.Data[0].Attributes.DefaultValue != "hi" {
		t.Fatalf("egg %+v", eg)
	}
	// The nest in the path does not matter: products made on Pterodactyl name their own.
	p.do("GET", fmt.Sprintf("/api/application/nests/5/eggs/%d?include=variables", eggID), nil, 200)

	// The client: not found by external id (404), not by email either, so created.
	_, status, _ := appErr(t, p.do("GET", "/api/application/users/external/7", nil, 404))
	if status != "404" {
		t.Fatalf("status %q", status)
	}
	if l := appList[struct{ ID int64 }](t, p.do("GET", "/api/application/users?filter%5Bemail%5D=buyer%40example.com", nil, 200)); l.Meta.Pagination.Total != 0 {
		t.Fatalf("users %+v", l)
	}
	user := appObj[struct {
		ID         int64
		Email      string
		Username   string
		ExternalID string `json:"external_id"`
	}](t, p.do("POST", "/api/application/users", map[string]any{
		"username": "Ab3dEf9h", "email": "Buyer@Example.com", "first_name": "Bea", "last_name": "Buyer", "external_id": "7",
	}, 201))
	if user.Email != "buyer@example.com" || user.ExternalID != "7" || user.Username != "buyer" {
		t.Fatalf("user %+v", user)
	}
	if got := appObj[struct{ ID int64 }](t, p.do("GET", "/api/application/users/external/7", nil, 200)); got.ID != user.ID {
		t.Fatalf("by external id %+v", got)
	}

	// The server, deployed to the location, started once installed.
	order := map[string]any{
		"name": "Bea's server", "user": user.ID, "nest": 1, "egg": eggID, "docker_image": "echo:latest", "startup": "run {{GREETING}}",
		"oom_disabled":        false,
		"limits":              map[string]any{"memory": 512, "swap": 0, "io": 500, "cpu": 100, "disk": 1000},
		"feature_limits":      map[string]any{"databases": nil, "allocations": 2, "backups": 0},
		"deploy":              map[string]any{"locations": []int64{loc}, "dedicated_ip": false, "port_range": []string{}},
		"environment":         map[string]any{"GREETING": "hello", "NOT_IN_EGG": "ignored"},
		"start_on_completion": true, "external_id": "1001",
	}
	srv := appObj[appServer](t, p.do("POST", "/api/application/servers?include=allocations", order, 201))
	a := srv.Relationships.Allocations.Data
	if len(a) != 1 || a[0].Attributes.Port != 25566 || a[0].Attributes.IP == "" {
		t.Fatalf("allocations %+v", a)
	}
	if srv.ID == 0 || srv.Identifier != srv.UUID[:8] || srv.User != user.ID || srv.Node != s.node || srv.Egg != eggID ||
		srv.FeatureLimits.Allocations != 2 || srv.FeatureLimits.Backups != 1 || srv.Limits.IO != 0 || srv.Container.Environment["GREETING"] != "hello" {
		t.Fatalf("server %+v", srv)
	}
	waitFor(t, "installed", func() bool {
		v := appObj[appServer](t, p.do("GET", "/api/application/servers/external/1001", nil, 200))
		return v.Container.Installed == 1 && v.Status == nil
	})
	waitFor(t, "started", func() bool { return statusOf(s, srv.UUID) == "running" })

	// The same order again is refused, as Pterodactyl does.
	code, _, field := appErr(t, p.do("POST", "/api/application/servers", order, 422))
	if code != "ValidationException" || field != "external_id" {
		t.Fatalf("duplicate external id: %s %s", code, field)
	}

	// Overdue, then paid.
	p.do("POST", fmt.Sprintf("/api/application/servers/%d/suspend", srv.ID), nil, 204)
	if v := appObj[appServer](t, p.do("GET", "/api/application/servers/external/1001", nil, 200)); !v.Suspended || v.Status == nil || *v.Status != "suspended" {
		t.Fatalf("suspended %+v", v)
	}
	waitFor(t, "stopped", func() bool { return statusOf(s, srv.UUID) == "offline" })
	p.do("POST", fmt.Sprintf("/api/application/servers/%d/unsuspend", srv.ID), nil, 204)

	// Change password: read the user, send everything back with the password.
	full := appObj[map[string]any](t, p.do("GET", fmt.Sprintf("/api/application/users/%d", user.ID), nil, 200))
	upd := map[string]any{"username": full["username"], "email": full["email"], "first_name": full["first_name"], "last_name": full["last_name"], "password": "short"}
	if _, _, field := appErr(t, p.do("PATCH", fmt.Sprintf("/api/application/users/%d", user.ID), upd, 422)); field != "password" {
		t.Fatalf("weak password field %q", field)
	}
	upd["password"] = "a-much-longer-customer-password"
	p.do("PATCH", fmt.Sprintf("/api/application/users/%d", user.ID), upd, 200)
	jar, _ := cookiejar.New(nil)
	cust := &client{t, s.admin.base, &http.Client{Jar: jar}}
	cust.do("POST", "/api/auth/login", map[string]string{"email": "buyer@example.com", "password": "a-much-longer-customer-password"}, 200)

	// Change package: build, then startup, each from what the server has.
	// The node may still be finishing the suspension's stop, and says so
	// with a 409, as it does to GamePanel's own API: a module tries again.
	cur := appObj[appServer](t, p.do("GET", "/api/application/servers/external/1001", nil, 200))
	var builtRaw []byte
	waitFor(t, "the build change", func() bool {
		var code int
		code, builtRaw = p.try("PATCH", fmt.Sprintf("/api/application/servers/%d/build", cur.ID), map[string]any{
			"allocation": cur.Allocation, "memory": 1024, "swap": 0, "io": 500, "cpu": 200, "disk": 2000, "oom_disabled": false,
			"feature_limits": map[string]any{"databases": 1, "allocations": 3, "backups": 2},
		})
		if code != 200 && code != 409 {
			t.Fatalf("build: %d %s", code, builtRaw)
		}
		return code == 200
	})
	built := appObj[appServer](t, builtRaw)
	if built.Limits.Memory != 1024 || built.Limits.CPU != 200 || built.Limits.Disk != 2000 || built.FeatureLimits.Databases != 1 || built.FeatureLimits.Allocations != 3 || built.FeatureLimits.Backups != 2 {
		t.Fatalf("build %+v", built)
	}
	env := cur.Container.Environment
	env["GREETING"] = "bye"
	started := appObj[appServer](t, p.do("PATCH", fmt.Sprintf("/api/application/servers/%d/startup", cur.ID), map[string]any{
		"environment": env, "startup": cur.Container.StartupCommand, "egg": eggID, "image": cur.Container.Image, "skip_scripts": false,
	}, 200))
	if started.Container.Environment["GREETING"] != "bye" {
		t.Fatalf("startup %+v", started.Container)
	}
	// Another egg is refused rather than half-done.
	if _, _, field := appErr(t, p.do("PATCH", fmt.Sprintf("/api/application/servers/%d/startup", cur.ID), map[string]any{"egg": eggID + 100}, 422)); field != "egg" {
		t.Fatalf("egg change field %q", field)
	}

	// Terminate.
	p.do("DELETE", fmt.Sprintf("/api/application/servers/%d", cur.ID), nil, 204)
	p.do("GET", "/api/application/servers/external/1001", nil, 404)
	p.do("GET", fmt.Sprintf("/api/application/servers/%d", cur.ID), nil, 404)
}

// try sends a request and returns the status and body, whatever they are.
func (p ptero) try(method, path string, body any) (int, []byte) {
	p.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, p.base+path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// statusOf is a server's state as GamePanel's own API reports it.
func statusOf(s *stack, uuid string) string {
	var v struct{ State string }
	json.Unmarshal(s.admin.do("GET", "/api/servers/"+uuid, nil, 200), &v)
	return v.State
}

func TestApplicationAPIPaymenterAndBlesta(t *testing.T) {
	t.Parallel()
	s, p, loc, eggID := appStack(t)

	// Product set-up lists.
	if l := appList[struct{ Short string }](t, p.do("GET", "/api/application/locations", nil, 200)); len(l.Data) != 1 || l.Data[0].Attributes.Short != "eu" {
		t.Fatalf("locations %+v", l)
	}
	if l := appList[struct{ ID int64 }](t, p.do("GET", "/api/application/nests", nil, 200)); len(l.Data) != 1 || l.Data[0].Attributes.ID != 1 {
		t.Fatalf("nests %+v", l)
	}
	if l := appList[struct{ ID int64 }](t, p.do("GET", "/api/application/nests/1/eggs", nil, 200)); len(l.Data) != 1 || l.Data[0].Attributes.ID != eggID {
		t.Fatalf("eggs %+v", l)
	}

	s.admin.do("PUT", fmt.Sprintf("/api/nodes/%d/capacity", s.node), map[string]any{"memory_mb": 4096, "disk_mb": 0, "overallocate_pct": 0}, 204)

	// Paymenter picks a node and a free allocation itself.
	type depNode struct {
		ID            int64
		Relationships struct {
			Allocations struct {
				Data []struct{ Attributes appAlloc }
			}
		}
	}
	dep := appList[depNode](t, p.do("GET", fmt.Sprintf("/api/application/nodes/deployable?memory=512&disk=0&location_ids%%5B0%%5D=%d&include%%5B0%%5D=allocations", loc), nil, 200))
	if len(dep.Data) != 1 || len(dep.Data[0].Attributes.Relationships.Allocations.Data) != 6 {
		t.Fatalf("deployable %+v", dep)
	}
	var free, next appAlloc
	for _, a := range dep.Data[0].Attributes.Relationships.Allocations.Data {
		switch {
		case a.Attributes.Port == 25565 && !a.Attributes.Assigned:
			t.Fatal("the stack's server's port is not marked assigned")
		case !a.Attributes.Assigned && free.ID == 0:
			free = a.Attributes
		case !a.Attributes.Assigned && next.ID == 0:
			next = a.Attributes
		}
	}
	if free.Port != 25566 || next.Port != 25567 {
		t.Fatalf("free allocations %+v %+v", free, next)
	}
	if l := appList[depNode](t, p.do("GET", "/api/application/nodes/deployable?memory=999999999&disk=0", nil, 200)); len(l.Data) != 0 {
		t.Fatalf("a node without room is deployable: %+v", l)
	}

	// No external id, a user found by email; numbers sent as strings.
	user := appObj[struct{ ID int64 }](t, p.do("POST", "/api/application/users", map[string]any{
		"email": "pay@example.com", "username": "pay_x1y2", "first_name": "", "last_name": "",
	}, 201))
	if l := appList[struct{ ID int64 }](t, p.do("GET", "/api/application/users?filter[email]=pay@example.com", nil, 200)); len(l.Data) != 1 || l.Data[0].Attributes.ID != user.ID {
		t.Fatalf("by email %+v", l)
	}
	body := fmt.Sprintf(`{"external_id":"55","name":"Pay #55","user":%d,"egg":"%d","docker_image":"echo:latest","startup":"run {{GREETING}}",
		"environment":{"GREETING":42},"skip_scripts":false,"oom_disabled":true,
		"limits":{"memory":"512","swap":"0","disk":"0","io":"500","cpu":"0","threads":null},
		"feature_limits":{"databases":"0","allocations":"2","backups":"1"},"start_on_completion":false,
		"allocation":{"default":%d,"additional":[%d]}}`, user.ID, eggID, free.ID, next.ID)
	srv := appObj[appServer](t, p.do("POST", "/api/application/servers", body, 201))
	if srv.Allocation != free.ID || srv.Container.Environment["GREETING"] != "42" || srv.FeatureLimits.Allocations != 2 {
		t.Fatalf("server %+v", srv)
	}
	ports := appObj[appServer](t, p.do("GET", fmt.Sprintf("/api/application/servers/%d?include=allocations", srv.ID), nil, 200)).Relationships.Allocations.Data
	if len(ports) != 2 || ports[1].Attributes.Port != next.Port {
		t.Fatalf("ports %+v", ports)
	}

	// Upgrade with pinning, then a build change that leaves threads out keeps it.
	b1 := appObj[appServer](t, p.do("PATCH", fmt.Sprintf("/api/application/servers/%d/build", srv.ID), map[string]any{
		"allocation": srv.Allocation, "memory": 768, "swap": 0, "disk": 0, "io": 500, "cpu": 0, "threads": "0-1",
		"feature_limits": map[string]any{"databases": "0", "allocations": "2", "backups": "1"},
	}, 200))
	b2 := appObj[appServer](t, p.do("PATCH", fmt.Sprintf("/api/application/servers/%d/build", srv.ID), map[string]any{
		"allocation": srv.Allocation, "memory": 768, "swap": 0, "io": 500, "cpu": 0, "disk": 0,
		"feature_limits": map[string]any{"databases": 0, "allocations": 2, "backups": 1},
	}, 200))
	if b1.Limits.Threads == nil || *b1.Limits.Threads != "0-1" || b2.Limits.Threads == nil || *b2.Limits.Threads != "0-1" {
		t.Fatalf("threads %+v / %+v", b1.Limits, b2.Limits)
	}
	// Ports: make the second one primary, then drop the first.
	moved := appObj[appServer](t, p.do("PATCH", fmt.Sprintf("/api/application/servers/%d/build", srv.ID), map[string]any{
		"allocation": next.ID, "remove_allocations": []int64{free.ID},
	}, 200))
	if moved.Allocation != next.ID {
		t.Fatalf("primary %+v", moved)
	}
	if _, _, field := appErr(t, p.do("PATCH", fmt.Sprintf("/api/application/servers/%d/build", srv.ID), map[string]any{"remove_allocations": []int64{next.ID}}, 422)); field != "remove_allocations" {
		t.Fatalf("removing the primary: %q", field)
	}

	// Blesta: an account with a generated password shorter than the policy
	// allows is made anyway, without it; servers looked up by id; details.
	bl := appObj[struct{ ID int64 }](t, p.do("POST", "/api/application/users", map[string]any{
		"username": "bl_9", "password": "ab3!xyz9q1", "email": "blesta@example.com", "first_name": "B", "last_name": "L", "external_id": "bl-9",
	}, 201))
	jar, _ := cookiejar.New(nil)
	(&client{t, s.admin.base, &http.Client{Jar: jar}}).do("POST", "/api/auth/login", map[string]string{"email": "blesta@example.com", "password": "ab3!xyz9q1"}, 401)
	det := appObj[appServer](t, p.do("PATCH", fmt.Sprintf("/api/application/servers/%d/details", srv.ID), map[string]any{
		"external_id": "9-55", "name": "Renamed", "description": "from Blesta", "user": bl.ID,
	}, 200))
	if det.Name != "Renamed" || det.Description != "from Blesta" || det.User != bl.ID || det.ExternalID == nil || *det.ExternalID != "9-55" {
		t.Fatalf("details %+v", det)
	}
	if v := appObj[appServer](t, p.do("GET", "/api/application/servers/external/9-55", nil, 200)); v.ID != srv.ID {
		t.Fatalf("by new external id %+v", v)
	}
	p.do("POST", fmt.Sprintf("/api/application/servers/%d/suspend", srv.ID), nil, 204)
	p.do("DELETE", fmt.Sprintf("/api/application/servers/%d", srv.ID), nil, 204)

	// An account that owns nothing can go.
	p.do("DELETE", fmt.Sprintf("/api/application/users/%d", bl.ID), nil, 204)
}

func TestApplicationAPIRefusals(t *testing.T) {
	t.Parallel()
	s, p, loc, eggID := appStack(t)

	// Only an administrator's API key: no key, a browser session, a customer's key.
	resp, err := http.Get(s.admin.base + "/api/application/users")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without a key: %d", resp.StatusCode)
	}
	if code, status, _ := appErr(t, s.admin.do("GET", "/api/application/users", nil, 403)); code != "AccessDeniedHttpException" || status != "403" {
		t.Fatalf("session: %s %s", code, status)
	}
	s.admin.do("POST", "/api/users", map[string]any{"email": "cust@example.com", "password": "a-long-customer-password"}, 201)
	jar, _ := cookiejar.New(nil)
	cust := &client{t, s.admin.base, &http.Client{Jar: jar}}
	cust.do("POST", "/api/auth/login", map[string]string{"email": "cust@example.com", "password": "a-long-customer-password"}, 200)
	cp := ptero{t, s.admin.base, s.apiKey(cust, "a-long-customer-password")}
	cp.do("GET", "/api/application/users", nil, 403)
	cp.do("POST", "/api/application/servers", map[string]any{}, 403)

	// Validation errors name the field.
	for _, c := range []struct {
		body  any
		field string
	}{
		{map[string]any{"name": "x", "user": "abc"}, "user"},
		{map[string]any{"name": "x", "user": 1, "egg": 999, "limits": map[string]any{"memory": 512}, "deploy": map[string]any{}}, "egg"},
		{map[string]any{"name": "x", "user": 1, "egg": eggID, "limits": map[string]any{"memory": 0}, "deploy": map[string]any{}}, "limits.memory"},
		{map[string]any{"name": "x", "user": 1, "egg": eggID, "limits": map[string]any{"memory": 512}}, "deploy"},
		{map[string]any{"name": "x", "user": 1, "egg": eggID, "limits": map[string]any{"memory": 512}, "deploy": map[string]any{"port_range": []string{"9-1"}}}, "deploy.port_range"},
		{map[string]any{"name": "", "user": 1}, "name"},
	} {
		if code, _, field := appErr(t, p.do("POST", "/api/application/servers", c.body, 422)); code != "ValidationException" || field != c.field {
			t.Errorf("%v: %s %q, want %q", c.body, code, field, c.field)
		}
	}
	// No node can take it: Pterodactyl's 400.
	other := appObj[struct{ ID int64 }](t, p.do("POST", "/api/application/locations", map[string]string{"short": "us"}, 201))
	for _, deploy := range []map[string]any{
		{"locations": []int64{other.ID}},
		{"locations": []int64{loc}, "port_range": []string{"30000-30010"}},
	} {
		code, status, _ := appErr(t, p.do("POST", "/api/application/servers", map[string]any{
			"name": "x", "user": 1, "egg": eggID, "limits": map[string]any{"memory": 512}, "deploy": deploy,
		}, 400))
		if code != "NoViableNodeException" || status != "400" {
			t.Fatalf("%v: %s %s", deploy, code, status)
		}
	}
	// A location with nodes stays; an unknown id is a 404 in Pterodactyl's format.
	p.do("DELETE", fmt.Sprintf("/api/application/locations/%d", loc), nil, 400)
	p.do("DELETE", fmt.Sprintf("/api/application/locations/%d", other.ID), nil, 204)
	if code, _, _ := appErr(t, p.do("GET", "/api/application/servers/999", nil, 404)); code != "NotFoundHttpException" {
		t.Fatalf("code %s", code)
	}
	p.do("GET", "/api/application/servers/abc", nil, 404)
	// Email taken; admin rights cannot be handed out through PATCH.
	if _, _, field := appErr(t, p.do("POST", "/api/application/users", map[string]any{"email": "cust@example.com"}, 422)); field != "email" {
		t.Fatalf("taken email: %q", field)
	}
	if _, _, field := appErr(t, p.do("PATCH", "/api/application/users/1", map[string]any{"root_admin": false}, 422)); field != "root_admin" {
		t.Fatalf("root_admin: %q", field)
	}
	// The admin's own email and password stay out of reach, even with its key.
	p.do("PATCH", "/api/application/users/1", map[string]any{"email": "admin@example.com", "password": "another-long-password"}, 403)
	// Lists page.
	if l := appList[struct{ ID int64 }](t, p.do("GET", "/api/application/users?per_page=1&page=2", nil, 200)); l.Meta.Pagination.Total != 2 || l.Meta.Pagination.TotalPages != 2 || len(l.Data) != 1 || l.Meta.Pagination.CurrentPage != 2 {
		t.Fatalf("paging %+v", l)
	}
	if !strings.Contains(string(p.do("GET", "/api/application/servers", nil, 200)), `"identifier"`) {
		t.Fatal("servers list")
	}
}
