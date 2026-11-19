package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

// An egg with three images, a hidden admin-only variable, a read-only one
// and an editable one with rules.
const startupEgg = `{"name":"Paper","startup":"java -jar {{JAR}}","config":{"stop":"stop"},
 "docker_images":{"Java 21":"yolks:java_21","Java 17":"yolks:java_17","Java 8":"yolks:java_8"},
 "variables":[
  {"name":"Jar","env_variable":"JAR","default_value":"server.jar","user_viewable":true,"user_editable":true,"rules":"required|regex:/^[\\w.-]+\\.jar$/"},
  {"name":"Version","env_variable":"VERSION","default_value":"latest","user_viewable":true,"user_editable":false,"rules":"required|string"},
  {"name":"License","env_variable":"LICENSE","default_value":"secret-key","user_viewable":false,"user_editable":false,"rules":"nullable|string"}]}`

type startupResp struct {
	Command, Image string
	CustomImage    bool `json:"custom_image"`
	CanChangeImage bool `json:"can_change_image"`
	CanEditCommand bool `json:"can_edit_command"`
	Pending        bool
	Variables      []struct {
		Env, Value string
		Editable   bool
	}
}

func TestStartupSettings(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	var e struct{ ID int64 }
	json.Unmarshal(s.admin.do("POST", "/api/eggs/import", startupEgg, 201), &e)
	var created struct{ UUID string }
	json.Unmarshal(s.admin.do("POST", "/api/servers", map[string]any{"name": "paper", "node_id": s.node, "egg_id": e.ID, "memory_mb": 256}, 201), &created)
	srv := "/api/servers/" + created.UUID
	spec := func() (image, startup string, env map[string]string) {
		sp, ok := s.fake.SpecOf(created.UUID)
		if !ok {
			t.Fatal("no container")
		}
		return sp.Image, sp.Startup, sp.Env
	}
	get := func(c *client) (r startupResp) {
		json.Unmarshal(c.do("GET", srv+"/startup", nil, 200), &r)
		return r
	}

	// The owner (here: an admin) sees everything.
	r := get(s.admin)
	if len(r.Variables) != 3 || r.Image != "yolks:java_21" || !r.CanEditCommand {
		t.Fatalf("admin view %+v", r)
	}

	// A subuser with startup.read sees only what the egg shows.
	s.st.CreateUser(s.ctx, "bob@example.com", "another-long-password", false)
	jar, _ := cookiejar.New(nil)
	bob := &client{t, s.admin.base, &http.Client{Jar: jar}}
	bob.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "another-long-password"}, 200)
	s.admin.do("POST", srv+"/users", map[string]any{"email": "bob@example.com", "permissions": []string{"startup.read"}}, 201)
	r = get(bob)
	if len(r.Variables) != 2 || r.CanChangeImage || r.CanEditCommand {
		t.Fatalf("subuser view %+v", r)
	}
	for _, v := range r.Variables {
		if v.Env == "LICENSE" || v.Editable {
			t.Fatalf("subuser variable %+v", v)
		}
	}
	if b := string(bob.do("GET", srv, nil, 200)); strings.Contains(b, "secret-key") {
		t.Fatal("the server view leaks a hidden variable")
	}
	// Read-only: no changes.
	bob.do("PUT", srv+"/startup", map[string]any{"env": map[string]string{"JAR": "paper.jar"}}, 403)
	bob.do("PUT", srv+"/startup", map[string]any{"image": "yolks:java_17"}, 403)

	// With startup.update and startup.docker-image: only editable variables,
	// only the egg's images, never the command.
	s.admin.do("PUT", srv+"/users/"+bobID(t, s), map[string]any{"permissions": []string{"startup.read", "startup.update", "startup.docker-image"}}, 204)
	bob.do("PUT", srv+"/startup", map[string]any{"env": map[string]string{"VERSION": "1.20"}}, 403)
	bob.do("PUT", srv+"/startup", map[string]any{"env": map[string]string{"LICENSE": "mine"}}, 422) // hidden: as if absent
	bob.do("PUT", srv+"/startup", map[string]any{"env": map[string]string{"NOPE": "x"}}, 422)
	bob.do("PUT", srv+"/startup", map[string]any{"env": map[string]string{"JAR": "evil.sh"}}, 422) // fails the egg's regex
	bob.do("PUT", srv+"/startup", map[string]any{"image": "alpine:latest"}, 422)
	bob.do("PUT", srv+"/startup", map[string]any{"startup": "sh -c 'curl evil'"}, 403)

	// A stopped server gets a new container at once.
	json.Unmarshal(bob.do("PUT", srv+"/startup", map[string]any{"env": map[string]string{"JAR": "paper.jar"}, "image": "yolks:java_17"}, 200), &r)
	if r.Pending {
		t.Fatal("a stopped server reported a pending change")
	}
	img, _, env := spec()
	if img != "yolks:java_17" || env["JAR"] != "paper.jar" || env["LICENSE"] != "secret-key" {
		t.Fatalf("container spec image=%s env=%v", img, env)
	}

	// A running server keeps its container until the next start.
	s.admin.do("POST", srv+"/power", map[string]string{"action": "start"}, 202)
	waitFor(t, "running", func() bool { return s.hub.State(created.UUID) == "running" })
	json.Unmarshal(s.admin.do("PUT", srv+"/startup", map[string]any{"image": "yolks:java_8", "startup": "java -Xmx1G -jar {{JAR}}"}, 200), &r)
	if !r.Pending {
		t.Fatal("a running server's change was not marked pending")
	}
	if img, _, _ := spec(); img != "yolks:java_17" {
		t.Fatalf("the running container was replaced (image %s)", img)
	}
	waitFor(t, "pending reported by the node", func() bool { return get(s.admin).Pending })
	s.admin.do("POST", srv+"/power", map[string]string{"action": "restart"}, 202)
	waitFor(t, "rebuilt at restart", func() bool {
		img, _, _ := spec()
		return img == "yolks:java_8"
	})
	if _, startup, _ := spec(); startup != "java -Xmx1G -jar ${JAR}" {
		t.Fatalf("startup %q", startup)
	}
	waitFor(t, "pending cleared", func() bool { return !get(s.admin).Pending })

	// An admin's custom image locks the subuser out of the image picker.
	s.admin.do("POST", srv+"/power", map[string]string{"action": "kill"}, 202)
	waitFor(t, "offline", func() bool { return s.hub.State(created.UUID) == "offline" })
	s.admin.do("PUT", srv+"/startup", map[string]any{"image": "registry.example.com/custom:1"}, 200)
	if r = get(bob); !r.CustomImage || r.CanChangeImage {
		t.Fatalf("custom image view %+v", r)
	}
	bob.do("PUT", srv+"/startup", map[string]any{"image": "yolks:java_21"}, 403)
	s.admin.do("PUT", srv+"/startup", map[string]any{"image": "--privileged"}, 422)

	// Values never reach the audit log.
	if b := string(s.admin.do("GET", "/api/audit?q=server.startup", nil, 200)); strings.Contains(b, "paper.jar") || !strings.Contains(b, "JAR") {
		t.Fatalf("audit %s", b)
	}
}

// bobID looks up the id of bob@example.com, the subuser the tests share.
func bobID(t *testing.T, s *stack) string {
	t.Helper()
	const email = "bob@example.com"
	var id int64
	if err := s.st.DB.QueryRowContext(s.ctx, `SELECT id FROM users WHERE email=?`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(id)
	return string(b)
}
