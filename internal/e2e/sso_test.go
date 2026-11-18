package e2e

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// fakeIdP is an OpenID Connect provider that hands out codes for whichever
// person the test says is signing in, and checks PKCE like a real one.
type fakeIdP struct {
	srv *httptest.Server
	mu  sync.Mutex
	// code -> who, and the PKCE challenge and redirect it was issued for
	codes  map[string]idpGrant
	tokens map[string]idpUser
}
type idpUser struct {
	Sub, Email string
	Verified   bool
}
type idpGrant struct {
	user                idpUser
	challenge, redirect string
}

func newIdP(t *testing.T) *fakeIdP {
	f := &fakeIdP{codes: map[string]idpGrant{}, tokens: map[string]idpUser{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize",
			"token_endpoint": f.srv.URL + "/token", "userinfo_endpoint": f.srv.URL + "/userinfo"})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		g, ok := f.codes[r.Form.Get("code")]
		delete(f.codes, r.Form.Get("code"))
		f.mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || r.Form.Get("client_secret") != "s3cret" || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge || r.Form.Get("redirect_uri") != g.redirect {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		tok := "at-" + r.Form.Get("code")
		f.mu.Lock()
		f.tokens[tok] = g.user
		f.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"access_token": tok, "token_type": "Bearer"})
	})
	mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		u, ok := f.tokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"sub": u.Sub, "email": u.Email, "email_verified": u.Verified})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

type ssoEnv struct {
	t     *testing.T
	base  string
	idp   *fakeIdP
	st    *store.Store
	admin *client
}

func newSSOEnv(t *testing.T, signup string) *ssoEnv {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, _ := store.Open(filepath.Join(t.TempDir(), "p.db"))
	t.Cleanup(func() { st.Close() })
	st.CreateUser(context.Background(), "admin@example.com", "correct-horse-battery", true)
	idp := newIdP(t)
	deps := api.Deps{Store: st, Hub: hub.New(log), Log: log, Signup: signup,
		SSO: []*api.SSOProvider{api.OIDCSSO("Authentik", idp.srv.URL, "panel", "s3cret")}}
	ts := httptest.NewUnstartedServer(nil)
	ts.Start()
	deps.PublicURL = ts.URL
	ts.Config.Handler = api.New(deps)
	t.Cleanup(ts.Close)
	admin := anon(t, ts.URL)
	admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": "correct-horse-battery"}, 200)
	return &ssoEnv{t, ts.URL, idp, st, admin}
}

