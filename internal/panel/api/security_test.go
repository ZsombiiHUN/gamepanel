package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

type env struct {
	t  *testing.T
	st *store.Store
	ts *httptest.Server
}

func newEnv(t *testing.T, proxies string) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	st.CreateUser(ctx, "admin@example.com", "correct-horse-battery", true)
	st.CreateUser(ctx, "bob@example.com", "another-long-password", false)
	tp, err := api.ParseTrustedProxies(proxies)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer(api.New(api.Deps{Store: st, Hub: hub.New(log), Log: log, TrustedProxies: tp}))
	t.Cleanup(ts.Close)
	return &env{t, st, ts}
}

type cl struct {
	e *env
	c *http.Client
}

func (e *env) client() *cl {
	jar, _ := cookiejar.New(nil)
	return &cl{e, &http.Client{Jar: jar}}
}

func (c *cl) do(method, path string, body any, hdr map[string]string) (int, []byte) {
	c.e.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.e.ts.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (c *cl) login(email, pw string) int {
	code, _ := c.do("POST", "/api/auth/login", map[string]string{"email": email, "password": pw}, nil)
	return code
}

func TestChangePasswordRevokesOtherSessions(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "")
	a, b := e.client(), e.client() // two browsers, same user
	if a.login("bob@example.com", "another-long-password") != 200 || b.login("bob@example.com", "another-long-password") != 200 {
		t.Fatal("login failed")
	}
	if code, _ := b.do("GET", "/api/auth/me", nil, nil); code != 200 {
		t.Fatalf("b should be signed in, got %d", code)
	}
	pw := func(cur, next string) int {
		code, _ := a.do("POST", "/api/auth/password", map[string]string{"current_password": cur, "new_password": next}, nil)
		return code
	}
	if c := pw("wrong-password-here", "brand-new-password-1"); c != 403 {
		t.Fatalf("wrong current password: got %d want 403", c)
	}
	if c := pw("another-long-password", "short"); c != 422 {
		t.Fatalf("short new password: got %d want 422", c)
	}
	if c := pw("another-long-password", "brand-new-password-1"); c != 204 {
		t.Fatalf("change: got %d want 204", c)
	}
	if code, _ := a.do("GET", "/api/auth/me", nil, nil); code != 200 {
		t.Fatalf("changing browser must stay signed in, got %d", code)
	}
	if code, _ := b.do("GET", "/api/auth/me", nil, nil); code != 401 {
		t.Fatalf("other browser must be signed out, got %d", code)
	}
	if e.client().login("bob@example.com", "another-long-password") != 401 {
		t.Fatal("old password still works")
	}
	if e.client().login("bob@example.com", "brand-new-password-1") != 200 {
		t.Fatal("new password does not work")
	}
}

func TestPerAccountLockoutSurvivesIPRotation(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "127.0.0.1")
	// The attacker rotates source IPs via a trusted proxy, so the per-IP limit never trips.
	for i := range 20 {
		c := e.client()
		code, _ := c.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "guess-number-" + string(rune('a'+i))},
			map[string]string{"X-Forwarded-For": "203.0.113." + string(rune('1'+i%9)) + string(rune('0'+i/9))})
		if code != 401 {
			t.Fatalf("attempt %d: got %d want 401", i, code)
		}
	}
	// The right password is now refused too, until the window passes.
	if code := e.client().login("bob@example.com", "another-long-password"); code != 429 {
		t.Fatalf("locked account login: got %d want 429", code)
	}
	// Other accounts are unaffected.
	if code := e.client().login("admin@example.com", "correct-horse-battery"); code != 200 {
		t.Fatalf("other account: got %d want 200", code)
	}
}

func TestIPRateLimitUsesRealClientBehindProxy(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "127.0.0.1")
	// 10 failures from one real client trip the per-IP limit...
	for i := range 10 {
		e.client().do("POST", "/api/auth/login", map[string]string{"email": "nobody" + string(rune('a'+i)) + "@x.io", "password": "whatever-long-1"},
			map[string]string{"X-Forwarded-For": "198.51.100.7"})
	}
	code, _ := e.client().do("POST", "/api/auth/login", map[string]string{"email": "z@x.io", "password": "whatever-long-1"},
		map[string]string{"X-Forwarded-For": "198.51.100.7"})
	if code != 429 {
		t.Fatalf("same client: got %d want 429", code)
	}
	// ...but a different real client behind the same proxy is not locked out.
	code, _ = e.client().do("POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": "correct-horse-battery"},
		map[string]string{"X-Forwarded-For": "198.51.100.8"})
	if code != 200 {
		t.Fatalf("different client: got %d want 200", code)
	}
}

func TestForwardedForIgnoredFromUntrustedPeer(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "") // no trusted proxies: header must be ignored
	for i := range 10 {
		e.client().do("POST", "/api/auth/login", map[string]string{"email": "n@x.io", "password": "whatever-long-1"},
			map[string]string{"X-Forwarded-For": "10.9.9." + string(rune('0'+i))})
	}
	code, _ := e.client().do("POST", "/api/auth/login", map[string]string{"email": "n@x.io", "password": "whatever-long-1"},
		map[string]string{"X-Forwarded-For": "10.1.1.1"})
	if code != 429 {
		t.Fatalf("spoofed X-Forwarded-For must not evade the limit: got %d want 429", code)
	}
}

func TestCrossOriginWritesBlocked(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "")
	c := e.client()
	if c.login("bob@example.com", "another-long-password") != 200 {
		t.Fatal("login")
	}
	body := map[string]string{"current_password": "another-long-password", "new_password": "brand-new-password-1"}
	if code, _ := c.do("POST", "/api/auth/password", body, map[string]string{"Origin": "https://evil.example"}); code != 403 {
		t.Fatalf("foreign Origin: got %d want 403", code)
	}
	if code, _ := c.do("POST", "/api/auth/password", body, map[string]string{"Sec-Fetch-Site": "cross-site"}); code != 403 {
		t.Fatalf("cross-site fetch: got %d want 403", code)
	}
	if code, _ := c.do("POST", "/api/auth/password", body, map[string]string{"Origin": e.ts.URL}); code != 204 {
		t.Fatalf("same Origin: got %d want 204", code)
	}
}

func TestAuditLog(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "")
	bob, admin := e.client(), e.client()
	bob.login("bob@example.com", "wrong-password-xx")
	bob.login("bob@example.com", "another-long-password")
	if code, _ := bob.do("GET", "/api/audit", nil, nil); code != 403 {
		t.Fatalf("non-admin audit: got %d want 403", code)
	}
	admin.login("admin@example.com", "correct-horse-battery")
	code, b := admin.do("GET", "/api/audit", nil, nil)
	if code != 200 {
		t.Fatalf("admin audit: %d", code)
	}
	var es []store.AuditEntry
	if err := json.Unmarshal(b, &es); err != nil {
		t.Fatal(err)
	}
	var seen []string
	for _, x := range es {
		seen = append(seen, x.Action+":"+x.ActorEmail)
		if x.IP == "" {
			t.Errorf("entry %q has no IP", x.Action)
		}
	}
	got := strings.Join(seen, ",")
	for _, want := range []string{"login.fail:bob@example.com", "login.ok:bob@example.com", "login.ok:admin@example.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("audit log missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "wrong-password") {
		t.Error("audit log must never contain passwords")
	}
}
