package e2e

// Regression tests for access that must end, or be refused, after it was
// first granted: open consoles, open SFTP sessions, schedules, other nodes.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// login signs a new user in and returns their client and id.
func (s *stack) login(email string) (*client, int64) {
	s.t.Helper()
	u, err := s.st.CreateUser(s.ctx, email, "another-long-password", false)
	if err != nil {
		s.t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	c := &client{s.t, s.admin.base, &http.Client{Jar: jar}}
	c.do("POST", "/api/auth/login", map[string]string{"email": email, "password": "another-long-password"}, 200)
	return c, u.ID
}

// console opens the console websocket with c's cookies (none if c is nil).
func (s *stack) console(c *client) *websocket.Conn {
	s.t.Helper()
	h := http.Header{}
	if c != nil {
		u, _ := url.Parse(c.base)
		for _, k := range c.c.Jar.Cookies(u) {
			h.Add("Cookie", k.Name+"="+k.Value)
		}
	}
	ws, _, err := websocket.Dial(s.ctx, "ws"+strings.TrimPrefix(s.admin.base, "http")+"/api/servers/"+s.server+"/ws", &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(func() { ws.CloseNow() })
	return ws
}

// closedWith reads until the socket closes and returns its close code.
func closedWith(t *testing.T, ws *websocket.Conn) websocket.StatusCode {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if _, _, err := ws.Read(ctx); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("the console stayed open")
			}
			return websocket.CloseStatus(err)
		}
	}
}

func TestConsoleClosesWhenAccessEnds(t *testing.T) {
	s := newStack(t)
	srv := "/api/servers/" + s.server

	// Refused upgrades close with a code the browser can read (it cannot see
	// an HTTP status), so the UI knows to stop reconnecting.
	if code := closedWith(t, s.console(nil)); code != 4401 {
		t.Fatalf("no session: close code %d, want 4401", code)
	}
	reader, readerID := s.login("reader@example.com")
	s.admin.do("POST", srv+"/users", map[string]any{"email": "reader@example.com", "permissions": []string{"files.read"}}, 201)
	if code := closedWith(t, s.console(reader)); code != 4403 {
		t.Fatalf("no console permission: close code %d, want 4403", code)
	}

	// A subuser removed while watching is cut off.
	s.admin.do("PUT", srv+"/users/"+strconv.FormatInt(readerID, 10), map[string]any{"permissions": []string{"console.read"}}, 204)
	ws := s.console(reader)
	s.admin.do("DELETE", srv+"/users/"+strconv.FormatInt(readerID, 10), nil, 204)
	if code := closedWith(t, ws); code != 4404 {
		t.Fatalf("removed subuser: close code %d, want 4404", code)
	}

	// Signing out closes the consoles that session had open.
	other, _ := s.login("other@example.com")
	s.admin.do("POST", srv+"/users", map[string]any{"email": "other@example.com", "permissions": []string{"console.read"}}, 201)
	ws = s.console(other)
	other.do("POST", "/api/auth/logout", nil, 204)
	if code := closedWith(t, ws); code != 4401 {
		t.Fatalf("signed out: close code %d, want 4401", code)
	}
}

