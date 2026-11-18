package daemon

import (
	"errors"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/pkg/sftp"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
)

// SSH_FXF_* open flags from the SFTP protocol.
const (
	fxfWrite  = 0x02
	fxfAppend = 0x04
	fxfCreat  = 0x08
	fxfTrunc  = 0x10
)

func sftpHandler(t *testing.T, d *Daemon, id string) *sftpFS {
	t.Helper()
	dir, err := d.dir(id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dir.Close() })
	return &sftpFS{dir: dir, read: true, write: true, check: func(n int64) error { return d.reserveDisk(id, n) }, disk: d.cacheFor(id)}
}

func openForWrite(h *sftpFS, path string, flags uint32) (*limitedWriter, error) {
	r := sftp.NewRequest("Put", path)
	r.Flags = flags
	w, err := h.Filewrite(r)
	if err != nil {
		return nil, err
	}
	return w.(*limitedWriter), nil
}

// Clients open with the append flag and still send an offset with each write;
// O_APPEND would make every WriteAt fail.
func TestSFTPAppendWrites(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 0)
	h := sftpHandler(t, d, id)
	os.WriteFile(d.serverDir(id)+"/log.txt", []byte("hello"), 0o644)

	w, err := openForWrite(h, "/log.txt", fxfWrite|fxfAppend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteAt([]byte(" world"), 5); err != nil {
		t.Fatalf("append-mode write failed: %v", err)
	}
	w.Close()
	if b, _ := os.ReadFile(d.serverDir(id) + "/log.txt"); string(b) != "hello world" {
		t.Fatalf("content %q", b)
	}
}

// A truncating open refused for being over the disk limit must leave the file
// as it was; one that frees enough room to get back under the limit works.
func TestSFTPTruncateOverTheLimit(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 1) // 1 MB
	h := sftpHandler(t, d, id)
	small := make([]byte, 100<<10)
	os.WriteFile(d.serverDir(id)+"/small.bin", small, 0o644)
	os.WriteFile(d.serverDir(id)+"/big.bin", make([]byte, 2<<20), 0o644)

	if _, err := openForWrite(h, "/small.bin", fxfWrite|fxfTrunc); err == nil {
		t.Fatal("truncating open succeeded while still over the limit")
	}
	if fi, _ := os.Stat(d.serverDir(id) + "/small.bin"); fi.Size() != int64(len(small)) {
		t.Fatalf("a refused open changed the file: size %d", fi.Size())
	}
	if _, err := openForWrite(h, "/new.bin", fxfWrite|fxfCreat); err == nil {
		t.Fatal("a new file was opened for writing while over the limit")
	}
	if _, err := os.Stat(d.serverDir(id) + "/new.bin"); !os.IsNotExist(err) {
		t.Fatal("a refused open created the file")
	}

	w, err := openForWrite(h, "/big.bin", fxfWrite|fxfTrunc)
	if err != nil {
		t.Fatalf("emptying the big file should bring usage under the limit: %v", err)
	}
	if _, err := w.WriteAt(make([]byte, 100<<10), 0); err != nil {
		t.Fatalf("write after freeing space was refused: %v", err)
	}
	w.Close()
	if fi, _ := os.Stat(d.serverDir(id) + "/big.bin"); fi.Size() != 100<<10 {
		t.Fatalf("big.bin size %d after truncate and rewrite", fi.Size())
	}
}

// Concurrent truncating opens of one file must credit its size once: the
// filesystem frees it only once, and a double credit lets later writes pass
// the limit.
func TestSFTPConcurrentTruncatesCreditOnce(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 10) // 10 MB
	h := sftpHandler(t, d, id)
	os.WriteFile(d.serverDir(id)+"/other.bin", make([]byte, 4<<20), 0o644)
	os.WriteFile(d.serverDir(id)+"/big.bin", make([]byte, 4<<20), 0o644)
	if err := d.reserveDisk(id, 0); err != nil { // measure: 8 MB used
		t.Fatal(err)
	}

	const n = 16
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if w, err := openForWrite(h, "/big.bin", fxfWrite|fxfTrunc); err == nil {
				w.Close()
			}
		}()
	}
	close(start)
	wg.Wait()

	c := d.cacheFor(id)
	c.mu.Lock()
	cached := c.bytes
	c.mu.Unlock()
	if cached < 4<<20 {
		t.Fatalf("cache says %d bytes used, the files hold %d", cached, 4<<20)
	}
}

