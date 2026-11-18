package daemon

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// ---- resource limits per connection ----

// One connection cannot hold an unbounded number of the daemon's file
// descriptors (the daemon serves every server on the node).
func TestSFTPOpenFilesPerConnectionAreCapped(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	r.put("a.txt", "a")
	c := r.owner()
	var open []*sftp.File
	for i := range sftpMaxHandles {
		var f *sftp.File
		var err error
		if i%2 == 0 {
			f, err = c.Open("/a.txt")
		} else {
			f, err = c.OpenFile("/a.txt", os.O_WRONLY)
		}
		if err != nil {
			t.Fatalf("open %d of %d: %v", i+1, sftpMaxHandles, err)
		}
		open = append(open, f)
	}
	if f, err := c.Open("/a.txt"); err == nil {
		f.Close()
		t.Fatal("opened a file past the cap")
	}
	if f, err := c.Create("/b.txt"); err == nil {
		f.Close()
		t.Fatal("created a file past the cap")
	}
	if r.exists("b.txt") {
		t.Fatal("a refused create made the file")
	}
	// other requests still work at the cap
	if _, err := c.ReadDir("/"); err != nil {
		t.Fatalf("listing at the cap: %v", err)
	}
	// another connection has its own allowance
	if _, err := download(r.owner(), "/a.txt"); err != nil {
		t.Fatalf("a second connection was starved: %v", err)
	}
	open[0].Close()
	f, err := c.Open("/a.txt")
	if err != nil {
		t.Fatalf("a closed file's slot was not given back: %v", err)
	}
	f.Close()
	// failed opens give their slot back too
	for _, f := range open[1:] {
		f.Close()
	}
	for range sftpMaxHandles * 2 {
		c.Open("/missing")
		c.OpenFile("/dir-missing/x", os.O_WRONLY|os.O_CREATE)
	}
	for i := range sftpMaxHandles {
		f, err := c.Open("/a.txt")
		if err != nil {
			t.Fatalf("after failed opens, open %d: %v", i+1, err)
		}
		defer f.Close()
	}
}

func TestSFTPSessionsPerConnectionAreCapped(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	conn, err := r.sshDial(r.sshConfig(ownerUser, ownerKey))
	if err != nil {
		t.Fatal(err)
	}
	var clients []*sftp.Client
	for i := range sftpMaxChannels {
		c, err := sftp.NewClient(conn)
		if err != nil {
			t.Fatalf("session %d: %v", i+1, err)
		}
		clients = append(clients, c)
	}
	if c, err := sftp.NewClient(conn); err == nil {
		c.Close()
		t.Fatal("a session past the cap was opened")
	}
	if _, _, err := conn.OpenChannel("session", nil); err == nil {
		t.Fatal("a channel past the cap was accepted")
	}
	clients[0].Close()
	if !eventually(3*time.Second, func() bool {
		c, err := sftp.NewClient(conn)
		if err == nil {
			clients[0] = c
		}
		return err == nil
	}) {
		t.Fatal("a closed session's slot was not given back")
	}
	for _, c := range clients {
		if _, err := c.Getwd(); err != nil {
			t.Fatal(err)
		}
		c.Close()
	}
}

// ---- what the server advertises ----

func TestSFTPAdvertisedExtensions(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	for ext, want := range map[string]bool{
		"posix-rename@openssh.com": true,
		"statvfs@openssh.com":      true,
		"hardlink@openssh.com":     false, // refused, so not offered
	} {
		if _, got := c.HasExtension(ext); got != want {
			t.Errorf("%s advertised: %v, want %v", ext, got, want)
		}
	}
}

