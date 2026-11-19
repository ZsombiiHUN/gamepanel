package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// The SFTP protocol suite runs the daemon's real SSH server against a fake
// panel and drives it with a real SFTP client, so every request goes through
// the same code a FileZilla or WinSCP session does.

const rigToken = "node-token-for-tests"

// fakePanel answers /daemon/sftp-auth from a table of logins.
type fakePanel struct {
	srv *httptest.Server

	mu     sync.Mutex
	logins map[string]proto.SFTPAuthResp // user + "\x00" + password
	reqs   []proto.SFTPAuthReq
	bad    []string // anything about a request that a real panel would refuse
	// mode makes the panel misbehave: "" answers normally, "hangup" drops the
	// connection, "garbage" sends a body that is not JSON, and a number
	// ("502") answers with that status.
	mode  string
	delay time.Duration // wait this long before answering
}

func newFakePanel(t testing.TB) *fakePanel {
	t.Helper()
	p := &fakePanel{logins: map[string]proto.SFTPAuthResp{}}
	p.srv = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakePanel) serve(w http.ResponseWriter, r *http.Request) {
	var req proto.SFTPAuthReq
	json.NewDecoder(r.Body).Decode(&req)
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	if r.Method != http.MethodPost || r.URL.Path != "/daemon/sftp-auth" {
		p.bad = append(p.bad, r.Method+" "+r.URL.String())
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+rigToken {
		p.bad = append(p.bad, "authorization "+got)
	}
	resp, mode, delay := p.logins[req.Username+"\x00"+req.Password], p.mode, p.delay
	p.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
	}
	switch mode {
	case "":
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	case "hangup":
		c, _, _ := w.(http.Hijacker).Hijack()
		c.Close()
	case "garbage":
		io.WriteString(w, "<html>not json")
	default:
		code := 0
		for _, ch := range mode {
			code = code*10 + int(ch-'0')
		}
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(resp) // a proxy error page may well carry a body
	}
}

func (p *fakePanel) grant(user, pw string, r proto.SFTPAuthResp) {
	p.mu.Lock()
	p.logins[user+"\x00"+pw] = r
	p.mu.Unlock()
}

func (p *fakePanel) revoke(user, pw string) {
	p.mu.Lock()
	delete(p.logins, user+"\x00"+pw)
	p.mu.Unlock()
}

func (p *fakePanel) setMode(m string) {
	p.mu.Lock()
	p.mode = m
	p.mu.Unlock()
}

func (p *fakePanel) setDelay(d time.Duration) {
	p.mu.Lock()
	p.delay = d
	p.mu.Unlock()
}

func (p *fakePanel) requests() []proto.SFTPAuthReq {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]proto.SFTPAuthReq(nil), p.reqs...)
}

type sftpRig struct {
	t      testing.TB
	d      *Daemon
	p      *fakePanel
	addr   string
	id     string // the server every default login is for
	cancel context.CancelFunc
}

// Default logins, all for rig.id.
const (
	ownerUser, ownerKey   = "owner@example.com.00000000", "gp_owner"
	readerUser, readerKey = "reader@example.com.00000000", "gp_reader"
	writerUser, writerKey = "writer@example.com.00000000", "gp_writer"
)

// rigOpts are the less common rig settings.
type rigOpts struct {
	cfg    func(*Config)
	log    io.Writer // daemon log (default: discarded)
	listen string    // default 127.0.0.1:0
}

// newSFTPRig starts a daemon with SFTP on a random port and one server
// (diskMB 0 = unlimited). Rechecks and keepalives are off unless cfg sets them.
func newSFTPRig(t testing.TB, diskMB int64, cfg func(*Config)) *sftpRig {
	t.Helper()
	return newSFTPRigWith(t, diskMB, rigOpts{cfg: cfg})
}

