package store

import (
	"strings"
	"testing"
	"time"
)

func TestValidatePassword(t *testing.T) {
	t.Parallel()
	const email = "Gabriella.Kovacs@example.com"
	for pw, ok := range map[string]bool{
		"a-perfectly-fine-passphrase": true,
		"short-pw":                    false,
		strings.Repeat("x", 257):      false,
		// Length is counted in characters: 12 CJK characters are 36 bytes,
		// 11 are not enough even though they are 33 bytes.
		"東京大阪名古屋札幌福岡神": true,
		"東京大阪名古屋札幌福岡":  false,
		// Known leaked passwords, in any case.
		"q1w2e3r4t5y6": false,
		"Q1W2E3R4T5Y6": false,
		"1qaz2wsx3edc": false,
		// Mostly the account's own email or the site's name...
		"gabriella.kovacs-2026": false,
		"GamePanel2026!":        false,
		// ...but a long passphrase that happens to contain them is fine.
		"gabriella-likes-long-passphrases": true,
		"another-long-password":            true, // contains "other"
	} {
		if err := ValidatePassword(pw, email); (err == nil) != ok {
			t.Errorf("ValidatePassword(%q): err=%v, want ok=%v", pw, err, ok)
		}
	}
}

func TestCommonPasswordListLoaded(t *testing.T) {
	t.Parallel()
	if n := len(commonPasswords()); n < 10000 {
		t.Fatalf("only %d common passwords loaded", n)
	}
	for p := range commonPasswords() {
		if len([]rune(p)) < MinPasswordLen {
			t.Fatalf("%q is shorter than the length rule; the list should not carry it", p)
		}
	}
}

func TestMFALockSchedule(t *testing.T) {
	t.Parallel()
	for n, want := range map[int64]time.Duration{
		1: 0, 4: 0,
		5: time.Minute, 6: 2 * time.Minute, 10: 32 * time.Minute,
		16: mfaMaxLock, 100: mfaMaxLock, 1 << 40: mfaMaxLock,
	} {
		if got := mfaLockFor(n); got != want {
			t.Errorf("mfaLockFor(%d) = %v, want %v", n, got, want)
		}
	}
	// Over a year, someone with the password gets a few hundred guesses, not
	// the ~700,000 the in-memory per-account limit alone would allow.
	var elapsed time.Duration
	guesses := 0
	for n := int64(1); elapsed < 365*24*time.Hour; n++ {
		guesses++
		elapsed += mfaLockFor(n)
	}
	if guesses > 400 {
		t.Fatalf("%d guesses a year", guesses)
	}
}
