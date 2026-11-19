package daemon

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

func (r *sftpRig) realUsage() int64 {
	r.t.Helper()
	n, err := r.d.walkUsage(r.id)
	if err != nil {
		r.t.Fatal(err)
	}
	return n
}

// measured makes sure the figure exists before the test starts changing files.
func (r *sftpRig) measured() {
	r.t.Helper()
	if _, err := r.d.diskUsage(r.id); err != nil {
		r.t.Fatal(err)
	}
}

func TestSFTPUploadStopsAtTheLimit(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 1, nil) // 1 MB
	r.measured()
	c := r.login(ownerUser, ownerKey, sftp.UseConcurrentWrites(true))
	err := upload(c, "/huge.bin", make([]byte, 3<<20))
	if err == nil {
		t.Fatal("a 3 MB upload into a 1 MB server succeeded")
	}
	if real := r.realUsage(); real > 1<<20 {
		t.Fatalf("%d bytes on disk, the limit is %d", real, 1<<20)
	}
	if used, real := usedBytes(r.d, r.id), r.realUsage(); used != real {
		t.Fatalf("figure %d, disk %d", used, real)
	}
	// deleting the partial upload frees the room again
	if err := c.Remove("/huge.bin"); err != nil {
		t.Fatal(err)
	}
	if err := upload(c, "/fits.bin", make([]byte, 900<<10)); err != nil {
		t.Fatalf("an upload that fits after cleaning up: %v", err)
	}
	if used := usedBytes(r.d, r.id); used != 900<<10 {
		t.Fatalf("figure %d after cleanup and upload", used)
	}
}

// Over the limit (the game grew past it), a user can still clean up, but
// nothing grows.
func TestSFTPOverTheLimit(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 1, nil)
	r.put("a.bin", string(make([]byte, 700<<10)))
	r.put("b.bin", string(make([]byte, 700<<10)))
	r.put("c.bin", string(make([]byte, 100<<10)))
	r.measured()
	c := r.owner()

	refused := map[string]func() error{
		"new file":         func() error { return upload(c, "/new.bin", []byte("x")) },
		"append":           func() error { f, _ := c.OpenFile("/c.bin", os.O_WRONLY); return writeAndClose(f, []byte("x"), 100<<10) },
		"grow by truncate": func() error { return c.Truncate("/c.bin", 200<<10) },
		// even without growing: over the limit, nothing is opened for writing
		"open for writing": func() error { f, err := c.OpenFile("/c.bin", os.O_WRONLY); closeIf(f); return err },
	}
	for name, f := range refused {
		if err := f(); err == nil {
			t.Errorf("%s: allowed over the limit", name)
		}
	}
	allowed := map[string]func() error{
		"stat and list": func() error { _, err := c.ReadDir("/"); return err },
		"read":          func() error { _, err := download(c, "/c.bin"); return err },
		"mkdir":         func() error { return c.Mkdir("/d") },
		"rename":        func() error { return c.Rename("/c.bin", "/d/c.bin") },
		"shrink":        func() error { return c.Truncate("/d/c.bin", 10) },
		"remove":        func() error { return c.Remove("/a.bin") },
	}
	for _, name := range []string{"stat and list", "read", "mkdir", "rename", "shrink", "remove"} {
		if err := allowed[name](); err != nil {
			t.Errorf("%s: refused over the limit: %v", name, err)
		}
	}
	// back under: writing works again
	if err := upload(c, "/new.bin", make([]byte, 100<<10)); err != nil {
		t.Fatalf("upload after getting under the limit: %v", err)
	}
	if used, real := usedBytes(r.d, r.id), r.realUsage(); used != real {
		t.Fatalf("figure %d, disk %d", used, real)
	}
}

