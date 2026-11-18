package api_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
)

// withDeps is the same panel and database, restarted with changed settings.
func (e *env) withDeps(f func(*api.Deps)) *env {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := api.Deps{Store: e.st, Hub: hub.New(log), Log: log}
	f(&d)
	ts := httptest.NewServer(api.New(d))
	e.t.Cleanup(ts.Close)
	return &env{e.t, e.st, ts}
}

func mfaRequiredFlag(t *testing.T, c *cl) bool {
	t.Helper()
	code, body := c.do("GET", "/api/auth/me", nil, nil)
	if code != 200 {
		t.Fatalf("me: %d %s", code, body)
	}
	var me struct {
		MFARequired bool `json:"mfa_required"`
	}
	json.Unmarshal(body, &me)
	return me.MFARequired
}

// ASVS v5.0.0-6.3.3: with GAMEPANEL_REQUIRE_MFA=all, an account without a
// second factor can only set one up; nothing else works until it has.
func TestRequireMFAHoldsSessionsToEnrolment(t *testing.T) {
	e := newEnv(t, "").withDeps(func(d *api.Deps) { d.RequireMFA = api.MFAAll })
	c := e.client()
	code, body := c.do("POST", "/api/auth/login", map[string]string{"email": bobEmail, "password": bobPW}, nil)
	var u struct {
		MFARequired bool `json:"mfa_required"`
	}
	json.Unmarshal(body, &u)
	if code != 200 || !u.MFARequired {
		t.Fatalf("login: %d %s", code, body)
	}
	if !mfaRequiredFlag(t, c) {
		t.Fatal("me must report mfa_required")
	}
	for _, r := range []struct{ method, path string }{
		{"GET", "/api/servers"},
		{"GET", "/api/servers/00000000-0000-0000-0000-000000000000/ws"},
		{"GET", "/api/openapi.json"},
		{"POST", "/api/auth/keys"},
		{"GET", "/api/auth/keys"},
	} {
		if code, body := c.do(r.method, r.path, map[string]any{}, nil); code != 403 || !strings.Contains(string(body), "two-factor") {
			t.Fatalf("%s %s before enrolment: got %d %s, want the policy's 403", r.method, r.path, code, body)
		}
	}
	// Account security pages work.
	if code, _ := c.do("GET", "/api/auth/sessions", nil, nil); code != 200 {
		t.Fatalf("sessions during enrolment: %d", code)
	}
	enrolTOTP(t, c, bobPW)
	if mfaRequiredFlag(t, c) {
		t.Fatal("enrolling must lift the restriction for this session")
	}
	if code, _ := c.do("GET", "/api/servers", nil, nil); code != 200 {
		t.Fatalf("after enrolment: %d", code)
	}
	// The last second factor cannot be removed while the policy holds.
	if code, body := c.do("POST", "/api/auth/totp/disable", map[string]string{"password": bobPW, "code": "000000"}, nil); code != 409 {
		t.Fatalf("disable last factor: %d %s", code, body)
	}
}

func TestRequireMFAForAdminsOnly(t *testing.T) {
	e := newEnv(t, "").withDeps(func(d *api.Deps) { d.RequireMFA = api.MFAAdmins })
	bob, admin := e.client(), e.client()
	bob.login(bobEmail, bobPW)
	admin.login("admin@example.com", "correct-horse-battery")
	if code, _ := bob.do("GET", "/api/servers", nil, nil); code != 200 || mfaRequiredFlag(t, bob) {
		t.Fatalf("a regular user is not affected: %d", code)
	}
	if code, body := admin.do("GET", "/api/nodes", nil, nil); code != 403 || !strings.Contains(string(body), "two-factor") || !mfaRequiredFlag(t, admin) {
		t.Fatalf("an admin without two factors: %d", code)
	}
}