func usedBytes(d *Daemon, id string) int64 {
	c := d.cacheFor(id)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bytes
}

func cmd(h *sftpFS, method, path, target string) error {
	r := sftp.NewRequest(method, path)
	r.Target = target
	return h.Filecmd(r)
}

// setstat with a size used to report success and change nothing, leaving
// stale bytes at the end of files written by sshfs or rclone.
func TestSFTPSetstatTruncates(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 1) // 1 MB
	h := sftpHandler(t, d, id)
	path := d.serverDir(id) + "/f.bin"
	os.WriteFile(path, make([]byte, 600<<10), 0o644)
	d.reserveDisk(id, 0)

	setSize := func(n uint64) error {
		r := sftp.NewRequest("Setstat", "/f.bin")
		r.Flags = 0x1 // SSH_FILEXFER_ATTR_SIZE
		r.Attrs = []byte{0, 0, 0, 0, 0, 0, 0, 0}
		for i := range 8 {
			r.Attrs[i] = byte(n >> (56 - 8*i))
		}
		return h.Filecmd(r)
	}
	if err := setSize(100 << 10); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Size() != 100<<10 {
		t.Fatalf("size %d after shrinking to 100 KB", fi.Size())
	}
	if got := usedBytes(d, id); got != 100<<10 {
		t.Fatalf("usage %d after the shrink, want %d", got, 100<<10)
	}
	if err := setSize(2 << 20); err == nil {
		t.Fatal("growing past the disk limit was allowed")
	}
	if fi, _ := os.Stat(path); fi.Size() != 100<<10 {
		t.Fatalf("a refused grow changed the size to %d", fi.Size())
	}
}

// Deleting files must make room at once, not after the next measurement.
func TestSFTPRemoveFreesSpace(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 1)
	h := sftpHandler(t, d, id)
	os.WriteFile(d.serverDir(id)+"/big.bin", make([]byte, 2<<20), 0o644)
	if _, err := openForWrite(h, "/new.bin", fxfWrite|fxfCreat); err == nil {
		t.Fatal("upload allowed while over the limit")
	}
	if err := cmd(h, "Remove", "/big.bin", ""); err != nil {
		t.Fatal(err)
	}
	w, err := openForWrite(h, "/new.bin", fxfWrite|fxfCreat)
	if err != nil {
		t.Fatalf("still refused right after freeing space: %v", err)
	}
	if _, err := w.WriteAt(make([]byte, 100<<10), 0); err != nil {
		t.Fatal(err)
	}
	w.Close()
}

// Plain rename must not replace a file; posix-rename does, and frees its size.
func TestSFTPRename(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 10)
	h := sftpHandler(t, d, id)
	dir := d.serverDir(id)
	os.WriteFile(dir+"/a", []byte("new"), 0o644)
	os.WriteFile(dir+"/b", make([]byte, 1<<20), 0o644)
	d.reserveDisk(id, 0)
	before := usedBytes(d, id)

	if err := cmd(h, "Rename", "/a", "/b"); err == nil {
		t.Fatal("rename replaced an existing file")
	}
	if fi, _ := os.Stat(dir + "/b"); fi.Size() != 1<<20 {
		t.Fatal("the refused rename touched the target")
	}
	if err := cmd(h, "PosixRename", "/a", "/b"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dir + "/b"); string(b) != "new" {
		t.Fatalf("target holds %q", b)
	}
	if got := usedBytes(d, id); got != before-1<<20 {
		t.Fatalf("usage %d, want %d", got, before-1<<20)
	}
	if err := cmd(h, "Rename", "/b", "/c"); err != nil {
		t.Fatalf("rename to a free name: %v", err)
	}
}

