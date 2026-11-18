package daemon

import (
	"bytes"
	"context"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The OpenSSH sftp client, the one most people (and WinSCP's and
// FileZilla's protocol choices) are measured against. Skipped where it is
// not installed.
func runOpenSSH(t *testing.T, r *sftpRig, user, key, local string, script string) string {
	t.Helper()
	bin, err := exec.LookPath("sftp")
	if err != nil {
		t.Skip("no OpenSSH sftp client")
	}
	askpass := filepath.Join(t.TempDir(), "askpass.sh")
	os.WriteFile(askpass, []byte("#!/bin/sh\necho '"+key+"'\n"), 0o700)
	_, port, _ := net.SplitHostPort(r.addr)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin,
		"-oUser="+user, "-oPort="+port,
		"-oStrictHostKeyChecking=no", "-oUserKnownHostsFile=/dev/null", "-oLogLevel=ERROR",
		"-oPubkeyAuthentication=no", "-oPreferredAuthentications=password", "-oNumberOfPasswordPrompts=1",
		"127.0.0.1")
	cmd.Dir = local
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = append(os.Environ(), "SSH_ASKPASS="+askpass, "SSH_ASKPASS_REQUIRE=force", "DISPLAY=none")
	out, _ := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("sftp hung:\n%s", out)
	}
	return string(out)
}

func TestSFTPOpenSSHClient(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	local := t.TempDir()
	data := make([]byte, 3<<20+123)
	rand.New(rand.NewSource(3)).Read(data)
	os.WriteFile(filepath.Join(local, "a.bin"), data, 0o644)
	os.WriteFile(filepath.Join(local, "c.bin"), []byte("replacement"), 0o644)
	os.WriteFile(filepath.Join(local, "resume.bin"), data, 0o644)
	os.WriteFile(filepath.Join(local, "half.bin"), data[:1<<20], 0o644)
	r.put("up/resume.bin", string(data[:1<<20])) // an interrupted upload
	r.put("dl.bin", string(data))

	out := runOpenSSH(t, r, ownerUser, ownerKey, local, strings.Join([]string{
		"mkdir up/sub",
		"put a.bin up/a.bin",
		"get up/a.bin got.bin",
		"put c.bin up/c.bin",
		"rename up/c.bin up/a.bin", // posix-rename: replaces
		"chmod 640 up/a.bin",
		"ls -l up",
		"df",
		"ln up/a.bin up/hard",
		"ln -s up/a.bin up/sym",
		"reput resume.bin up/resume.bin",
		"reget dl.bin half.bin",
		"put -p a.bin up/p.bin",
		"rm up/sub/../p.bin",
		"rmdir up/sub",
		"cd /..",
		"pwd",
		"bye",
	}, "\n")+"\n")
	t.Logf("sftp said:\n%s", out)

	if b, _ := os.ReadFile(filepath.Join(local, "got.bin")); !bytes.Equal(b, data) {
		t.Error("download differs from the upload")
	}
	if r.get("up/a.bin") != "replacement" {
		t.Error("rename did not replace the target (OpenSSH's rename uses posix-rename)")
	}
	if fi, _ := os.Stat(r.path("up/a.bin")); fi == nil || fi.Mode().Perm() != 0o640 {
		t.Error("chmod not applied")
	}
	if r.exists("up/hard") || r.exists("up/sym") {
		t.Error("a link was created")
	}
	if r.get("up/resume.bin") != string(data) {
		t.Error("resumed upload differs")
	}
	if b, _ := os.ReadFile(filepath.Join(local, "half.bin")); !bytes.Equal(b, data) {
		t.Error("resumed download differs")
	}
	if r.exists("up/p.bin") || r.exists("up/sub") {
		t.Error("rm/rmdir did not remove")
	}
	for _, want := range []string{"Remote working directory: /", "Size"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	if strings.Contains(out, "Connection closed") || strings.Contains(out, "Permission denied (") {
		t.Error("the session broke")
	}
}

// A read-only user through the real client: downloads work, every change is
// refused, and the session survives the refusals.
func TestSFTPOpenSSHClientReadOnly(t *testing.T) {
	r := newSFTPRig(t, 0, nil)
	local := t.TempDir()
	r.put("a.txt", "server copy")
	os.WriteFile(filepath.Join(local, "x.txt"), []byte("x"), 0o644)
	out := runOpenSSH(t, r, readerUser, readerKey, local, "put x.txt x.txt\nrm a.txt\nmkdir d\nrename a.txt b.txt\nchmod 777 a.txt\nget a.txt got.txt\nbye\n")
	t.Logf("sftp said:\n%s", out)
	if b, _ := os.ReadFile(filepath.Join(local, "got.txt")); string(b) != "server copy" {
		t.Error("download after refusals failed")
	}
	if r.exists("x.txt") || r.exists("d") || r.exists("b.txt") || r.get("a.txt") != "server copy" {
		t.Error("a read-only user changed files")
	}
	if fi, _ := os.Stat(r.path("a.txt")); fi.Mode().Perm() != 0o644 {
		t.Error("a read-only user changed a mode")
	}
}
