package daemon

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

// SFTP status codes.
const (
	fxNoSuchFile       = 2
	fxPermissionDenied = 3
	fxFailure          = 4
	fxOpUnsupported    = 8
)

// ---- permissions ----

// Every request a client can make, as a name and a call.
type sftpOp struct {
	name   string
	writes bool // needs write access
	reads  bool // needs read access
	do     func(c *sftp.Client) error
}

func allOps() []sftpOp {
	open := func(name string, flag int) func(c *sftp.Client) error {
		return func(c *sftp.Client) error {
			f, err := c.OpenFile(name, flag)
			if err == nil {
				f.Close()
			}
			return err
		}
	}
	return []sftpOp{
		{"open for reading", false, true, open("/a.txt", os.O_RDONLY)},
		{"read", false, true, func(c *sftp.Client) error { _, err := download(c, "/a.txt"); return err }},
		{"list", false, true, func(c *sftp.Client) error { _, err := c.ReadDir("/"); return err }},
		{"create", true, false, open("/new.txt", os.O_WRONLY|os.O_CREATE)},
		{"open for writing", true, false, open("/a.txt", os.O_WRONLY)},
		{"truncating open", true, false, open("/a.txt", os.O_WRONLY|os.O_TRUNC)},
		{"write", true, false, func(c *sftp.Client) error { return upload(c, "/up.txt", []byte("x")) }},
		{"remove", true, false, func(c *sftp.Client) error { return c.Remove("/a.txt") }},
		{"mkdir", true, false, func(c *sftp.Client) error { return c.Mkdir("/newdir") }},
		{"rmdir", true, false, func(c *sftp.Client) error { return c.RemoveDirectory("/dir") }},
		{"rename", true, false, func(c *sftp.Client) error { return c.Rename("/a.txt", "/b.txt") }},
		{"posix-rename", true, false, func(c *sftp.Client) error { return c.PosixRename("/a.txt", "/b.txt") }},
		{"chmod", true, false, func(c *sftp.Client) error { return c.Chmod("/a.txt", 0o600) }},
		{"chtimes", true, false, func(c *sftp.Client) error { return c.Chtimes("/a.txt", time.Unix(1, 0), time.Unix(1, 0)) }},
		{"truncate", true, false, func(c *sftp.Client) error { return c.Truncate("/a.txt", 0) }},
	}
}

func TestSFTPPermissionMatrix(t *testing.T) {
	for _, who := range []struct {
		name, user, key string
		read, write     bool
	}{
		{"owner", ownerUser, ownerKey, true, true},
		{"read-only", readerUser, readerKey, true, false},
		{"write-only", writerUser, writerKey, false, true},
	} {
		for _, op := range allOps() {
			t.Run(who.name+"/"+op.name, func(t *testing.T) {
				r := newSFTPRig(t, 0, nil)
				r.put("a.txt", "original")
				os.Mkdir(r.path("dir"), 0o755)
				c := r.login(who.user, who.key)
				allowed := (!op.reads || who.read) && (!op.writes || who.write)
				err := op.do(c)
				if allowed && err != nil {
					t.Fatalf("refused: %v", err)
				}
				if !allowed {
					if err == nil {
						t.Fatal("allowed")
					}
					if !isPermission(err) {
						t.Errorf("refused with %v, want permission denied", err)
					}
					// and nothing changed
					if r.get("a.txt") != "original" || !r.exists("dir") || r.exists("new.txt") || r.exists("newdir") || r.exists("up.txt") || r.exists("b.txt") {
						t.Fatal("a refused request changed the files")
					}
				}
			})
		}
	}
}

// Clients stat a target before uploading, so stat works with write access
// alone; listing a directory still needs read.
func TestSFTPStatNeedsEitherPermission(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	r.put("a.txt", "abc")
	for _, who := range [][2]string{{readerUser, readerKey}, {writerUser, writerKey}} {
		c := r.login(who[0], who[1])
		if fi, err := c.Stat("/a.txt"); err != nil || fi.Size() != 3 {
			t.Errorf("%s: stat %v", who[0], err)
		}
		if _, err := c.Lstat("/a.txt"); err != nil {
			t.Errorf("%s: lstat %v", who[0], err)
		}
		if p, err := c.RealPath("."); err != nil || p != "/" {
			t.Errorf("%s: realpath %q %v", who[0], p, err)
		}
	}
	r.p.grant("none", "gp_none", authOK(r.id, false, false))
	c := r.login("none", "gp_none")
	if _, err := c.Stat("/a.txt"); err == nil {
		t.Error("stat without any file permission")
	}
	for _, op := range allOps() {
		if op.do(c) == nil {
			t.Errorf("%s without any file permission", op.name)
		}
	}
}

