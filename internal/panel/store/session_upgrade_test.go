package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// enrolTOTP turns two-factor on for a fresh user and returns its id, secret
// and recovery codes.
func enrolTOTP(t *testing.T, s *Store) (int64, []byte, []string) {
	t.Helper()
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "a@example.com", "correct-horse-battery", false)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := s.BeginTOTP(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := b32.DecodeString(enc)
	rec, err := s.EnableTOTP(ctx, u.ID, hotp(secret, uint64(time.Now().Unix()/30)))
	if err != nil {
		t.Fatal(err)
	}
	return u.ID, secret, rec
}

// A burst of parallel wrong codes gets exactly the free guesses and no more:
// the requests cannot all pass the delay check before one records a failure.
func TestTOTPLockoutHoldsUnderConcurrency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, secret, rec := enrolTOTP(t, s)

	const tries = 40
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, tries)
	for range tries {
		wg.Go(func() {
			<-start
			_, err := s.VerifySecondFactor(ctx, id, "000000")
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	checked, locked := 0, 0
	for err := range results {
		switch {
		case errors.Is(err, ErrSecondFactorLocked):
			locked++
		case err != nil:
			checked++
		default:
			t.Fatal("wrong code accepted")
		}
	}
	if checked != mfaFreeFailures || locked != tries-mfaFreeFailures {
		t.Fatalf("%d codes checked, %d refused as locked; want %d and %d", checked, locked, mfaFreeFailures, tries-mfaFreeFailures)
	}
	var fails, until int64
	s.DB.QueryRow(`SELECT mfa_fail_count, mfa_locked_until FROM users WHERE id=?`, id).Scan(&fails, &until)
	if fails != mfaFreeFailures {
		t.Fatalf("fail count %d, want %d", fails, mfaFreeFailures)
	}
	if earliest := time.Now().Add(mfaLockFor(mfaFreeFailures) - 5*time.Second).Unix(); until < earliest {
		t.Fatalf("lock ends at %d, want at least %d", until, earliest)
	}

	// The right code waits too, in parallel or not.
	next := hotp(secret, uint64(time.Now().Unix()/30+1))
	if _, err := s.VerifySecondFactor(ctx, id, next); !errors.Is(err, ErrSecondFactorLocked) {
		t.Fatalf("right code during the delay: %v", err)
	}
	// A recovery code still works and clears the delay.
	if kind, err := s.VerifySecondFactor(ctx, id, rec[0]); err != nil || kind != "recovery" {
		t.Fatalf("recovery code during the delay: %q %v", kind, err)
	}
	if kind, err := s.VerifySecondFactor(ctx, id, next); err != nil || kind != "totp" {
		t.Fatalf("right code after the delay was cleared: %q %v", kind, err)
	}
	s.DB.QueryRow(`SELECT mfa_fail_count, mfa_locked_until FROM users WHERE id=?`, id).Scan(&fails, &until)
	if fails != 0 || until != 0 {
		t.Fatalf("a good code left count %d, lock %d", fails, until)
	}
	// And a code is still single use.
	if _, err := s.VerifySecondFactor(ctx, id, next); err == nil {
		t.Fatal("code replayed")
	}
}

// Migration 00010 caps sessions from before the 12-hour limit and marks them
// as allowed to move off the old cookie name once.
func TestSessionUpgradeMigration(t *testing.T) {
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
	if _, err := m.UpTo(ctx, 9); err != nil {
		t.Fatal(err)
	}
	n := now()
	db.Exec(`INSERT INTO users(id,email,password_hash,created_at) VALUES(1,'a@example.com','x',?)`, n)
	long, short, gone := "long-token", "short-token", "expired-token"
	for tok, exp := range map[string]int64{
		long:  n + 10*24*3600, // made under the old 14-day lifetime
		short: n + 3600,       // ends sooner than the cap
		gone:  n - 60,         // already over
	} {
		if _, err := db.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at,last_seen_at) VALUES(?,1,?,?)`, HashToken(tok), exp, n); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	exp := func(tok string) (e, legacy int64) {
		s.DB.QueryRow(`SELECT expires_at, legacy_cookie FROM sessions WHERE token_hash=?`, HashToken(tok)).Scan(&e, &legacy)
		return e, legacy
	}
	if e, l := exp(long); e > now()+12*3600 || e < n+12*3600-5 || l != 1 {
		t.Fatalf("10-day session: expires %d (now %d), legacy %d", e, n, l)
	}
	if e, _ := exp(short); e != n+3600 {
		t.Fatalf("session ending sooner was changed: %d, want %d", e, n+3600)
	}
	if _, err := s.UserBySession(ctx, gone, ""); err == nil {
		t.Fatal("expired session accepted after the migration")
	}
	if _, err := s.UserBySession(ctx, long, ""); err != nil {
		t.Fatalf("capped session rejected inside its grace period: %v", err)
	}
	// Past the grace period it is over.
	s.DB.Exec(`UPDATE sessions SET expires_at=expires_at-12*3600 WHERE token_hash=?`, HashToken(long))
	if _, err := s.UserBySession(ctx, long, ""); err == nil {
		t.Fatal("pre-upgrade session outlived the 12-hour cap")
	}

	// New sessions get the configured lifetimes and never the legacy mark.
	tok, err := s.NewSession(ctx, 1, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if e, l := exp(tok); e < now()+int64(s.SessionMax.Seconds())-5 || e > now()+int64(s.SessionMax.Seconds()) || l != 0 {
		t.Fatalf("new session: expires %d, legacy %d", e, l)
	}
	if _, err := s.ClaimLegacySession(ctx, tok); err == nil {
		t.Fatal("a new session was allowed onto the old cookie name")
	}
	// A pre-upgrade session moves once.
	if _, err := s.ClaimLegacySession(ctx, short); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if _, err := s.ClaimLegacySession(ctx, short); err == nil {
		t.Fatal("claimed twice")
	}
}