// API keys made before the policy stop working until their owner has a
// second factor, so a key cannot be used to skip it.
func TestRequireMFABlocksKeysWithoutSecondFactor(t *testing.T) {
	base := newEnv(t, "")
	ctx := context.Background()
	u, _ := base.st.UserByEmail(ctx, bobEmail)
	_, tok, err := base.st.CreateAPIKey(ctx, u.ID, "ci", 0)
	if err != nil {
		t.Fatal(err)
	}
	bearer := map[string]string{"Authorization": "Bearer " + tok}
	e := base.withDeps(func(d *api.Deps) { d.RequireMFA = api.MFAAll })
	if code, _ := e.client().do("GET", "/api/servers", nil, bearer); code != 403 {
		t.Fatalf("key of an account without two factors: %d", code)
	}
	c := e.client()
	c.login(bobEmail, bobPW)
	enrolTOTP(t, c, bobPW)
	if code, _ := e.client().do("GET", "/api/servers", nil, bearer); code != 200 {
		t.Fatalf("key after enrolment: %d", code)
	}
}

// An account that has a passkey but signs in with only its password cannot
// add a new factor to get past the policy: it may only sign out and sign in
// again with the passkey.
func TestRequireMFAEnrolledAccountMustUseItsFactor(t *testing.T) {
	e := newEnv(t, "").withDeps(func(d *api.Deps) { d.RequireMFA = api.MFAAll })
	u, _ := e.st.UserByEmail(context.Background(), bobEmail)
	if _, err := e.st.DB.Exec(`INSERT INTO passkeys(id,user_id,name,data,created_at) VALUES('pk1',?,'key','{}',1)`, u.ID); err != nil {
		t.Fatal(err)
	}
	c := e.client()
	code, body := c.do("POST", "/api/auth/login", map[string]string{"email": bobEmail, "password": bobPW}, nil)
	var u2 struct {
		MFARequired    bool `json:"mfa_required"`
		MFASignInAgain bool `json:"mfa_sign_in_again"`
	}
	json.Unmarshal(body, &u2)
	if code != 200 || !u2.MFARequired || !u2.MFASignInAgain || !mfaRequiredFlag(t, c) {
		t.Fatalf("password-only sign-in must be restricted: %d %s", code, body)
	}
	for _, r := range []struct{ method, path string }{
		{"POST", "/api/auth/totp/setup"},
		{"POST", "/api/auth/passkeys/register/begin"},
		{"POST", "/api/auth/password"},
		{"GET", "/api/auth/sessions"},
		{"GET", "/api/servers"},
	} {
		if code, body := c.do(r.method, r.path, map[string]string{"password": bobPW}, nil); code != 403 || !strings.Contains(string(body), "sign in again") {
			t.Fatalf("%s %s: got %d %s, want the policy's 403", r.method, r.path, code, body)
		}
	}
	if code, _ := c.do("POST", "/api/auth/logout", nil, nil); code != 204 && code != 200 {
		t.Fatalf("logout: %d", code)
	}
}

// Without the setting nothing changes.
func TestNoMFAPolicyByDefault(t *testing.T) {
	e := newEnv(t, "")
	c := e.client()
	c.login("admin@example.com", "correct-horse-battery")
	if code, _ := c.do("GET", "/api/nodes", nil, nil); code != 200 || mfaRequiredFlag(t, c) {
		t.Fatalf("default policy: %d", code)
	}
}

func TestHSTSSetting(t *testing.T) {
	e := newEnv(t, "").withDeps(func(d *api.Deps) {
		d.CookieSecure = true
		d.HSTS = "max-age=31536000; includeSubDomains"
	})
	resp := raw(t, e, "GET", "/api/auth/me", nil, nil)
	if got := resp.Header.Get("Strict-Transport-Security"); got != "max-age=31536000; includeSubDomains" {
		t.Fatalf("HSTS %q", got)
	}
	def := newEnv(t, "").withDeps(func(d *api.Deps) { d.CookieSecure = true })
	if got := raw(t, def, "GET", "/api/auth/me", nil, nil).Header.Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Fatalf("default HSTS %q", got)
	}
}
