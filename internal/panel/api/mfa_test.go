package api_test

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// totpAt computes the RFC 6238 code for the step `offset` steps from now.
func totpAt(t *testing.T, b32secret string, offset int64) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(b32secret)
	if err != nil {
		t.Fatal(err)
	}
	var c [8]byte
	binary.BigEndian.PutUint64(c[:], uint64(time.Now().Unix()/30+offset))
	m := hmac.New(sha1.New, key)
	m.Write(c[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7fffffff)%1_000_000)
}

func TestTOTPEnrolmentAndLogin(t *testing.T) {
	e := newEnv(t, "")
	const email, pw = "bob@example.com", "another-long-password"
	a := e.client()
	a.login(email, pw)

	if code, _ := a.do("POST", "/api/auth/totp/setup", map[string]string{"password": "wrong-wrong-wrong"}, nil); code != 403 {
		t.Fatalf("setup with wrong password: %d", code)
	}
	var setup struct{ Secret, URI string }
	code, body := a.do("POST", "/api/auth/totp/setup", map[string]string{"password": pw}, nil)
	if code != 200 {
		t.Fatalf("setup: %d %s", code, body)
	}
	json.Unmarshal(body, &setup)
	if !strings.HasPrefix(setup.URI, "otpauth://totp/GamePanel:bob%40example.com?secret="+setup.Secret) {
		t.Fatalf("bad uri %s", setup.URI)
	}
	// not enforced until confirmed
	if c := e.client().login(email, pw); c != 200 {
		t.Fatalf("2FA enforced before confirmation: %d", c)
	}
	if code, _ := a.do("POST", "/api/auth/totp/enable", map[string]string{"code": "000000"}, nil); code != 422 {
		t.Fatalf("enable with wrong code: %d", code)
	}
	var rec struct{ Codes []string }
	code, body = a.do("POST", "/api/auth/totp/enable", map[string]string{"code": totpAt(t, setup.Secret, 0)}, nil)
	if code != 200 {
		t.Fatalf("enable: %d %s", code, body)
	}
	json.Unmarshal(body, &rec)
	if len(rec.Codes) != 10 {
		t.Fatalf("want 10 recovery codes, got %d", len(rec.Codes))
	}

	login := func(c *cl, extra string) (int, string) {
		m := map[string]string{"email": email, "password": pw}
		if extra != "" {
			m["totp_code"] = extra
		}
		code, b := c.do("POST", "/api/auth/login", m, nil)
		return code, string(b)
	}
	// password alone is no longer enough, and the client is told why
	if code, b := login(e.client(), ""); code != 401 || !strings.Contains(b, "totp_required") {
		t.Fatalf("missing code: %d %s", code, b)
	}
	if code, _ := login(e.client(), "000000"); code != 401 {
		t.Fatalf("wrong code: %d", code)
	}
	// Enrolment used the current step; the next step's code is within the skew window.
	good := totpAt(t, setup.Secret, 1)
	b := e.client()
	if code, body := login(b, good); code != 200 {
		t.Fatalf("valid code: %d %s", code, body)
	}
	if code, _ := login(e.client(), good); code != 401 {
		t.Fatal("a TOTP code was accepted twice")
	}
	if code, body := b.do("GET", "/api/auth/me", nil, nil); code != 200 || !strings.Contains(string(body), `"totp_enabled":true`) {
		t.Fatalf("me: %d %s", code, body)
	}
	// recovery code works exactly once
	if code, _ := login(e.client(), rec.Codes[0]); code != 200 {
		t.Fatal("recovery code rejected")
	}
	if code, _ := login(e.client(), rec.Codes[0]); code != 401 {
		t.Fatal("recovery code reused")
	}
	// disabling needs password and a code
	off := func(p, c string) int {
		code, _ := b.do("POST", "/api/auth/totp/disable", map[string]string{"password": p, "code": c}, nil)
		return code
	}
	if c := off(pw, "000000"); c != 403 {
		t.Fatalf("disable with bad code: %d", c)
	}
	if c := off("wrong-wrong-wrong", rec.Codes[1]); c != 403 {
		t.Fatalf("disable with bad password: %d", c)
	}
	if c := off(pw, rec.Codes[1]); c != 204 {
		t.Fatalf("disable: %d", c)
	}
	if c := e.client().login(email, pw); c != 200 {
		t.Fatalf("login after disable: %d", c)
	}
}

