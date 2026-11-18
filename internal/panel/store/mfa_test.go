package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA-1 secret "12345678901234567890", 6-digit truncation
// of the published 8-digit values.
func TestTOTPRFC6238Vectors(t *testing.T) {
	t.Parallel()
	secret := []byte("12345678901234567890")
	for ts, want := range map[int64]string{59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037"} {
		if got := hotp(secret, uint64(ts/30)); got != want {
			t.Errorf("t=%d: got %s want %s", ts, got, want)
		}
	}
}

func TestVerifyTOTPWindowAndReplay(t *testing.T) {
	t.Parallel()
	secret := []byte("12345678901234567890")
	at := time.Unix(1111111109, 0)
	cur := at.Unix() / 30
	code := hotp(secret, uint64(cur))
	if step, ok := verifyTOTP(secret, code, at, 0); !ok || step != cur {
		t.Fatal("current code rejected")
	}
	if _, ok := verifyTOTP(secret, hotp(secret, uint64(cur-1)), at, 0); !ok {
		t.Fatal("previous step (clock skew) rejected")
	}
	if _, ok := verifyTOTP(secret, hotp(secret, uint64(cur+2)), at, 0); ok {
		t.Fatal("code two steps ahead accepted")
	}
	if _, ok := verifyTOTP(secret, code, at, cur); ok {
		t.Fatal("replay of an already-used step accepted")
	}
	if _, ok := verifyTOTP(secret, "12345", at, 0); ok {
		t.Fatal("short code accepted")
	}
}

func TestSecretsEncryptedAtRest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	key := make([]byte, 32)
	key[0] = 7
	s.SetSecretKey(key)
	u, _ := s.CreateUser(ctx, "a@example.com", "correct-horse-battery", false)
	secret, err := s.BeginTOTP(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	s.DB.QueryRow(`SELECT totp_secret FROM users WHERE id=?`, u.ID).Scan(&raw)
	if !strings.HasPrefix(raw, "gcm:") || strings.Contains(raw, secret) {
		t.Fatalf("secret not encrypted: %q", raw)
	}
	got, _, _, err := s.totpSecret(ctx, u.ID)
	if err != nil || string(got) == "" {
		t.Fatal(err)
	}
	// wrong key must not decrypt
	s.key = make([]byte, 32)
	if _, _, _, err := s.totpSecret(ctx, u.ID); err == nil {
		t.Fatal("decrypted with the wrong key")
	}
}

func TestRecoveryCodesAreSingleUse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := Open(filepath.Join(t.TempDir(), "p.db"))
	defer s.Close()
	u, _ := s.CreateUser(ctx, "a@example.com", "correct-horse-battery", false)
	codes, err := s.NewRecoveryCodes(ctx, u.ID)
	if err != nil || len(codes) != 10 {
		t.Fatal(err, len(codes))
	}
	if kind, err := s.VerifySecondFactor(ctx, u.ID, strings.ToUpper(codes[0])); err != nil || kind != "recovery" {
		t.Fatalf("first use: %v %s", err, kind)
	}
	if _, err := s.VerifySecondFactor(ctx, u.ID, codes[0]); err == nil {
		t.Fatal("recovery code accepted twice")
	}
	if _, err := s.VerifySecondFactor(ctx, u.ID, "aaaa-bbbb-cccc-dddd"); err == nil {
		t.Fatal("random code accepted")
	}
}