// ---- transfers ----

func TestSFTPRoundTrips(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	plain := r.owner()
	fast := r.login(ownerUser, ownerKey, sftp.UseConcurrentWrites(true), sftp.UseConcurrentReads(true), sftp.MaxConcurrentRequestsPerFile(64))
	rnd := rand.New(rand.NewSource(1))
	for _, size := range []int{0, 1, 32767, 32768, 32769, 1<<20 + 7, 8 << 20} {
		data := make([]byte, size)
		rnd.Read(data)
		for name, c := range map[string]*sftp.Client{"plain": plain, "concurrent": fast} {
			p := fmt.Sprintf("/%s-%d.bin", name, size)
			f, err := c.Create(p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.ReadFrom(bytes.NewReader(data)); err != nil {
				t.Fatalf("%s upload of %d bytes: %v", name, size, err)
			}
			f.Close()
			if sha256.Sum256([]byte(r.get(p))) != sha256.Sum256(data) {
				t.Fatalf("%s upload of %d bytes arrived corrupted", name, size)
			}
			f, err = c.Open(p)
			if err != nil {
				t.Fatal(err)
			}
			var got bytes.Buffer
			if _, err := f.WriteTo(&got); err != nil {
				t.Fatalf("%s download of %d bytes: %v", name, size, err)
			}
			f.Close()
			if !bytes.Equal(got.Bytes(), data) {
				t.Fatalf("%s download of %d bytes came back different", name, size)
			}
		}
	}
}

func TestSFTPRandomAccessWrites(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	r.put("f.bin", "0123456789")
	f, err := c.OpenFile("/f.bin", os.O_WRONLY)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteAt([]byte("ab"), 3)  // overwrite in the middle
	f.WriteAt([]byte("XY"), 14) // past the end: a hole
	f.Close()
	if got := r.get("f.bin"); got != "012ab56789\x00\x00\x00\x00XY" {
		t.Fatalf("content %q", got)
	}
	// reads at offsets, including past the end
	f, _ = c.Open("/f.bin")
	defer f.Close()
	b := make([]byte, 4)
	if n, err := f.ReadAt(b, 2); n != 4 || string(b) != "2ab5" {
		t.Fatalf("ReadAt: %q %v", b[:n], err)
	}
	if n, err := f.ReadAt(b, 100); n != 0 || err != io.EOF {
		t.Fatalf("read past the end: %d %v", n, err)
	}
}

func TestSFTPOpenFlags(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	r.put("e.txt", "keep me")

	if _, err := c.OpenFile("/e.txt", os.O_WRONLY|os.O_CREATE|os.O_EXCL); err == nil {
		t.Fatal("exclusive create of an existing file succeeded")
	}
	if r.get("e.txt") != "keep me" {
		t.Fatal("a failed exclusive create changed the file")
	}
	f, err := c.OpenFile("/x.txt", os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		t.Fatalf("exclusive create of a new file: %v", err)
	}
	f.Close()

	if _, err := c.OpenFile("/missing.txt", os.O_WRONLY); !isNotExist(err) {
		t.Fatalf("opening a missing file without create: %v", err)
	}
	if _, err := c.Open("/missing.txt"); !isNotExist(err) {
		t.Fatalf("reading a missing file: %v, want not-exist (clients rely on it)", err)
	}

	r.put("t.txt", "long old content")
	f, _ = c.OpenFile("/t.txt", os.O_WRONLY|os.O_TRUNC)
	f.Write([]byte("new"))
	f.Close()
	if got := r.get("t.txt"); got != "new" {
		t.Fatalf("after truncating open: %q", got)
	}

	r.put("ap.txt", "hello")
	f, err = c.OpenFile("/ap.txt", os.O_WRONLY|os.O_APPEND)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte(" world"))
	f.Close()
	if got := r.get("ap.txt"); got != "hello world" {
		t.Fatalf("append: %q", got)
	}

	f, err = c.OpenFile("/new-dir-less/x", os.O_WRONLY|os.O_CREATE)
	if err == nil {
		f.Close()
		t.Fatal("created a file in a missing directory")
	}
}

