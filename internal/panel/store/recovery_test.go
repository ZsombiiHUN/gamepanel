package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// ASVS v5.0.0-6.5.2 / 6.5.3: recovery codes stored with a plain hash must
// have at least 112 bits from a CSPRNG.
func TestRecoveryCodeStrength(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	u, _ := s.CreateUser(ctx, "a@example.com", "correct-horse-battery", false)
	codes, err := s.NewRecoveryCodes(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	shape := regexp.MustCompile(`^[a-z2-7]{4}(-[a-z2-7]{4}){5}$`)
	seen := map[string]bool{}
	for _, c := range codes {
		if !shape.MatchString(c) {
			t.Fatalf("code shape %q", c)
		}
		// 24 base32 characters = 120 bits.
		if bits := len(normalizeCode(c)) * 5; bits < 112 {
			t.Fatalf("%d bits", bits)
		}
		if seen[c] {
			t.Fatal("duplicate code")
		}
		seen[c] = true
	}
	var stored []string
	rows, _ := s.DB.Query(`SELECT code_hash FROM recovery_codes WHERE user_id=?`, u.ID)
	for rows.Next() {
		var h string
		rows.Scan(&h)
		stored = append(stored, h)
	}
	rows.Close()
	if len(stored) != RecoveryCodeCount || strings.Contains(strings.Join(stored, ","), strings.ReplaceAll(codes[0], "-", "")) {
		t.Fatalf("codes stored wrongly: %v", stored)
	}
	if st, _ := s.RecoveryCodeStatus(ctx, u.ID); st.Remaining != 10 || st.Outdated {
		t.Fatalf("status %+v", st)
	}

	// Typed however people type them: upper case, spaces, no dashes.
	for i, typed := range []string{
		strings.ToUpper(codes[0]),
		strings.ReplaceAll(codes[1], "-", " "),
		strings.ReplaceAll(codes[2], "-", ""),
		"  " + codes[3] + "\t",
	} {
		if kind, err := s.VerifySecondFactor(ctx, u.ID, typed); err != nil || kind != "recovery" {
			t.Fatalf("form %d %q: %v %s", i, typed, err, kind)
		}
	}
	if st, _ := s.RecoveryCodeStatus(ctx, u.ID); st.Remaining != 6 {
		t.Fatalf("after four uses: %+v", st)
	}
	// Wrong lengths never reach the database lookup, and a code is not a prefix match.
	for _, bad := range []string{codes[4][:19], codes[4] + "a", "", "aaaa-bbbb-cccc-dddd-eeee-ffff"} {
		if _, err := s.VerifySecondFactor(ctx, u.ID, bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	// Another account's code does not work here.
	v, _ := s.CreateUser(ctx, "b@example.com", "correct-horse-battery", false)
	if _, err := s.VerifySecondFactor(ctx, v.ID, codes[5]); err == nil {
		t.Fatal("code worked for another account")
	}
}

// Migration 00115 keeps 80-bit codes made before it working, once each, and
// marks them so the owner is asked to replace them. Making new codes removes
// them.
func TestLegacyRecoveryCodes(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "p.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(migrations)
	goose.SetDialect("sqlite3")
	goose.SetLogger(goose.NopLogger())
	if err := goose.UpTo(db, "migrations", 114); err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO users(id,email,password_hash,created_at) VALUES(1,'a@example.com','x',1)`)
	// How codes were made and stored before: 16 base32 characters in groups
	// of four, SHA-256 of the dashed form.
	old := []string{"abcd-efgh-ijkl-mnop", "qrst-uvwx-yz23-4567"}
	for _, c := range old {
		if _, err := db.Exec(`INSERT INTO recovery_codes(user_id,code_hash) VALUES(1,?)`, HashToken(c)); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if st, _ := s.RecoveryCodeStatus(ctx, 1); st.Remaining != 2 || !st.Outdated {
		t.Fatalf("status after upgrade: %+v", st)
	}
	if kind, err := s.VerifySecondFactor(ctx, 1, "ABCDEFGHIJKLMNOP"); err != nil || kind != "recovery" {
		t.Fatalf("legacy code typed without dashes: %v %s", err, kind)
	}
	if _, err := s.VerifySecondFactor(ctx, 1, old[0]); err == nil {
		t.Fatal("legacy code used twice")
	}
	if _, err := s.NewRecoveryCodes(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.VerifySecondFactor(ctx, 1, old[1]); err == nil {
		t.Fatal("legacy code survived new codes being made")
	}
	if st, _ := s.RecoveryCodeStatus(ctx, 1); st.Remaining != RecoveryCodeCount || st.Outdated {
		t.Fatalf("status after new codes: %+v", st)
	}
}
