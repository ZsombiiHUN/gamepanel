package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCrashedServersRestart(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	srv := "/api/servers/" + s.server
	power := func(a string) { s.admin.do("POST", srv+"/power", map[string]string{"action": a}, 202) }
	command := func(c string) { s.admin.do("POST", srv+"/command", map[string]string{"command": c}, 204) }
	is := func(want string) func() bool { return func() bool { return s.state() == want } }
	crashes := func() (out []string) {
		var es []struct{ Action, Detail, ActorEmail string }
		json.Unmarshal(s.admin.do("GET", srv+"/activity", nil, 200), &es)
		for _, e := range es {
			if e.Action == "server.crash" {
				out = append(out, e.Detail)
			}
		}
		return out
	}

	var st struct {
		CrashRestart bool `json:"crash_restart"`
	}
	json.Unmarshal(s.admin.do("GET", srv+"/startup", nil, 200), &st)
	if !st.CrashRestart {
		t.Fatal("a new server should restart after a crash")
	}

	// Stops that were asked for, and a clean exit from inside the game, are no crash.
	power("start")
	waitFor(t, "running", is("running"))
	power("stop")
	waitFor(t, "stopped", is("offline"))
	power("start")
	waitFor(t, "running", is("running"))
	power("kill")
	waitFor(t, "killed", is("offline"))
	power("start")
	waitFor(t, "running", is("running"))
	command("stop") // the fake game exits 0
	waitFor(t, "clean exit", is("offline"))
	if c := crashes(); len(c) != 0 {
		t.Fatalf("stops were taken for crashes: %v", c)
	}

	// A crash restarts the server and is recorded.
	power("start")
	waitFor(t, "running", is("running"))
	command("crash")
	waitFor(t, "crash recorded", func() bool { return len(crashes()) == 1 })
	if c := crashes()[0]; !strings.Contains(c, "exit code 1") || !strings.Contains(c, "restarted") {
		t.Fatalf("crash entry %q", c)
	}
	waitFor(t, "restarted", is("running"))

	// Crashing again within a minute leaves it stopped, shown as crashed.
	command("crash")
	waitFor(t, "crashed", is("crashed"))
	if c := crashes(); len(c) != 2 || !strings.Contains(c[0], "not restarted") {
		t.Fatalf("crash entries %v", c)
	}
	power("start")
	waitFor(t, "started by hand", is("running"))

	// Owners can turn restarts off; it applies at once without a rebuild.
	var after struct {
		CrashRestart bool `json:"crash_restart"`
		Pending      bool
	}
	json.Unmarshal(s.admin.do("PUT", srv+"/startup", map[string]any{"crash_restart": false}, 200), &after)
	if after.CrashRestart || after.Pending {
		t.Fatalf("after turning restarts off: %+v", after)
	}
	command("crash")
	waitFor(t, "left crashed", is("crashed"))
	if c := crashes(); len(c) != 3 || !strings.Contains(c[0], "automatic restart is off") {
		t.Fatalf("crash entries %v", c)
	}
}
