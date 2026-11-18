package api_test

import (
	"encoding/json"
	"strings"
	"testing"
)

type sessionRow struct {
	ID        string `json:"id"`
	Current   bool   `json:"current"`
	CreatedAt int64  `json:"created_at"`
	IP        string `json:"ip"`
	LastIP    string `json:"last_ip"`
	UserAgent string `json:"user_agent"`
	Method    string `json:"method"`
}

func listSessions(t *testing.T, c *cl, hdr map[string]string) []sessionRow {
	t.Helper()
	code, body := c.do("GET", "/api/auth/sessions", nil, hdr)
	if code != 200 {
		t.Fatalf("list sessions: %d %s", code, body)
	}
	var out []sessionRow
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// ASVS v5.0.0-7.5.2: the account owner can see their live sessions and end
// any one of them.
func TestSessionListAndRevoke(t *testing.T) {
	e := newEnv(t, "127.0.0.1")
	laptop, phone := e.client(), e.client()
	login := func(c *cl, ip, ua string) {
		code, body := c.do("POST", "/api/auth/login", map[string]string{"email": bobEmail, "password": bobPW},
			map[string]string{"X-Forwarded-For": ip, "User-Agent": ua})
		if code != 200 {
			t.Fatalf("login: %d %s", code, body)
		}
	}
	login(laptop, "198.51.100.10", "Mozilla/5.0 (X11; Linux x86_64) Firefox/140.0")
	login(phone, "203.0.113.20", "Mozilla/5.0 (iPhone) Safari/604.1\tforged")

	hdr := map[string]string{"X-Forwarded-For": "198.51.100.10"}
	l := listSessions(t, laptop, hdr)
	if len(l) != 2 {
		t.Fatalf("want 2 sessions, got %+v", l)
	}
	if !l[0].Current || l[1].Current {
		t.Fatalf("own session must come first and be the only current one: %+v", l)
	}
	if l[0].IP != "198.51.100.10" || l[0].Method != "password" || l[0].CreatedAt == 0 || !strings.Contains(l[0].UserAgent, "Firefox") {
		t.Fatalf("laptop row: %+v", l[0])
	}
	other := l[1]
	if other.IP != "203.0.113.20" || strings.Contains(other.UserAgent, "\t") {
		t.Fatalf("phone row (control characters must be dropped): %+v", other)
	}
	if len(other.ID) != 32 {
		t.Fatalf("id shape %q", other.ID)
	}
	// The list never carries anything a session could be resumed with.
	_, raw := laptop.do("GET", "/api/auth/sessions", nil, hdr)
	var hashes int
	e.st.DB.QueryRow(`SELECT COUNT(*) FROM sessions WHERE instr(?, token_hash) > 0`, string(raw)).Scan(&hashes)
	if hashes != 0 {
		t.Fatal("session list leaks token hashes")
	}

	// Using a session from a new address updates where it was last seen.
	if code, _ := phone.do("GET", "/api/auth/me", nil, map[string]string{"X-Forwarded-For": "192.0.2.77"}); code != 200 {
		t.Fatal("phone signed out early")
	}
	if l := listSessions(t, laptop, hdr); l[1].LastIP != "192.0.2.77" || l[1].IP != "203.0.113.20" {
		t.Fatalf("last_ip not updated: %+v", l[1])
	}

	// Revoking your own session by id is refused: signing out does that.
	if code, _ := laptop.do("DELETE", "/api/auth/sessions/"+l[0].ID, nil, hdr); code != 409 {
		t.Fatalf("revoke current: got %d want 409", code)
	}
	// Someone else cannot end bob's session, even knowing its id.
	admin := e.client()
	admin.login("admin@example.com", "correct-horse-battery")
	if code, _ := admin.do("DELETE", "/api/auth/sessions/"+other.ID, nil, nil); code != 404 {
		t.Fatalf("revoke another user's session: got %d want 404", code)
	}
	if code, _ := phone.do("GET", "/api/auth/me", nil, nil); code != 200 {
		t.Fatal("phone must still be signed in")
	}
	if code, _ := laptop.do("DELETE", "/api/auth/sessions/"+other.ID, nil, hdr); code != 204 {
		t.Fatalf("revoke: got %d want 204", code)
	}
	if code, _ := phone.do("GET", "/api/auth/me", nil, nil); code != 401 {
		t.Fatalf("revoked session still works: %d", code)
	}
	if code, _ := laptop.do("DELETE", "/api/auth/sessions/"+other.ID, nil, hdr); code != 404 {
		t.Fatalf("revoke twice: got %d want 404", code)
	}
	if code, _ := laptop.do("DELETE", "/api/auth/sessions/not-an-id", nil, hdr); code != 422 {
		t.Fatalf("malformed id: got %d want 422", code)
	}
	if l := listSessions(t, laptop, hdr); len(l) != 1 || !l[0].Current {
		t.Fatalf("after revoke: %+v", l)
	}
	var n int
	e.st.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='sessions.revoke'`).Scan(&n)
	if n != 1 {
		t.Fatalf("revoke not audited once: %d", n)
	}
}

// An API key acts as the user but cannot see or end their sessions.
func TestSessionListNeedsBrowserSession(t *testing.T) {
	e := newEnv(t, "")
	a := e.client()
	a.login(bobEmail, bobPW)
	code, body := a.do("POST", "/api/auth/keys", map[string]any{"name": "ci", "password": bobPW}, nil)
	if code != 201 {
		t.Fatalf("create key: %d %s", code, body)
	}
	var k struct{ Token string }
	json.Unmarshal(body, &k)
	bearer := map[string]string{"Authorization": "Bearer " + k.Token}
	id := listSessions(t, a, nil)[0].ID
	if code, _ := e.client().do("GET", "/api/auth/sessions", nil, bearer); code != 403 {
		t.Fatalf("list with API key: got %d want 403", code)
	}
	if code, _ := e.client().do("DELETE", "/api/auth/sessions/"+id, nil, bearer); code != 403 {
		t.Fatalf("revoke with API key: got %d want 403", code)
	}
}

// Full-length recovery codes go through every endpoint that takes a code,
// and the account can see how many are left.
func TestRecoveryCodesThroughTheAPI(t *testing.T) {
	e := newEnv(t, "")
	a := e.client()
	a.login(bobEmail, bobPW)
	_, codes := enrolTOTP(t, a, bobPW)
	if len(codes) != 10 || len(codes[0]) != 29 {
		t.Fatalf("codes %q", codes)
	}
	status := func() (st struct {
		Remaining int
		Outdated  bool
	}) {
		code, body := a.do("GET", "/api/auth/totp/recovery-codes", nil, nil)
		if code != 200 {
			t.Fatalf("status: %d %s", code, body)
		}
		json.Unmarshal(body, &st)
		return
	}
	if st := status(); st.Remaining != 10 || st.Outdated {
		t.Fatalf("status %+v", st)
	}
	c := e.client()
	if code, body := c.do("POST", "/api/auth/login", map[string]string{"email": bobEmail, "password": bobPW, "totp_code": strings.ToUpper(codes[0])}, nil); code != 200 {
		t.Fatalf("login with recovery code: %d %s", code, body)
	}
	if l := listSessions(t, c, nil); l[0].Method != "password+recovery" {
		t.Fatalf("method %q", l[0].Method)
	}
	if code, body := a.do("POST", "/api/auth/password", map[string]string{"current_password": bobPW, "new_password": "a-whole-new-password", "code": codes[1]}, nil); code != 204 {
		t.Fatalf("password change with recovery code: %d %s", code, body)
	}
	if st := status(); st.Remaining != 8 {
		t.Fatalf("after two uses: %+v", st)
	}
	if code, _ := e.client().do("GET", "/api/auth/totp/recovery-codes", nil, nil); code != 401 {
		t.Fatalf("status signed out: %d", code)
	}
}