// sshfs and some editors open files read-write and read back through the
// same handle.
func TestSFTPReadWriteHandle(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	r.put("rw.txt", "abcdef")
	c := r.owner()
	f, err := c.OpenFile("/rw.txt", os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("XY"), 2); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 6)
	if n, err := f.ReadAt(b, 0); n != 6 || string(b) != "abXYef" {
		t.Fatalf("read back through a read-write handle: %q %v", b[:n], err)
	}
	f.Close()

	f, err = c.OpenFile("/new-rw.txt", os.O_RDWR|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("hello"))
	if n, err := f.ReadAt(b[:5], 0); n != 5 || string(b[:5]) != "hello" {
		t.Fatalf("read back a new file: %q %v", b[:n], err)
	}
	f.Close()

	// write-only access: the handle writes, but never reads
	w := r.login(writerUser, writerKey)
	f, err = w.OpenFile("/rw.txt", os.O_RDWR)
	if err == nil {
		if n, err := f.ReadAt(b, 0); err == nil || n > 0 {
			t.Fatalf("write-only user read %q through a read-write handle", b[:n])
		}
		f.Close()
	}
	// read-only access: no read-write handle at all
	ro := r.login(readerUser, readerKey)
	if f, err := ro.OpenFile("/rw.txt", os.O_RDWR); err == nil {
		f.Close()
		t.Fatal("read-only user opened a file read-write")
	}
	if got := r.get("rw.txt"); got != "abXYef" {
		t.Fatalf("content %q", got)
	}
}

// ---- attributes ----

func TestSFTPAttributes(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	r.put("a.txt", "12345")
	mt := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := c.Chtimes("/a.txt", mt, mt); err != nil {
		t.Fatal(err)
	}
	if err := c.Chmod("/a.txt", 0o640); err != nil {
		t.Fatal(err)
	}
	fi, err := c.Stat("/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != 5 || !fi.ModTime().Equal(mt) || fi.Mode().Perm() != 0o640 || !fi.Mode().IsRegular() || fi.Name() != "a.txt" {
		t.Fatalf("stat: size %d mtime %v mode %v name %q", fi.Size(), fi.ModTime(), fi.Mode(), fi.Name())
	}
	host, _ := os.Stat(r.path("a.txt"))
	if host.Mode().Perm() != 0o640 || !host.ModTime().Equal(mt) {
		t.Fatalf("on disk: %v %v", host.Mode(), host.ModTime())
	}

	// no special bits, and the owner can always read and write
	c.Chmod("/a.txt", os.ModeSetuid|os.ModeSetgid|os.ModeSticky|0o777)
	host, _ = os.Stat(r.path("a.txt"))
	if host.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		t.Fatalf("special bits set: %v", host.Mode())
	}
	c.Chmod("/a.txt", 0)
	host, _ = os.Stat(r.path("a.txt"))
	if host.Mode().Perm()&0o600 != 0o600 {
		t.Fatalf("the owner lost access: %v", host.Mode())
	}

	// A directory keeps its search bit: FileZilla applies one mode to a whole
	// tree, and a 0644 directory locks the game (and the panel) out of it.
	os.Mkdir(r.path("world"), 0o755)
	r.put("world/level.dat", "lvl")
	if err := c.Chmod("/world", 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := download(c, "/world/level.dat"); err != nil || string(got) != "lvl" {
		t.Fatalf("after chmod 644 on a directory: %q %v", got, err)
	}
	if err := upload(c, "/world/new.dat", []byte("n")); err != nil {
		t.Fatalf("after chmod 644 on a directory: %v", err)
	}
	if err := c.Chmod("/", 0o600); err != nil {
		t.Logf("chmod of the root refused: %v", err)
	}
	if _, err := c.ReadDir("/world"); err != nil {
		t.Fatalf("after chmod of the root: %v", err)
	}

	// directory listings carry the same attributes
	ents, err := c.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.Name() == "world" && !e.IsDir() {
			t.Fatal("directory listed as a file")
		}
	}
}

