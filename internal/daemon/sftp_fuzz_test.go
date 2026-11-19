package daemon

import (
	"bytes"
	"encoding/binary"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
)

// Raw SFTP packets straight into the request server with the daemon's
// handlers: whatever a client sends, the daemon must not panic, hang, or
// touch anything outside the server's folder.

// SFTP v3 packet types.
const (
	pInit     = 1
	pOpen     = 3
	pClose    = 4
	pRead     = 5
	pWrite    = 6
	pLstat    = 7
	pFstat    = 8
	pSetstat  = 9
	pFsetstat = 10
	pOpendir  = 11
	pReaddir  = 12
	pRemove   = 13
	pMkdir    = 14
	pRmdir    = 15
	pRealpath = 16
	pStat     = 17
	pRename   = 18
	pReadlink = 19
	pSymlink  = 20
	pExtended = 200
)

type pkt struct{ b []byte }

func (p *pkt) u8(v byte) *pkt    { p.b = append(p.b, v); return p }
func (p *pkt) u32(v uint32) *pkt { p.b = binary.BigEndian.AppendUint32(p.b, v); return p }
func (p *pkt) u64(v uint64) *pkt { p.b = binary.BigEndian.AppendUint64(p.b, v); return p }
func (p *pkt) str(s string) *pkt { p.u32(uint32(len(s))); p.b = append(p.b, s...); return p }

// frame prefixes a packet body with its length.
func frame(body []byte) []byte {
	return append(binary.BigEndian.AppendUint32(nil, uint32(len(body))), body...)
}

func initPacket() []byte { return frame((&pkt{}).u8(pInit).u32(3).b) }

// fuzzRoot is a daemon with one server, plus a file outside it that must
// survive every input.
type fuzzRoot struct {
	d       *Daemon
	id      string
	outside string
	before  string // every path outside the server's folder, at the start
}

// outsidePaths lists everything under the data directory except the
// server's own folder.
func (f *fuzzRoot) outsidePaths() string {
	var out []string
	mine := f.d.serverDir(f.id)
	filepath.Walk(f.d.cfg.DataDir, func(p string, _ os.FileInfo, _ error) error {
		if p == mine {
			return filepath.SkipDir
		}
		out = append(out, p)
		return nil
	})
	return strings.Join(out, "\n")
}

func newFuzzRoot(t testing.TB) *fuzzRoot {
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 1)
	dir := d.serverDir(id)
	os.WriteFile(dir+"/a.txt", []byte("hello"), 0o644)
	os.Mkdir(dir+"/d", 0o755)
	outside := filepath.Join(d.cfg.DataDir, "outside.txt")
	os.WriteFile(outside, []byte("keep"), 0o644)
	os.Symlink(outside, dir+"/out")
	os.Symlink(d.cfg.DataDir, dir+"/updir")
	if _, err := d.diskUsage(id); err != nil {
		t.Fatal(err)
	}
	f := &fuzzRoot{d: d, id: id, outside: outside}
	f.before = f.outsidePaths()
	return f
}

type rwc struct {
	io.Reader
	io.Writer
}

func (rwc) Close() error { return nil }

// frames splits input into length-prefixed packets; whatever does not parse
// as one is sent as a final chunk.
func frames(in []byte) [][]byte {
	var out [][]byte
	for len(in) >= 4 {
		n := int(binary.BigEndian.Uint32(in))
		if n < 0 || n > len(in)-4 {
			break
		}
		out = append(out, in[:4+n])
		in = in[4+n:]
	}
	if len(in) > 0 {
		out = append(out, in)
	}
	return out
}

// serve runs one session over the input the way a client does: one packet
// at a time, waiting (briefly) for each answer. A client that sends data for
// a handle before the open is answered races inside the sftp package itself
// (it registers the handle before opening the file); real clients always
// wait for the handle.
func (f *fuzzRoot) serve(t testing.TB, input []byte) {
	t.Helper()
	dir, err := f.d.dir(f.id)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	h := &sftpFS{
		dir: dir, read: true, write: true, maxOpen: 16, disk: f.d.cacheFor(f.id),
		check:  func(n int64) error { return f.d.reserveDisk(f.id, n) },
		statfs: func() (*sftp.StatVFS, error) { return f.d.sftpStatVFS(f.id) },
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	srv := sftp.NewRequestServer(rwc{inR, outW}, sftp.Handlers{FileGet: h, FilePut: h, FileCmd: h, FileList: h})
	done := make(chan struct{})
	go func() {
		srv.Serve()
		srv.Close()
		inR.Close()
		outW.Close()
		close(done)
	}()
	answers := make(chan struct{}, 1<<16)
	go func() {
		var hdr [4]byte
		for {
			if _, err := io.ReadFull(outR, hdr[:]); err != nil {
				return
			}
			if _, err := io.CopyN(io.Discard, outR, int64(binary.BigEndian.Uint32(hdr[:]))); err != nil {
				return
			}
			answers <- struct{}{}
		}
	}()
	deadline := time.After(20 * time.Second)
feed:
	for _, p := range frames(append(initPacket(), input...)) {
		if _, err := inW.Write(p); err != nil {
			break
		}
		select {
		case <-answers:
		case <-time.After(50 * time.Millisecond):
		case <-done:
			break feed
		case <-deadline:
			t.Fatal("the session hung on the input")
		}
	}
	inW.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not end")
	}
	outR.Close()
}

