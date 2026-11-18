package daemon

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/fsx"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

const (
	sftpMaxPerIP    = 20              // concurrent connections from one address
	sftpAuthWindow  = 5 * time.Minute // failed logins counted per address
	sftpAuthMaxFail = 10
	// A session is closed when keepalives go unanswered this many intervals.
	sftpKeepAliveMisses = 3
	// No real user name or API key comes near this; longer ones are refused
	// as a guess without asking the panel.
	sftpMaxCredential = 1024
	// Per connection: files open at once (each holds a descriptor of the
	// daemon, which serves every server on the node) and SFTP sessions
	// (each runs its own request server). Clients use one session and a few
	// dozen files; these only stop one connection from starving the rest.
	sftpMaxHandles  = 256
	sftpMaxChannels = 8
)

// Only the extensions served here are advertised. The sftp package's default
// list also offers hardlink@openssh.com, which is refused (links are how
// directories get escaped), and clients that see it try it.
func init() {
	if err := sftp.SetSFTPExtensions("posix-rename@openssh.com", "statvfs@openssh.com"); err != nil {
		panic(err)
	}
}

// sftpServer serves each server's data directory over SFTP. Authentication is
// delegated to the panel; file access is confined by fsx like everything else.
type sftpServer struct {
	d       *Daemon
	cfg     *ssh.ServerConfig
	ln      net.Listener
	authURL string
	http    *http.Client

	mu       sync.Mutex
	conns    map[string]int
	fails    map[string][]time.Time
	sessions map[string]map[*ssh.ServerConn]struct{} // by server uuid
}

// hostKey loads (or creates, mode 0600) the daemon's persistent SSH host key,
// so clients see a stable fingerprint across restarts.
func hostKey(path string) (ssh.Signer, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		_, priv, gerr := ed25519.GenerateKey(rand.Reader)
		if gerr != nil {
			return nil, gerr
		}
		der, gerr := ssh.MarshalPrivateKey(priv, "gamepanel daemon host key")
		if gerr != nil {
			return nil, gerr
		}
		b = pemEncode(der)
		if gerr := os.WriteFile(path, b, 0o600); gerr != nil {
			return nil, gerr
		}
	} else if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(b)
}

// StartSFTP begins listening; it returns the bound address (useful with ":0").
func (d *Daemon) StartSFTP(ctx context.Context, addr string) (string, error) {
	signer, err := hostKey(filepath.Join(d.cfg.DataDir, "sftp_host_ed25519"))
	if err != nil {
		return "", err
	}
	u, err := url.Parse(d.cfg.PanelURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	default:
		u.Scheme = "http"
	}
	u.Path, u.RawQuery = "/daemon/sftp-auth", ""

	s := &sftpServer{d: d, authURL: u.String(), http: &http.Client{Timeout: d.cfg.sftpPanelTimeout()},
		conns: map[string]int{}, fails: map[string][]time.Time{},
		sessions: map[string]map[*ssh.ServerConn]struct{}{}}
	s.cfg = &ssh.ServerConfig{
		MaxAuthTries:  3,
		ServerVersion: "SSH-2.0-GamePanel",
		// Password only: the "password" is an API key, checked by the panel.
		PasswordCallback: s.auth,
	}
	s.cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	s.ln = ln
	go func() { <-ctx.Done(); ln.Close() }()
	d.mu.Lock()
	d.sftp = s
	d.mu.Unlock()
	go s.serve(ctx)
	d.log.Info("sftp listening", "addr", ln.Addr().String())
	return ln.Addr().String(), nil
}

func (s *sftpServer) serve(ctx context.Context) {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		ip, _, _ := net.SplitHostPort(c.RemoteAddr().String())
		if !s.admit(ip) {
			c.Close()
			continue
		}
		go func() {
			defer s.release(ip)
			s.handle(ctx, c, ip)
		}()
	}
}

func (s *sftpServer) admit(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns[ip] >= sftpMaxPerIP || s.tooManyFails(ip) {
		return false
	}
	s.conns[ip]++
	return true
}

func (s *sftpServer) release(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns[ip]--; s.conns[ip] <= 0 {
		delete(s.conns, ip)
	}
}

