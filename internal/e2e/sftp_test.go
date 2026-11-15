package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func (s *stack) apiKey(c *client) string {
	s.t.Helper()
	var r struct{ Token string }
	json.Unmarshal(c.do("POST", "/api/auth/keys", map[string]any{"name": "sftp"}, 201), &r)
	return r.Token
}

// dial returns an SFTP client or the connection error.
func (s *stack) dial(user, pass string) (*sftp.Client, *ssh.Client, error) {
	cfg := &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{ssh.Password(pass)}, HostKeyCallback: ssh.InsecureIgnoreHostKey()}
	conn, err := ssh.Dial("tcp", s.sftp, cfg)
	if err != nil {
		return nil, nil, err
	}
	c, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	s.t.Cleanup(func() { c.Close(); conn.Close() })
	return c, conn, nil
}

func TestSFTP(t *testing.T) {
	s := newStack(t)
	pre := s.server[:8]
	adminUser := "admin@example.com." + pre
	adminKey := s.apiKey(s.admin)

	c, conn, err := s.dial(adminUser, adminKey)
	if err != nil {
		t.Fatalf("owner login: %v", err)
	}
	// full round trip, visible through the panel's own file API
	f, err := c.Create("/up.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("hello sftp"))
	f.Close()
	if got, code := s.read("up.txt"); code != 200 || got != "hello sftp" {
		t.Fatalf("panel sees %q (%d)", got, code)
	}
	s.write("from-panel.txt", "p")
	r, err := c.Open("/from-panel.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != "p" {
		t.Fatalf("read %q", b)
	}
	if err := c.MkdirAll("/world/region"); err != nil && c.Mkdir("/world") != nil {
		t.Fatal(err)
	}
	if err := c.Rename("/up.txt", "/renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if ents, err := c.ReadDir("/"); err != nil || len(ents) < 3 {
		t.Fatalf("readdir: %v %d", err, len(ents))
	}
	if err := c.Remove("/renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove("/"); err == nil {
		t.Fatal("removed the server root")
	}

	// a hostile server plants symlinks pointing outside its directory
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("top secret"), 0o600)
	srvDir := filepath.Join(s.dataDir, "servers", s.server)
	os.Symlink(outside, filepath.Join(srvDir, "linkdir"))
	os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(srvDir, "linkfile"))
	if r, err := c.Open("/linkfile"); err == nil {
		b, _ := io.ReadAll(r)
		t.Fatalf("read through a symlink: %q", b)
	}
	if _, err := c.ReadDir("/linkdir"); err == nil {
		t.Fatal("listed through a directory symlink")
	}
	if w, err := c.Create("/linkdir/pwned"); err == nil {
		w.Close()
		t.Fatal("wrote through a directory symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned")); err == nil {
		t.Fatal("file escaped the server directory")
	}
	if err := c.Symlink("/etc/passwd", "/mine"); err == nil {
		t.Fatal("client created a symlink")
	}
	if _, err := c.ReadLink("/linkfile"); err == nil {
		t.Fatal("readlink allowed")
	}

	// no shell, no exec, no port forwarding
	sess, err := conn.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Run("id"); err == nil {
		t.Fatal("exec allowed")
	}
	sess.Close()
	if err := sess.Shell(); err == nil {
		t.Fatal("shell allowed")
	}
	if _, err := conn.Dial("tcp", "127.0.0.1:22"); err == nil {
		t.Fatal("port forwarding allowed")
	}
}

func TestSFTPAuthRules(t *testing.T) {
	s := newStack(t)
	pre := s.server[:8]
	bobUser, _ := s.st.CreateUser(s.ctx, "bob@example.com", "another-long-password", false)
	jar, _ := cookiejar.New(nil)
	bob := &client{t, s.admin.base, &http.Client{Jar: jar}}
	bob.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "another-long-password"}, 200)
	bobKey := s.apiKey(bob)
	adminKey := s.apiKey(s.admin)
	s.write("a.txt", "A")

	// not a subuser yet: indistinguishable from a wrong password
	if _, _, err := s.dial("bob@example.com."+pre, bobKey); err == nil {
		t.Fatal("stranger logged in")
	}
	s.admin.do("POST", "/api/servers/"+s.server+"/users", map[string]any{"email": "bob@example.com", "permissions": []string{"files.read"}}, 201)
	c, _, err := s.dial("bob@example.com."+pre, bobKey)
	if err != nil {
		t.Fatalf("read-only subuser: %v", err)
	}
	if r, err := c.Open("/a.txt"); err != nil {
		t.Fatalf("read denied: %v", err)
	} else {
		r.Close()
	}
	if w, err := c.Create("/b.txt"); err == nil {
		w.Close()
		t.Fatal("read-only user wrote a file")
	}
	if err := c.Remove("/a.txt"); err == nil {
		t.Fatal("read-only user deleted a file")
	}
	// permission changes apply to new connections
	s.admin.do("PUT", "/api/servers/"+s.server+"/users/"+itoa(bobUser.ID), map[string]any{"permissions": []string{"console.read"}}, 204)
	if _, _, err := s.dial("bob@example.com."+pre, bobKey); err == nil {
		t.Fatal("login without any file permission")
	}

	// credentials that must not work
	for name, c := range map[string][2]string{
		"account password instead of a key": {"admin@example.com." + pre, "correct-horse-battery"},
		"someone else's email":              {"admin@example.com." + pre, bobKey},
		"unknown server prefix":             {"admin@example.com.deadbeef", adminKey},
		"missing prefix":                    {"admin@example.com", adminKey},
		"wrong key":                         {"admin@example.com." + pre, "gp_0000000000000000000000000000000000000000000000000000000000000000"},
		"prefix wildcard":                   {"admin@example.com.%%%%%%%%", adminKey},
	} {
		if _, _, err := s.dial(c[0], c[1]); err == nil {
			t.Errorf("%s: logged in", name)
		}
	}
	if _, _, err := s.dial("admin@example.com."+pre, adminKey); err != nil {
		t.Fatalf("valid owner login rejected after failures: %v", err)
	}
	// a revoked key stops working
	var keys []struct{ ID int64 }
	json.Unmarshal(s.admin.do("GET", "/api/auth/keys", nil, 200), &keys)
	s.admin.do("DELETE", "/api/auth/keys/"+itoa(keys[0].ID), nil, 204)
	if _, _, err := s.dial("admin@example.com."+pre, adminKey); err == nil {
		t.Fatal("revoked key still works")
	}
}
