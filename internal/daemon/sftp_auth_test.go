package daemon

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// ---- sign-in ----

func TestSFTPLoginAsksThePanel(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	if _, err := c.Stat("/"); err != nil {
		t.Fatal(err)
	}
	reqs := r.p.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d auth requests for one login", len(reqs))
	}
	want := proto.SFTPAuthReq{Username: ownerUser, Password: ownerKey, IP: "127.0.0.1"}
	if reqs[0] != want {
		t.Fatalf("panel was asked %+v, want %+v", reqs[0], want)
	}
	if len(r.p.bad) > 0 {
		t.Fatalf("the panel would refuse the request: %v", r.p.bad)
	}
}

func TestSFTPAuthURL(t *testing.T) {
	t.Parallel()
	for panel, want := range map[string]string{
		"ws://panel.local:8080/daemon/connect":            "http://panel.local:8080/daemon/sftp-auth",
		"wss://panel.example.com/daemon/connect?x=1":      "https://panel.example.com/daemon/sftp-auth",
		"wss://panel.example.com/sub/path/daemon/connect": "https://panel.example.com/daemon/sftp-auth",
	} {
		d, err := New(Config{PanelURL: panel, DataDir: t.TempDir()}, runtime.NewFake(), slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		if _, err := d.StartSFTP(ctx, "127.0.0.1:0"); err != nil {
			t.Fatal(err)
		}
		if got := d.sftp.authURL; got != want {
			t.Errorf("%s: asks %s, want %s", panel, got, want)
		}
		cancel()
	}
}

func TestSFTPRefusedLogins(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	other := uuidN(2) // a server the panel knows but this node does not run
	r.p.grant("ghost", "gp_ghost", proto.SFTPAuthResp{OK: true, ServerUUID: other, Read: true, Write: true})
	r.p.grant("badid", "gp_badid", proto.SFTPAuthResp{OK: true, ServerUUID: "../../etc", Read: true, Write: true})
	r.p.grant("empty", "gp_empty", proto.SFTPAuthResp{OK: true, Read: true})
	r.p.grant("notok", "gp_notok", proto.SFTPAuthResp{ServerUUID: r.id, Read: true, Write: true})
	for name, login := range map[string][2]string{
		"wrong key":                    {ownerUser, "gp_wrong"},
		"right key, other user":        {readerUser, ownerKey},
		"empty password":               {ownerUser, ""},
		"server not on this node":      {"ghost", "gp_ghost"},
		"malformed server id":          {"badid", "gp_badid"},
		"no server id":                 {"empty", "gp_empty"},
		"refused with permissions set": {"notok", "gp_notok"},
	} {
		if _, err := r.dial(login[0], login[1]); err == nil {
			t.Errorf("%s: logged in", name)
		}
	}
	// a login refused because the server is unknown must not reach its files
	if _, err := os.Stat(r.d.serverDir(other)); err == nil {
		t.Error("a refused login created a server folder")
	}
}

// The panel misbehaving must never let anyone in.
func TestSFTPPanelFailuresDenyLogin(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	for _, mode := range []string{"hangup", "garbage", "500", "502", "401", "404"} {
		r.p.setMode(mode)
		if _, err := r.dial(ownerUser, ownerKey); err == nil {
			t.Errorf("panel %s: logged in", mode)
		}
	}
	r.p.setMode("")
	r.owner()
}

// Only passwords (API keys) are accepted: no public keys, no "none".
func TestSFTPOnlyPasswordAuth(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	for name, auth := range map[string][]ssh.AuthMethod{
		"public key":           {ssh.PublicKeys(signer)},
		"none":                 {},
		"keyboard-interactive": {ssh.KeyboardInteractive(func(string, string, []string, []bool) ([]string, error) { return []string{ownerKey}, nil })},
	} {
		cfg := r.sshConfig(ownerUser, "")
		cfg.Auth = auth
		if _, err := r.sshDial(cfg); err == nil {
			t.Errorf("%s: logged in", name)
		}
	}
}

func TestSFTPServerIdentity(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	var key1 ssh.PublicKey
	cfg := r.sshConfig(ownerUser, ownerKey)
	cfg.HostKeyCallback = func(_ string, _ net.Addr, k ssh.PublicKey) error { key1 = k; return nil }
	conn, err := r.sshDial(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if v := string(conn.ServerVersion()); v != "SSH-2.0-GamePanel" {
		t.Errorf("server version %q", v)
	}
	if key1.Type() != ssh.KeyAlgoED25519 {
		t.Errorf("host key type %s", key1.Type())
	}
	keyFile := filepath.Join(r.d.cfg.DataDir, "sftp_host_ed25519")
	fi, err := os.Stat(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("host key file mode %v", fi.Mode().Perm())
	}

	// A restart presents the same key, so clients do not see a MITM warning.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr, err := r.d.StartSFTP(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var key2 ssh.PublicKey
	cfg.HostKeyCallback = func(_ string, _ net.Addr, k ssh.PublicKey) error { key2 = k; return nil }
	c2, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	c2.Close()
	if !bytes.Equal(key1.Marshal(), key2.Marshal()) {
		t.Fatal("the host key changed across a restart")
	}

	// A damaged key file is an error, never silently replaced.
	os.WriteFile(keyFile, []byte("garbage"), 0o600)
	if _, err := r.d.StartSFTP(ctx, "127.0.0.1:0"); err == nil {
		t.Fatal("started with a corrupt host key")
	}
	if b, _ := os.ReadFile(keyFile); string(b) != "garbage" {
		t.Fatal("the corrupt host key was overwritten")
	}
}

// ---- guessing and connection limits ----

func TestSFTPFailedLoginsLockOutTheAddress(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	// The panel being down is not a guess: it never locks anyone out.
	r.p.setMode("hangup")
	for range sftpAuthMaxFail + 5 {
		r.dial(ownerUser, ownerKey)
	}
	r.p.setMode("")
	r.owner()

	for range sftpAuthMaxFail {
		if _, err := r.dial(ownerUser, "gp_guess"); err == nil {
			t.Fatal("a wrong key logged in")
		}
	}
	before := len(r.p.requests())
	if _, err := r.dial(ownerUser, ownerKey); err == nil {
		t.Fatal("an address that kept guessing could still log in")
	}
	if len(r.p.requests()) != before {
		t.Fatal("a locked-out address still reaches the panel")
	}
	// Sessions opened before the lockout keep working.
}

// Absurd credentials are refused without bothering the panel, and count as
// a guess.
func TestSFTPOversizedCredentials(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	long := "gp_" + strings.Repeat("a", 64<<10)
	if _, err := r.dial(ownerUser, long); err == nil {
		t.Fatal("logged in with a 64 KB password")
	}
	if _, err := r.dial(strings.Repeat("u", 64<<10), ownerKey); err == nil {
		t.Fatal("logged in with a 64 KB user name")
	}
	for _, q := range r.p.requests() {
		if len(q.Password) > 1024 || len(q.Username) > 1024 {
			t.Fatalf("an oversized login (%d/%d bytes) was sent to the panel", len(q.Username), len(q.Password))
		}
	}
	r.d.sftp.mu.Lock()
	fails := len(r.d.sftp.fails["127.0.0.1"])
	r.d.sftp.mu.Unlock()
	if fails != 2 {
		t.Fatalf("%d failures counted, want 2", fails)
	}
}

func TestSFTPConnectionsPerAddressAreCapped(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	var raw []net.Conn
	for range sftpMaxPerIP {
		c, err := net.Dial("tcp", r.addr)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, c)
		t.Cleanup(func() { c.Close() })
	}
	// all slots held by connections that never even finish the handshake
	if !eventually(2*time.Second, func() bool {
		r.d.sftp.mu.Lock()
		defer r.d.sftp.mu.Unlock()
		return r.d.sftp.conns["127.0.0.1"] == sftpMaxPerIP
	}) {
		t.Fatal("connections were not all admitted")
	}
	if _, err := r.dial(ownerUser, ownerKey); err == nil {
		t.Fatal("a connection past the per-address cap was served")
	}
	raw[0].Close()
	if !eventually(2*time.Second, func() bool { _, err := r.dial(ownerUser, ownerKey); return err == nil }) {
		t.Fatal("a freed slot was never given back")
	}
	for _, c := range raw[1:] {
		c.Close()
	}
	if !eventually(2*time.Second, func() bool {
		r.d.sftp.mu.Lock()
		defer r.d.sftp.mu.Unlock()
		return r.d.sftp.conns["127.0.0.1"] == 1 // the session still open above
	}) {
		t.Fatal("closed connections still hold slots")
	}
}

// ---- what a session may do besides SFTP: nothing ----

func TestSFTPNothingButSFTP(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	conn, err := r.sshDial(r.sshConfig(ownerUser, ownerKey))
	if err != nil {
		t.Fatal(err)
	}
	try := func(name string, f func(*ssh.Session) error) {
		s, err := conn.NewSession()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		defer s.Close()
		if err := f(s); err == nil {
			t.Errorf("%s was allowed", name)
		}
	}
	try("exec", func(s *ssh.Session) error { return s.Run("id") })
	try("shell", func(s *ssh.Session) error { return s.Shell() })
	try("pty", func(s *ssh.Session) error { return s.RequestPty("xterm", 24, 80, nil) })
	try("env", func(s *ssh.Session) error { return s.Setenv("LD_PRELOAD", "/tmp/x.so") })
	try("other subsystem", func(s *ssh.Session) error { return s.RequestSubsystem("netconf") })
	try("subsystem with sftp prefix", func(s *ssh.Session) error { return s.RequestSubsystem("sftpx") })
	try("agent forwarding", func(s *ssh.Session) error {
		ok, err := s.SendRequest("auth-agent-req@openssh.com", true, nil)
		if err == nil && !ok {
			return os.ErrPermission
		}
		return err
	})
	if _, err := conn.Dial("tcp", "127.0.0.1:22"); err == nil {
		t.Error("local port forwarding allowed")
	}
	if _, err := conn.Listen("tcp", "127.0.0.1:0"); err == nil {
		t.Error("remote port forwarding allowed")
	}
	if _, _, err := conn.OpenChannel("x11", nil); err == nil {
		t.Error("an x11 channel was accepted")
	}
	// A refused request does not spoil the connection for SFTP afterwards,
	// and one connection may carry several SFTP sessions.
	a, err := sftp.NewClient(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := sftp.NewClient(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := upload(a, "/a.txt", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if got, err := download(b, "/a.txt"); err != nil || string(got) != "a" {
		t.Fatalf("second session read %q, %v", got, err)
	}
}

// ---- session lifetime ----

func TestSFTPRecheckEndsSessions(t *testing.T) {
	t.Parallel()
	cases := map[string]func(r *sftpRig){
		"key revoked": func(r *sftpRig) { r.p.revoke(ownerUser, ownerKey) },
		"write taken away": func(r *sftpRig) {
			r.p.grant(ownerUser, ownerKey, proto.SFTPAuthResp{OK: true, ServerUUID: r.id, Read: true})
		},
		"read given": func(r *sftpRig) {
			r.p.grant(writerUser, writerKey, proto.SFTPAuthResp{OK: true, ServerUUID: r.id, Read: true, Write: true})
		},
		"pointed at another server": func(r *sftpRig) {
			r.p.grant(ownerUser, ownerKey, proto.SFTPAuthResp{OK: true, ServerUUID: uuidN(9), Read: true, Write: true})
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r := newSFTPRig(t, 0, func(c *Config) { c.SFTPRecheck = 50 * time.Millisecond })
			user, key := ownerUser, ownerKey
			if name == "read given" {
				user, key = writerUser, writerKey
			}
			c := r.login(user, key)
			if closedWithin(c, 300*time.Millisecond) {
				t.Fatal("an unchanged session was closed")
			}
			change(r)
			if !closedWithin(c, 3*time.Second) {
				t.Fatal("the session survived the change")
			}
		})
	}
}

func TestSFTPRecheckIsNotASignIn(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, func(c *Config) { c.SFTPRecheck = 30 * time.Millisecond })
	r.owner()
	if !eventually(3*time.Second, func() bool { return len(r.p.requests()) >= 4 }) {
		t.Fatal("no rechecks")
	}
	for i, q := range r.p.requests() {
		if q.Recheck != (i > 0) || q.Password != ownerKey || q.Username != ownerUser {
			t.Fatalf("request %d: %+v", i, q)
		}
	}
}

// A panel restart (connection refused, or a proxy's 502 while it is down)
// must not cut every open transfer.
func TestSFTPPanelOutageKeepsSessions(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"hangup", "502", "503", "504"} {
		t.Run(mode, func(t *testing.T) {
			r := newSFTPRig(t, 0, func(c *Config) { c.SFTPRecheck = 30 * time.Millisecond })
			c := r.owner()
			n := len(r.p.requests())
			r.p.setMode(mode)
			if !eventually(3*time.Second, func() bool { return len(r.p.requests()) >= n+3 }) {
				t.Fatal("no rechecks during the outage")
			}
			if _, err := c.Stat("/"); err != nil {
				t.Fatalf("the session was cut by a panel outage: %v", err)
			}
			r.p.setMode("")
			r.p.revoke(ownerUser, ownerKey) // and rechecks resume once it is back
			if !closedWithin(c, 3*time.Second) {
				t.Fatal("rechecks did not resume after the outage")
			}
		})
	}
}

func TestSFTPDeletingAServerEndsOnlyItsSessions(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	other := uuidN(2)
	addServer(t, r.d, other, 0)
	r.p.grant("other", "gp_other", proto.SFTPAuthResp{OK: true, ServerUUID: other, Read: true, Write: true})
	mine, survivor := r.owner(), r.login("other", "gp_other")
	if err := r.d.remove(context.Background(), r.id); err != nil {
		t.Fatal(err)
	}
	if !closedWithin(mine, 3*time.Second) {
		t.Fatal("the deleted server's session stayed open")
	}
	if _, err := survivor.Stat("/"); err != nil {
		t.Fatalf("another server's session was closed: %v", err)
	}
	if _, err := r.dial(ownerUser, ownerKey); err == nil {
		t.Fatal("logged in to a deleted server")
	}
}

func TestSFTPShutdownClosesEverything(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	r.cancel()
	if !closedWithin(c, 3*time.Second) {
		t.Fatal("a session outlived the daemon")
	}
	if !eventually(2*time.Second, func() bool {
		nc, err := net.DialTimeout("tcp", r.addr, 200*time.Millisecond)
		if err == nil {
			nc.Close()
		}
		return err != nil
	}) {
		t.Fatal("still listening after shutdown")
	}
}

func TestSFTPKeepAliveSparesLiveClients(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, func(c *Config) { c.SFTPKeepAlive = 20 * time.Millisecond })
	c := r.owner()
	for range 10 {
		time.Sleep(30 * time.Millisecond)
		if _, err := c.Stat("/"); err != nil {
			t.Fatalf("a live client was dropped: %v", err)
		}
	}
	// idle, but answering keepalives: still there
	time.Sleep(300 * time.Millisecond)
	if _, err := c.Stat("/"); err != nil {
		t.Fatalf("an idle live client was dropped: %v", err)
	}
}

// Error messages go to the client: they must not reveal where the files are
// on the host.
func TestSFTPErrorsDoNotLeakHostPaths(t *testing.T) {
	t.Parallel()
	r := newSFTPRig(t, 0, nil)
	c := r.owner()
	r.put("file.txt", "x")
	os.Symlink("/etc", r.path("out"))
	var errs []error
	_, err := c.Open("/missing")
	errs = append(errs, err)
	_, err = c.ReadDir("/missing/dir")
	errs = append(errs, err,
		c.Mkdir("/missing/a/b"),
		c.Rename("/missing", "/x"),
		c.Remove("/missing"),
		c.Mkdir("/file.txt/sub"))
	_, err = c.Open("/out/passwd")
	errs = append(errs, err, c.Chmod("/out/passwd", 0o777))
	for _, err := range errs {
		if err == nil {
			t.Fatal("an impossible request succeeded")
		}
		if strings.Contains(err.Error(), r.d.cfg.DataDir) {
			t.Errorf("error reveals the host path: %v", err)
		}
	}
}
