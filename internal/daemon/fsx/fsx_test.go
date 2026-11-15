package fsx

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestArchiveRoundTripIgnoreAndSymlinks(t *testing.T) {
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("top secret"), 0o600)
	src := filepath.Join(t.TempDir(), "src")
	d, _ := Open(src)
	defer d.Close()
	d.Write("a.txt", []byte("A"))
	d.Write("world/level.dat", []byte("LEVEL"))
	d.Write("logs/latest.log", []byte("noise"))
	d.Write("cache/x/y.bin", []byte("noise"))
	os.Symlink(outside, filepath.Join(src, "link-dir"))
	os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(src, "link-file"))

	var buf bytes.Buffer
	if err := d.Archive(&buf, []string{"logs", "cache/", "*.tmp"}); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "dst")
	e, _ := Open(dst)
	defer e.Close()
	if err := e.Extract(&buf, false, 0); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"a.txt", "world/level.dat"} {
		if _, err := os.Stat(filepath.Join(dst, p)); err != nil {
			t.Errorf("%s missing from restore: %v", p, err)
		}
	}
	for _, p := range []string{"logs", "cache", "link-dir", "link-file"} {
		if _, err := os.Lstat(filepath.Join(dst, p)); err == nil {
			t.Errorf("%s must not be in the backup", p)
		}
	}
}

func TestExtractRejectsEscapesAndBombs(t *testing.T) {
	mk := func(entries map[string]string) *bytes.Buffer {
		var b bytes.Buffer
		gz := gzip.NewWriter(&b)
		tw := tar.NewWriter(gz)
		for n, c := range entries {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: n, Size: int64(len(c)), Mode: 0o644})
			tw.Write([]byte(c))
		}
		tw.Close()
		gz.Close()
		return &b
	}
	base := t.TempDir()
	d, _ := Open(filepath.Join(base, "data"))
	defer d.Close()
	for _, name := range []string{"../evil.txt", "/abs.txt", "a/../../evil.txt"} {
		if err := d.Extract(mk(map[string]string{name: "x"}), false, 0); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "evil.txt")); err == nil {
		t.Fatal("file escaped the root")
	}
	// symlink + file written through it must not escape either
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeSymlink, Name: "ln", Linkname: base})
	tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "ln/pwned", Size: 1, Mode: 0o644})
	tw.Write([]byte("x"))
	tw.Close()
	gz.Close()
	d.Extract(&b, false, 0)
	if _, err := os.Stat(filepath.Join(base, "pwned")); err == nil {
		t.Fatal("wrote through an archived symlink")
	}
	if err := d.Extract(mk(map[string]string{"big": strings.Repeat("a", 1000)}), false, 100); !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("size cap not enforced: %v", err)
	}
	// truncate empties the directory first
	d.Write("old.txt", []byte("old"))
	if err := d.Extract(mk(map[string]string{"new.txt": "n"}), true, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "data", "old.txt")); err == nil {
		t.Fatal("truncate left old files")
	}
}
