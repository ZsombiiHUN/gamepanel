package fsx

import (
	"archive/zip"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func newDir(t *testing.T) (*Dir, string) {
	t.Helper()
	root := t.TempDir()
	d, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, root
}

func mustWrite(t *testing.T, d *Dir, p, content string) {
	t.Helper()
	if err := d.Write(p, []byte(content)); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, d *Dir, p string) string {
	t.Helper()
	b, err := d.Read(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

func noLimit(int64) error { return nil }

func TestMoveRulesAndParents(t *testing.T) {
	d, _ := newDir(t)
	mustWrite(t, d, "a.txt", "A")
	mustWrite(t, d, "b.txt", "B")
	mustWrite(t, d, "dir/x.txt", "X")
	if err := d.Move("a.txt", "new/deep/a.txt"); err != nil {
		t.Fatal(err)
	}
	if read(t, d, "new/deep/a.txt") != "A" {
		t.Fatal("moved file lost its content")
	}
	if err := d.Move("b.txt", "dir/x.txt"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("overwrote an existing file: %v", err)
	}
	if err := d.Move("dir", "dir/inside"); err == nil {
		t.Fatal("moved a folder into itself")
	}
	for _, bad := range [][2]string{{"/", "x"}, {"b.txt", "/"}, {"b.txt", "../../escape.txt"}} {
		err := d.Move(bad[0], bad[1])
		if bad[1] == "../../escape.txt" { // cleaned to "escape.txt" inside the root: allowed, and stays inside
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("move %v was allowed", bad)
		}
	}
}

func TestCopyNamesAndQuota(t *testing.T) {
	d, _ := newDir(t)
	mustWrite(t, d, "cfg/server.properties", "motd=hi")
	mustWrite(t, d, "world.tar.gz", "x")
	mustWrite(t, d, ".env", "x")
	for p, want := range map[string]string{"cfg/server.properties": "/cfg/server copy.properties", "world.tar.gz": "/world copy.tar.gz", ".env": "/.env copy", "cfg": "/cfg copy"} {
		if got := d.CopyName(p); got != want {
			t.Errorf("CopyName(%s) = %s, want %s", p, got, want)
		}
	}
	if err := d.Copy("cfg", d.CopyName("cfg"), noLimit); err != nil {
		t.Fatal(err)
	}
	if read(t, d, "cfg copy/server.properties") != "motd=hi" {
		t.Fatal("folder copy is missing its file")
	}
	if got := d.CopyName("cfg"); got != "/cfg copy 2" {
		t.Fatalf("second copy name %s", got)
	}
	full := errors.New("full")
	if err := d.Copy("cfg", "cfg2", func(int64) error { return full }); !errors.Is(err, full) {
		t.Fatalf("copy ignored the disk limit: %v", err)
	}
}

func TestCopySkipsSymlinksThatLeadOut(t *testing.T) {
	d, root := newDir(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("s3cret"), 0o600)
	mustWrite(t, d, "dir/ok.txt", "ok")
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(root, "dir", "link")); err != nil {
		t.Fatal(err)
	}
	if err := d.Copy("dir", "copy", noLimit); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read("copy/link"); err == nil {
		t.Fatal("a symlink was copied")
	}
	if err := d.Copy("dir/link", "stolen", noLimit); err == nil {
		if b, _ := d.Read("stolen"); string(b) == "s3cret" {
			t.Fatal("copying a symlink read a file outside the server")
		}
	}
}

func TestWriteStreamIsAllOrNothing(t *testing.T) {
	d, root := newDir(t)
	mustWrite(t, d, "plugins/a.jar", "old")
	if _, err := d.WriteStream("plugins/a.jar", strings.NewReader("much too long"), 4); !errors.Is(err, ErrTooBig) {
		t.Fatalf("cap not enforced: %v", err)
	}
	if read(t, d, "plugins/a.jar") != "old" {
		t.Fatal("a failed upload damaged the existing file")
	}
	ents, _ := os.ReadDir(filepath.Join(root, "plugins"))
	if len(ents) != 1 {
		t.Fatalf("a failed upload left a temporary file: %v", ents)
	}
	if n, err := d.WriteStream("plugins/a.jar", strings.NewReader("new!"), 4); err != nil || n != 4 {
		t.Fatal(n, err)
	}
	if read(t, d, "plugins/a.jar") != "new!" {
		t.Fatal("upload did not replace the file")
	}
	if _, err := d.WriteStream("plugins", strings.NewReader("x"), 0); err == nil {
		t.Fatal("wrote over a folder")
	}
}

func names(t *testing.T, d *Dir, dir string) []string {
	t.Helper()
	var out []string
	var walk func(string)
	walk = func(p string) {
		l, err := d.List(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range l {
			rel := strings.TrimPrefix(p+"/"+f.Name, "/")
			if f.Dir {
				walk(rel)
			} else {
				out = append(out, strings.TrimPrefix(rel, strings.TrimPrefix(dir, "/")+"/"))
			}
		}
	}
	walk(dir)
	sort.Strings(out)
	return out
}

func TestCompressAndDecompressRoundTrip(t *testing.T) {
	for _, format := range []string{"zip", "tar.gz"} {
		t.Run(format, func(t *testing.T) {
			d, _ := newDir(t)
			mustWrite(t, d, "plugins/Essentials/config.yml", "a: 1")
			mustWrite(t, d, "plugins/Essentials/userdata/u.yml", "b: 2")
			mustWrite(t, d, "plugins/WorldEdit.jar", "jar")
			mustWrite(t, d, "plugins/skip.txt", "not selected")
			out, err := d.Compress("plugins", []string{"plugins/Essentials", "plugins/WorldEdit.jar"}, format, 0)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(out, "/plugins/archive-") || !strings.HasSuffix(out, "."+format) {
				t.Fatalf("archive at %s", out)
			}
			if err := d.Decompress(out, "restored", 0); err != nil {
				t.Fatal(err)
			}
			got := names(t, d, "restored")
			want := []string{"Essentials/config.yml", "Essentials/userdata/u.yml", "WorldEdit.jar"}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("got %v want %v", got, want)
			}
			if read(t, d, "restored/Essentials/userdata/u.yml") != "b: 2" {
				t.Fatal("content changed")
			}
			if _, err := d.Compress("plugins", []string{"config/outside.yml"}, format, 0); err == nil {
				t.Fatal("compressed a path outside the chosen folder")
			}
			if _, err := d.Compress("plugins", []string{"plugins/Essentials"}, format, 10); err == nil {
				t.Fatal("archive grew past its limit")
			}
		})
	}
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func TestDecompressRefusesHostileArchives(t *testing.T) {
	d, root := newDir(t)
	outside := t.TempDir()
	for name, files := range map[string]map[string]string{
		"slip.zip": {"../../escaped.txt": "x"},
		"abs.zip":  {"/etc/escaped.txt": "x"},
		"win.zip":  {"..\\..\\escaped.txt": "x"},
		"bomb.zip": {"big.bin": strings.Repeat("0", 1<<20)},
		"fine.zip": {"ok/a.txt": "a"},
	} {
		os.WriteFile(filepath.Join(root, name), zipOf(t, files), 0o644)
	}
	for _, name := range []string{"slip.zip", "abs.zip", "win.zip"} {
		if err := d.Decompress(name, ".", 0); err == nil || !strings.Contains(err.Error(), "unsafe path") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := d.Decompress("bomb.zip", ".", 1000); !errors.Is(err, ErrArchiveTooLarge) {
		t.Errorf("bomb: %v", err)
	}
	// A symlink planted in the destination must not redirect extraction outside.
	os.Symlink(outside, filepath.Join(root, "ok"))
	err := d.Decompress("fine.zip", ".", 0)
	if _, statErr := os.Stat(filepath.Join(outside, "a.txt")); statErr == nil {
		t.Fatalf("extraction followed a symlink out of the server (err %v)", err)
	}
	os.WriteFile(filepath.Join(root, "x.rar"), []byte("Rar!"), 0o644)
	if err := d.Decompress("x.rar", ".", 0); !errors.Is(err, ErrUnsupportedArchive) {
		t.Errorf("rar: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escaped.txt")); err == nil {
		t.Fatal("a file escaped the server folder")
	}
}

func TestListReportsModeAndLinks(t *testing.T) {
	d, root := newDir(t)
	mustWrite(t, d, "start.sh", "#!/bin/sh")
	if err := d.Chmod("start.sh", 0o750); err != nil {
		t.Fatal(err)
	}
	os.Symlink("/etc/passwd", filepath.Join(root, "link"))
	l, _ := d.List("/")
	byName := map[string]uint32{}
	links := map[string]bool{}
	for _, f := range l {
		byName[f.Name], links[f.Name] = f.Mode, f.Symlink
	}
	if byName["start.sh"] != 0o750 || !links["link"] || links["start.sh"] {
		t.Fatalf("modes %v links %v", byName, links)
	}
}

// A copy or archive name is looked for only while candidates can be checked:
// under a file (ENOTDIR) or a symlink that leads out, every Lstat fails the
// same way, and the search must end instead of spinning forever.
func TestFreeNameSearchEndsOnErrors(t *testing.T) {
	d, root := newDir(t)
	mustWrite(t, d, "server.properties", "motd=hi")
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "out")); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, p := range []string{"server.properties/x", "out/x.txt"} {
			to := d.CopyName(p)
			if err := d.Copy(p, to, func(int64) error { return nil }); err == nil {
				t.Errorf("copy %s: want an error", p)
			}
		}
		for _, base := range []string{"server.properties", "out"} {
			if _, err := d.Compress(base, []string{base + "/x"}, "tar.gz", 0); err == nil {
				t.Errorf("compress in %s: want an error", base)
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the free-name search did not end")
	}
}
