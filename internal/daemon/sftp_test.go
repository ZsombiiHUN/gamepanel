package daemon

import (
	"os"
	"sync"
	"testing"

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