func TestSFTPStatVFS(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 10, nil) // 10 MB
	r.put("used.bin", string(make([]byte, 3<<20)))
	r.measured()
	v, err := r.owner().StatVFS("/")
	if err != nil {
		t.Fatal(err)
	}
	bs := v.Frsize
	if total := v.TotalSpace(); total > 10<<20 || total < 10<<20-bs {
		t.Errorf("total %d, want the 10 MB limit", total)
	}
	if free := v.FreeSpace(); free > 7<<20 || free < 7<<20-bs {
		t.Errorf("free %d, want the 7 MB left under the limit", free)
	}
	if v.Flag&1 != 0 {
		t.Error("read-only flag set for an owner")
	}
	rv, err := r.login(readerUser, readerKey).StatVFS("/")
	if err != nil {
		t.Fatal(err)
	}
	if rv.Flag&1 == 0 {
		t.Error("read-only flag missing for a read-only user")
	}
	// over the limit: nothing free, never a negative wrapped to a huge number
	r.put("more.bin", string(make([]byte, 9<<20)))
	r.d.cacheFor(r.id).invalidate()
	r.measured()
	if v, _ := r.owner().StatVFS("/"); v.Bfree != 0 || v.Bavail != 0 {
		t.Errorf("over the limit: %d blocks free", v.Bfree)
	}

	// unlimited: the real filesystem
	u := newSFTPRig(t, 0, nil)
	v, err = u.owner().StatVFS("/")
	if err != nil {
		t.Fatal(err)
	}
	var st syscall.Statfs_t
	syscall.Statfs(u.d.serverDir(u.id), &st)
	if v.Blocks != uint64(st.Blocks) {
		t.Errorf("unlimited: %d blocks, the filesystem has %d", v.Blocks, st.Blocks)
	}
}

// ---- the panel answering badly or slowly ----

func TestSFTPUnreadablePanelAnswer(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, func(c *Config) { c.SFTPRecheck = 30 * time.Millisecond })
	c := r.owner()
	n := len(r.p.requests())
	r.p.setMode("garbage")
	if !eventually(3*time.Second, func() bool { return len(r.p.requests()) >= n+3 }) {
		t.Fatal("no rechecks")
	}
	if _, err := c.Stat("/"); err != nil {
		t.Fatalf("an unreadable recheck answer closed the session: %v", err)
	}
	// and a sign-in it answers that way is refused, not counted as a guess
	if _, _, err := r.dial(ownerUser, ownerKey); err == nil {
		t.Fatal("logged in on an unreadable answer")
	}
	if n := r.failCount("127.0.0.1"); n != 0 {
		t.Fatalf("%d guesses counted for the panel's fault", n)
	}
}

func (r *sftpRig) failCount(ip string) int {
	r.d.sftp.mu.Lock()
	defer r.d.sftp.mu.Unlock()
	return len(r.d.sftp.fails[ip])
}

func TestSFTPSlowPanel(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, func(c *Config) {
		c.sftpPanelWait = 200 * time.Millisecond
		c.SFTPRecheck = 30 * time.Millisecond
	})
	c := r.owner()
	r.p.setDelay(2 * time.Second)
	start := time.Now()
	if _, _, err := r.dial(ownerUser, ownerKey); err == nil {
		t.Fatal("logged in although the panel never answered")
	}
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Fatalf("a login waited %v for a slow panel", d)
	}
	if n := r.failCount("127.0.0.1"); n != 0 {
		t.Fatalf("%d guesses counted for a slow panel", n)
	}
	time.Sleep(500 * time.Millisecond) // several timed-out rechecks
	if _, err := c.Stat("/"); err != nil {
		t.Fatalf("a slow panel closed an open session: %v", err)
	}
	r.p.setDelay(0)
	r.owner()
}

// ---- connections that never sign in ----