// setstat of a size (path or open handle) truncates or extends the file.
func TestSFTPTruncate(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	r.put("t.txt", "0123456789")
	if err := c.Truncate("/t.txt", 4); err != nil {
		t.Fatal(err)
	}
	if got := r.get("t.txt"); got != "0123" {
		t.Fatalf("shrink: %q", got)
	}
	if err := c.Truncate("/t.txt", 6); err != nil {
		t.Fatal(err)
	}
	if got := r.get("t.txt"); got != "0123\x00\x00" {
		t.Fatalf("grow: %q", got)
	}
	f, _ := c.OpenFile("/t.txt", os.O_WRONLY)
	if err := f.Truncate(1); err != nil {
		t.Fatalf("truncate through a handle: %v", err)
	}
	f.Close()
	if got := r.get("t.txt"); got != "0" {
		t.Fatalf("handle truncate: %q", got)
	}
	os.Mkdir(r.path("d"), 0o755)
	if err := c.Truncate("/d", 0); err == nil {
		t.Fatal("truncated a directory")
	}
	if err := c.Truncate("/missing", 0); err == nil {
		t.Fatal("truncate created a file")
	}
	if r.exists("missing") {
		t.Fatal("truncate created a file")
	}
}

// ---- directories ----

func TestSFTPDirectories(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	if err := c.Mkdir("/a"); err != nil {
		t.Fatal(err)
	}
	if err := c.Mkdir("/a"); err == nil {
		t.Fatal("mkdir of an existing directory succeeded")
	}
	if err := c.Mkdir("/x/y"); err == nil {
		t.Fatal("mkdir with a missing parent succeeded")
	}
	if err := c.MkdirAll("/a/b/c"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(r.path("a/b/c")); err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o755 {
		t.Fatalf("mkdir -p: %v %v", fi, err)
	}
	r.put("a/b/c/f.txt", "f")
	if err := c.RemoveDirectory("/a/b/c"); err == nil {
		t.Fatal("removed a directory that is not empty")
	}
	if err := c.RemoveDirectory("/a/b/c/f.txt"); err == nil {
		t.Fatal("rmdir removed a file")
	}
	c.Remove("/a/b/c/f.txt")
	if err := c.RemoveDirectory("/a/b/c"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove("/a/b"); err != nil { // remove falls back to rmdir
		t.Fatalf("remove of an empty directory: %v", err)
	}
	for _, root := range []string{"/", "", ".", "/..", "//"} {
		if err := c.RemoveDirectory(root); err == nil {
			t.Errorf("rmdir %q succeeded", root)
		}
		if err := c.Remove(root); err == nil {
			t.Errorf("remove %q succeeded", root)
		}
		if err := c.Mkdir(root); err == nil {
			t.Errorf("mkdir %q succeeded", root)
		}
	}
	if _, err := os.Stat(r.d.serverDir(r.id)); err != nil {
		t.Fatal("the server folder is gone")
	}
	r.put("file", "x")
	if err := c.Mkdir("/file/sub"); err == nil {
		t.Fatal("mkdir under a file")
	}
}

func TestSFTPListing(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	const n = 1234 // several ReadDir batches
	want := []string{}
	for i := range n {
		name := fmt.Sprintf("f%04d", i)
		r.put("big/"+name, strings.Repeat("x", i%7))
		want = append(want, name)
	}
	r.put("big/.hidden", "h")
	want = append(want, ".hidden")
	sort.Strings(want)
	ents, err := c.ReadDir("/big")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range ents {
		got = append(got, e.Name())
		if e.Name() != ".hidden" {
			var i int
			fmt.Sscanf(e.Name(), "f%04d", &i)
			if e.Size() != int64(i%7) {
				t.Fatalf("%s listed with size %d", e.Name(), e.Size())
			}
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("listed %d entries, want %d (sorted, each once)", len(got), len(want))
	}
	if ents, err := c.ReadDir("/empty-missing"); err == nil || !isNotExist(err) {
		t.Fatalf("listing a missing directory: %v %v", ents, err)
	}
	if _, err := c.ReadDir("/big/f0001"); err == nil {
		t.Fatal("listed a file as a directory")
	}
	if ents, err := c.ReadDir("/"); err != nil || len(ents) != 1 {
		t.Fatalf("root listing: %v %v", len(ents), err)
	}
}

// ---- rename ----

func TestSFTPRenameMatrix(t *testing.T) {
	type tc struct {
		name     string
		posix    bool
		from, to string
		ok       bool
		check    func(r *sftpRig) string // "" if the outcome is right
	}
	same := func(r *sftpRig) string {
		if r.get("a.txt") != "A" || r.get("b.txt") != "B" || r.get("full/x") != "X" || !r.exists("empty") {
			return "a refused rename changed something"
		}
		return ""
	}
	for _, c := range []tc{
		{"file to a new name", false, "/a.txt", "/c.txt", true, func(r *sftpRig) string {
			if r.exists("a.txt") || r.get("c.txt") != "A" {
				return "not moved"
			}
			return ""
		}},
		{"file into a directory", false, "/a.txt", "/full/a.txt", true, func(r *sftpRig) string {
			if r.get("full/a.txt") != "A" {
				return "not moved"
			}
			return ""
		}},
		{"file onto a file", false, "/a.txt", "/b.txt", false, same},
		{"file onto a directory", false, "/a.txt", "/empty", false, same},
		{"file onto itself", false, "/a.txt", "/a.txt", false, same},
		{"directory to a new name", false, "/full", "/moved", true, func(r *sftpRig) string {
			if r.get("moved/x") != "X" || r.exists("full") {
				return "not moved"
			}
			return ""
		}},
		{"directory onto an empty directory", false, "/full", "/empty", false, same},
		{"directory into itself", false, "/full", "/full/sub", false, same},
		{"missing source", false, "/nope", "/c.txt", false, same},
		{"into a missing directory", false, "/a.txt", "/nope/a.txt", false, same},
		{"the root", false, "/", "/r", false, same},
		{"onto the root", false, "/a.txt", "/", false, same},
		{"posix: file onto a file", true, "/a.txt", "/b.txt", true, func(r *sftpRig) string {
			if r.exists("a.txt") || r.get("b.txt") != "A" {
				return "not replaced"
			}
			return ""
		}},
		{"posix: file onto itself", true, "/a.txt", "/a.txt", true, same},
		{"posix: file onto a directory", true, "/a.txt", "/empty", false, same},
		// Go's rename never replaces a directory, even an empty one.
		{"posix: directory onto an empty directory", true, "/full", "/empty", false, same},
		{"posix: directory onto a non-empty one", true, "/empty", "/full", false, same},
		{"posix: directory onto a file", true, "/full", "/a.txt", false, same},
		{"posix: the root", true, "/", "/r", false, same},
		{"posix: onto the root", true, "/full", "/", false, same},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newSFTPRig(t, 0, nil)
			r.put("a.txt", "A")
			r.put("b.txt", "B")
			r.put("full/x", "X")
			os.Mkdir(r.path("empty"), 0o755)
			cl := r.owner()
			var err error
			if c.posix {
				err = cl.PosixRename(c.from, c.to)
			} else {
				err = cl.Rename(c.from, c.to)
			}
			if c.ok != (err == nil) {
				t.Fatalf("rename %s -> %s: %v", c.from, c.to, err)
			}
			if msg := c.check(r); msg != "" {
				t.Fatal(msg)
			}
			if _, err := os.Stat(r.d.serverDir(r.id)); err != nil {
				t.Fatal("the server folder is gone")
			}
		})
	}
}

// ---- names and paths ----

func TestSFTPNames(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	for _, name := range []string{
		"spaces in name.txt", "ünïcödé-名前.txt", "-leading-dash", "#hash%pct?q*star",
		"back\\slash", "trailing space ", " leading space", "tab\tin", ".dotfile", "..dots",
		"emoji-🎮.json", strings.Repeat("n", 255),
	} {
		if err := upload(c, "/"+name, []byte(name)); err != nil {
			t.Errorf("%q: upload %v", name, err)
			continue
		}
		if got, err := os.ReadFile(r.path(name)); err != nil || string(got) != name {
			t.Errorf("%q: stored as something else (%v)", name, err)
		}
		if got, err := download(c, "/"+name); err != nil || string(got) != name {
			t.Errorf("%q: download %q %v", name, got, err)
		}
		if err := c.Remove("/" + name); err != nil || r.exists(name) {
			t.Errorf("%q: remove %v", name, err)
		}
	}
	// "a" and "a " are different files
	r.put("a", "plain")
	if err := upload(c, "/a ", []byte("spaced")); err != nil {
		t.Fatal(err)
	}
	if r.get("a") != "plain" {
		t.Fatal("writing \"a \" overwrote \"a\"")
	}
	for _, bad := range []string{strings.Repeat("n", 256), "nul\x00byte"} {
		if err := upload(c, "/"+bad, []byte("x")); err == nil {
			t.Errorf("%.20q: upload succeeded", bad)
		}
	}
}

// Every way of climbing out stays inside the server's folder.
func TestSFTPPathsStayInside(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	parent := r.d.cfg.DataDir
	for _, p := range []string{"../escape", "/../escape", "/a/../../escape", "../../../../../../escape", "..\\..\\escape", "./escape", "//escape", "/./././escape"} {
		upload(c, p, []byte("x"))
	}
	err := fsWalk(parent, func(path string) {
		if strings.Contains(path, "escape") && !strings.HasPrefix(path, r.d.serverDir(r.id)) {
			t.Errorf("a file landed outside the server: %s", path)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/..", "../..", "/../../..", ""} {
		if rp, err := c.RealPath(p); err != nil || rp != "/" {
			t.Errorf("realpath %q = %q %v", p, rp, err)
		}
	}
	if wd, err := c.Getwd(); err != nil || wd != "/" {
		t.Errorf("getwd %q %v", wd, err)
	}
	// another server's files are not reachable by any path
	other := uuidN(2)
	addServer(t, r.d, other, 0)
	os.WriteFile(r.d.serverDir(other)+"/secret.txt", []byte("s"), 0o644)
	for _, p := range []string{"../" + other + "/secret.txt", "/../" + other + "/secret.txt"} {
		if _, err := download(c, p); err == nil {
			t.Errorf("read another server's file via %s", p)
		}
	}
}

func fsWalk(dir string, f func(string)) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		p := dir + "/" + e.Name()
		f(p)
		if e.IsDir() {
			fsWalk(p, f)
		}
	}
	return nil
}

// ---- links planted by the game (or anyone with a shell in the container) ----

func TestSFTPPlantedSymlinks(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	outside := t.TempDir()
	secret := outside + "/secret.txt"
	os.WriteFile(secret, []byte("top secret"), 0o600)
	mt := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	os.Chtimes(secret, mt, mt)
	os.Symlink(outside, r.path("linkdir"))
	os.Symlink(secret, r.path("linkfile"))
	os.Symlink("../../../../../../../../"+outside+"/secret.txt", r.path("rellink"))
	os.Symlink("loop", r.path("loop"))
	r.put("inside.txt", "inside")
	os.Symlink("inside.txt", r.path("innerlink"))

	attempts := map[string]func() error{
		"read file link":     func() error { _, err := download(c, "/linkfile"); return err },
		"read relative link": func() error { _, err := download(c, "/rellink"); return err },
		"read through dir":   func() error { _, err := download(c, "/linkdir/secret.txt"); return err },
		"list through dir":   func() error { _, err := c.ReadDir("/linkdir"); return err },
		"stat through dir":   func() error { _, err := c.Stat("/linkdir/secret.txt"); return err },
		"stat file link":     func() error { _, err := c.Stat("/linkfile"); return err },
		"write file link":    func() error { return upload(c, "/linkfile", []byte("pwned")) },
		"create in dir":      func() error { return upload(c, "/linkdir/new.txt", []byte("pwned")) },
		"truncating open":    func() error { f, err := c.OpenFile("/linkfile", os.O_WRONLY|os.O_TRUNC); closeIf(f); return err },
		"append open":        func() error { f, err := c.OpenFile("/linkfile", os.O_WRONLY|os.O_APPEND); closeIf(f); return err },
		"mkdir in dir":       func() error { return c.Mkdir("/linkdir/sub") },
		"remove in dir":      func() error { return c.Remove("/linkdir/secret.txt") },
		"chmod file link":    func() error { return c.Chmod("/linkfile", 0o777) },
		"chmod in dir":       func() error { return c.Chmod("/linkdir/secret.txt", 0o777) },
		"chtimes file link":  func() error { return c.Chtimes("/linkfile", time.Now(), time.Now()) },
		"truncate file link": func() error { return c.Truncate("/linkfile", 0) },
		"truncate in dir":    func() error { return c.Truncate("/linkdir/secret.txt", 0) },
		"rename into dir":    func() error { return c.Rename("/inside.txt", "/linkdir/stolen.txt") },
		"rename out of dir":  func() error { return c.Rename("/linkdir/secret.txt", "/got.txt") },
		"posix into dir":     func() error { return c.PosixRename("/inside.txt", "/linkdir/secret.txt") },
		"read a loop":        func() error { _, err := download(c, "/loop"); return err },
		"write a loop":       func() error { return upload(c, "/loop", []byte("x")) },
		"readlink":           func() error { _, err := c.ReadLink("/linkfile"); return err },
		"make a symlink":     func() error { return c.Symlink("/etc/passwd", "/mine") },
		"make a hard link":   func() error { return c.Link("/inside.txt", "/hard") },
	}
	for name, try := range attempts {
		done := make(chan error, 1)
		go func() { done <- try() }()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("%s: allowed", name)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: hung", name)
		}
	}
	if b, _ := os.ReadFile(secret); string(b) != "top secret" {
		t.Fatalf("the outside file changed: %q", b)
	}
	if fi, _ := os.Stat(secret); fi.Mode().Perm() != 0o600 || !fi.ModTime().Equal(mt) {
		t.Fatalf("the outside file's attributes changed: %v %v", fi.Mode(), fi.ModTime())
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 1 {
		t.Fatalf("files appeared outside: %d entries", len(ents))
	}
	if r.exists("mine") || r.exists("hard") || r.exists("got.txt") {
		t.Fatal("a link request created something")
	}

	// What can be done to a link itself: see it, list it, remove it, rename it.
	if fi, err := c.Lstat("/linkfile"); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("lstat of a link: %v %v", fi, err)
	}
	ents, err := c.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.Name() == "linkdir" && e.IsDir() {
			t.Fatal("a directory link is listed as a directory")
		}
	}
	if err := c.Rename("/linkfile", "/renamed-link"); err != nil {
		t.Fatalf("renaming a link: %v", err)
	}
	if err := c.Remove("/renamed-link"); err != nil {
		t.Fatalf("removing a link: %v", err)
	}
	if err := c.Remove("/linkdir"); err != nil {
		t.Fatalf("removing a directory link: %v", err)
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatal("removing a link removed its target")
	}
}