func TestWrongSecondFactorCountsTowardLockout(t *testing.T) {
	e := newEnv(t, "")
	const email, pw = "bob@example.com", "another-long-password"
	a := e.client()
	a.login(email, pw)
	var setup struct{ Secret string }
	_, body := a.do("POST", "/api/auth/totp/setup", map[string]string{"password": pw}, nil)
	json.Unmarshal(body, &setup)
	a.do("POST", "/api/auth/totp/enable", map[string]string{"code": totpAt(t, setup.Secret, 0)}, nil)
	got429 := false
	for i := 0; i < 40 && !got429; i++ {
		// rotate IPs so only the per-account limiter can stop this
		code, _ := e.client().do("POST", "/api/auth/login", map[string]string{"email": email, "password": pw, "totp_code": "000000"}, map[string]string{"X-Forwarded-For": fmt.Sprintf("10.9.%d.1", i)})
		got429 = code == 429
	}
	if !got429 {
		t.Fatal("6-digit codes can be brute-forced without limit")
	}
}

func TestAPIKeys(t *testing.T) {
	e := newEnv(t, "")
	const pw = "another-long-password"
	a := e.client()
	a.login("bob@example.com", pw)

	var created struct {
		Key   struct{ ID int64 }
		Token string
	}
	// A key outlives the session that made it, so making one asks for the password.
	if code, _ := a.do("POST", "/api/auth/keys", map[string]any{"name": "ci"}, nil); code != 422 {
		t.Fatalf("create without password: %d", code)
	}
	if code, _ := a.do("POST", "/api/auth/keys", map[string]any{"name": "ci", "password": "wrong-wrong-wrong"}, nil); code != 403 {
		t.Fatalf("create with wrong password: %d", code)
	}
	code, body := a.do("POST", "/api/auth/keys", map[string]any{"name": "ci", "password": pw}, nil)
	if code != 201 {
		t.Fatalf("create: %d %s", code, body)
	}
	json.Unmarshal(body, &created)
	if !strings.HasPrefix(created.Token, "gp_") || len(created.Token) < 40 {
		t.Fatalf("token shape %q", created.Token)
	}
	// the secret is never listed again
	if _, body := a.do("GET", "/api/auth/keys", nil, nil); strings.Contains(string(body), created.Token) {
		t.Fatal("list leaks the token")
	}

	bearer := map[string]string{"Authorization": "Bearer " + created.Token}
	k := e.client() // no cookie at all
	if code, _ := k.do("GET", "/api/servers", nil, bearer); code != 200 {
		t.Fatalf("key rejected: %d", code)
	}
	if code, _ := k.do("GET", "/api/servers", nil, map[string]string{"Authorization": "Bearer gp_deadbeef"}); code != 401 {
		t.Fatalf("bogus key: %d", code)
	}
	// a leaked key must not escalate: no new keys, no password change, no 2FA
	if code, _ := k.do("POST", "/api/auth/keys", map[string]any{"name": "evil", "password": pw}, bearer); code != 403 {
		t.Fatalf("key minted a key: %d", code)
	}
	if code, _ := k.do("POST", "/api/auth/password", map[string]string{"current_password": pw, "new_password": "brand-new-password-1"}, bearer); code != 403 {
		t.Fatalf("key changed the password: %d", code)
	}
	if code, _ := k.do("POST", "/api/auth/totp/setup", map[string]string{"password": pw}, bearer); code != 403 {
		t.Fatalf("key started 2FA setup: %d", code)
	}
	// another user cannot delete it
	other := e.client()
	other.login("admin@example.com", "correct-horse-battery")
	if code, _ := other.do("DELETE", fmt.Sprintf("/api/auth/keys/%d", created.Key.ID), nil, nil); code != 404 {
		t.Fatalf("foreign delete: %d", code)
	}
	if code, _ := a.do("DELETE", fmt.Sprintf("/api/auth/keys/%d", created.Key.ID), nil, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := k.do("GET", "/api/servers", nil, bearer); code != 401 {
		t.Fatalf("revoked key still works: %d", code)
	}
}
