package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClip(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in, want string
		n        int
	}{
		{"plain", "plain", 10},
		{"a\x00b\nc\x7fd\te", "abcde", 10},
		{"bad\xffutf8", "badutf8", 10},
		{"ééé", "é", 3}, // never splits a two-byte rune
	} {
		if got := clip(c.in, c.n); got != c.want {
			t.Errorf("clip(%q,%d) = %q want %q", c.in, c.n, got, c.want)
		}
	}
	long := clip(strings.Repeat("ü", 1000), maxUALen)
	if len(long) > maxUALen || !utf8.ValidString(long) {
		t.Fatalf("long user agent: %d bytes, valid=%v", len(long), utf8.ValidString(long))
	}
}

func TestSessionListIsPerUserAndSkipsDeadSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	a, _ := s.CreateUser(ctx, "a@b.co", "a-long-enough-password", false)
	b, _ := s.CreateUser(ctx, "b@b.co", "a-long-enough-password", false)
	mine, _ := s.NewSession(ctx, a.ID, SessionMeta{IP: "198.51.100.1", Method: MethodPassword})
	s.NewSession(ctx, a.ID, SessionMeta{IP: "198.51.100.2", Method: MethodPasskey})
	expired, _ := s.NewSession(ctx, a.ID, SessionMeta{})
	idle, _ := s.NewSession(ctx, a.ID, SessionMeta{})
	theirs, _ := s.NewSession(ctx, b.ID, SessionMeta{})
	s.DB.Exec(`UPDATE sessions SET expires_at=1 WHERE token_hash=?`, HashToken(expired))
	s.DB.Exec(`UPDATE sessions SET last_seen_at=1 WHERE token_hash=?`, HashToken(idle))

	l, err := s.ListSessions(ctx, a.ID, mine)
	if err != nil {
		t.Fatal(err)
	}
	if len(l) != 2 || !l[0].Current || l[0].IP != "198.51.100.1" || l[1].Method != MethodPasskey {
		t.Fatalf("list: %+v", l)
	}
	if l[0].IdleEnds > l[0].ExpiresAt || l[0].IdleEnds <= l[0].LastSeen {
		t.Fatalf("idle end out of range: %+v", l[0])
	}
	var theirID string
	s.DB.QueryRow(`SELECT public_id FROM sessions WHERE token_hash=?`, HashToken(theirs)).Scan(&theirID)
	if err := s.DeleteSessionByID(ctx, a.ID, theirID, mine); err != ErrNotFound {
		t.Fatalf("deleting another user's session: %v", err)
	}
	if err := s.DeleteSessionByID(ctx, a.ID, "", mine); err != ErrNotFound {
		t.Fatalf("empty id must not match: %v", err)
	}
	if err := s.DeleteSessionByID(ctx, a.ID, l[0].ID, mine); err != ErrCurrentSession {
		t.Fatalf("deleting own session: %v", err)
	}
	if err := s.DeleteSessionByID(ctx, a.ID, l[1].ID, mine); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.ListSessions(ctx, a.ID, mine); len(l) != 1 {
		t.Fatalf("after delete: %+v", l)
	}
	if _, err := s.UserBySession(ctx, theirs, ""); err != nil {
		t.Fatal("other user's session was touched")
	}
}

// Migration 00114 gives sessions from before it a distinct public id, so each
// shows up in the list and can be revoked on its own.
func TestSessionDetailsMigration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "p.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.UpTo(context.Background(), 113); err != nil {
		t.Fatal(err)
	}
	n := now()
	db.Exec(`INSERT INTO users(id,email,password_hash,created_at) VALUES(1,'a@example.com','x',?)`, n)
	for _, tok := range []string{"one", "two"} {
		if _, err := db.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at,last_seen_at) VALUES(?,1,?,?)`, HashToken(tok), n+3600, n); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	l, err := s.ListSessions(ctx, 1, "one")
	if err != nil {
		t.Fatal(err)
	}
	if len(l) != 2 || len(l[0].ID) != 32 || l[0].ID == l[1].ID || l[0].CreatedAt != 0 || !l[0].Current {
		t.Fatalf("pre-upgrade sessions: %+v", l)
	}
	if err := s.DeleteSessionByID(ctx, 1, l[1].ID, "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserBySession(ctx, "one", ""); err != nil {
		t.Fatal("revoking one pre-upgrade session ended the other")
	}
}

// Migration 00116 counts sessions as two-factor from their recorded method,
// or when the account has an authenticator; new sessions from their method.
func TestSessionMFAMigration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "p.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.UpTo(context.Background(), 115); err != nil {
		t.Fatal(err)
	}
	n := now()
	db.Exec(`INSERT INTO users(id,email,password_hash,created_at,totp_enabled) VALUES(1,'a@example.com','x',?,0),(2,'b@example.com','x',?,1)`, n, n)
	for tok, row := range map[string]struct {
		user   int
		method string
	}{
		"pw": {1, MethodPassword}, "pk": {1, MethodPasskey}, "sso": {1, "sso:GitHub"},
		"ssototp": {1, "sso+totp:GitHub"}, "old": {1, ""}, "totpuser": {2, ""},
	} {
		if _, err := db.Exec(`INSERT INTO sessions(token_hash,public_id,user_id,expires_at,last_seen_at,method) VALUES(?,?,?,?,?,?)`,
			HashToken(tok), tok, row.user, n+3600, n, row.method); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	want := map[string]bool{"pw": false, "pk": true, "sso": false, "ssototp": true, "old": false, "totpuser": true}
	for tok, w := range want {
		u, err := s.UserBySession(ctx, tok, "")
		if err != nil || u.SessionMFA != w {
			t.Errorf("%s: mfa %v want %v (%v)", tok, u.SessionMFA, w, err)
		}
	}
	tok, _ := s.NewSession(ctx, 1, SessionMeta{Method: MethodTOTP})
	if u, _ := s.UserBySession(ctx, tok, ""); !u.SessionMFA {
		t.Fatal("password+totp session not marked")
	}
	tok, _ = s.NewSession(ctx, 1, SessionMeta{Method: MethodPassword})
	if u, _ := s.UserBySession(ctx, tok, ""); u.SessionMFA {
		t.Fatal("password session marked")
	}
	s.MarkSessionMFA(ctx, tok)
	if u, _ := s.UserBySession(ctx, tok, ""); !u.SessionMFA {
		t.Fatal("MarkSessionMFA did not stick")
	}
}