func closeIf(f *sftp.File) {
	if f != nil {
		f.Close()
	}
}

// Named pipes, sockets and directories are not files: opening one must fail
// promptly instead of blocking the session.
func TestSFTPSpecialFiles(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	syscall.Mkfifo(r.path("fifo"), 0o644)
	ln, err := net.Listen("unix", r.path("sock"))
	if err == nil {
		defer ln.Close()
	}
	os.Mkdir(r.path("dir"), 0o755)
	for _, name := range []string{"fifo", "sock", "dir"} {
		for mode, flag := range map[string]int{"read": os.O_RDONLY, "write": os.O_WRONLY, "trunc": os.O_WRONLY | os.O_TRUNC, "rdwr": os.O_RDWR} {
			done := make(chan error, 1)
			go func() {
				f, err := c.OpenFile("/"+name, flag)
				closeIf(f)
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Errorf("%s opened for %s", name, mode)
				}
			case <-time.After(3 * time.Second):
				t.Fatalf("opening %s for %s blocked", name, mode)
			}
		}
	}
	if _, err := c.Stat("/fifo"); err != nil {
		t.Errorf("stat of a fifo: %v", err)
	}
	if err := c.Remove("/fifo"); err != nil {
		t.Errorf("removing a fifo: %v", err)
	}
}

