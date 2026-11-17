package daemon

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
)

// sftpServer serves each server's data directory over SFTP. Authentication is
// delegated to the panel; file access is confined by fsx like everything else.
type sftpServer struct {
	d       *Daemon
	cfg     *ssh.ServerConfig
	ln      net.Listener
	authURL string
	http    *http.Client

	mu    sync.Mutex
	conns map[string]int
	fails map[string][]time.Time
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

	s := &sftpServer{d: d, authURL: u.String(), http: &http.Client{Timeout: 10 * time.Second},
		conns: map[string]int{}, fails: map[string][]time.Time{}}
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

// ask has the panel check a login. err is set only when the panel could not
// be asked; a refusal is a response with OK false.
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
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&r) != nil || !uuidRe.MatchString(r.ServerUUID) {
		return proto.SFTPAuthResp{}, nil
	}
	return r, nil
}

func (s *sftpServer) auth(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
	ip, _, _ := net.SplitHostPort(c.RemoteAddr().String())
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

func (s *sftpServer) handle(ctx context.Context, nc net.Conn, ip string) {
	defer nc.Close()
	nc.SetDeadline(time.Now().Add(30 * time.Second)) // handshake + auth
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

	ext := conn.Permissions.Extensions
	dir, err := s.d.dir(ext["server"])
	if err != nil {
		return
	}
	defer dir.Close()
	h := &sftpFS{dir: dir, read: ext["read"] == "1", write: ext["write"] == "1",
		check: func(extra int64) error { return s.d.reserveDisk(ext["server"], extra) },
		disk:  s.d.cacheFor(ext["server"])}

	for nch := range chans {
		if nch.ChannelType() != "session" { // no port forwarding, no x11
			nch.Reject(ssh.UnknownChannelType, "only sftp sessions are allowed")
			continue
		}
		ch, creqs, err := nch.Accept()
		if err != nil {
			continue
		}
		go func() {
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
	// check fails when extra more bytes would exceed the server's disk limit.
	check func(extra int64) error
	disk  *diskCache // the server's usage cache; its trunc lock guards truncating opens
}

// limitedWriter refuses writes that would grow the file past the disk limit.
// Only growth past the high-water mark is reserved, so rewriting existing
// bytes is free and every new byte is counted exactly once. The sftp package
// may call WriteAt concurrently, hence the lock.
type limitedWriter struct {
	f     *os.File
	mu    sync.Mutex
	high  int64                   // file size so far (when opened, after any truncate)
	check func(extra int64) error // reserves extra bytes, failing past the limit
}

func (w *limitedWriter) WriteAt(p []byte, off int64) (int, error) {
	w.mu.Lock()
	if end := off + int64(len(p)); end > w.high {
		if err := w.check(end - w.high); err != nil {
			w.mu.Unlock()
			return 0, err
		}
		w.high = end
	}
	w.mu.Unlock()
	return w.f.WriteAt(p, off)
}

func (w *limitedWriter) Close() error { return w.f.Close() }

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

func (h *sftpFS) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	if !h.read {
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	f, err := h.dir.OpenFile(r.Filepath, os.O_RDONLY, 0)
	if err != nil {
		return nil, deny(err)
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	return f, nil
}

func (h *sftpFS) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	if !h.write {
		return nil, sftp.ErrSSHFxPermissionDenied
	}
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
	// No O_APPEND: os.File.WriteAt refuses files opened with it, and the
	// client sends the offset of every write anyway.
	//
	// No O_TRUNC either: the open must not change the file until the disk
	// check has passed. Truncating counts as freeing the file's current size,
	// so emptying a file still works over the limit if that brings usage
	// back under it.
	//
	// Crediting and truncating happen under the server's truncate lock: two
	// concurrent opens of the same file would otherwise both credit its size
	// while the filesystem frees it only once.
	var freed int64
	if p.Trunc {
		h.disk.trunc.Lock()
		defer h.disk.trunc.Unlock()
		if fi, err := h.dir.Stat(r.Filepath); err == nil && fi.Mode().IsRegular() {
			freed = fi.Size()
		}
	}
	if err := h.check(-freed); err != nil { // over the limit: no new writes
		return nil, err
	}
	stale := true // until the truncate frees exactly what was credited
	if freed > 0 {
		// Wrong credit (open or truncate failed, or the file changed since
		// the stat): measure again on the next check.
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
	size := fi.Size()
	if p.Trunc {
		if err := f.Truncate(0); err != nil {
			f.Close()
			return nil, deny(err)
		}
		stale = size != freed
		size = 0
	}
	return &limitedWriter{f: f, high: size, check: h.check}, nil
}

func (h *sftpFS) Filecmd(r *sftp.Request) error {
	if !h.write {
		return sftp.ErrSSHFxPermissionDenied
	}
	switch r.Method {
	case "Setstat":
		a := r.Attributes()
		if r.AttrFlags().Permissions {
			if err := h.dir.Chmod(r.Filepath, a.FileMode()); err != nil {
				return deny(err)
			}
		}
		if r.AttrFlags().Acmodtime {
			return deny(h.dir.Chtimes(r.Filepath, a.AccessTime(), a.ModTime()))
		}
		return nil // uid/gid/size changes are ignored
	case "Rename", "PosixRename":
		return deny(h.dir.Rename(r.Filepath, r.Target))
	case "Rmdir", "Remove":
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

func (h *sftpFS) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	if !h.read {
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