func newSFTPRigWith(t testing.TB, diskMB int64, o rigOpts) *sftpRig {
	t.Helper()
	p := newFakePanel(t)
	c := Config{
		PanelURL: "ws" + strings.TrimPrefix(p.srv.URL, "http") + "/daemon/connect", Token: rigToken,
		DataDir: t.TempDir(), DiskCacheTTL: time.Hour, SFTPRecheck: time.Hour, SFTPKeepAlive: time.Hour,
	}
	if o.cfg != nil {
		o.cfg(&c)
	}
	if o.log == nil {
		o.log = io.Discard
	}
	if o.listen == "" {
		o.listen = "127.0.0.1:0"
	}
	d, err := New(c, runtime.NewFake(), slog.New(slog.NewTextHandler(o.log, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatal(err)
	}
	r := &sftpRig{t: t, d: d, p: p, id: uuidN(1)}
	addServer(t, d, r.id, diskMB)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.cancel = cancel
	if r.addr, err = d.StartSFTP(ctx, o.listen); err != nil {
		t.Fatal(err)
	}
	p.grant(ownerUser, ownerKey, proto.SFTPAuthResp{OK: true, ServerUUID: r.id, Read: true, Write: true})
	p.grant(readerUser, readerKey, proto.SFTPAuthResp{OK: true, ServerUUID: r.id, Read: true})
	p.grant(writerUser, writerKey, proto.SFTPAuthResp{OK: true, ServerUUID: r.id, Write: true})
	return r
}

func (r *sftpRig) sshConfig(user, pw string) *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User: user, Auth: []ssh.AuthMethod{ssh.Password(pw)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second,
	}
}

// sshDial opens an SSH connection, closed when the test ends.
func (r *sftpRig) sshDial(cfg *ssh.ClientConfig) (*ssh.Client, error) {
	conn, err := ssh.Dial("tcp", r.addr, cfg)
	if err != nil {
		return nil, err
	}
	r.t.Cleanup(func() { conn.Close() })
	return conn, nil
}

// dial logs in over SFTP. The SSH connection is closed at cleanup (sshDial).
func (r *sftpRig) dial(user, pw string, opts ...sftp.ClientOption) (*sftp.Client, error) {
	conn, err := r.sshDial(r.sshConfig(user, pw))
	if err != nil {
		return nil, err
	}
	c, err := sftp.NewClient(conn, opts...)
	if err != nil {
		return nil, err
	}
	r.t.Cleanup(func() { c.Close() })
	return c, nil
}

func (r *sftpRig) login(user, pw string, opts ...sftp.ClientOption) *sftp.Client {
	r.t.Helper()
	c, err := r.dial(user, pw, opts...)
	if err != nil {
		r.t.Fatalf("login as %s: %v", user, err)
	}
	return c
}

func (r *sftpRig) owner() *sftp.Client { r.t.Helper(); return r.login(ownerUser, ownerKey) }

// path is where a server-relative name lives on the host.
func (r *sftpRig) path(name string) string {
	return filepath.Join(r.d.serverDir(r.id), filepath.FromSlash(name))
}

func (r *sftpRig) put(name, content string) {
	r.t.Helper()
	p := r.path(name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *sftpRig) get(name string) string {
	r.t.Helper()
	b, err := os.ReadFile(r.path(name))
	if err != nil {
		r.t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func (r *sftpRig) exists(name string) bool {
	_, err := os.Lstat(r.path(name))
	return err == nil
}

// upload writes content to name over SFTP and reports the first error.
func upload(c *sftp.Client, name string, content []byte) error {
	f, err := c.Create(name)
	if err != nil {
		return err
	}
	_, err = f.Write(content)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func download(c *sftp.Client, name string) ([]byte, error) {
	f, err := c.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// closedWithin reports whether the session stops working in time.
func closedWithin(c *sftp.Client, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if _, err := c.Getwd(); err != nil {
			return true
		}
		if _, err := c.Stat("/"); err != nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// eventually polls cond until it holds or the time is up.
func eventually(within time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(within)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func isNotExist(err error) bool   { return errors.Is(err, os.ErrNotExist) }
func isPermission(err error) bool { return errors.Is(err, os.ErrPermission) }

var _ = net.Dial

func authOK(id string, read, write bool) proto.SFTPAuthResp {
	return proto.SFTPAuthResp{OK: true, ServerUUID: id, Read: read, Write: write}
}