func TestSFTPUnsupportedRequests(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	r.put("a", "a")
	if _, err := c.StatVFS("/"); err == nil {
		t.Log("statvfs is supported")
	}
	if _, err := c.ReadLink("/a"); err == nil {
		t.Error("readlink allowed")
	}
	// the session is still fine afterwards
	if got, err := download(c, "/a"); err != nil || string(got) != "a" {
		t.Fatalf("session broken after unsupported requests: %v", err)
	}
}

var _ = path.Join

// Resuming an upload: OpenSSH's reput, WinSCP and curl open with the append
// flag and send real offsets from the current end; Go's pkg/sftp sends the
// flag with offsets counted from zero. Both must end up appended, also when
// the client pipelines its writes.
func TestSFTPAppendStyles(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	head := bytes.Repeat([]byte("h"), 100<<10)
	tail := make([]byte, 2<<20)
	rand.New(rand.NewSource(7)).Read(tail)
	want := append(append([]byte{}, head...), tail...)

	// offsets from zero, pipelined
	r.put("rel.bin", string(head))
	c := r.login(ownerUser, ownerKey, sftp.UseConcurrentWrites(true), sftp.MaxConcurrentRequestsPerFile(64))
	f, err := c.OpenFile("/rel.bin", os.O_WRONLY|os.O_APPEND)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.ReadFrom(bytes.NewReader(tail)); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if got := r.get("rel.bin"); got != string(want) {
		t.Fatalf("zero-based append: %d bytes, want %d (or content differs)", len(got), len(want))
	}

	// real offsets from the end, pipelined
	r.put("abs.bin", string(head))
	f, err = c.OpenFile("/abs.bin", os.O_WRONLY|os.O_APPEND)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(int64(len(head)), io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ReadFrom(bytes.NewReader(tail)); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if got := r.get("abs.bin"); got != string(want) {
		t.Fatalf("offset append: %d bytes, want %d (or content differs)", len(got), len(want))
	}

	// append to an empty or new file, and append + truncate
	f, _ = c.OpenFile("/new.bin", os.O_WRONLY|os.O_APPEND|os.O_CREATE)
	f.Write([]byte("abc"))
	f.Write([]byte("def"))
	f.Close()
	if got := r.get("new.bin"); got != "abcdef" {
		t.Fatalf("append to a new file: %q", got)
	}
	f, _ = c.OpenFile("/new.bin", os.O_WRONLY|os.O_APPEND|os.O_TRUNC)
	f.Write([]byte("xy"))
	f.Close()
	if got := r.get("new.bin"); got != "xy" {
		t.Fatalf("append after truncating: %q", got)
	}
}

// Pipelined append writes can reach the writer out of order. Where they land
// must not depend on which one gets there first.
func TestSFTPAppendOutOfOrder(t *testing.T) {
	head := []byte("0123456789") // the file before the append
	open := func(t *testing.T) (*limitedWriter, string) {
		p := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(p, head, 0o644); err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(p, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return &limitedWriter{f: f, shape: &sync.RWMutex{}, check: func(int64) error { return nil }, appendAt: int64(len(head))}, p
	}
	// Each case writes "AAAA" then "BBBB" as the client numbers them, but
	// the second reaches the writer first.
	for _, c := range []struct {
		name         string
		first, later int64 // offsets of "AAAA" and "BBBB"
	}{
		{"counted from zero", 0, 4},
		{"counted from zero, overtaking write past the end", 0, 12},
		{"counted from the end", 10, 14},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, p := open(t)
			done := make(chan error, 1)
			go func() { _, err := w.WriteAt([]byte("BBBB"), c.later); done <- err }()
			time.Sleep(20 * time.Millisecond) // "BBBB" is waiting
			if _, err := w.WriteAt([]byte("AAAA"), c.first); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(p)
			shift := int64(0)
			if c.first < int64(len(head)) {
				shift = int64(len(head))
			}
			want := make([]byte, max(int64(len(head)), c.later+shift+4))
			copy(want, head)
			copy(want[c.first+shift:], "AAAA")
			copy(want[c.later+shift:], "BBBB")
			if !bytes.Equal(got, want) {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
	// A lone write at the end, from a client counting from the end, is not
	// held long.
	w, p := open(t)
	start := time.Now()
	w.WriteAt([]byte("AAAA"), 10)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("a write at the end waited %v", d)
	}
	if got, _ := os.ReadFile(p); string(got) != "0123456789AAAA" {
		t.Fatalf("got %q", got)
	}
}
