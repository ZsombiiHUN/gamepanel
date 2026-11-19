package daemon

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

func TestPublicIP(t *testing.T) {
	t.Parallel()
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "1.1.1.1": true, "2606:4700:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "0.0.0.0": false, "::1": false,
		"fe80::1": false, "fc00::1": false, "::ffff:127.0.0.1": false, "64:ff9b::a00:1": false,
		"224.0.0.1": false, "255.255.255.255": false, "198.18.0.1": false,
	} {
		if got := publicIP(net.ParseIP(addr)); got != want {
			t.Errorf("publicIP(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestPullName(t *testing.T) {
	t.Parallel()
	u, _ := url.Parse("https://cdn.example.com/files/Paper-1.21.jar?token=x")
	for disp, want := range map[string]string{
		"":                                "Paper-1.21.jar",
		`attachment; filename="pack.zip"`: "pack.zip",
		`attachment; filename="../../etc/passwd"`: "passwd",
		`attachment; filename=".."`:               "Paper-1.21.jar",
	} {
		r := &http.Response{Header: http.Header{"Content-Disposition": {disp}}}
		got := pullName(r, u)
		if disp == `attachment; filename=".."` {
			if got != "download" {
				t.Errorf("%q -> %q", disp, got)
			}
			continue
		}
		if got != want {
			t.Errorf("%q -> %q, want %q", disp, got, want)
		}
	}
}

func TestPullFetchesIntoTheServer(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	d.cfg.PullAllowPrivate = true // the test server is on loopback
	uuid := uuidN(1)
	addServer(t, d, uuid, 1)
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/files/plugin.jar", http.StatusFound)
		case "/huge":
			w.Write(make([]byte, 2<<20)) // past the 1 MB disk limit
		default:
			w.Write([]byte("jar bytes"))
		}
	}))
	defer src.Close()
	res, err := d.pull(context.Background(), proto.Transfer{UUID: uuid, URL: src.URL + "/redirect", Path: "/plugins/"})
	if err != nil || res.Path != "/plugins/plugin.jar" || res.Size != 9 {
		t.Fatalf("pull: %+v %v", res, err)
	}
	if _, err := d.pull(context.Background(), proto.Transfer{UUID: uuid, URL: src.URL + "/huge", Path: "/big.bin"}); proto.CodeOf(err) != proto.CodeDiskFull {
		t.Fatalf("over-quota pull: %v", err)
	}
	// With the default (private addresses refused) the same server is out of reach.
	d.cfg.PullAllowPrivate = false
	if _, err := d.pull(context.Background(), proto.Transfer{UUID: uuid, URL: src.URL + "/x", Path: "/"}); err == nil || !strings.Contains(err.Error(), "private network") {
		t.Fatalf("private pull: %v", err)
	}
}

func TestPullChecksTheChecksum(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	d.cfg.PullAllowPrivate = true
	uuid := uuidN(2)
	addServer(t, d, uuid, 0)
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("mod bytes")) }))
	defer src.Close()
	good := "e6b3c2e4c1d17b2f2f1b8a95ac76d2bd4e33a2e0d2e8f3c2f1d4f5a7c7b0e9a1" // wrong on purpose
	if _, err := d.pull(context.Background(), proto.Transfer{UUID: uuid, URL: src.URL + "/a.jar", Path: "/mods/", SHA512: good}); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("bad checksum accepted: %v", err)
	}
	dir, _ := d.dir(uuid)
	defer dir.Close()
	if _, err := dir.Read("mods/a.jar"); err == nil {
		t.Fatal("a file with a bad checksum was kept")
	}
	sum := sha512.Sum512([]byte("mod bytes"))
	if _, err := d.pull(context.Background(), proto.Transfer{UUID: uuid, URL: src.URL + "/a.jar", Path: "/mods/", SHA512: hex.EncodeToString(sum[:])}); err != nil {
		t.Fatal(err)
	}
}
