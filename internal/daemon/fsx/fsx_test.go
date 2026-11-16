package fsx

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
	if err := e.Extract(bytes.NewReader(buf.Bytes()), false, 0); err != nil {
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
	mk := func(entries map[string]string) *bytes.Reader {
		var b bytes.Buffer
		gz := gzip.NewWriter(&b)
		tw := tar.NewWriter(gz)
		for n, c := range entries {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: n, Size: int64(len(c)), Mode: 0o644})
			tw.Write([]byte(c))
		}
		tw.Close()
		gz.Close()
		return bytes.NewReader(b.Bytes())
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
	d.Extract(bytes.NewReader(b.Bytes()), false, 0)
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

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for n, c := range files {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: n, Size: int64(len(c)), Mode: 0o644})
		tw.Write([]byte(c))
	}
	tw.Close()
	gz.Close()
	return b.Bytes()
}

// A restore that cannot finish must not have changed anything: the archive is
// checked before the directory is emptied or any file is written.
func TestExtractChangesNothingWhenItCannotFinish(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	d, _ := Open(data)
	defer d.Close()
	d.Write("world/level.dat", []byte("precious"))

	good := tarGz(t, map[string]string{"a.txt": strings.Repeat("a", 4000), "b.txt": strings.Repeat("b", 4000)})
	cases := map[string]struct {
		archive []byte
		max     int64
		want    error
	}{
		"cut short":   {good[:len(good)/2], 0, nil},
		"not gzip":    {[]byte("definitely not an archive"), 0, nil},
		"over limit":  {good, 5000, ErrArchiveTooLarge},
		"unsafe path": {tarGz(t, map[string]string{"ok.txt": "x", "../evil": "x"}), 0, nil},
	}
	for name, c := range cases {
		err := d.Extract(bytes.NewReader(c.archive), true, c.max)
		if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
			t.Fatalf("%s: got %v", name, err)
		}
		if b, err := os.ReadFile(filepath.Join(data, "world", "level.dat")); err != nil || string(b) != "precious" {
			t.Fatalf("%s: existing files were touched (%q, %v)", name, b, err)
		}
		if _, err := os.Stat(filepath.Join(data, "a.txt")); err == nil {
			t.Fatalf("%s: part of the archive was written", name)
		}
	}
}

func TestScanCountsRegularFiles(t *testing.T) {
	n, err := Scan(bytes.NewReader(tarGz(t, map[string]string{"a": "123", "d/b": "45"})))
	if err != nil || n != 5 {
		t.Fatalf("got %d, %v", n, err)
	}
}

// When the daemon runs as root, everything it creates must be handed to the
// directory's owner, or the game (which runs as that owner with no
// capabilities) cannot write it. Every way of creating a path goes through adopt.
func TestEverythingCreatedIsAdopted(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	d, _ := Open(data)
	defer d.Close()
	if d.chown && os.Geteuid() != 0 {
		t.Fatal("chown must only be on when the daemon runs as root")
	}
	// Chowning to ourselves is allowed without root, so the real call runs too.
	d.chown, d.uid, d.gid = true, os.Getuid(), os.Getgid()
	var got []string
	d.adopted = func(name string) { got = append(got, name) }
	expect := func(what string, want ...string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s: adopted %v, want %v", what, got, want)
		}
		got = nil
	}

	d.Write("cfg/sub/a.yml", []byte("x"))
	expect("write", "cfg", "cfg/sub", "cfg/sub/a.yml")
	d.Write("cfg/sub/a.yml", []byte("y"))
	expect("overwrite keeps the existing file's owner")
	d.Mkdir("m/n")
	expect("mkdir", "m", "m/n")
	d.MkdirOne("one")
	expect("mkdir one", "one")
	f, err := d.OpenFile("up.bin", os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	expect("sftp upload", "up.bin")
	if err := d.Extract(bytes.NewReader(tarGz(t, map[string]string{"w/level.dat": "x"})), false, 0); err != nil {
		t.Fatal(err)
	}
	expect("restore", "w", "w/level.dat")
}

// As root (how the daemon usually runs), files the panel, SFTP or a restore
// create must belong to the data directory's owner, the user the game runs as.
// Skipped unless the tests run as root.
func TestRootDaemonHandsNewFilesToTheGameUser(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	const game = 4242
	data := filepath.Join(t.TempDir(), "data")
	os.MkdirAll(data, 0o755)
	if err := os.Chown(data, game, game); err != nil {
		t.Fatal(err)
	}
	d, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Write("cfg/server.properties", []byte("x"))
	d.Mkdir("plugins/a")
	f, _ := d.OpenFile("upload.jar", os.O_WRONLY|os.O_CREATE, 0o644)
	f.Close()
	d.Extract(bytes.NewReader(tarGz(t, map[string]string{"world/level.dat": "w"})), false, 0)
	for _, p := range []string{"cfg", "cfg/server.properties", "plugins", "plugins/a", "upload.jar", "world", "world/level.dat"} {
		fi, err := os.Lstat(filepath.Join(data, p))
		if err != nil {
			t.Fatal(err)
		}
		if st := fi.Sys().(*syscall.Stat_t); st.Uid != game || st.Gid != game {
			t.Errorf("%s belongs to %d:%d, want %d:%d", p, st.Uid, st.Gid, game, game)
		}
	}
}
