package api_test

// Tests for the authentication and session controls mapped to OWASP ASVS
// 5.0.0 in docs/asvs.md. Requirement IDs are version-qualified.

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
)

const bobEmail, bobPW = "bob@example.com", "another-long-password"

// enrolTOTP turns two-factor on for the signed-in client and returns the
// secret and recovery codes. Enrolment uses the current step.
func enrolTOTP(t *testing.T, c *cl, pw string) (string, []string) {
	t.Helper()
	var setup struct{ Secret string }
	_, body := c.do("POST", "/api/auth/totp/setup", map[string]string{"password": pw}, nil)
	json.Unmarshal(body, &setup)
	var rec struct{ Codes []string }
	code, body := c.do("POST", "/api/auth/totp/enable", map[string]string{"code": totpAt(t, setup.Secret, 0)}, nil)
	if code != 200 {
		t.Fatalf("enable: %d %s", code, body)
	}
	json.Unmarshal(body, &rec)
	return setup.Secret, rec.Codes
}

// restarted is a fresh panel process on the same database: nothing kept in
// memory (rate limiters) survives.
func (e *env) restarted() *env {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer(api.New(api.Deps{Store: e.st, Hub: hub.New(log), Log: log}))
	e.t.Cleanup(ts.Close)
	return &env{e.t, e.st, ts}
}

// ASVS v5.0.0-6.3.1 / NIST SP 800-63B §5.2.2: guessing the second factor is
// bounded across IP addresses and restarts, not only per 15-minute in-memory
// window.
func TestSecondFactorLockoutSurvivesRestart(t *testing.T) {
	e := newEnv(t, "127.0.0.1")
	a := e.client()
	a.login(bobEmail, bobPW)
	secret, recovery := enrolTOTP(t, a, bobPW)

	try := func(e *env, code string, ip string) int {
		c, _ := e.client().do("POST", "/api/auth/login",
			map[string]string{"email": bobEmail, "password": bobPW, "totp_code": code},
			map[string]string{"X-Forwarded-For": ip})
		return c
	}
	for i := range 5 {
		if c := try(e, "000000", "203.0.113."+string(rune('1'+i))); c != 401 {
			t.Fatalf("wrong code %d: got %d want 401", i, c)
		}
	}
	// Five wrong codes in a row: now even the right code must wait.
	if c := try(e, totpAt(t, secret, 1), "198.51.100.1"); c != 429 {
		t.Fatalf("right code during the delay: got %d want 429", c)
	}
	// A restart forgets the in-memory limiters, not the delay.
	r := e.restarted()
	if c := try(r, totpAt(t, secret, 1), "198.51.100.2"); c != 429 {
		t.Fatalf("after restart: got %d want 429", c)
	}
	// The owner can still get in with a recovery code, which clears the delay.
	if c := try(r, recovery[0], "198.51.100.3"); c != 200 {
		t.Fatalf("recovery code during the delay: got %d want 200", c)
	}
	if c := try(r, totpAt(t, secret, 1), "198.51.100.4"); c != 200 {
		t.Fatalf("authenticator code after the delay was cleared: got %d want 200", c)
	}
}

// ASVS v5.0.0-7.3.1 and v5.0.0-7.3.2: a session ends after a period without
// use, and after an absolute lifetime however busy it is.
func TestSessionIdleAndAbsoluteTimeout(t *testing.T) {
	e := newEnv(t, "")
	shift := func(col string, secs int) {
		t.Helper()
		if _, err := e.st.DB.Exec(`UPDATE sessions SET `+col+`=`+col+`-?`, secs); err != nil {
			t.Fatal(err)
		}
	}
	me := func(c *cl) int { code, _ := c.do("GET", "/api/auth/me", nil, nil); return code }

	a := e.client()
	a.login(bobEmail, bobPW)
	// Used every 29 minutes: stays signed in, well past 30 minutes in total.
	for i := range 3 {
		shift("last_seen_at", 29*60)
		if c := me(a); c != 200 {
			t.Fatalf("active session, round %d: got %d want 200", i, c)
		}
	}
	// Then 31 minutes without use: signed out.
	shift("last_seen_at", 31*60)
	if c := me(a); c != 401 {
		t.Fatalf("idle session: got %d want 401", c)
	}

	b := e.client()
	b.login(bobEmail, bobPW)
	shift("expires_at", 12*3600)
	if c := me(b); c != 401 {
		t.Fatalf("session past its absolute lifetime: got %d want 401", c)
	}
}