func TestSFTPSessionEndsWhenAccessEnds(t *testing.T) {
	s := newStack(t)
	var k struct {
		Key   struct{ ID int64 }
		Token string
	}
	json.Unmarshal(s.admin.do("POST", "/api/auth/keys", map[string]any{"name": "sftp", "password": "correct-horse-battery"}, 201), &k)
	c, _, err := s.dial("admin@example.com."+s.server[:8], k.Token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadDir("/"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond) // several rechecks
	var logins []store.AuditEntry
	json.Unmarshal(s.admin.do("GET", "/api/audit?q=sftp.login", nil, 200), &logins)
	if len(logins) != 1 {
		t.Fatalf("rechecks must not be audited as sign-ins: %d sftp.login entries", len(logins))
	}

	s.admin.do("DELETE", "/api/auth/keys/"+strconv.FormatInt(k.Key.ID, 10), nil, 204)
	waitFor(t, "the session to close after the key was revoked", func() bool {
		_, err := c.ReadDir("/")
		return err != nil
	})

	// A subuser who loses write access is disconnected (and reconnects read-only).
	srv := "/api/servers/" + s.server
	bob, bobID := s.login("bob@example.com")
	s.admin.do("POST", srv+"/users", map[string]any{"email": "bob@example.com", "permissions": []string{"files.read", "files.write"}}, 201)
	c, _, err = s.dial("bob@example.com."+s.server[:8], s.apiKey(bob, "another-long-password"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadDir("/"); err != nil {
		t.Fatal(err)
	}
	s.admin.do("PUT", srv+"/users/"+strconv.FormatInt(bobID, 10), map[string]any{"permissions": []string{"files.read"}}, 204)
	waitFor(t, "the session to close after write access was removed", func() bool {
		_, err := c.ReadDir("/")
		return err != nil
	})
}

// A node may only report on its own servers.
func TestANodeCannotSpeakForAnotherNodesServers(t *testing.T) {
	s := newStack(t)
	var n2 struct {
		Token string             `json:"token"`
		Node  struct{ ID int64 } `json:"node"`
	}
	json.Unmarshal(s.admin.do("POST", "/api/nodes", map[string]any{"name": "n2", "port_start": 26000, "port_end": 26010}, 201), &n2)
	ws, _, err := websocket.Dial(s.ctx, "ws"+strings.TrimPrefix(s.admin.base, "http")+"/daemon/connect",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + n2.Token}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	go func() { // a daemon reads everything the panel sends
		for {
			if _, _, err := ws.Read(s.ctx); err != nil {
				return
			}
		}
	}()
	waitFor(t, "node 2 online", func() bool { return s.hub.Online(n2.Node.ID) })
	send := func(typ string, v any) {
		b, _ := json.Marshal(proto.Envelope{Type: typ, Data: proto.Marshal(v)})
		if err := ws.Write(s.ctx, websocket.MessageText, b); err != nil {
			t.Fatal(err)
		}
	}
	before := s.hub.State(s.server)
	send(proto.EvtConsole, proto.Console{UUID: s.server, Line: "injected by node 2"})
	send(proto.EvtState, proto.State{UUID: s.server, State: "spoofed"})
	time.Sleep(300 * time.Millisecond)
	if st := s.hub.State(s.server); st != before {
		t.Fatalf("node 2 changed node 1's server state to %q", st)
	}
	if _, backlog := s.hub.Subscribe(s.server); strings.Contains(strings.Join(backlog, "\n"), "injected") {
		t.Fatal("node 2 wrote into node 1's server console")
	}

	// Deleting a node: refused while it has servers, and the connection of a
	// deleted node is dropped.
	if got := string(s.admin.do("DELETE", "/api/nodes/"+strconv.FormatInt(s.node, 10), nil, 409)); !strings.Contains(got, "servers") {
		t.Fatalf("delete of a node with servers: %s", got)
	}
	s.admin.do("DELETE", "/api/nodes/999", nil, 404)
	s.admin.do("DELETE", "/api/nodes/"+strconv.FormatInt(n2.Node.ID, 10), nil, 204)
	waitFor(t, "the deleted node to be disconnected", func() bool { return !s.hub.Online(n2.Node.ID) })
}

// A schedule runs with its author's permissions as they are now, not as they
// were when it was saved.
func TestScheduleStopsWhenItsAuthorLosesThePermission(t *testing.T) {
	s := newStack(t)
	srv := "/api/servers/" + s.server
	bob, bobID := s.login("bob@example.com")
	s.admin.do("POST", srv+"/users", map[string]any{"email": "bob@example.com", "permissions": []string{"power", "schedules.manage"}}, 201)

	// bob's server list carries his permissions (one query for the whole list)
	var list []struct {
		UUID        string
		Permissions []string
	}
	json.Unmarshal(bob.do("GET", "/api/servers", nil, 200), &list)
	if len(list) != 1 || strings.Join(list[0].Permissions, ",") != "power,schedules.manage" {
		t.Fatalf("bob's list: %+v", list)
	}

	var sch store.Schedule
	json.Unmarshal(bob.do("POST", srv+"/schedules", map[string]any{
		"name": "boot", "cron": "0 0 1 1 *", "enabled": false,
		"tasks": []map[string]any{{"action": "power", "payload": "start"}},
	}, 201), &sch)
	if sch.CreatedBy != bobID {
		t.Fatalf("schedule author: %d, want %d", sch.CreatedBy, bobID)
	}
	status := func() string {
		var v store.Schedule
		for _, x := range func() []store.Schedule {
			var l []store.Schedule
			json.Unmarshal(s.admin.do("GET", srv+"/schedules", nil, 200), &l)
			return l
		}() {
			if x.ID == sch.ID {
				v = x
			}
		}
		return v.LastStatus
	}
	run := srv + "/schedules/" + strconv.FormatInt(sch.ID, 10) + "/run"

	s.admin.do("PUT", srv+"/users/"+strconv.FormatInt(bobID, 10), map[string]any{"permissions": []string{"schedules.manage"}}, 204)
	s.admin.do("POST", run, nil, 202)
	waitFor(t, "the run to be skipped", func() bool { return strings.Contains(status(), "no longer has the power permission") })
	if st := s.state(); st == "running" {
		t.Fatal("the schedule started the server for an author without the power permission")
	}

	s.admin.do("DELETE", srv+"/users/"+strconv.FormatInt(bobID, 10), nil, 204)
	s.admin.do("POST", run, nil, 202)
	waitFor(t, "the run to be skipped", func() bool { return strings.Contains(status(), "no longer has access") })

	// Saved by someone who may: it runs.
	s.admin.do("PUT", srv+"/schedules/"+strconv.FormatInt(sch.ID, 10), map[string]any{
		"name": "boot", "cron": "0 0 1 1 *", "enabled": false,
		"tasks": []map[string]any{{"action": "power", "payload": "start"}},
	}, 200)
	s.admin.do("POST", run, nil, 202)
	waitFor(t, "the server to start", func() bool { return s.state() == "running" })
}

func TestServerActionsAreAudited(t *testing.T) {
	s := newStack(t)
	srv := "/api/servers/" + s.server
	s.admin.do("POST", srv+"/power", map[string]string{"action": "start"}, 202)
	waitFor(t, "running", func() bool { return s.state() == "running" })
	s.admin.do("POST", srv+"/command", map[string]string{"command": "say hi"}, 204)
	s.write("notes.txt", "x")
	s.admin.do("DELETE", srv+"/files?path=notes.txt", nil, 204)
	b := s.makeBackup(map[string]any{"name": "b"})
	s.admin.do("PUT", srv+"/backups/"+b.UUID+"/lock", map[string]bool{"locked": true}, 204)
	s.admin.do("PUT", srv+"/backups/"+b.UUID+"/lock", map[string]bool{"locked": false}, 204)

	for action, detail := range map[string]string{
		"server.power": "start", "server.command": "say hi", "file.write": "notes.txt",
		"file.delete": "notes.txt", "backup.lock": b.UUID, "backup.unlock": b.UUID,
	} {
		var es []store.AuditEntry
		json.Unmarshal(s.admin.do("GET", "/api/audit?q="+url.QueryEscape(action), nil, 200), &es)
		found := false
		for _, e := range es {
			if e.Action == action && e.Target == s.server && strings.Contains(e.Detail, detail) && e.ActorEmail == "admin@example.com" {
				found = true
			}
		}
		if !found {
			t.Errorf("no %s audit entry with %q: %+v", action, detail, es)
		}
	}
}