// tooManyFails must be called with s.mu held.
func (s *sftpServer) tooManyFails(ip string) bool {
	cut := time.Now().Add(-sftpAuthWindow)
	kept := s.fails[ip][:0]
	for _, t := range s.fails[ip] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(s.fails, ip)
	} else {
		s.fails[ip] = kept
	}
	return len(kept) >= sftpAuthMaxFail
}

func (s *sftpServer) noteFail(ip string) {
	s.mu.Lock()
	s.fails[ip] = append(s.fails[ip], time.Now())
	s.mu.Unlock()
}

// ask has the panel check a login. err is set when the panel could not
// answer: unreachable, or any status but 200 (a reverse proxy answers 502
// while the panel restarts). A refusal is a 200 with OK false.
func (s *sftpServer) ask(user, pw, ip string, recheck bool) (proto.SFTPAuthResp, error) {
	var r proto.SFTPAuthResp
	body, _ := json.Marshal(proto.SFTPAuthReq{Username: user, Password: pw, IP: ip, Recheck: recheck})
	req, _ := http.NewRequest(http.MethodPost, s.authURL, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.d.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return r, fmt.Errorf("panel answered %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&r); err != nil {
		// not an answer (a captive page, a half-written body): the panel
		// could not be asked, which neither refuses a login nor ends a session
		return proto.SFTPAuthResp{}, fmt.Errorf("unreadable panel answer: %w", err)
	}
	if !uuidRe.MatchString(r.ServerUUID) {
		return proto.SFTPAuthResp{}, nil
	}
	return r, nil
}

func (s *sftpServer) auth(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
	ip, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	if len(c.User()) > sftpMaxCredential || len(pw) > sftpMaxCredential {
		s.noteFail(ip)
		return nil, errors.New("denied")
	}
	r, err := s.ask(c.User(), string(pw), ip, false)
	if err != nil {
		return nil, errors.New("unavailable")
	}
	if !r.OK {
		s.noteFail(ip)
		return nil, errors.New("denied")
	}
	if _, err := s.d.spec(r.ServerUUID); err != nil { // panel and node disagree about this server
		return nil, errors.New("denied")
	}
	// The key stays in memory for the session so watchAccess can re-check it.
	ext := map[string]string{"server": r.ServerUUID, "key": string(pw)}
	if r.Read {
		ext["read"] = "1"
	}
	if r.Write {
		ext["write"] = "1"
	}
	return &ssh.Permissions{Extensions: ext}, nil
}

// watchAccess re-checks an open session with the panel and closes it once the
// key is revoked, the account loses access, or its file permissions change
// (the client reconnects with the new ones). Access is otherwise only checked
// at sign-in. If the panel cannot be reached the session is kept: a panel
// restart should not cut every transfer.
func (s *sftpServer) watchAccess(ctx context.Context, conn *ssh.ServerConn, ip string) {
	ext := conn.Permissions.Extensions
	for sleep(ctx, s.d.cfg.sftpRecheck()) {
		r, err := s.ask(conn.User(), ext["key"], ip, true)
		if err != nil {
			continue
		}
		if !r.OK || r.ServerUUID != ext["server"] || r.Read != (ext["read"] == "1") || r.Write != (ext["write"] == "1") {
			s.d.log.Info("sftp access changed, closing the session", "server", ext["server"], "ip", ip)
			conn.Close()
			return
		}
	}
}

// track registers an open session so closeSFTP can end it; the returned
// func unregisters it.
func (s *sftpServer) track(uuid string, c *ssh.ServerConn) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions[uuid] == nil {
		s.sessions[uuid] = map[*ssh.ServerConn]struct{}{}
	}
	s.sessions[uuid][c] = struct{}{}
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.sessions[uuid], c)
		if len(s.sessions[uuid]) == 0 {
			delete(s.sessions, uuid)
		}
	}
}

// closeSFTP ends every open SFTP session for a server (it is being deleted:
// a session would otherwise keep writing into the removed directory).
func (d *Daemon) closeSFTP(uuid string) {
	d.mu.Lock()
	s := d.sftp
	d.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.sessions[uuid] {
		c.Close()
	}
}