// ASVS v5.0.0-7.5.1: making a credential that outlives the session needs full
// re-authentication, the second factor too when the account has one.
func TestAPIKeyNeedsSecondFactor(t *testing.T) {
	e := newEnv(t, "")
	a := e.client()
	a.login(bobEmail, bobPW)
	secret, _ := enrolTOTP(t, a, bobPW)
	if code, body := a.do("POST", "/api/auth/keys", map[string]any{"name": "ci", "password": bobPW}, nil); code != 403 || !strings.Contains(string(body), "totp_required") {
		t.Fatalf("no code: %d %s", code, body)
	}
	if code, _ := a.do("POST", "/api/auth/keys", map[string]any{"name": "ci", "password": bobPW, "code": "000000"}, nil); code != 403 {
		t.Fatalf("wrong code: %d", code)
	}
	if code, body := a.do("POST", "/api/auth/keys", map[string]any{"name": "ci", "password": bobPW, "code": totpAt(t, secret, 1)}, nil); code != 201 {
		t.Fatalf("password and code: %d %s", code, body)
	}
}

// ASVS v5.0.0-6.2.4 and v5.0.0-6.2.12: common and known-leaked passwords are
// refused.
func TestCommonPasswordRefused(t *testing.T) {
	e := newEnv(t, "")
	a := e.client()
	a.login(bobEmail, bobPW)
	code, body := a.do("POST", "/api/auth/password", map[string]string{"current_password": bobPW, "new_password": "Q1w2e3r4t5y6"}, nil)
	if code != 422 || !strings.Contains(string(body), "too common") {
		t.Fatalf("common password: %d %s", code, body)
	}
}

// raw sends one request and returns the whole response.
func raw(t *testing.T, e *env, method, path string, body any, hdr map[string]string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp
}

func sessionSetCookie(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if strings.HasSuffix(c.Name, "gp_session") {
			return c
		}
	}
	t.Fatalf("no session cookie in %v", resp.Header.Values("Set-Cookie"))
	return nil
}

// ASVS v5.0.0-3.3.1, 3.3.3, 3.3.4, 7.4.1 and 14.3.2: over HTTPS the session
// cookie is Secure, HttpOnly and __Host- prefixed, signing out ends the session
// on the server, and API answers are not cached. HSTS is sent, but without
// includeSubDomains, so v5.0.0-3.4.1 is only met at Level 1 (docs/asvs.md).
func TestCookieAndHeaderHardening(t *testing.T) {
	login := map[string]string{"email": bobEmail, "password": bobPW}

	// Behind a trusted HTTPS proxy.
	e := newEnv(t, "127.0.0.1")
	resp := raw(t, e, "POST", "/api/auth/login", login, map[string]string{"X-Forwarded-Proto": "https"})
	c := sessionSetCookie(t, resp)
	if c.Name != "__Host-gp_session" || !c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" {
		t.Fatalf("HTTPS cookie: %+v", c)
	}
	if c.MaxAge != 12*3600 {
		t.Fatalf("cookie lifetime %d, want the 12 h session maximum", c.MaxAge)
	}
	if !strings.HasPrefix(resp.Header.Get("Strict-Transport-Security"), "max-age=") {
		t.Fatal("no HSTS over HTTPS")
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control %q on an API answer", got)
	}
	auth := map[string]string{"Cookie": c.Name + "=" + c.Value, "X-Forwarded-Proto": "https"}
	if r := raw(t, e, "GET", "/api/auth/me", nil, auth); r.StatusCode != 200 {
		t.Fatalf("__Host- cookie not accepted: %d", r.StatusCode)
	}
	// Signing out revokes it on the server and clears it under the same name.
	out := raw(t, e, "POST", "/api/auth/logout", nil, auth)
	if oc := sessionSetCookie(t, out); oc.Name != "__Host-gp_session" || oc.MaxAge >= 0 {
		t.Fatalf("logout cookie: %+v", oc)
	}
	if r := raw(t, e, "GET", "/api/auth/me", nil, auth); r.StatusCode != 401 {
		t.Fatalf("session still valid after logout: %d", r.StatusCode)
	}

	// Plain HTTP (local development): no Secure flag, no HSTS.
	resp = raw(t, e, "POST", "/api/auth/login", login, nil)
	if c := sessionSetCookie(t, resp); c.Name != "gp_session" || c.Secure {
		t.Fatalf("plain HTTP cookie: %+v", c)
	}
	if resp.Header.Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS over plain HTTP")
	}

	// X-Forwarded-Proto from a peer that is not a trusted proxy is ignored.
	u := newEnv(t, "")
	resp = raw(t, u, "POST", "/api/auth/login", login, map[string]string{"X-Forwarded-Proto": "https"})
	if c := sessionSetCookie(t, resp); c.Secure || resp.Header.Get("Strict-Transport-Security") != "" {
		t.Fatalf("forged X-Forwarded-Proto believed: %+v", c)
	}
}