// browser follows no redirects, so each hop can be checked.
func browser(t *testing.T) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// signIn runs the whole round trip as person u in browser b and returns
// where the panel sent the browser at the end.
func (e *ssoEnv) signIn(b *http.Client, u idpUser, startQuery string) string {
	e.t.Helper()
	resp, err := b.Get(e.base + "/api/auth/sso/oidc/start" + startQuery)
	if err != nil || resp.StatusCode != 302 {
		e.t.Fatalf("start: %v %v", resp, err)
	}
	auth, _ := url.Parse(resp.Header.Get("Location"))
	q := auth.Query()
	if !strings.HasPrefix(auth.String(), e.idp.srv.URL+"/authorize") || q.Get("code_challenge_method") != "S256" || q.Get("state") == "" {
		e.t.Fatalf("authorize url %s", auth)
	}
	// The person signs in at the provider, which sends them back with a code.
	code := fmt.Sprintf("code-%d", time.Now().UnixNano())
	e.idp.mu.Lock()
	e.idp.codes[code] = idpGrant{user: u, challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
	e.idp.mu.Unlock()
	resp, err = b.Get(q.Get("redirect_uri") + "?code=" + code + "&state=" + url.QueryEscape(q.Get("state")))
	if err != nil || resp.StatusCode != 302 {
		e.t.Fatalf("callback: %v %v", resp, err)
	}
	return resp.Header.Get("Location")
}

func me(b *http.Client, base string) (string, int) {
	resp, err := b.Get(base + "/api/auth/me")
	if err != nil {
		return "", 0
	}
	defer resp.Body.Close()
	var u struct{ Email string }
	json.NewDecoder(resp.Body).Decode(&u)
	return u.Email, resp.StatusCode
}

func TestSSOLinkAndSignIn(t *testing.T) {
	t.Parallel()
	e := newSSOEnv(t, "invite")
	alice := idpUser{Sub: "alice-123", Email: "admin@example.com", Verified: true}

	// An identity nobody linked does not sign anyone in, even with a matching
	// email: the account must link it itself.
	if got := e.signIn(browser(t), alice, ""); got != "/login?sso_error=not_linked" {
		t.Fatalf("unlinked: %s", got)
	}

	// The admin links it from their account page (signed in with a password).
	b := browser(t)
	resp, _ := b.Post(e.base+"/api/auth/login", "application/json", strings.NewReader(`{"email":"admin@example.com","password":"correct-horse-battery"}`))
	resp.Body.Close()
	if got := e.signIn(b, alice, "?mode=link"); got != "/account?sso_linked=oidc" {
		t.Fatalf("link: %s", got)
	}
	var ids struct {
		Identities []struct{ Provider, Email string }
	}
	json.Unmarshal(e.admin.do("GET", "/api/auth/identities", nil, 200), &ids)
	if len(ids.Identities) != 1 || ids.Identities[0].Provider != "oidc" {
		t.Fatalf("identities %+v", ids)
	}

	// Now a fresh browser signs in with it.
	fresh := browser(t)
	if got := e.signIn(fresh, alice, ""); got != "/" {
		t.Fatalf("sign in: %s", got)
	}
	if email, code := me(fresh, e.base); code != 200 || email != "admin@example.com" {
		t.Fatalf("signed in as %q (%d)", email, code)
	}

	// The same identity cannot be linked to a second account.
	e.st.CreateUser(context.Background(), "bob@example.com", "another-long-password", false)
	bb := browser(t)
	resp, _ = bb.Post(e.base+"/api/auth/login", "application/json", strings.NewReader(`{"email":"bob@example.com","password":"another-long-password"}`))
	resp.Body.Close()
	if got := e.signIn(bb, alice, "?mode=link"); got != "/account?sso_error=taken" {
		t.Fatalf("relink: %s", got)
	}

	// Linking needs a signed-in browser.
	resp, _ = browser(t).Get(e.base + "/api/auth/sso/oidc/start?mode=link")
	if resp.StatusCode != 401 {
		t.Fatalf("anonymous link start: %d", resp.StatusCode)
	}
}

func TestSSOStateIsBoundToTheBrowser(t *testing.T) {
	t.Parallel()
	e := newSSOEnv(t, "open")
	attacker, victim := browser(t), browser(t)
	resp, _ := attacker.Get(e.base + "/api/auth/sso/oidc/start")
	auth, _ := url.Parse(resp.Header.Get("Location"))
	q := auth.Query()
	e.idp.mu.Lock()
	e.idp.codes["c1"] = idpGrant{user: idpUser{Sub: "mallory", Email: "mallory@example.com", Verified: true}, challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
	e.idp.mu.Unlock()
	// The attacker sends the victim their callback link (login CSRF).
	resp, _ = victim.Get(q.Get("redirect_uri") + "?code=c1&state=" + url.QueryEscape(q.Get("state")))
	if loc := resp.Header.Get("Location"); loc != "/login?sso_error=expired" {
		t.Fatalf("forged callback went to %s", loc)
	}
	if _, code := me(victim, e.base); code != 401 {
		t.Fatal("the victim was signed in as the attacker")
	}
}

func TestSSOSignUp(t *testing.T) {
	t.Parallel()
	e := newSSOEnv(t, "open")
	if got := e.signIn(browser(t), idpUser{Sub: "u1", Email: "new@example.com", Verified: false}, ""); got != "/login?sso_error=unverified" {
		t.Fatalf("unverified: %s", got)
	}
	if got := e.signIn(browser(t), idpUser{Sub: "u2", Email: "admin@example.com", Verified: true}, ""); got != "/login?sso_error=email_taken" {
		t.Fatalf("existing email: %s", got)
	}
	b := browser(t)
	if got := e.signIn(b, idpUser{Sub: "u3", Email: "New@Example.com", Verified: true}, ""); got != "/" {
		t.Fatalf("sign-up: %s", got)
	}
	if email, _ := me(b, e.base); email != "new@example.com" {
		t.Fatalf("new account %q", email)
	}
	// A password-less account cannot drop its only way in, until it sets a password.
	c := &client{t, e.base, b}
	c.do("DELETE", "/api/auth/identities/oidc", nil, 409)
	c.do("POST", "/api/auth/password/first", map[string]string{"password": "now-with-a-password"}, 204)
	c.do("POST", "/api/auth/password/first", map[string]string{"password": "again-a-password-x"}, 422)
	c.do("DELETE", "/api/auth/identities/oidc", nil, 204)
	anon(t, e.base).do("POST", "/api/auth/login", map[string]string{"email": "new@example.com", "password": "now-with-a-password"}, 200)
}

func TestSSOInvitation(t *testing.T) {
	t.Parallel()
	e := newSSOEnv(t, "invite")
	var res struct{ Link string }
	json.Unmarshal(e.admin.do("POST", "/api/invites", map[string]any{"email": "carol@example.com"}, 201), &res)
	tok := tokenRe.FindStringSubmatch(res.Link)[1]
	if got := e.signIn(browser(t), idpUser{Sub: "c9", Email: "someone-else@example.com", Verified: true}, "?invite="+tok); got != "/login?sso_error=invite" {
		t.Fatalf("wrong email: %s", got)
	}
	b := browser(t)
	if got := e.signIn(b, idpUser{Sub: "c1", Email: "carol@example.com", Verified: true}, "?invite="+tok); got != "/" {
		t.Fatalf("invited: %s", got)
	}
	if email, _ := me(b, e.base); email != "carol@example.com" {
		t.Fatalf("account %q", email)
	}
}

// totp computes the current code for a base32 secret (RFC 6238).
func totp(secret string) string {
	key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(time.Now().Unix()/30))
	h := hmac.New(sha1.New, key)
	h.Write(msg[:])
	sum := h.Sum(nil)
	o := sum[len(sum)-1] & 0xf
	n := binary.BigEndian.Uint32(sum[o:o+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1000000)
}

func TestSSOStillAsksForTwoFactor(t *testing.T) {
	t.Parallel()
	e := newSSOEnv(t, "invite")
	alice := idpUser{Sub: "alice", Email: "admin@example.com", Verified: true}
	b := browser(t)
	resp, _ := b.Post(e.base+"/api/auth/login", "application/json", strings.NewReader(`{"email":"admin@example.com","password":"correct-horse-battery"}`))
	resp.Body.Close()
	if got := e.signIn(b, alice, "?mode=link"); got != "/account?sso_linked=oidc" {
		t.Fatalf("link: %s", got)
	}
	var setup struct{ Secret string }
	json.Unmarshal(e.admin.do("POST", "/api/auth/totp/setup", map[string]string{"password": "correct-horse-battery"}, 200), &setup)
	var rec struct{ Codes []string }
	json.Unmarshal(e.admin.do("POST", "/api/auth/totp/enable", map[string]string{"code": totp(setup.Secret)}, 200), &rec)

	fresh := browser(t)
	if got := e.signIn(fresh, alice, ""); got != "/login?sso=totp" {
		t.Fatalf("expected the two-factor step, got %s", got)
	}
	if _, code := me(fresh, e.base); code != 401 {
		t.Fatal("signed in before the second factor")
	}
	c := &client{t, e.base, fresh}
	c.do("POST", "/api/auth/sso/verify", map[string]string{"code": "000000"}, 403)
	c.do("POST", "/api/auth/sso/verify", map[string]string{"code": rec.Codes[0]}, 200)
	if email, code := me(fresh, e.base); code != 200 || email != "admin@example.com" {
		t.Fatalf("after the code: %q %d", email, code)
	}
	// The pending sign-in is spent.
	c.do("POST", "/api/auth/sso/verify", map[string]string{"code": rec.Codes[1]}, 401)
	// Without the cookie from the round trip there is nothing to finish.
	anon(t, e.base).do("POST", "/api/auth/sso/verify", map[string]string{"code": rec.Codes[1]}, 401)
}