// keepAlive closes a session whose client stopped answering: a dropped
// connection would otherwise hold one of the address's slots until the
// kernel gives up on it, which can take hours.
func (s *sftpServer) keepAlive(ctx context.Context, conn *ssh.ServerConn) {
	every := s.d.cfg.sftpKeepAlive()
	for sleep(ctx, every) {
		reply := make(chan error, 1)
		go func() { // returns when the client answers or the connection closes
			_, _, err := conn.SendRequest("keepalive@openssh.com", true, nil)
			reply <- err
		}()
		t := time.NewTimer(sftpKeepAliveMisses * every)
		select {
		case err := <-reply:
			t.Stop()
			if err != nil {
				return
			}
		case <-t.C:
			s.d.log.Info("sftp client stopped answering, closing the session", "server", conn.Permissions.Extensions["server"])
			conn.Close()
			return
		case <-ctx.Done():
			t.Stop()
			return
		}
	}
}

func (s *sftpServer) handle(ctx context.Context, nc net.Conn, ip string) {
	defer nc.Close()
	nc.SetDeadline(time.Now().Add(s.d.cfg.sftpHandshake())) // handshake + auth
	conn, chans, reqs, err := ssh.NewServerConn(nc, s.cfg)
	if err != nil {
		return
	}
	nc.SetDeadline(time.Time{})
	defer conn.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go ssh.DiscardRequests(reqs)
	go func() { <-ctx.Done(); conn.Close() }()
	go s.watchAccess(ctx, conn, ip)
	go s.keepAlive(ctx, conn)

	ext := conn.Permissions.Extensions
	uuid := ext["server"]
	defer s.track(uuid, conn)()
	// Tracked first, then checked: a delete either sees this session or has
	// already removed the server, never neither.
	if _, err := s.d.spec(uuid); err != nil {
		return
	}
	dir, err := fsx.OpenExisting(s.d.serverDir(uuid))
	if err != nil {
		return
	}
	defer dir.Close()
	h := &sftpFS{dir: dir, read: ext["read"] == "1", write: ext["write"] == "1", maxOpen: sftpMaxHandles,
		check: func(extra int64) error {
			if _, err := s.d.spec(uuid); err != nil { // deleted: no limit left to check against
				return sftp.ErrSSHFxPermissionDenied
			}
			return s.d.reserveDisk(uuid, extra)
		},
		disk: s.d.cacheFor(uuid), log: s.d.log,
		statfs: func() (*sftp.StatVFS, error) { return s.d.sftpStatVFS(uuid) }}

	var mu sync.Mutex
	channels := 0
	for nch := range chans {
		if nch.ChannelType() != "session" { // no port forwarding, no x11
			nch.Reject(ssh.UnknownChannelType, "only sftp sessions are allowed")
			continue
		}
		mu.Lock()
		full := channels >= sftpMaxChannels
		if !full {
			channels++
		}
		mu.Unlock()
		if full {
			nch.Reject(ssh.ResourceShortage, "too many sessions on one connection")
			continue
		}
		ch, creqs, err := nch.Accept()
		if err != nil {
			mu.Lock()
			channels--
			mu.Unlock()
			continue
		}
		go func() {
			defer func() {
				mu.Lock()
				channels--
				mu.Unlock()
			}()
			defer ch.Close()
			for r := range creqs {
				// Only the sftp subsystem: no shell, exec, pty, env or agent.
				ok := r.Type == "subsystem" && len(r.Payload) >= 4 && string(r.Payload[4:]) == "sftp"
				r.Reply(ok, nil)
				if !ok {
					continue
				}
				srv := sftp.NewRequestServer(ch, sftp.Handlers{FileGet: h, FilePut: h, FileCmd: h, FileList: h})
				srv.Serve()
				srv.Close()
				return
			}
		}()
	}
}

// ---- sftp handlers over fsx ----

type sftpFS struct {
	dir         *fsx.Dir
	read, write bool
	// check reserves extra bytes, failing when they would pass the server's
	// disk limit.
	check func(extra int64) error
	disk  *diskCache // the server's usage figure; its shape lock guards releases

	statfs func() (*sftp.StatVFS, error) // for statvfs; nil = unsupported
	log    *slog.Logger                  // where handler panics go (nil: the default)
	link   func(from, to string) error   // test hook replacing dir.Link

	mu      sync.Mutex
	open    int // files open through this connection
	maxOpen int // 0 = no cap (tests that build a handler directly)
}

