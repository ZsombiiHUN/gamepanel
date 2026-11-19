package api_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// The connect details: who may see them, and what an admin's node settings do.
func TestServerSFTPInfo(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "")
	ctx := context.Background()
	admin, _ := e.st.UserByEmail(ctx, "admin@example.com")
	n, _, _ := e.st.CreateNode(ctx, "n", 25565, 25600)
	egg, _ := e.st.CreateEgg(ctx, store.Egg{Name: "e", DockerImage: "i", Startup: "x", Stop: "^C"})
	const id = "abcdef12-1111-4111-8111-111111111111"
	if _, err := e.st.CreateServer(ctx, store.Server{UUID: id, Name: "s", OwnerID: admin.ID, NodeID: n.ID, EggID: egg.ID, Limits: store.Limits{MemoryMB: 128}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AddSubuser(ctx, id, "bob@example.com", []string{"console.read"}); err != nil {
		t.Fatal(err)
	}
	a, b := e.client(), e.client()
	if a.login("admin@example.com", "correct-horse-battery") != 200 || b.login("bob@example.com", "another-long-password") != 200 {
		t.Fatal("login failed")
	}
	type info struct {
		Host, Username, Status string
		Port                   int
		Read, Write            bool
	}
	get := func(c *cl) (int, info) {
		code, body := c.do("GET", "/api/servers/"+id+"/sftp", nil, nil)
		var i info
		json.Unmarshal(body, &i)
		return code, i
	}

	if code, _ := get(b); code != 403 {
		t.Fatalf("a subuser without file access got %d, want 403", code)
	}
	code, i := get(a)
	if code != 200 || i.Username != "admin@example.com.abcdef12" || i.Status != "offline" || i.Host != "" || i.Port != 0 || !i.Read || !i.Write {
		t.Fatalf("owner: %d %+v", code, i)
	}

	nodeSFTP := "/api/nodes/" + strconv.FormatInt(n.ID, 10) + "/sftp"
	if code, _ := b.do("PUT", nodeSFTP, map[string]any{"host": "evil.example", "port": 22}, nil); code != 403 {
		t.Fatalf("a user set a node's SFTP address: %d", code)
	}
	for _, bad := range []string{"sftp://x.example", "x.example:22", "a b", "-x.example", "x..example", "x/y"} {
		if code, _ := a.do("PUT", nodeSFTP, map[string]any{"host": bad, "port": 0}, nil); code != 422 {
			t.Errorf("host %q: got %d, want 422", bad, code)
		}
	}
	if code, _ := a.do("PUT", "/api/nodes/999/sftp", map[string]any{"host": "", "port": 0}, nil); code != 404 {
		t.Errorf("unknown node: got %d, want 404", code)
	}
	for _, good := range []string{"node1.example.com", "203.0.113.7", "2001:db8::1", "[2001:db8::1]", "localhost"} {
		if code, body := a.do("PUT", nodeSFTP, map[string]any{"host": good, "port": 2222}, nil); code != 204 {
			t.Errorf("host %q: got %d %s", good, code, body)
		}
	}
	a.do("PUT", nodeSFTP, map[string]any{"host": " sftp.example.com ", "port": 2222}, nil)
	if _, i := get(a); i.Host != "sftp.example.com" || i.Port != 2222 {
		t.Fatalf("after setting the address: %+v", i)
	}

	// File access is enough, and the flags say which kind.
	if err := e.st.SetSubuserPermissions(ctx, id, mustID(t, e, "bob@example.com"), []string{"files.read"}); err != nil {
		t.Fatal(err)
	}
	if code, i := get(b); code != 200 || i.Username != "bob@example.com.abcdef12" || !i.Read || i.Write {
		t.Fatalf("read-only subuser: %d %+v", code, i)
	}
}

func mustID(t *testing.T, e *env, email string) int64 {
	t.Helper()
	u, err := e.st.UserByEmail(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}
