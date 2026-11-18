package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// The panel's side of SFTP sign-in: /daemon/sftp-auth, called by a node's
// daemon with its node token.

func (s *stack) sftpAuth(token string, req proto.SFTPAuthReq) (proto.SFTPAuthResp, int) {
	s.t.Helper()
	b, _ := json.Marshal(req)
	return s.sftpAuthRaw(token, b)
}

func (s *stack) sftpAuthRaw(token string, body []byte) (proto.SFTPAuthResp, int) {
	s.t.Helper()
	hr, _ := http.NewRequest("POST", s.admin.base+"/daemon/sftp-auth", bytes.NewReader(body))
	if token != "" {
		hr.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(hr)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	var r proto.SFTPAuthResp
	if resp.StatusCode == 200 {
		json.NewDecoder(resp.Body).Decode(&r)
	}
	return r, resp.StatusCode
}

func TestSFTPAuthEndpoint(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	pre := s.server[:8]
	key := s.apiKey(s.admin, "correct-horse-battery")
	login := func(user, pw string) proto.SFTPAuthReq {
		return proto.SFTPAuthReq{Username: user, Password: pw, IP: "203.0.113.1"}
	}
	good := login("admin@example.com."+pre, key)

	if r, code := s.sftpAuth(s.token, good); code != 200 || !r.OK || r.ServerUUID != s.server || !r.Read || !r.Write {
		t.Fatalf("owner login: %d %+v", code, r)
	}
	// only a node may ask
	for name, tok := range map[string]string{"no token": "", "wrong token": "nope", "an API key": key} {
		if _, code := s.sftpAuth(tok, good); code != 401 {
			t.Errorf("%s: status %d, want 401", name, code)
		}
	}
	// another node cannot authorise access to this node's servers
	var n2 struct{ Token string }
	json.Unmarshal(s.admin.do("POST", "/api/nodes", map[string]any{"name": "n2", "port_start": 30000, "port_end": 30005}, 201), &n2)
	if r, code := s.sftpAuth(n2.Token, good); code != 200 || r.OK {
		t.Fatalf("another node's token: %d %+v", code, r)
	}
	// a request too big to be a login
	big, _ := json.Marshal(login("admin@example.com."+pre, strings.Repeat("a", 8<<10)))
	if _, code := s.sftpAuthRaw(s.token, big); code != 400 {
		t.Errorf("oversized request: status %d", code)
	}
	if _, code := s.sftpAuthRaw(s.token, []byte("{not json")); code != 400 {
		t.Errorf("malformed request: status %d", code)
	}

	// the e-mail part is case-insensitive; the prefix is lower-case hex
	if r, _ := s.sftpAuth(s.token, login("ADMIN@Example.COM."+pre, key)); !r.OK {
		t.Error("e-mail case mattered")
	}
	bad := map[string]string{
		"short prefix":      "admin@example.com." + pre[:7],
		"long prefix":       "admin@example.com." + s.server[:9],
		"no prefix":         "admin@example.com",
		"prefix only":       "." + pre,
		"empty":             "",
		"wildcard prefix":   "admin@example.com.%%%%%%%%",
		"underscore prefix": "admin@example.com.________",
		"other e-mail":      "someone@example.com." + pre,
	}
	// A server id's prefix can be all digits; upper-casing it then changes
	// nothing and proves nothing.
	if up := strings.ToUpper(pre); up != pre {
		bad["upper-case prefix"] = "admin@example.com." + up
	}
	for name, user := range bad {
		if r, _ := s.sftpAuth(s.token, login(user, key)); r.OK {
			t.Errorf("%s (%q): accepted", name, user)
		}
	}
	for name, pw := range map[string]string{"account password": "correct-horse-battery", "empty": "", "made-up key": "gp_" + strings.Repeat("0", 64)} {
		if r, _ := s.sftpAuth(s.token, login("admin@example.com."+pre, pw)); r.OK {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSFTPAuthEndpointPermissions(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	pre := s.server[:8]
	s.st.CreateUser(s.ctx, "bob@example.com", "another-long-password", false)
	jar, _ := cookiejar.New(nil)
	bob := &client{t, s.admin.base, &http.Client{Jar: jar}}
	bob.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "another-long-password"}, 200)
	key := s.apiKey(bob, "another-long-password")
	ask := func() proto.SFTPAuthResp {
		r, _ := s.sftpAuth(s.token, proto.SFTPAuthReq{Username: "bob@example.com." + pre, Password: key, IP: "203.0.113.2"})
		return r
	}
	if ask().OK {
		t.Fatal("a stranger was let in")
	}
	s.admin.do("POST", "/api/servers/"+s.server+"/users", map[string]any{"email": "bob@example.com", "permissions": []string{"files.write"}}, 201)
	if r := ask(); !r.OK || r.Read || !r.Write {
		t.Fatalf("write-only subuser: %+v", r)
	}
	var users []struct {
		UserID int64 `json:"user_id"`
	}
	json.Unmarshal(s.admin.do("GET", "/api/servers/"+s.server+"/users", nil, 200), &users)
	if len(users) != 1 {
		t.Fatalf("%d subusers", len(users))
	}
	s.admin.do("PUT", "/api/servers/"+s.server+"/users/"+itoa(users[0].UserID), map[string]any{"permissions": []string{"files.read", "console.read"}}, 204)
	if r := ask(); !r.OK || !r.Read || r.Write {
		t.Fatalf("read-only subuser: %+v", r)
	}
	s.admin.do("PUT", "/api/servers/"+s.server+"/users/"+itoa(users[0].UserID), map[string]any{"permissions": []string{"console.read"}}, 204)
	if ask().OK {
		t.Fatal("a subuser without file permissions was let in")
	}
}

func TestSFTPAuthEndpointGuessing(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	pre := s.server[:8]
	key := s.apiKey(s.admin, "correct-horse-battery")
	user := "admin@example.com." + pre
	ask := func(pw, ip string, recheck bool) bool {
		r, code := s.sftpAuth(s.token, proto.SFTPAuthReq{Username: user, Password: pw, IP: ip, Recheck: recheck})
		if code != 200 {
			t.Fatalf("status %d", code)
		}
		return r.OK
	}

	// Rechecks never count as guesses, however many fail.
	for range 30 {
		ask("gp_wrong", "198.51.100.1", true)
	}
	if !ask(key, "198.51.100.1", false) {
		t.Fatal("failed rechecks locked the account")
	}

	// Guesses from many addresses lock the account (for sign-ins only).
	for i := range 25 {
		ask("gp_wrong", "198.51.100."+itoa(int64(10+i)), false)
	}
	if ask(key, "198.51.100.99", false) {
		t.Fatal("the account was not locked after many wrong keys")
	}
	// an open session's recheck still goes through: guessing at an account
	// must not cut its owner's transfers
	if !ask(key, "198.51.100.99", true) {
		t.Fatal("guessing cut the owner's open session")
	}

	var fails, logins []any
	json.Unmarshal(s.admin.do("GET", "/api/audit?q=sftp.fail", nil, 200), &fails)
	json.Unmarshal(s.admin.do("GET", "/api/audit?q=sftp.login", nil, 200), &logins)
	if len(fails) < 20 {
		t.Errorf("%d failed sign-ins audited", len(fails))
	}
	if len(logins) != 1 {
		t.Errorf("%d sign-ins audited, want 1 (rechecks are not sign-ins)", len(logins))
	}
}

func TestSFTPAuthEndpointPerAddressLimit(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	pre := s.server[:8]
	key := s.apiKey(s.admin, "correct-horse-battery")
	// one address guessing at many accounts
	for i := range 25 {
		s.sftpAuth(s.token, proto.SFTPAuthReq{Username: "u" + itoa(int64(i)) + "@example.com." + pre, Password: "gp_x", IP: "192.0.2.7"})
	}
	if r, _ := s.sftpAuth(s.token, proto.SFTPAuthReq{Username: "admin@example.com." + pre, Password: key, IP: "192.0.2.7"}); r.OK {
		t.Fatal("an address that kept guessing was let in")
	}
	if r, _ := s.sftpAuth(s.token, proto.SFTPAuthReq{Username: "admin@example.com." + pre, Password: key, IP: "192.0.2.8"}); !r.OK {
		t.Fatal("another address was locked out too")
	}
}