// survive turns a panic in a handler into a failed request. The sftp package
// runs handlers on its own goroutines without recovering, so a panic there
// (a malformed packet reaching a code path that did not expect it) would
// take down the daemon and every server on the node with it.
func survive(log *slog.Logger, err *error) {
	if p := recover(); p != nil {
		if log == nil {
			log = slog.Default()
		}
		log.Error("sftp request panicked", "panic", p, "stack", string(debug.Stack()))
		*err = sftp.ErrSSHFxFailure
	}
}

var errTooManyOpen = errors.New("too many open files on this connection")

// hold counts a file about to be opened; the returned func gives the slot
// back and must be called exactly once (on close, or if the open fails).
func (h *sftpFS) hold() (func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.maxOpen > 0 && h.open >= h.maxOpen {
		return nil, errTooManyOpen
	}
	h.open++
	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			h.open--
			h.mu.Unlock()
		})
	}, nil
}

// heldFile is a file opened for reading that gives its slot back on close.
type heldFile struct {
	*os.File
	free func()
}

func (f heldFile) Close() error { f.free(); return f.File.Close() }

// limitedWriter refuses writes that would grow the file past the disk limit.
// Only growth is reserved: a write is measured against the file's current
// size (so rewriting existing bytes is free, and a truncate by another
// session is noticed) and against this handle's writes still in flight,
// which the size does not show yet. The sftp package calls WriteAt
// concurrently, hence the lock.
//
// Each write holds the server's shape lock shared from the size check until
// its bytes land: a truncate in between would release the size the write
// was measured against, and the write would then grow the file unaccounted.
//
// A handle opened with the append flag (SSH_FXF_APPEND) must add to the end of
// the file. Clients differ in the offsets they send with it: most (OpenSSH's
// reput, WinSCP, curl) send real offsets starting at the end, some (Go's
// pkg/sftp) count from zero and leave the placing to the server. Writes are
// served concurrently, so "always at the current end" would scramble a
// pipelined upload; instead the first write decides. An offset below the
// size at open can only be counted from zero, and then every write of the
// handle is moved past that size.
type limitedWriter struct {
	f        *os.File
	shape    *sync.RWMutex
	mu       sync.Mutex
	inflight int   // writes reserved but not finished
	ahead    int64 // furthest end among them
	check    func(extra int64) error
	read     bool       // the handle may also be read (opened read-write with read access)
	free     func()     // gives the connection's open-file slot back
	disk     *diskCache // for pending growth (nil in tests that do not need it)
	log      *slog.Logger

	appendAt int64 // size at open for an append handle, -1 otherwise
	decided  bool  // the first append write has fixed shift
	shift    int64 // added to every offset of an append handle
}

func (w *limitedWriter) ReadAt(p []byte, off int64) (n int, err error) {
	defer survive(w.log, &err)
	if !w.read {
		return 0, sftp.ErrSSHFxPermissionDenied
	}
	return w.f.ReadAt(p, off)
}

func (w *limitedWriter) WriteAt(p []byte, off int64) (n int, err error) {
	defer survive(w.log, &err)
	w.shape.RLock()
	defer w.shape.RUnlock()
	w.mu.Lock()
	if w.appendAt >= 0 {
		if !w.decided {
			w.decided = true
			if off < w.appendAt {
				w.shift = w.appendAt
			}
		}
		off += w.shift
	}
	end := off + int64(len(p))
	if off < 0 || end < off { // offsets past 2^63 (or wrapping past it) never reach the size check
		w.mu.Unlock()
		return 0, sftp.ErrSSHFxBadMessage
	}
	fi, err := w.f.Stat()
	if err != nil {
		w.mu.Unlock()
		return 0, err
	}
	var grow int64
	if base := max(fi.Size(), w.ahead); end > base {
		grow = end - base
		w.pend(grow)
		if err := w.check(grow); err != nil {
			w.pend(-grow)
			w.mu.Unlock()
			return 0, err
		}
	}
	w.inflight++
	w.ahead = max(w.ahead, end)
	w.mu.Unlock()

	n, err = w.f.WriteAt(p, off)
	w.pend(-grow)

	w.mu.Lock()
	if w.inflight--; w.inflight == 0 {
		w.ahead = 0 // all landed: the size shows them now
	}
	w.mu.Unlock()
	return n, err
}