func writeAndClose(f *sftp.File, b []byte, off int64) error {
	if f == nil {
		return os.ErrInvalid
	}
	_, err := f.WriteAt(b, off)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// stormOp does one random thing to a small set of names, the way a mix of
// clients would. Errors are expected (a name may be missing, a write may not
// fit) and ignored: the figure must be right whatever happened.
func stormOp(c *sftp.Client, rnd *rand.Rand) string {
	names := []string{"/a", "/b", "/c", "/d/e", "/d/f", "/g"}
	n := func() string { return names[rnd.Intn(len(names))] }
	size := func() int { return rnd.Intn(300 << 10) }
	switch k := rnd.Intn(11); k {
	case 0, 1:
		p, s := n(), size()
		upload(c, p, make([]byte, s))
		return fmt.Sprintf("upload %s %d", p, s)
	case 2:
		p, off, s := n(), rnd.Intn(400<<10), rnd.Intn(64<<10)
		f, err := c.OpenFile(p, os.O_WRONLY|os.O_CREATE)
		if err == nil {
			writeAndClose(f, make([]byte, s), int64(off))
		}
		return fmt.Sprintf("write %s @%d +%d", p, off, s)
	case 3:
		p, s := n(), size()
		c.Truncate(p, int64(s))
		return fmt.Sprintf("truncate %s %d", p, s)
	case 4:
		p := n()
		c.Remove(p)
		return "remove " + p
	case 5:
		a, b := n(), n()
		c.Rename(a, b)
		return "rename " + a + " " + b
	case 6:
		a, b := n(), n()
		c.PosixRename(a, b)
		return "posix-rename " + a + " " + b
	case 7:
		p, s := n(), rnd.Intn(64<<10)
		f, err := c.OpenFile(p, os.O_WRONLY|os.O_TRUNC)
		if err == nil {
			writeAndClose(f, make([]byte, s), 0)
		}
		return fmt.Sprintf("truncating open %s +%d", p, s)
	case 8:
		c.Mkdir("/d")
		return "mkdir /d"
	case 9:
		c.RemoveDirectory("/d")
		return "rmdir /d"
	default:
		p, s := n(), rnd.Intn(64<<10)
		f, err := c.OpenFile(p, os.O_RDWR|os.O_CREATE)
		if err == nil {
			writeAndClose(f, make([]byte, s), int64(rnd.Intn(300<<10)))
		}
		return fmt.Sprintf("rdwr %s +%d", p, s)
	}
}

// One client, many operations: after each one the figure matches the disk
// exactly, so nothing is ever double-counted or forgotten.
func TestSFTPFigureMatchesDisk(t *testing.T) {
	t.Parallel()
	for seed := range int64(4) {
		t.Run(fmt.Sprint("seed ", seed), func(t *testing.T) {
			r := newSFTPRig(t, 2, nil) // 2 MB: some writes will not fit
			os.Mkdir(r.path("d"), 0o755)
			r.measured()
			c := r.owner()
			rnd := rand.New(rand.NewSource(seed))
			var log []string
			for range 300 {
				log = append(log, stormOp(c, rnd))
				if used, real := usedBytes(r.d, r.id), r.realUsage(); used != real {
					t.Fatalf("figure %d, disk %d after:\n%v", used, real, log[max(0, len(log)-8):])
				}
				if real := r.realUsage(); real > 2<<20 {
					t.Fatalf("%d bytes on disk, limit %d, after %v", real, 2<<20, log[len(log)-1])
				}
			}
		})
	}
}

// Several clients at once: the disk never passes the limit and the figure
// never counts less than is there (counting more is safe; the next walk
// corrects it).
func TestSFTPConcurrentClientsRespectTheLimit(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 2, nil)
	os.Mkdir(r.path("d"), 0o755)
	r.measured()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var worst int64
	for i := range 6 {
		c := r.login(ownerUser, ownerKey, sftp.UseConcurrentWrites(true))
		wg.Add(1)
		go func() {
			defer wg.Done()
			rnd := rand.New(rand.NewSource(int64(i)))
			for range 120 {
				stormOp(c, rnd)
				if real := r.realUsage(); real > 2<<20 {
					mu.Lock()
					worst = max(worst, real)
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if worst > 0 {
		t.Fatalf("%d bytes on disk at one point, limit %d", worst, 2<<20)
	}
	if used, real := usedBytes(r.d, r.id), r.realUsage(); used < real {
		t.Fatalf("figure %d is below the %d bytes on disk", used, real)
	}
}

// Many uploads in parallel, each with its own client: every file arrives
// intact.
func TestSFTPParallelUploads(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := range 16 {
		c := r.login(ownerUser, ownerKey, sftp.UseConcurrentWrites(true))
		wg.Add(1)
		go func() {
			defer wg.Done()
			data := bytes.Repeat([]byte{byte(i)}, 300<<10+i)
			if err := upload(c, fmt.Sprintf("/p%d.bin", i), data); err != nil {
				errs <- err
				return
			}
			got, err := download(c, fmt.Sprintf("/p%d.bin", i))
			if err != nil || !bytes.Equal(got, data) {
				errs <- fmt.Errorf("file %d came back different (%w)", i, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// Uploads racing for the last of the room never pass the limit together.
func TestSFTPParallelUploadsShareTheRoom(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 1, nil)
	r.measured()
	var wg sync.WaitGroup
	for i := range 8 {
		c := r.login(ownerUser, ownerKey, sftp.UseConcurrentWrites(true))
		wg.Add(1)
		go func() {
			defer wg.Done()
			upload(c, fmt.Sprintf("/r%d.bin", i), make([]byte, 400<<10))
		}()
	}
	wg.Wait()
	if real := r.realUsage(); real > 1<<20 {
		t.Fatalf("%d bytes on disk, limit %d", real, 1<<20)
	}
	if used, real := usedBytes(r.d, r.id), r.realUsage(); used != real {
		t.Fatalf("figure %d, disk %d", used, real)
	}
}

// The same storm with the figure expiring all the time, so walks of the
// directory run in the middle of uploads, truncates, removes and renames.
// The figure may count more than is there while a walk settles, never less,
// and the disk never passes the limit.
func TestSFTPStormWithWalksRunning(t *testing.T) {
	t.Parallel()
	for _, clients := range []int{1, 4} {
		t.Run(fmt.Sprint(clients, " clients"), func(t *testing.T) {
			r := newSFTPRig(t, 2, func(c *Config) { c.DiskCacheTTL = time.Millisecond })
			os.Mkdir(r.path("d"), 0o755)
			r.measured()
			var wg sync.WaitGroup
			var mu sync.Mutex
			var problems []string
			for i := range clients {
				c := r.login(ownerUser, ownerKey, sftp.UseConcurrentWrites(true))
				wg.Add(1)
				go func() {
					defer wg.Done()
					rnd := rand.New(rand.NewSource(int64(100 + i)))
					for range 150 {
						op := stormOp(c, rnd)
						if real := r.realUsage(); real > 2<<20 {
							mu.Lock()
							problems = append(problems, fmt.Sprintf("%d bytes on disk after %s", real, op))
							mu.Unlock()
						}
					}
				}()
			}
			// a watcher comparing the figure to the disk while it all runs
			stop := make(chan struct{})
			watched := make(chan struct{})
			go func() {
				defer close(watched)
				for {
					select {
					case <-stop:
						return
					default:
					}
					c := r.d.cacheFor(r.id)
					c.shape.Lock() // no shrink or write in flight: figure and disk comparable
					c.mu.Lock()
					used, walking := c.bytes, c.run != nil
					state := fmt.Sprintf("at=%v gen=%d pending=%d", c.at.Format("15:04:05.000000"), c.gen, c.pending)
					c.mu.Unlock()
					real := r.realUsage()
					c.shape.Unlock()
					if !walking && used < real {
						mu.Lock()
						problems = append(problems, fmt.Sprintf("figure %d below disk %d (%s, now %s)", used, real, state, time.Now().Format("15:04:05.000000")))
						mu.Unlock()
					}
					time.Sleep(2 * time.Millisecond)
				}
			}()
			wg.Wait()
			close(stop)
			<-watched
			for _, p := range problems {
				t.Error(p)
			}
			// once everything is quiet, a fresh measurement agrees exactly
			if got, err := r.d.diskUsage(r.id); err != nil || got != r.realUsage() {
				t.Fatalf("settled figure %d, disk %d (%v)", got, r.realUsage(), err)
			}
		})
	}
}

// ---- throughput ----

func benchTransfer(b *testing.B, download bool) {
	r := newSFTPRig(b, 0, nil)
	c := r.login(ownerUser, ownerKey, sftp.UseConcurrentWrites(true), sftp.UseConcurrentReads(true), sftp.MaxConcurrentRequestsPerFile(64))
	data := make([]byte, 16<<20)
	if download {
		r.put("bench.bin", string(data))
	}
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for range b.N {
		if download {
			f, err := c.Open("/bench.bin")
			if err != nil {
				b.Fatal(err)
			}
			if _, err := f.WriteTo(io.Discard); err != nil {
				b.Fatal(err)
			}
			f.Close()
		} else {
			f, err := c.Create("/bench.bin")
			if err != nil {
				b.Fatal(err)
			}
			if _, err := f.ReadFrom(bytes.NewReader(data)); err != nil {
				b.Fatal(err)
			}
			f.Close()
		}
	}
}

func BenchmarkSFTPUpload(b *testing.B)   { benchTransfer(b, false) }
func BenchmarkSFTPDownload(b *testing.B) { benchTransfer(b, true) }

// Uploads against a disk limit pay for the size check on every write.
func BenchmarkSFTPUploadWithLimit(b *testing.B) {
	r := newSFTPRig(b, 1<<20, nil) // 1 TB: never refused
	c := r.login(ownerUser, ownerKey, sftp.UseConcurrentWrites(true), sftp.MaxConcurrentRequestsPerFile(64))
	data := make([]byte, 16<<20)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for range b.N {
		c.Remove("/bench.bin")
		f, err := c.Create("/bench.bin")
		if err != nil {
			b.Fatal(err)
		}
		if _, err := f.ReadFrom(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
		f.Close()
	}
}