func cookieNamed(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// ASVS v5.0.0-3.3.1 and v5.0.0-3.3.3, for sessions from before the upgrade.
// Sessions from before the __Host- name may use the old gp_session name over
// HTTPS once: they are re-issued under __Host- and the old cookie is removed.
// Any other gp_session cookie over HTTPS is refused; plain HTTP is unchanged.
func TestLegacyCookieMigration(t *testing.T) {
	e := newEnv(t, "127.0.0.1")
	login := map[string]string{"email": bobEmail, "password": bobPW}
	https := func(cookie string) map[string]string {
		return map[string]string{"Cookie": cookie, "X-Forwarded-Proto": "https"}
	}

	// A session signed in before the upgrade (plain gp_session cookie).
	old := sessionSetCookie(t, raw(t, e, "POST", "/api/auth/login", login, nil)).Value
	if _, err := e.st.DB.Exec(`UPDATE sessions SET legacy_cookie=1`); err != nil {
		t.Fatal(err)
	}
	// A session made after the upgrade, held under the plain name.
	fresh := sessionSetCookie(t, raw(t, e, "POST", "/api/auth/login", login, nil)).Value

	resp := raw(t, e, "GET", "/api/auth/me", nil, https("gp_session="+old))
	if resp.StatusCode != 200 {
		t.Fatalf("pre-upgrade session over HTTPS: %d", resp.StatusCode)
	}
	hc := cookieNamed(resp, "__Host-gp_session")
	if hc == nil || hc.Value != old || !hc.Secure || !hc.HttpOnly || hc.Path != "/" || hc.Domain != "" || hc.SameSite != http.SameSiteLaxMode || hc.MaxAge <= 0 || hc.MaxAge > 12*3600 {
		t.Fatalf("migrated cookie: %+v", hc)
	}
	if lc := cookieNamed(resp, "gp_session"); lc == nil || lc.MaxAge >= 0 {
		t.Fatalf("old cookie not removed: %+v", lc)
	}
	// The old name works once; the session lives on under __Host-.
	if r := raw(t, e, "GET", "/api/auth/me", nil, https("gp_session="+old)); r.StatusCode != 401 {
		t.Fatalf("old name accepted twice: %d", r.StatusCode)
	}
	if r := raw(t, e, "GET", "/api/auth/me", nil, https("__Host-gp_session="+old)); r.StatusCode != 200 {
		t.Fatalf("migrated session: %d", r.StatusCode)
	}
	// A post-upgrade session is never accepted under the old name over HTTPS.
	r := raw(t, e, "GET", "/api/auth/me", nil, https("gp_session="+fresh))
	if r.StatusCode != 401 {
		t.Fatalf("new session under the old name over HTTPS: %d", r.StatusCode)
	}
	if lc := cookieNamed(r, "gp_session"); lc == nil || lc.MaxAge >= 0 {
		t.Fatalf("refused old cookie not removed: %+v", lc)
	}
	// ...but plain HTTP (local development) still uses it.
	if r := raw(t, e, "GET", "/api/auth/me", nil, map[string]string{"Cookie": "gp_session=" + fresh}); r.StatusCode != 200 {
		t.Fatalf("plain HTTP session: %d", r.StatusCode)
	}
	// __Host- wins when both are sent; the other one is ignored.
	if r := raw(t, e, "GET", "/api/auth/me", nil, https("gp_session=planted; __Host-gp_session="+old)); r.StatusCode != 200 {
		t.Fatalf("secure cookie not preferred: %d", r.StatusCode)
	}
	// Signing out over HTTPS clears both names.
	out := raw(t, e, "POST", "/api/auth/logout", nil, https("__Host-gp_session="+old))
	for _, n := range []string{"__Host-gp_session", "gp_session"} {
		if c := cookieNamed(out, n); c == nil || c.MaxAge >= 0 {
			t.Fatalf("logout did not clear %s: %+v", n, c)
		}
	}
	if r := raw(t, e, "GET", "/api/auth/me", nil, https("__Host-gp_session="+old)); r.StatusCode != 401 {
		t.Fatalf("session valid after logout: %d", r.StatusCode)
	}
}

// ASVS v5.0.0-6.2.3, 7.4.3 and 7.5.1: with two-factor on, changing the
// password needs a code as well as the current password; a failed attempt
// changes nothing.
func TestPasswordChangeNeedsSecondFactor(t *testing.T) {
	e := newEnv(t, "")
	ctx := t.Context()
	id, _ := e.st.UserIDByEmail(ctx, bobEmail)
	a, b := e.client(), e.client()
	a.login(bobEmail, bobPW)
	b.login(bobEmail, bobPW)
	secret, recovery := enrolTOTP(t, a, bobPW)
	const next = "a-brand-new-passphrase"
	change := func(cur, nw, code string) (int, string) {
		body := map[string]string{"current_password": cur, "new_password": nw}
		if code != "" {
			body["code"] = code
		}
		c, out := a.do("POST", "/api/auth/password", body, nil)
		return c, string(out)
	}
	unchanged := func(step string) {
		t.Helper()
		if !e.st.CheckPassword(ctx, id, bobPW) {
			t.Fatalf("%s: password changed", step)
		}
		if c, _ := b.do("GET", "/api/auth/me", nil, nil); c != 200 {
			t.Fatalf("%s: other session revoked (%d)", step, c)
		}
	}

	if c, out := change(bobPW, next, ""); c != 403 || !strings.Contains(out, "totp_required") {
		t.Fatalf("no code: %d %s", c, out)
	}
	unchanged("no code")
	if c, _ := change("not-the-password", next, totpAt(t, secret, 1)); c != 403 {
		t.Fatalf("wrong password: %d", c)
	}
	unchanged("wrong password")
	if c, _ := change(bobPW, next, "000000"); c != 403 {
		t.Fatalf("wrong code: %d", c)
	}
	unchanged("wrong code")
	// A new password that fails the policy is refused before a code is spent.
	if c, _ := change(bobPW, "Q1w2e3r4t5y6", totpAt(t, secret, 1)); c != 422 {
		t.Fatalf("common new password: %d", c)
	}
	unchanged("common new password")
	if c, out := change(bobPW, next, totpAt(t, secret, 1)); c != 204 {
		t.Fatalf("password and authenticator code: %d %s", c, out)
	}
	if !e.st.CheckPassword(ctx, id, next) {
		t.Fatal("password not changed")
	}
	if c, _ := b.do("GET", "/api/auth/me", nil, nil); c != 401 {
		t.Fatalf("other session survived the change: %d", c)
	}
	// A recovery code works too.
	if c, out := change(next, bobPW, recovery[0]); c != 204 {
		t.Fatalf("password and recovery code: %d %s", c, out)
	}

	// Without two-factor the current password is enough.
	adm := e.client()
	adm.login("admin@example.com", "correct-horse-battery")
	if c, out := adm.do("POST", "/api/auth/password", map[string]string{"current_password": "correct-horse-battery", "new_password": next}, nil); c != 204 {
		t.Fatalf("account without two-factor: %d %s", c, out)
	}
}
