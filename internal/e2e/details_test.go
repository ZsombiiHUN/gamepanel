package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

func TestRenameAndOwner(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	srv := "/api/servers/" + s.server
	login := func(email string) *client {
		s.st.CreateUser(s.ctx, email, "another-long-password", false)
		jar, _ := cookiejar.New(nil)
		c := &client{t, s.admin.base, &http.Client{Jar: jar}}
		c.do("POST", "/api/auth/login", map[string]string{"email": email, "password": "another-long-password"}, 200)
		return c
	}
	bob, carol := login("bob@example.com"), login("carol@example.com")

	// Renaming needs settings.rename.
	s.admin.do("POST", srv+"/users", map[string]any{"email": "bob@example.com", "permissions": []string{"console.read"}}, 201)
	bob.do("PUT", srv+"/details", map[string]any{"name": "Mine now"}, 403)
	s.admin.do("PUT", srv+"/users/"+userID(t, s, "bob@example.com"), map[string]any{"permissions": []string{"console.read", "settings.rename"}}, 204)
	var v struct {
		Name, Description string
		OwnerEmail        string `json:"owner_email"`
	}
	json.Unmarshal(bob.do("PUT", srv+"/details", map[string]any{"name": "  Survival  ", "description": "Season 3"}, 200), &v)
	if v.Name != "Survival" || v.Description != "Season 3" {
		t.Fatalf("renamed %+v", v)
	}
	bob.do("PUT", srv+"/details", map[string]any{"name": "two\nlines"}, 422)
	bob.do("PUT", srv+"/details", map[string]any{"name": ""}, 422)

	// Only admins hand a server over; the new owner stops being a subuser.
	bob.do("PUT", srv+"/owner", map[string]any{"email": "bob@example.com"}, 403)
	s.admin.do("PUT", srv+"/owner", map[string]any{"email": "nobody@example.com"}, 422)
	s.admin.do("PUT", srv+"/owner", map[string]any{"email": "bob@example.com"}, 200)
	json.Unmarshal(s.admin.do("GET", srv, nil, 200), &v)
	if v.OwnerEmail != "bob@example.com" {
		t.Fatalf("owner %q", v.OwnerEmail)
	}
	var subs []struct{ Email string }
	json.Unmarshal(s.admin.do("GET", srv+"/users", nil, 200), &subs)
	if len(subs) != 0 {
		t.Fatalf("new owner still listed as a subuser: %+v", subs)
	}
	var mine struct{ Owner bool }
	json.Unmarshal(bob.do("GET", srv, nil, 200), &mine)
	if !mine.Owner {
		t.Fatal("new owner is not the owner")
	}
	// The owner email is not shown to non-admins.
	if b := string(bob.do("GET", srv, nil, 200)); strings.Contains(b, "owner_email") {
		t.Fatal("owner email shown to a non-admin")
	}
	// Strangers still see nothing.
	carol.do("GET", srv, nil, 404)
	carol.do("PUT", srv+"/details", map[string]any{"name": "x"}, 404)
}

func TestServerActivity(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	srv := "/api/servers/" + s.server
	s.write("/a.txt", "x")
	s.admin.do("POST", srv+"/files/folder", map[string]string{"path": "/dir"}, 204)
	s.st.CreateUser(s.ctx, "bob@example.com", "another-long-password", false)
	jar, _ := cookiejar.New(nil)
	bob := &client{t, s.admin.base, &http.Client{Jar: jar}}
	bob.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "another-long-password"}, 200)
	s.admin.do("POST", srv+"/users", map[string]any{"email": "bob@example.com", "permissions": []string{"console.read"}}, 201)
	bob.do("GET", srv+"/activity", nil, 403)
	s.admin.do("PUT", srv+"/users/"+userID(t, s, "bob@example.com"), map[string]any{"permissions": []string{"activity.read"}}, 204)

	var es []struct{ Action, IP, ActorEmail string }
	json.Unmarshal(bob.do("GET", srv+"/activity", nil, 200), &es)
	actions := map[string]bool{}
	for _, e := range es {
		actions[e.Action] = true
		if e.IP != "" {
			t.Fatal("a subuser sees addresses")
		}
	}
	if !actions["file.write"] || !actions["file.mkdir"] || !actions["subuser.add"] {
		t.Fatalf("activity %v", actions)
	}
	if actions["login.ok"] {
		t.Fatal("another account's sign-in shows in a server's activity")
	}
	json.Unmarshal(s.admin.do("GET", srv+"/activity?q=mkdir", nil, 200), &es)
	if len(es) != 1 || es[0].IP == "" {
		t.Fatalf("owner search %+v", es)
	}
}
