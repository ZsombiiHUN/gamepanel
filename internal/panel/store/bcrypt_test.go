package store

import (
	"context"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// laravelHash is the hash of "password" that Laravel's own user factory
// ships, made by PHP's password_hash: a real "$2y$" hash, not one this
// package made.
const laravelHash = "$2y$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi"

// importedUser stores a user the way the Pterodactyl import will: with the
// bcrypt hash as it was.
func importedUser(t *testing.T, s *Store, email, hash string) int64 {
	t.Helper()
	res, err := s.DB.Exec(`INSERT INTO users(email,password_hash,is_admin,created_at) VALUES(?,?,0,?)`, email, hash, now())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func storedHash(t *testing.T, s *Store, id int64) string {
	t.Helper()
	var h string
	if err := s.DB.QueryRow(`SELECT password_hash FROM users WHERE id=?`, id).Scan(&h); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestImportedBcryptSignInUpgradesTheHash(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	id := importedUser(t, s, "ptero@b.co", laravelHash)

	if _, err := s.Login(ctx, "ptero@b.co", "Password"); err == nil {
		t.Fatal("wrong password accepted")
	}
	if got := storedHash(t, s, id); got != laravelHash {
		t.Fatalf("a failed sign-in changed the hash: %q", got)
	}
	if _, err := s.Login(ctx, "ptero@b.co", "password"); err != nil {
		t.Fatalf("Pterodactyl password refused: %v", err)
	}
	if got := storedHash(t, s, id); !strings.HasPrefix(got, "argon2id$") {
		t.Fatalf("hash not upgraded after sign-in: %q", got)
	}
	// The same password still works against the new hash.
	if _, err := s.Login(ctx, "ptero@b.co", "password"); err != nil {
		t.Fatalf("sign-in after upgrade: %v", err)
	}
}

func TestImportedBcryptCheckPasswordUpgrades(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	id := importedUser(t, s, "ptero@b.co", laravelHash)
	if s.CheckPassword(ctx, id, "nope") {
		t.Fatal("wrong password accepted")
	}
	if !s.CheckPassword(ctx, id, "password") {
		t.Fatal("Pterodactyl password refused")
	}
	if got := storedHash(t, s, id); !strings.HasPrefix(got, "argon2id$") {
		t.Fatalf("hash not upgraded: %q", got)
	}
}

func TestBcryptReadsOnlyTheFirst72Bytes(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 72) + "-ignored-by-php"
	h, err := bcrypt.GenerateFromPassword([]byte(long[:72]), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	// PHP writes $2y$; make the fixture look like it did.
	enc := "$2y$" + string(h[4:])
	if !checkPassword(long, enc) {
		t.Fatal("password longer than 72 bytes refused; PHP would accept it")
	}
	if checkPassword(long[:71], enc) {
		t.Fatal("a different password accepted")
	}
}

func TestBcryptRefusesBadHashes(t *testing.T) {
	t.Parallel()
	for name, enc := range map[string]string{
		// Cost 31 would take minutes; it is refused without hashing.
		"huge cost":  "$2y$31$" + laravelHash[7:],
		"truncated":  laravelHash[:20],
		"not bcrypt": "$1$abc$def",
		"oversized":  "$2y$10$" + strings.Repeat("a", maxHashLen),
	} {
		if checkPassword("password", enc) {
			t.Errorf("%s: accepted", name)
		}
	}
}
