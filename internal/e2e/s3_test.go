package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon"
	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/s3"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store/storetest"
)

// TestS3Backups runs against a real S3-compatible store. It is skipped unless
// GAMEPANEL_TEST_S3_ENDPOINT is set (with _BUCKET, _ACCESS_KEY, _SECRET_KEY;
// path-style addressing), e.g. a throwaway MinIO:
//
//	docker run -d --name gp-minio -p 9000:9000 minio/minio server /data
func TestS3Backups(t *testing.T) {
	t.Parallel()
	ep := os.Getenv("GAMEPANEL_TEST_S3_ENDPOINT")
	if ep == "" {
		t.Skip("GAMEPANEL_TEST_S3_ENDPOINT not set")
	}
	cl, err := s3.New(s3.Config{
		Endpoint: ep, Bucket: os.Getenv("GAMEPANEL_TEST_S3_BUCKET"), AccessKey: os.Getenv("GAMEPANEL_TEST_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("GAMEPANEL_TEST_S3_SECRET_KEY"), PathStyle: true, Prefix: "test-" + time.Now().Format("150405.000") + "/",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := cl.CreateBucket(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := cl.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	old := s3.MinPartSize
	s3.MinPartSize = 5 << 20 // several parts for a ~12 MB archive
	defer func() { s3.MinPartSize = old }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, _ := storetest.Open(filepath.Join(t.TempDir(), "panel.db"))
	defer st.Close()
	st.CreateUser(ctx, "admin@example.com", "correct-horse-battery", true)
	h := hub.New(log)
	ts := httptest.NewServer(api.New(api.Deps{Store: st, Hub: h, Log: log, S3: cl}))
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	admin := &client{t, ts.URL, &http.Client{Jar: jar}}
	admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": "correct-horse-battery"}, 200)
	var node struct {
		Token string
		Node  struct{ ID int64 }
	}
	json.Unmarshal(admin.do("POST", "/api/nodes", map[string]any{"name": "n", "port_start": 25565, "port_end": 25570}, 201), &node)
	dataDir := t.TempDir()
	dm, _ := daemon.New(daemon.Config{PanelURL: "ws" + strings.TrimPrefix(ts.URL, "http") + "/daemon/connect", Token: node.Token, DataDir: dataDir}, runtime.NewFake(), log)
	go dm.Run(ctx)
	waitFor(t, "node online", func() bool { return h.Online(node.Node.ID) })
	var e struct{ ID int64 }
	json.Unmarshal(admin.do("POST", "/api/eggs/import", egg, 201), &e)
	var srv struct{ UUID string }
	json.Unmarshal(admin.do("POST", "/api/servers", map[string]any{"name": "s", "node_id": node.Node.ID, "egg_id": e.ID, "memory_mb": 128}, 201), &srv)
	base := "/api/servers/" + srv.UUID

	// Random data does not compress, so the archive spans several parts.
	world := make([]byte, 12<<20)
	rand.Read(world)
	if code, b, _ := admin.raw("PUT", base+"/files/upload?path=/world.bin", world); code != 201 {
		t.Fatalf("upload %d %s", code, b)
	}
	var b store.Backup
	json.Unmarshal(admin.do("POST", base+"/backups", map[string]any{"name": "to s3"}, 202), &b)
	waitFor(t, "backup done", func() bool {
		var list []store.Backup
		json.Unmarshal(admin.do("GET", base+"/backups", nil, 200), &list)
		if len(list) == 1 && list[0].Status == "failed" {
			t.Fatalf("backup failed: %s", list[0].Error)
		}
		return len(list) == 1 && list[0].Status == "ok"
	})
	if _, err := os.Stat(filepath.Join(dataDir, "backups", srv.UUID, b.UUID+".tar.gz")); err == nil {
		t.Fatal("the node kept a local copy of an S3 backup")
	}

	// Download: a redirect to a presigned URL that serves the archive.
	noRedirect := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noRedirect.Get(ts.URL + base + "/backups/" + b.UUID + "/download")
	if err != nil || resp.StatusCode != http.StatusFound {
		t.Fatalf("download: %v %v", resp, err)
	}
	loc := resp.Header.Get("Location")
	if strings.Contains(loc, os.Getenv("GAMEPANEL_TEST_S3_SECRET_KEY")) {
		t.Fatal("the secret key is in the download URL")
	}
	arch, err := http.Get(loc)
	if err != nil || arch.StatusCode != http.StatusOK {
		t.Fatalf("presigned GET: %v %v", arch, err)
	}
	body, _ := io.ReadAll(arch.Body)
	arch.Body.Close()
	if len(body) < 12<<20 || body[0] != 0x1f {
		t.Fatalf("archive %d bytes", len(body))
	}

	// Restore pulls it back from S3.
	admin.do("DELETE", base+"/files?path=/world.bin", nil, 204)
	admin.do("POST", base+"/backups/"+b.UUID+"/restore", map[string]any{}, 202)
	waitFor(t, "restored", func() bool {
		code, got, _ := admin.raw("GET", base+"/files/download?path=/world.bin", nil)
		return code == 200 && bytes.Equal(got, world)
	})

	// Delete removes the object.
	admin.do("DELETE", base+"/backups/"+b.UUID, nil, 204)
	if r, _ := http.Get(cl.Presign("GET", cl.Key(srv.UUID, b.UUID), time.Minute, nil)); r == nil || r.StatusCode != http.StatusNotFound {
		t.Fatalf("object still there after delete: %v", r)
	}
}