// Opening a named pipe used to block until the other end opened.
func TestSFTPNamedPipe(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 0)
	h := sftpHandler(t, d, id)
	if err := syscall.Mkfifo(d.serverDir(id)+"/pipe", 0o644); err != nil {
		t.Skip("mkfifo:", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := h.Fileread(sftp.NewRequest("Get", "/pipe")); err == nil {
			t.Error("reading a pipe was allowed")
		}
		if _, err := openForWrite(h, "/pipe", fxfWrite); err == nil {
			t.Error("writing a pipe was allowed")
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("opening a named pipe hung")
	}
}

// Write-only users may look up paths (clients do before uploading) but not
// list directories.
func TestSFTPWriteOnlyStat(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 0)
	h := sftpHandler(t, d, id)
	h.read = false
	os.WriteFile(d.serverDir(id)+"/f", []byte("x"), 0o644)
	if _, err := h.Filelist(sftp.NewRequest("Stat", "/f")); err != nil {
		t.Fatalf("stat refused for a write-only user: %v", err)
	}
	if _, err := h.Filelist(sftp.NewRequest("List", "/")); err == nil {
		t.Fatal("listing allowed without read access")
	}
	if _, err := h.Fileread(sftp.NewRequest("Get", "/f")); err == nil {
		t.Fatal("reading allowed without read access")
	}
}

// Writes arriving out of order, as the sftp package delivers them, are each
// counted once, and a file another session truncated is counted again when
// rewritten.
func TestSFTPWriterCountsGrowthOnce(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 10)
	h := sftpHandler(t, d, id)
	d.reserveDisk(id, 0)

	w, err := openForWrite(h, "/f.bin", fxfWrite|fxfCreat)
	if err != nil {
		t.Fatal(err)
	}
	const chunk, n = 32 << 10, 64
	var wg sync.WaitGroup
	for i := n - 1; i >= 0; i-- {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := w.WriteAt(make([]byte, chunk), int64(i*chunk)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := usedBytes(d, id); got < chunk*n {
		t.Fatalf("usage %d is below the %d bytes written", got, chunk*n)
	}

	// Another session empties the file; rewriting it must be counted again.
	w2, err := openForWrite(h, "/f.bin", fxfWrite|fxfTrunc)
	if err != nil {
		t.Fatal(err)
	}
	w2.Close()
	if _, err := w.WriteAt(make([]byte, chunk*n), 0); err != nil {
		t.Fatal(err)
	}
	w.Close()
	if got := usedBytes(d, id); got < chunk*n {
		t.Fatalf("usage %d after rewriting a truncated file, want at least %d", got, chunk*n)
	}
}

// A stale figure is used while a fresh one is measured in the background, so
// writes do not wait for a walk of the whole directory.
func TestDiskEstimateDoesNotWait(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 10)
	d.reserveDisk(id, 0)
	os.WriteFile(d.serverDir(id)+"/f", make([]byte, 1<<20), 0o644)

	c := d.cacheFor(id)
	c.mu.Lock()
	c.at = time.Now().Add(-time.Hour)
	c.mu.Unlock()
	if n, _ := d.diskEstimate(id); n != 0 {
		t.Fatalf("estimate %d: should have returned the stale figure", n)
	}
	deadline := time.Now().Add(5 * time.Second)
	for usedBytes(d, id) != 1<<20 {
		if time.Now().After(deadline) {
			t.Fatalf("background measurement never landed: %d", usedBytes(d, id))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A measurement after the server is deleted must not recreate its folder.
func TestDiskWalkDoesNotRecreateFolder(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 10)
	os.RemoveAll(d.serverDir(id))
	if _, err := d.walkUsage(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d.serverDir(id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("measuring recreated the deleted folder")
	}
}

// A truncate must not slip between a write's size check and its bytes
// landing: it would release the size the write was measured against, and the
// write would then grow the file without being counted.
func TestSFTPWriteAndTruncateDoNotInterleave(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 10)
	h := sftpHandler(t, d, id)
	path := d.serverDir(id) + "/f"
	os.WriteFile(path, make([]byte, 1000), 0o644)
	d.reserveDisk(id, 0)

	w, err := openForWrite(h, "/f", fxfWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	truncated := make(chan error, 1)
	check := w.check
	w.check = func(extra int64) error { // runs after the size check, before the write
		go func() { truncated <- h.truncate("/f", 0) }()
		select {
		case <-truncated:
			t.Error("truncate ran between the write's size check and its write")
		case <-time.After(100 * time.Millisecond):
		}
		return check(extra)
	}
	if _, err := w.WriteAt(make([]byte, 600), 900); err != nil {
		t.Fatal(err)
	}
	if err := <-truncated; err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	if got := usedBytes(d, id); got != fi.Size() {
		t.Fatalf("usage %d, file is %d bytes", got, fi.Size())
	}
}

// walkControl replaces the disk walk with one that returns the sizes the test
// sends, so a walk can be held open while files change.
func walkControl(d *Daemon) chan<- int64 {
	sizes := make(chan int64)
	d.walkHook = func(string) (int64, error) { return <-sizes, nil }
	return sizes
}

func walking(d *Daemon, id string) bool {
	c := d.cacheFor(id)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.run != nil
}

// Space freed while a walk runs must not be lost when the walk lands, whether
// the walk reached the freed file before it was deleted (and counted it) or
// after (and did not).
func TestDiskReleaseDuringWalk(t *testing.T) {
	t.Parallel()
	const mb = 1 << 20
	for _, tc := range []struct {
		name string
		seen bool // the walk counted the file before it was deleted
	}{{"walk counted the file", true}, {"walk missed the file", false}} {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDaemon(t, runtime.NewFake())
			id := uuidN(1)
			addServer(t, d, id, 2) // 2 MB
			sizes := walkControl(d)

			first := make(chan error, 1)
			go func() { first <- d.reserveDisk(id, 0) }()
			sizes <- 3 * mb // over the limit: a 2 MB file and 1 MB of other data
			<-first

			c := d.cacheFor(id)
			c.mu.Lock()
			c.at = time.Now().Add(-time.Hour)
			c.mu.Unlock()
			d.diskEstimate(id) // starts a background walk, held open below
			if !walking(d, id) {
				t.Fatal("no walk started")
			}
			c.release(2 * mb) // the 2 MB file is deleted mid-walk
			if tc.seen {
				sizes <- 3 * mb
			} else {
				sizes <- 1 * mb
			}

			// A second walk is now settling the figure; a write that fits
			// once the freed file is gone must wait for it, not fail.
			deadline := time.Now().Add(5 * time.Second)
			for {
				if s := c.settling(); s != nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("no settling walk after a release during the walk")
				}
				time.Sleep(time.Millisecond)
			}
			wrote := make(chan error, 1)
			go func() { wrote <- d.reserveDisk(id, mb/2) }()
			if tc.seen {
				select {
				case err := <-wrote:
					t.Fatalf("write decided before the settling walk: %v", err)
				case <-time.After(50 * time.Millisecond):
				}
			}
			sizes <- 1 * mb
			if err := <-wrote; err != nil {
				t.Fatalf("write refused after freeing room: %v", err)
			}
			for walking(d, id) {
				time.Sleep(time.Millisecond)
			}
			if got := usedBytes(d, id); got != mb+mb/2 {
				t.Fatalf("usage %d, want %d", got, mb+mb/2)
			}
		})
	}
}