func (w *limitedWriter) pend(n int64) {
	if w.disk != nil && n != 0 {
		w.disk.pend(n)
	}
}

func (w *limitedWriter) Close() error {
	if w.free != nil {
		w.free()
	}
	return w.f.Close()
}

// deny maps os.Root escape/permission failures to a plain permission error.
func deny(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "escapes") || errors.Is(err, fs.ErrPermission) {
		return sftp.ErrSSHFxPermissionDenied
	}
	return err
}

func (h *sftpFS) Fileread(r *sftp.Request) (_ io.ReaderAt, err error) {
	defer survive(h.log, &err)
	if !h.read {
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	free, err := h.hold()
	if err != nil {
		return nil, err
	}
	f, err := h.dir.OpenFile(r.Filepath, os.O_RDONLY, 0)
	if err != nil {
		free()
		return nil, deny(err)
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		f.Close()
		free()
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	return heldFile{f, free}, nil
}

func (h *sftpFS) Filewrite(r *sftp.Request) (_ io.WriterAt, err error) {
	defer survive(h.log, &err)
	if !h.write {
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	free, err := h.hold()
	if err != nil {
		return nil, err
	}
	opened := false
	defer func() {
		if !opened {
			free()
		}
	}()
	p := r.Pflags()
	flag := os.O_WRONLY
	if p.Read {
		flag = os.O_RDWR
	}
	if p.Creat {
		flag |= os.O_CREATE
	}
	if p.Excl {
		flag |= os.O_EXCL
	}
	// No O_APPEND: os.File.WriteAt refuses files opened with it; the
	// writer places appended data itself (see limitedWriter).
	//
	// No O_TRUNC either: the open must not change the file until the disk
	// check has passed. Truncating releases the file's current size, so
	// emptying a file still works over the limit if that brings usage back
	// under it.
	//
	// Releasing and truncating happen under the shape lock: two concurrent
	// opens of the same file would otherwise both release its size while the
	// filesystem frees it only once.
	var freed int64
	if p.Trunc {
		h.disk.shape.Lock()
		defer h.disk.shape.Unlock()
		if fi, err := h.dir.Stat(r.Filepath); err == nil && fi.Mode().IsRegular() {
			freed = fi.Size()
		}
	}
	if err := h.check(-freed); err != nil { // over the limit: no new writes
		return nil, err
	}
	stale := true // until the truncate frees exactly what was released
	if freed > 0 {
		// Wrong release (open or truncate failed, or the file changed since
		// the stat): measure again before the figure is used.
		defer func() {
			if stale {
				h.disk.invalidate()
			}
		}()
	}
	f, err := h.dir.OpenFile(r.Filepath, flag, 0o644)
	if err != nil {
		return nil, deny(err)
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	if p.Trunc {
		if err := f.Truncate(0); err != nil {
			f.Close()
			return nil, deny(err)
		}
		stale = fi.Size() != freed
	}
	w := &limitedWriter{f: f, shape: &h.disk.shape, disk: h.disk, check: h.check, appendAt: -1, free: free, log: h.log}
	opened = true
	if p.Append {
		w.appendAt = fi.Size()
		if p.Trunc {
			w.appendAt = 0
		}
	}
	return w, nil
}

// OpenFile serves opens for reading and writing at once (sshfs, editors).
// Without read access the handle still writes, but every read is refused.
func (h *sftpFS) OpenFile(r *sftp.Request) (_ sftp.WriterAtReaderAt, err error) {
	defer survive(h.log, &err)
	w, err := h.Filewrite(r)
	if err != nil {
		return nil, err
	}
	lw := w.(*limitedWriter)
	lw.read = h.read
	return lw, nil
}

// StatVFS answers statvfs@openssh.com (df on an sshfs mount, rclone about).
func (h *sftpFS) StatVFS(r *sftp.Request) (_ *sftp.StatVFS, err error) {
	defer survive(h.log, &err)
	if (!h.read && !h.write) || h.statfs == nil {
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	v, err := h.statfs()
	if err != nil {
		return nil, deny(err)
	}
	if !h.write {
		v.Flag |= 1 // SSH_FXE_STATVFS_ST_RDONLY
	}
	return v, nil
}

// sftpStatVFS describes the space a server can use: with a disk limit, a
// filesystem the size of the limit with what is left of it free (never
// more than the real disk has); without one, the real filesystem.
func (d *Daemon) sftpStatVFS(uuid string) (*sftp.StatVFS, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(d.serverDir(uuid), &st); err != nil {
		return nil, err
	}
	bs := uint64(st.Bsize)
	if bs == 0 {
		bs = 4096
	}
	v := &sftp.StatVFS{Bsize: bs, Frsize: bs, Blocks: uint64(st.Blocks), Bfree: uint64(st.Bfree), Bavail: uint64(st.Bavail),
		Files: uint64(st.Files), Ffree: uint64(st.Ffree), Favail: uint64(st.Ffree), Namemax: 255}
	if limit := d.diskLimitBytes(uuid); limit > 0 {
		used, err := d.diskEstimate(uuid)
		if err != nil {
			return nil, err
		}
		left := uint64(max(limit-used, 0)) / bs
		v.Blocks = uint64(limit) / bs
		v.Bfree, v.Bavail = min(left, v.Bfree), min(left, v.Bavail)
	}
	return v, nil
}

// PosixRename replaces an existing target (posix-rename@openssh.com). The
// sftp package hands it to Filecmd as a plain rename unless this exists.
func (h *sftpFS) PosixRename(r *sftp.Request) error { return h.Filecmd(r) }

// Lstat reports a symlink as a symlink. The sftp package answers lstat with
// a stat, following the link, unless this exists.
func (h *sftpFS) Lstat(r *sftp.Request) (sftp.ListerAt, error) { return h.Filelist(r) }

// truncate sets a file's size (setstat with a size, as sent by sshfs, rclone
// and resuming clients), reserving growth and releasing what a shrink frees.
func (h *sftpFS) truncate(p string, size int64) error {
	h.disk.shape.Lock()
	defer h.disk.shape.Unlock()
	f, err := h.dir.OpenFile(p, os.O_WRONLY, 0)
	if err != nil {
		return deny(err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return sftp.ErrSSHFxPermissionDenied
	}
	old := fi.Size()
	if size > old {
		h.disk.pend(size - old)
		defer h.disk.pend(old - size)
		if err := h.check(size - old); err != nil {
			return err
		}
	}
	// Freed space is released before it is freed (see diskCache.unrelease).
	if size < old {
		h.disk.release(old - size)
	}
	if err := f.Truncate(size); err != nil {
		if size < old {
			h.disk.unrelease(old - size)
		}
		return deny(err)
	}
	return nil
}

// remove deletes a file or an empty directory, releasing a file's size.
func (h *sftpFS) remove(p string) error {
	h.disk.shape.Lock()
	defer h.disk.shape.Unlock()
	fi, err := h.dir.Lstat(p)
	if err != nil {
		return deny(err)
	}
	var freed int64
	if fi.Mode().IsRegular() {
		freed = fi.Size()
	}
	h.disk.release(freed) // before the space is freed: see diskCache.unrelease
	if err := h.dir.Remove(p); err != nil {
		h.disk.unrelease(freed)
		return deny(err)
	}
	return nil
}

// rename moves from to to. Plain SFTP rename must not replace an existing
// target (clients rely on it to avoid clobbering files); posix-rename does,
// releasing the replaced file's size.
func (h *sftpFS) rename(from, to string, replace bool) error {
	h.disk.shape.Lock()
	defer h.disk.shape.Unlock()
	defer h.disk.renaming()()
	src, err := h.dir.Lstat(from)
	if err != nil {
		return deny(err)
	}
	dst, err := h.dir.Lstat(to)
	switch {
	case err == nil && !replace:
		return sftp.ErrSSHFxFailure
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return deny(err)
	}
	if !replace {
		return h.renameNoReplace(from, to, src)
	}
	// Renaming one name of a file onto another name of it changes nothing.
	var freed int64
	if dst != nil && dst.Mode().IsRegular() && !os.SameFile(src, dst) {
		freed = dst.Size()
	}
	h.disk.release(freed)
	if err := h.dir.Rename(from, to); err != nil {
		h.disk.unrelease(freed)
		return deny(err)
	}
	return nil
}

// renameNoReplace renames without ever replacing a target that appears after
// the check (the game can create files at any time). A file is hard-linked
// to the new name, which fails if it exists, then unlinked from the old one.
// A directory is renamed directly: rename(2) can then only replace an empty
// directory created in between, never a file or anything with content.
func (h *sftpFS) renameNoReplace(from, to string, src fs.FileInfo) error {
	if src.IsDir() {
		return deny(h.dir.Rename(from, to))
	}
	link := h.dir.Link
	if h.link != nil {
		link = h.link
	}
	err := link(from, to)
	switch {
	case errors.Is(err, fs.ErrExist):
		return sftp.ErrSSHFxFailure
	case err != nil:
		// No hard links here (some filesystems, a file another user owns
		// under protected_hardlinks): the check above is all there is.
		return deny(h.dir.Rename(from, to))
	}
	if err := h.dir.Remove(from); err != nil {
		h.dir.Remove(to) // undo: the file keeps its old name only
		return deny(err)
	}
	return nil
}

func (h *sftpFS) Filecmd(r *sftp.Request) (err error) {
	defer survive(h.log, &err)
	if !h.write {
		return sftp.ErrSSHFxPermissionDenied
	}
	switch r.Method {
	case "Setstat":
		a, set := r.Attributes(), r.AttrFlags()
		if a == nil { // attributes cut short or malformed
			return sftp.ErrSSHFxBadMessage
		}
		if set.Size {
			if err := h.truncate(r.Filepath, int64(a.Size)); err != nil {
				return err
			}
		}
		if set.Permissions {
			if err := h.dir.Chmod(r.Filepath, a.FileMode()); err != nil {
				return deny(err)
			}
		}
		if set.Acmodtime {
			return deny(h.dir.Chtimes(r.Filepath, a.AccessTime(), a.ModTime()))
		}
		return nil // uid/gid changes are ignored
	case "Rename":
		return h.rename(r.Filepath, r.Target, false)
	case "PosixRename":
		return h.rename(r.Filepath, r.Target, true)
	case "Remove":
		return h.remove(r.Filepath)
	case "Rmdir":
		fi, err := h.dir.Lstat(r.Filepath)
		if err != nil {
			return deny(err)
		}
		if !fi.IsDir() { // a file goes through Remove, which frees its size
			return &fs.PathError{Op: "rmdir", Path: r.Filepath, Err: syscall.ENOTDIR}
		}
		return deny(h.dir.Remove(r.Filepath))
	case "Mkdir":
		return deny(h.dir.MkdirOne(r.Filepath))
	}
	// Symlink and Link are refused: links are how directories get escaped.
	return sftp.ErrSSHFxOpUnsupported
}

type listerAt []os.FileInfo

func (l listerAt) ListAt(f []os.FileInfo, off int64) (int, error) {
	if off >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(f, l[off:])
	if n < len(f) {
		return n, io.EOF
	}
	return n, nil
}

func (h *sftpFS) Filelist(r *sftp.Request) (_ sftp.ListerAt, err error) {
	defer survive(h.log, &err)
	// Stat of a path the client names is allowed with write access alone:
	// clients look up the target before every upload. Listing needs read.
	if !h.read && (r.Method == "List" || !h.write) {
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	switch r.Method {
	case "List":
		l, err := h.dir.ReadDir(r.Filepath)
		return listerAt(l), deny(err)
	case "Stat":
		fi, err := h.dir.Stat(r.Filepath)
		if err != nil {
			return nil, deny(err)
		}
		return listerAt{fi}, nil
	case "Lstat":
		fi, err := h.dir.Lstat(r.Filepath)
		if err != nil {
			return nil, deny(err)
		}
		return listerAt{fi}, nil
	}
	return nil, sftp.ErrSSHFxOpUnsupported // Readlink etc.
}

func pemEncode(b *pem.Block) []byte { return pem.EncodeToMemory(b) }