func TestSFTPHandshakeDeadline(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, func(c *Config) { c.sftpHandshakeTimeout = 200 * time.Millisecond })
	silent, err := net.Dial("tcp", r.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	// one that sends its version and then stalls mid-handshake
	stalled, err := net.Dial("tcp", r.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer stalled.Close()
	stalled.Write([]byte("SSH-2.0-Stall\r\n"))
	for name, nc := range map[string]net.Conn{"silent": silent, "stalled": stalled} {
		nc.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, err := io.Copy(io.Discard, nc); err != nil {
			t.Fatalf("%s connection was not closed by the daemon: %v", name, err)
		}
	}
	if !eventually(2*time.Second, func() bool {
		r.d.sftp.mu.Lock()
		defer r.d.sftp.mu.Unlock()
		return r.d.sftp.conns["127.0.0.1"] == 0
	}) {
		t.Fatal("dropped connections still hold slots")
	}
	// a signed-in session is not subject to the deadline
	c := r.owner()
	time.Sleep(400 * time.Millisecond)
	if _, err := c.Stat("/"); err != nil {
		t.Fatalf("the handshake deadline cut a signed-in session: %v", err)
	}
}

func TestSFTPLockoutExpires(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	for range sftpAuthMaxFail {
		r.dial(ownerUser, "gp_guess")
	}
	if _, _, err := r.dial(ownerUser, ownerKey); err == nil {
		t.Fatal("not locked out")
	}
	r.d.sftp.mu.Lock()
	for i := range r.d.sftp.fails["127.0.0.1"] {
		r.d.sftp.fails["127.0.0.1"][i] = r.d.sftp.fails["127.0.0.1"][i].Add(-sftpAuthWindow - time.Second)
	}
	r.d.sftp.mu.Unlock()
	if _, _, err := r.dial(ownerUser, ownerKey); err != nil {
		t.Fatalf("still locked out after the window: %v", err)
	}
	r.d.sftp.mu.Lock()
	_, kept := r.d.sftp.fails["127.0.0.1"]
	r.d.sftp.mu.Unlock()
	if kept {
		t.Fatal("expired failures are kept forever")
	}
}

// Addresses are counted without their port, IPv6 included.
func TestSFTPIPv6(t *testing.T) {
	t.Parallel()
	if ln, err := net.Listen("tcp", "[::1]:0"); err != nil {
		t.Skip("no IPv6 loopback")
	} else {
		ln.Close()
	}
	r := newSFTPRigWith(t, 0, rigOpts{listen: "[::1]:0"})
	r.owner()
	if q := r.p.requests(); len(q) != 1 || q[0].IP != "::1" {
		t.Fatalf("panel told the address %+v", q)
	}
	for range sftpAuthMaxFail {
		r.dial(ownerUser, "gp_guess")
	}
	if n := r.failCount("::1"); n != sftpAuthMaxFail {
		t.Fatalf("%d failures counted for ::1", n)
	}
	if _, _, err := r.dial(ownerUser, ownerKey); err == nil {
		t.Fatal("an IPv6 address that kept guessing could log in")
	}
}

// ---- the server changing under an open session ----

func TestSFTPServerDeletedMidUpload(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	f, err := c.Create("/big.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(make([]byte, 64<<10)); err != nil {
		t.Fatal(err)
	}
	if err := r.d.remove(context.Background(), r.id); err != nil {
		t.Fatal(err)
	}
	var werr error
	for range 50 {
		if _, werr = f.Write(make([]byte, 64<<10)); werr != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if werr == nil {
		t.Fatal("writes went on after the server was deleted")
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(r.d.serverDir(r.id)); !os.IsNotExist(err) {
		t.Fatal("the deleted server's folder is back")
	}
}

func TestSFTPLimitChangesApplyToOpenSessions(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 10, nil)
	r.measured()
	c := r.owner()
	if err := upload(c, "/a.bin", make([]byte, 2<<20)); err != nil {
		t.Fatal(err)
	}
	setLimit := func(mb int64) {
		spec, _ := r.d.spec(r.id)
		spec.DiskMB = mb
		if _, err := r.d.update(context.Background(), spec); err != nil {
			t.Fatal(err)
		}
	}
	setLimit(1)
	if err := upload(c, "/b.bin", []byte("x")); err == nil {
		t.Fatal("wrote past a lowered limit")
	}
	setLimit(0) // unlimited
	if err := upload(c, "/b.bin", make([]byte, 4<<20)); err != nil {
		t.Fatalf("write after the limit was lifted: %v", err)
	}
}

// Reinstalling runs the installer over the files the session is using: the
// session stays, and usage is measured again.
func TestSFTPReinstallKeepsSessionsAndRemeasures(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	id := uuidN(3)
	if err := r.d.create(context.Background(), proto.ServerSpec{UUID: id, Image: "img", Startup: "run", Stop: "^C", MemoryMB: 128, DiskMB: 50,
		Install: &proto.InstallSpec{Image: "inst", Entrypoint: "bash", Script: strings.Repeat("echo installing\n", 1000)}}); err != nil {
		t.Fatal(err)
	}
	installed := func() bool { st, _ := r.d.installState(id); return st == "" }
	if !eventually(5*time.Second, installed) {
		t.Fatal("install never finished")
	}
	r.p.grant("inst", "gp_inst", authOK(id, true, true))
	c := r.login("inst", "gp_inst")
	if _, err := r.d.diskUsage(id, 0); err != nil {
		t.Fatal(err)
	}
	// the game writes behind the figure's back; the reinstall must not keep it
	os.WriteFile(r.d.serverDir(id)+"/world.dat", make([]byte, 1<<20), 0o644)
	if err := r.d.reinstall(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if !eventually(5*time.Second, installed) {
		t.Fatal("reinstall never finished")
	}
	if _, err := download(c, "/installed.txt"); err != nil {
		t.Fatalf("the session did not survive the reinstall: %v", err)
	}
	got, err := r.d.diskEstimate(id)
	if err != nil {
		t.Fatal(err)
	}
	real, _ := r.d.walkUsage(id)
	if got != real {
		t.Fatalf("figure %d after reinstall, disk %d", got, real)
	}
}

// ---- files the game shares or changes ----

func TestSFTPGameHardLinks(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 10, nil)
	r.put("a.bin", string(make([]byte, 1<<20)))
	os.Link(r.path("a.bin"), r.path("b.bin"))
	r.measured()
	c := r.owner()
	if err := c.Remove("/b.bin"); err != nil {
		t.Fatal(err)
	}
	if used, real := usedBytes(r.d, r.id), r.realUsage(); used != real {
		t.Fatalf("figure %d, disk %d after removing one of two names", used, real)
	}
	if got, err := download(c, "/a.bin"); err != nil || len(got) != 1<<20 {
		t.Fatal("removing one name lost the other")
	}
}

func TestSFTPReadWhileTheGameRewrites(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	r.put("log.txt", strings.Repeat("x", 100<<10))
	c := r.owner()
	f, err := c.Open("/log.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	os.Truncate(r.path("log.txt"), 10)
	b := make([]byte, 64<<10)
	n, err := f.ReadAt(b, 0)
	if n != 10 || err != io.EOF {
		t.Fatalf("read of a shrunk file: %d %v", n, err)
	}
	if n, err := f.ReadAt(b, 50<<10); n != 0 || err != io.EOF {
		t.Fatalf("read past the new end: %d %v", n, err)
	}
	os.WriteFile(r.path("log.txt"), []byte("rewritten"), 0o644)
	if n, _ := f.ReadAt(b, 0); string(b[:n]) != "rewritten" {
		t.Fatalf("after the game rewrote it: %q", b[:n])
	}
	// replaced by a new file under the same name: the handle keeps the old one
	os.WriteFile(r.path("log.new"), []byte("new"), 0o644)
	os.Rename(r.path("log.new"), r.path("log.txt"))
	if n, _ := f.ReadAt(b, 0); string(b[:n]) != "rewritten" {
		t.Fatalf("handle followed the name: %q", b[:n])
	}
}

// ---- setstat with several attributes at once ----

// attrs encodes SFTP v3 attributes.
func attrs(size *uint64, uidgid *[2]uint32, perm *uint32, times *[2]uint32) (uint32, []byte) {
	var flags uint32
	var b []byte
	if size != nil {
		flags |= 0x1
		b = binary.BigEndian.AppendUint64(b, *size)
	}
	if uidgid != nil {
		flags |= 0x2
		b = binary.BigEndian.AppendUint32(b, uidgid[0])
		b = binary.BigEndian.AppendUint32(b, uidgid[1])
	}
	if perm != nil {
		flags |= 0x4
		b = binary.BigEndian.AppendUint32(b, *perm)
	}
	if times != nil {
		flags |= 0x8
		b = binary.BigEndian.AppendUint32(b, times[0])
		b = binary.BigEndian.AppendUint32(b, times[1])
	}
	return flags, b
}

func setstat(h *sftpFS, p string, flags uint32, a []byte) error {
	r := sftp.NewRequest("Setstat", p)
	r.Flags, r.Attrs = flags, a
	return h.Filecmd(r)
}

func TestSFTPSetstatCombined(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 1)
	h := sftpHandler(t, d, id)
	p := d.serverDir(id) + "/f.txt"
	os.WriteFile(p, []byte("0123456789"), 0o644)
	if _, err := d.diskUsage(id, 0); err != nil {
		t.Fatal(err)
	}

	size, perm, times := uint64(4), uint32(0o640), [2]uint32{1000, 2000}
	ug := [2]uint32{0, 0}
	fl, a := attrs(&size, &ug, &perm, &times)
	if err := setstat(h, "/f.txt", fl, a); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	st := fi.Sys().(*syscall.Stat_t)
	if fi.Size() != 4 || fi.Mode().Perm() != 0o640 || fi.ModTime().Unix() != 2000 {
		t.Fatalf("size %d mode %v mtime %d", fi.Size(), fi.Mode(), fi.ModTime().Unix())
	}
	if int(st.Uid) != os.Getuid() {
		t.Fatal("setstat changed the owner")
	}
	if used := usedBytes(d, id); used != 4 {
		t.Fatalf("figure %d after a combined shrink", used)
	}

	// growing past the limit fails before anything else is applied
	big, perm2 := uint64(5<<20), uint32(0o600)
	fl, a = attrs(&big, nil, &perm2, nil)
	if err := setstat(h, "/f.txt", fl, a); err == nil {
		t.Fatal("grew past the limit")
	}
	if fi, _ := os.Stat(p); fi.Size() != 4 || fi.Mode().Perm() != 0o640 {
		t.Fatalf("a refused setstat changed the file: %d %v", fi.Size(), fi.Mode())
	}

	// owner changes alone are accepted and ignored
	fl, a = attrs(nil, &ug, nil, nil)
	if err := setstat(h, "/f.txt", fl, a); err != nil {
		t.Fatalf("chown-only setstat: %v", err)
	}
	// no attributes at all
	if err := setstat(h, "/f.txt", 0, nil); err != nil {
		t.Fatalf("empty setstat: %v", err)
	}
	// on a missing path every attribute fails
	fl, a = attrs(nil, nil, &perm, &times)
	if err := setstat(h, "/missing", fl, a); err == nil {
		t.Fatal("setstat of a missing file succeeded")
	}
}

func TestSFTPChownIsIgnored(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	r.put("a", "a")
	if err := r.owner().Chown("/a", 0, 0); err != nil {
		t.Fatalf("chown: %v (clients preserving attributes send it)", err)
	}
	fi, _ := os.Stat(r.path("a"))
	if int(fi.Sys().(*syscall.Stat_t).Uid) != os.Getuid() {
		t.Fatal("chown changed the owner")
	}
}

// ---- rename without hard links ----

func TestSFTPRenameWithoutHardLinks(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 0)
	h := sftpHandler(t, d, id)
	dir := d.serverDir(id)
	os.WriteFile(dir+"/a", []byte("A"), 0o644)
	os.WriteFile(dir+"/c", []byte("C"), 0o644)

	// a filesystem (or protected_hardlinks) refusing the link: plain rename
	h.link = func(string, string) error { return &fs.PathError{Op: "link", Err: syscall.EPERM} }
	if err := cmd(h, "Rename", "/a", "/b"); err != nil {
		t.Fatalf("rename without hard links: %v", err)
	}
	if b, _ := os.ReadFile(dir + "/b"); string(b) != "A" {
		t.Fatal("not moved")
	}
	if err := cmd(h, "Rename", "/b", "/c"); err == nil {
		t.Fatal("replaced an existing file without hard links")
	}
	// a target appearing between the check and the link
	h.link = func(string, string) error { return &fs.PathError{Op: "link", Err: syscall.EEXIST} }
	if err := cmd(h, "Rename", "/b", "/new"); err == nil {
		t.Fatal("a target that appeared was not reported")
	}
	if b, _ := os.ReadFile(dir + "/b"); string(b) != "A" {
		t.Fatal("the source was lost")
	}
}

// ---- secrets in the log ----

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestSFTPKeysNeverLogged(t *testing.T) {
	t.Parallel()
	var log syncBuf
	r := newSFTPRigWith(t, 0, rigOpts{log: &log, cfg: func(c *Config) {
		c.SFTPRecheck = 30 * time.Millisecond
		c.SFTPKeepAlive = 30 * time.Millisecond
	}})
	c := r.owner()
	r.dial(ownerUser, "gp_wrong_secret")
	r.dial(readerUser, readerKey)
	upload(c, "/x", []byte("x"))
	r.p.setMode("502")
	time.Sleep(100 * time.Millisecond)
	r.p.setMode("")
	r.p.revoke(ownerUser, ownerKey)
	if !closedWithin(c, 3*time.Second) {
		t.Fatal("not closed")
	}
	out := log.String()
	if !strings.Contains(out, "sftp") {
		t.Fatal("nothing was logged; the test would prove nothing")
	}
	for _, secret := range []string{ownerKey, readerKey, "gp_wrong_secret", rigToken} {
		if strings.Contains(out, secret) {
			t.Errorf("the log contains %q", secret)
		}
	}
}

var _ = errors.New
var _ = ssh.Dial
