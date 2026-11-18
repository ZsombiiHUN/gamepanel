package e2e

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/mail"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

type outbox struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (o *outbox) Send(_ context.Context, m mail.Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, m)
	return nil
}

func (o *outbox) last(t *testing.T, to string) mail.Message {
	t.Helper()
	var m mail.Message
	waitFor(t, "an email to "+to, func() bool {
		o.mu.Lock()
		defer o.mu.Unlock()
		for i := len(o.sent) - 1; i >= 0; i-- {
			if o.sent[i].To == to {
				m = o.sent[i]
				return true
			}
		}
		return false
	})
	return m
}

func (o *outbox) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.sent)
}

var tokenRe = regexp.MustCompile(`#token=([A-Za-z0-9_-]+)`)

func linkPanel(t *testing.T, deps api.Deps) (*client, *store.Store, string) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, _ := store.Open(filepath.Join(t.TempDir(), "p.db"))
	t.Cleanup(func() { st.Close() })
	st.CreateUser(context.Background(), "admin@example.com", "correct-horse-battery", true)
	deps.Store, deps.Hub, deps.Log = st, hub.New(log), log
	ts := httptest.NewServer(api.New(deps))
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	admin := &client{t, ts.URL, &http.Client{Jar: jar}}
	admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": "correct-horse-battery"}, 200)
	return admin, st, ts.URL
}

func anon(t *testing.T, base string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t, base, &http.Client{Jar: jar}}
}

func TestPasswordReset(t *testing.T) {
	t.Parallel()
	box := &outbox{}
	admin, st, base := linkPanel(t, api.Deps{Mail: box, PublicURL: "https://panel.example.com"})
	st.CreateUser(context.Background(), "bob@example.com", "another-long-password", false)
	bob := anon(t, base)
	bob.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "another-long-password"}, 200)

	var opts struct {
		PasswordReset bool `json:"password_reset"`
		Signup        string
	}
	json.Unmarshal(anon(t, base).do("GET", "/api/auth/options", nil, 200), &opts)
	if !opts.PasswordReset || opts.Signup != "invite" {
		t.Fatalf("options %+v", opts)
	}

	// Unknown and known emails get the same answer; only the real one gets mail.
	anon(t, base).do("POST", "/api/auth/forgot", map[string]string{"email": "nobody@example.com"}, 202)
	anon(t, base).do("POST", "/api/auth/forgot", map[string]string{"email": "Bob@Example.com"}, 202)
	m := box.last(t, "bob@example.com")
	if !strings.Contains(m.Text, "https://panel.example.com/reset#token=") {
		t.Fatalf("mail %q", m.Text)
	}
	time.Sleep(50 * time.Millisecond)
	if box.count() != 1 {
		t.Fatalf("%d mails sent", box.count())
	}
	tok := tokenRe.FindStringSubmatch(m.Text)[1]

	r := anon(t, base)
	r.do("POST", "/api/auth/reset", map[string]string{"token": tok, "password": "password1234"}, 422) // too common
	r.do("POST", "/api/auth/reset", map[string]string{"token": tok, "password": "a-brand-new-passphrase"}, 204)
	r.do("POST", "/api/auth/reset", map[string]string{"token": tok, "password": "yet-another-passphrase"}, 422) // used
	bob.do("GET", "/api/auth/me", nil, 401)                                                                     // signed out everywhere
	r.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "another-long-password"}, 401)
	r.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "a-brand-new-passphrase"}, 200)
	admin.do("GET", "/api/audit?q=password.reset", nil, 200)
}

func TestPasswordResetNeedsMail(t *testing.T) {
	t.Parallel()
	_, _, base := linkPanel(t, api.Deps{}) // no SMTP
	anon(t, base).do("POST", "/api/auth/forgot", map[string]string{"email": "admin@example.com"}, 404)
	_, _, base = linkPanel(t, api.Deps{Mail: &outbox{}}) // SMTP but no public address: no links in mail
	anon(t, base).do("POST", "/api/auth/forgot", map[string]string{"email": "admin@example.com"}, 404)
}

