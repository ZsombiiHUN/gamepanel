package store

import (
	"os"
	"path/filepath"
	"testing"
)

// The database holds password hashes and node tokens, so a new one must not be
// readable by other local users.
func TestNewDatabaseIsPrivate(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "panel.db")
	s, err := openT(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Fatalf("new database has mode %o, want 600", mode)
	}
}

// An operator's chosen mode on an existing database is kept.
func TestExistingDatabaseKeepsMode(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "panel.db")
	s, err := openT(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if s, err = openT(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fi.Mode().Perm(); mode != 0o640 {
		t.Fatalf("existing database mode changed to %o", mode)
	}
}