// A release larger than a stale figure must not turn into room that does
// not exist.
func TestDiskCreditFloorsAtZero(t *testing.T) {
	t.Parallel()
	var c diskCache
	c.bytes = 100
	if err := c.reserve(-500, 1000); err != nil {
		t.Fatal(err)
	}
	c.release(500)
	if c.bytes != 0 {
		t.Fatalf("figure %d, want 0", c.bytes)
	}
	if err := c.reserve(1001, 1000); err == nil {
		t.Fatal("reserved past the limit")
	}
}

// Plain rename moves files and directories but never replaces either.
func TestSFTPRenameNoReplace(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 10)
	h := sftpHandler(t, d, id)
	dir := d.serverDir(id)
	os.WriteFile(dir+"/f", []byte("x"), 0o644)
	os.Mkdir(dir+"/d", 0o755)
	os.Mkdir(dir+"/other", 0o755)

	if err := cmd(h, "Rename", "/d", "/other"); err == nil {
		t.Fatal("rename replaced an existing directory")
	}
	if err := cmd(h, "Rename", "/d", "/e"); err != nil {
		t.Fatalf("directory rename: %v", err)
	}
	if err := cmd(h, "Rename", "/f", "/e/f"); err != nil {
		t.Fatalf("file rename: %v", err)
	}
	if _, err := os.Stat(dir + "/f"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the old name is still there")
	}
	if b, _ := os.ReadFile(dir + "/e/f"); string(b) != "x" {
		t.Fatalf("moved file holds %q", b)
	}
}