func (f *fuzzRoot) check(t testing.TB) {
	t.Helper()
	if b, err := os.ReadFile(f.outside); err != nil || string(b) != "keep" {
		t.Fatalf("the file outside the server changed: %q %v", b, err)
	}
	if now := f.outsidePaths(); now != f.before {
		t.Fatalf("something changed outside the server's folder:\n%s\nwas:\n%s", now, f.before)
	}
	if used, real := usedBytes(f.d, f.id), mustWalk(t, f.d, f.id); used < real {
		t.Fatalf("figure %d below the %d bytes on disk", used, real)
	}
}

func mustWalk(t testing.TB, d *Daemon, id string) int64 {
	n, err := d.walkUsage(id)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

var fuzzPaths = []string{
	"/a.txt", "a.txt", "/d", "/d/x", "/new", "../../outside.txt", "/out", "/updir/outside.txt",
	"/updir", "/", "", ".", "..", "/../..", "/d/../../x", "\x00", "/a.txt\x00.png", strings.Repeat("a/", 300), "//d//x",
}

// randomPacket builds a well-formed-ish packet: random type, plausible fields.
func randomPacket(rnd *rand.Rand, handles []string) []byte {
	path := func() string { return fuzzPaths[rnd.Intn(len(fuzzPaths))] }
	handle := func() string {
		if len(handles) > 0 && rnd.Intn(4) > 0 {
			return handles[rnd.Intn(len(handles))]
		}
		return string(rune('0' + rnd.Intn(10)))
	}
	p := &pkt{}
	types := []byte{
		pOpen, pClose, pRead, pWrite, pLstat, pFstat, pSetstat, pFsetstat, pOpendir, pReaddir,
		pRemove, pMkdir, pRmdir, pRealpath, pStat, pRename, pReadlink, pSymlink, pExtended, 99, 0,
	}
	typ := types[rnd.Intn(len(types))]
	p.u8(typ).u32(rnd.Uint32())
	attrs := func() {
		flags := uint32(rnd.Intn(16))
		p.u32(flags)
		if flags&1 != 0 {
			p.u64(uint64(rnd.Intn(4 << 20)))
		}
		if flags&2 != 0 {
			p.u32(0).u32(0)
		}
		if flags&4 != 0 {
			p.u32(uint32(rnd.Intn(0o10000)))
		}
		if flags&8 != 0 {
			p.u32(rnd.Uint32()).u32(rnd.Uint32())
		}
	}
	switch typ {
	case pOpen:
		p.str(path()).u32(uint32(rnd.Intn(64)))
		attrs()
	case pClose, pFstat, pReaddir:
		p.str(handle())
	case pRead:
		p.str(handle()).u64(uint64(rnd.Intn(1 << 20))).u32(uint32(rnd.Intn(1 << 18)))
	case pWrite:
		data := make([]byte, rnd.Intn(70000))
		p.str(handle()).u64(uint64(rnd.Intn(2 << 20))).str(string(data))
	case pSetstat:
		p.str(path())
		attrs()
	case pFsetstat:
		p.str(handle())
		attrs()
	case pRename, pSymlink:
		p.str(path()).str(path())
	case pExtended:
		ext := []string{"posix-rename@openssh.com", "statvfs@openssh.com", "hardlink@openssh.com", "fsync@openssh.com", "bogus"}[rnd.Intn(5)]
		p.str(ext).str(path()).str(path())
	default:
		p.str(path())
		attrs()
	}
	b := p.b
	if rnd.Intn(10) == 0 { // damage it
		b = b[:rnd.Intn(len(b)+1)]
	}
	return frame(b)
}

func TestSFTPRandomPackets(t *testing.T) {
	t.Parallel()
	for seed := range int64(20) {
		f := newFuzzRoot(t)
		rnd := rand.New(rand.NewSource(seed))
		var in []byte
		// handles are small decimal strings in the sftp package
		handles := []string{"1", "2", "3", "4", "5"}
		for range 400 {
			in = append(in, randomPacket(rnd, handles)...)
		}
		f.serve(t, in)
		f.check(t)
	}
}

// Hostile framing: lengths that lie, huge lengths, empty packets.
func TestSFTPMalformedFraming(t *testing.T) {
	t.Parallel()
	f := newFuzzRoot(t)
	for name, in := range map[string][]byte{
		"length past the end":    {0, 0, 0, 50, pStat, 0, 0, 0, 1},
		"4 GB length":            {0xff, 0xff, 0xff, 0xff, pWrite},
		"zero length":            {0, 0, 0, 0},
		"one-byte packet":        frame([]byte{pOpen}),
		"string past the end":    frame((&pkt{}).u8(pStat).u32(1).u32(1000).b),
		"string of 4 GB":         frame((&pkt{}).u8(pOpen).u32(1).u32(0xffffffff).b),
		"second init":            initPacket(),
		"write without open":     frame((&pkt{}).u8(pWrite).u32(1).str("9").u64(0).str("x").b),
		"read of 4 GB":           frame((&pkt{}).u8(pRead).u32(1).str("1").u64(0).u32(0xffffffff).b),
		"write at 2^63":          frame((&pkt{}).u8(pWrite).u32(1).str("1").u64(1 << 63).str("x").b),
		"truncate to 2^63":       frame((&pkt{}).u8(pSetstat).u32(1).str("/a.txt").u32(1).u64(1 << 63).b),
		"garbage":                bytes.Repeat([]byte{0xde, 0xad, 0xbe, 0xef}, 1000),
		"unknown packet type":    frame((&pkt{}).u8(250).u32(1).b),
		"open then huge offsets": append(frame((&pkt{}).u8(pOpen).u32(1).str("/a.txt").u32(0x1a).u32(0).b), frame((&pkt{}).u8(pWrite).u32(2).str("1").u64(1<<62).str("x").b)...),
	} {
		t.Run(name, func(t *testing.T) {
			f.serve(t, in)
			f.check(t)
			if b, _ := os.ReadFile(f.d.serverDir(f.id) + "/a.txt"); len(b) > 1<<20 {
				t.Fatalf("a.txt grew to %d bytes past the 1 MB limit", len(b))
			}
		})
	}
}

func FuzzSFTPPackets(f *testing.F) {
	rnd := rand.New(rand.NewSource(1))
	for range 30 {
		var in []byte
		for range 8 {
			in = append(in, randomPacket(rnd, []string{"1", "2"})...)
		}
		f.Add(in)
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		r := newFuzzRoot(t)
		r.serve(t, in)
		r.check(t)
	})
}

