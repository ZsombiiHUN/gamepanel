package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSymlinkEscapeBlocked(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	os.WriteFile(secret, []byte("top secret"), 0o600)

	base := t.TempDir()
	data := filepath.Join(base, "data")
	d, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// A hostile server plants symlinks pointing outside its directory.
	os.Symlink(secret, filepath.Join(data, "link-file"))
	os.Symlink(outside, filepath.Join(data, "link-dir"))

	if b, err := d.Read("link-file"); err == nil {
		t.Fatalf("read through symlink succeeded: %q", b)
	}
	if _, err := d.List("link-dir"); err == nil {
		t.Fatal("list through dir symlink succeeded")
	}
	if err := d.Write("link-dir/pwned", []byte("x")); err == nil {
		t.Fatal("write through dir symlink succeeded")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned")); err == nil {
		t.Fatal("file escaped the root")
	}
	if err := d.Delete("link-dir/secret.txt"); err == nil {
		t.Fatal("delete through symlink succeeded")
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatal("secret outside root was deleted")
	}
}

func TestTraversalAndRoot(t *testing.T) {
	d, _ := Open(filepath.Join(t.TempDir(), "data"))
	defer d.Close()

	// ../ is normalised to stay inside; it must never touch the parent.
	if err := d.Write("../../../escape.txt", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read("/escape.txt"); err != nil {
		t.Fatal("normalised path should land inside root:", err)
	}
	if err := d.Delete("/"); err == nil {
		t.Fatal("deleting the root must fail")
	}
}

func TestRoundTrip(t *testing.T) {
	d, _ := Open(filepath.Join(t.TempDir(), "data"))
	defer d.Close()
	if err := d.Write("plugins/a.yml", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	b, err := d.Read("plugins/a.yml")
	if err != nil || string(b) != "hi" {
		t.Fatalf("got %q %v", b, err)
	}
	l, _ := d.List("plugins")
	if len(l) != 1 || l[0].Name != "a.yml" {
		t.Fatalf("bad list %+v", l)
	}
}

func FuzzClean(f *testing.F) {
	for _, s := range []string{"/", "..", "../../x", "a/../../b", "\x00", "a//b"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out := clean(s)
		if filepath.IsAbs(out) || out == ".." || (len(out) > 2 && out[:3] == "../") {
			t.Fatalf("clean(%q)=%q escapes", s, out)
		}
	})
}
