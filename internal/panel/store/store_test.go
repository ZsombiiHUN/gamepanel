package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPasswordsAndSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	if _, err := s.CreateUser(ctx, "a@b.co", "short", false); err == nil {
		t.Fatal("short password accepted")
	}
	u, err := s.CreateUser(ctx, "a@b.co", "a-long-enough-password", true)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	s.DB.QueryRow(`SELECT password_hash FROM users WHERE id=?`, u.ID).Scan(&stored)
	if !strings.HasPrefix(stored, "argon2id$") || strings.Contains(stored, "a-long-enough-password") {
		t.Fatalf("password not hashed properly: %q", stored)
	}
	if _, err := s.Login(ctx, "a@b.co", "wrong-password-here"); err == nil {
		t.Fatal("wrong password accepted")
	}
	if _, err := s.Login(ctx, "nobody@b.co", "a-long-enough-password"); err == nil {
		t.Fatal("unknown user accepted")
	}
	got, err := s.Login(ctx, "a@b.co", "a-long-enough-password")
	if err != nil || !got.IsAdmin {
		t.Fatalf("login: %v %+v", err, got)
	}

	tok, _ := s.NewSession(ctx, got.ID, SessionMeta{})
	var rawStored string
	s.DB.QueryRow(`SELECT token_hash FROM sessions`).Scan(&rawStored)
	if rawStored == tok {
		t.Fatal("session token stored in plaintext")
	}
	if _, err := s.UserBySession(ctx, tok, ""); err != nil {
		t.Fatal(err)
	}
	s.DeleteSession(ctx, tok)
	if _, err := s.UserBySession(ctx, tok, ""); err != ErrNotFound {
		t.Fatalf("revoked session still valid: %v", err)
	}
}

func TestPortAllocation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	u, _ := s.CreateUser(ctx, "a@b.co", "a-long-enough-password", true)
	n, _, _ := s.CreateNode(ctx, "n", 25565, 25566)
	e, _ := s.CreateEgg(ctx, Egg{Name: "e", DockerImage: "i", Startup: "s", Stop: "^C"})
	mk := func(id string, port int) (Server, error) {
		return s.CreateServer(ctx, Server{UUID: id, Name: id, OwnerID: u.ID, NodeID: n.ID, EggID: e.ID, MemoryMB: 128, Port: port})
	}
	a, _ := mk("a", 0)
	b, _ := mk("b", 0)
	if a.Port != 25565 || b.Port != 25566 {
		t.Fatalf("ports %d %d", a.Port, b.Port)
	}
	if _, err := mk("c", 0); err == nil {
		t.Fatal("expected the range to be exhausted")
	}
	if _, err := mk("d", 80); err == nil {
		t.Fatal("port outside node range accepted")
	}
	if _, err := mk("e", 25565); err == nil {
		t.Fatal("duplicate port accepted")
	}
}

func TestNodeTokenLookup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	n, tok, _ := s.CreateNode(ctx, "n", 25565, 25570)
	got, err := s.NodeByToken(ctx, tok)
	if err != nil || got.ID != n.ID {
		t.Fatal("token lookup failed")
	}
	if _, err := s.NodeByToken(ctx, tok+"x"); err == nil {
		t.Fatal("bad token accepted")
	}
}