func TestInvitations(t *testing.T) {
	t.Parallel()
	box := &outbox{}
	admin, st, base := linkPanel(t, api.Deps{Mail: box, PublicURL: "https://panel.example.com"})
	ctx := context.Background()
	n, _, _ := st.CreateNode(ctx, "n", 25565, 25570)
	e, _ := st.CreateEgg(ctx, store.Egg{Name: "e", DockerImage: "i", Startup: "s", Stop: "^C"})
	srv, _ := st.CreateServer(ctx, store.Server{UUID: "11111111-2222-4333-8444-555555555555", Name: "Survival", OwnerID: 1, NodeID: n.ID, EggID: e.ID, MemoryMB: 128})

	// Open sign-up is off by default.
	anon(t, base).do("POST", "/api/auth/register", map[string]string{"email": "x@example.com", "password": "a-long-enough-pass"}, 403)

	// Inviting someone without an account onto a server.
	var res struct {
		Added   bool
		Link    string
		Emailed bool
	}
	json.Unmarshal(admin.do("POST", "/api/servers/"+srv.UUID+"/invites", map[string]any{"email": "carol@example.com", "permissions": []string{"console.read", "files.read"}}, 200), &res)
	if res.Added || !res.Emailed || !strings.Contains(res.Link, "/register#token=") {
		t.Fatalf("invite %+v", res)
	}
	m := box.last(t, "carol@example.com")
	if !strings.Contains(m.Text, "Survival") {
		t.Fatalf("mail %q", m.Text)
	}
	tok := tokenRe.FindStringSubmatch(res.Link)[1]
	var inv struct {
		Email      string
		ServerName string `json:"server_name"`
	}
	json.Unmarshal(anon(t, base).do("POST", "/api/auth/invite", map[string]string{"token": tok}, 200), &inv)
	if inv.Email != "carol@example.com" || inv.ServerName != "Survival" {
		t.Fatalf("lookup %+v", inv)
	}

	// The invited email wins over whatever is typed, and access comes with it.
	carol := anon(t, base)
	carol.do("POST", "/api/auth/register", map[string]string{"token": tok, "email": "evil@example.com", "password": "carols-long-passphrase"}, 201)
	var me struct{ Email string }
	json.Unmarshal(carol.do("GET", "/api/auth/me", nil, 200), &me)
	if me.Email != "carol@example.com" {
		t.Fatalf("registered as %s", me.Email)
	}
	var v struct{ Permissions []string }
	json.Unmarshal(carol.do("GET", "/api/servers/"+srv.UUID, nil, 200), &v)
	if strings.Join(v.Permissions, ",") != "console.read,files.read" {
		t.Fatalf("permissions %v", v.Permissions)
	}
	anon(t, base).do("POST", "/api/auth/register", map[string]string{"token": tok, "password": "another-long-passphrase"}, 422) // spent

	// An email that already has an account is simply added.
	st.CreateUser(ctx, "dave@example.com", "another-long-password", false)
	res.Link, res.Emailed = "", false
	json.Unmarshal(admin.do("POST", "/api/servers/"+srv.UUID+"/invites", map[string]any{"email": "dave@example.com", "permissions": []string{"console.read"}}, 200), &res)
	if !res.Added || res.Link != "" {
		t.Fatalf("existing account %+v", res)
	}

	// Pending invitations can be listed and withdrawn; only the owner sees them.
	admin.do("POST", "/api/servers/"+srv.UUID+"/invites", map[string]any{"email": "erin@example.com", "permissions": []string{"console.read"}}, 200)
	var list []struct{ ID int64 }
	json.Unmarshal(admin.do("GET", "/api/servers/"+srv.UUID+"/invites", nil, 200), &list)
	if len(list) != 1 {
		t.Fatalf("pending %+v", list)
	}
	carol.do("GET", "/api/servers/"+srv.UUID+"/invites", nil, 403)
	admin.do("DELETE", "/api/servers/"+srv.UUID+"/invites/"+itoa(list[0].ID), nil, 204)

	// Admin account invitations, including admin ones.
	json.Unmarshal(admin.do("POST", "/api/invites", map[string]any{"email": "frank@example.com", "admin": true}, 201), &res)
	frank := anon(t, base)
	frank.do("POST", "/api/auth/register", map[string]string{"token": tokenRe.FindStringSubmatch(res.Link)[1], "password": "franks-long-passphrase"}, 201)
	var fu struct {
		IsAdmin bool `json:"is_admin"`
	}
	json.Unmarshal(frank.do("GET", "/api/auth/me", nil, 200), &fu)
	if !fu.IsAdmin {
		t.Fatal("admin invite did not make an admin")
	}
	admin.do("POST", "/api/invites", map[string]any{"email": "frank@example.com"}, 409) // has an account now
	carol.do("POST", "/api/invites", map[string]any{"email": "z@example.com"}, 403)
}

func TestOpenSignup(t *testing.T) {
	t.Parallel()
	_, _, base := linkPanel(t, api.Deps{Signup: api.SignupOpen})
	c := anon(t, base)
	c.do("POST", "/api/auth/register", map[string]string{"email": "new@example.com", "password": "short"}, 422)
	c.do("POST", "/api/auth/register", map[string]string{"email": "new@example.com", "password": "a-long-enough-pass"}, 201)
	anon(t, base).do("POST", "/api/auth/register", map[string]string{"email": "NEW@example.com", "password": "a-long-enough-pass"}, 422)
	var me struct {
		IsAdmin bool `json:"is_admin"`
	}
	json.Unmarshal(c.do("GET", "/api/auth/me", nil, 200), &me)
	if me.IsAdmin {
		t.Fatal("open sign-up made an admin")
	}
}