// A setstat whose attributes are cut short used to dereference nil and crash
// the whole daemon (every server on the node) from one packet.
func TestSFTPShortSetstatDoesNotCrash(t *testing.T) {
	t.Parallel()
	f := newFuzzRoot(t)
	for flags := uint32(1); flags < 16; flags++ {
		for _, cut := range []int{0, 3, 7} {
			body := (&pkt{}).u8(pSetstat).u32(1).str("/a.txt").u32(flags).b
			body = append(body, make([]byte, cut)...)
			f.serve(t, frame(body))
			fbody := (&pkt{}).u8(pFsetstat).u32(1).str("1").u32(flags).b
			fbody = append(fbody, make([]byte, cut)...)
			open := frame((&pkt{}).u8(pOpen).u32(2).str("/a.txt").u32(0x02).u32(0).b)
			f.serve(t, append(open, frame(fbody)...))
		}
	}
	f.check(t)
	if b, _ := os.ReadFile(f.d.serverDir(f.id) + "/a.txt"); string(b) != "hello" {
		t.Fatalf("a malformed setstat changed the file: %q", b)
	}
}

// Whatever goes wrong inside a handler fails that request, not the daemon.
func TestSFTPHandlerPanicFailsTheRequestOnly(t *testing.T) {
	t.Parallel()
	// reach a handler through a hook that panics
	d := newTestDaemon(t, runtime.NewFake())
	addServer(t, d, uuidN(1), 0)
	h := sftpHandler(t, d, uuidN(1))
	os.WriteFile(d.serverDir(uuidN(1))+"/x", []byte("x"), 0o644)
	h.link = func(string, string) error { panic("boom") }
	if err := cmd(h, "Rename", "/x", "/y"); err == nil {
		t.Fatal("a panicking rename reported success")
	}
	if _, err := os.Stat(d.serverDir(uuidN(1)) + "/x"); err != nil {
		t.Fatal("the source was lost")
	}
	// the shape lock was released on the way out: later shrinks still work
	done := make(chan error, 1)
	go func() { done <- cmd(h, "Remove", "/x", "") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a panic left the server's shape lock held")
	}
}
