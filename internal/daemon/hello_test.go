package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// helloFrom runs a daemon against a panel that only reads the first frame.
func helloFrom(t *testing.T, sftp bool) proto.Hello {
	t.Helper()
	got := make(chan proto.Hello, 1)
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		_, b, err := ws.Read(r.Context())
		if err != nil {
			return
		}
		var env proto.Envelope
		var h proto.Hello
		if json.Unmarshal(b, &env) == nil && env.Type == proto.EvtHello && json.Unmarshal(env.Data, &h) == nil {
			select {
			case got <- h:
			default:
			}
		}
		<-r.Context().Done()
	}))
	t.Cleanup(panel.Close)
	d, err := New(Config{PanelURL: "ws" + strings.TrimPrefix(panel.URL, "http"), Token: "t", DataDir: t.TempDir()},
		runtime.NewFake(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	want := 0
	if sftp {
		addr, err := d.StartSFTP(ctx, "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		_, p, _ := net.SplitHostPort(addr)
		want, _ = strconv.Atoi(p)
	}
	go d.Run(ctx)
	select {
	case h := <-got:
		if h.SFTPPort != want {
			t.Fatalf("hello reports SFTP port %d, want %d", h.SFTPPort, want)
		}
		return h
	case <-time.After(5 * time.Second):
		t.Fatal("no hello")
	}
	return proto.Hello{}
}

// The panel builds sftp:// links from the port the daemon reports.
func TestHelloReportsTheSFTPPort(t *testing.T) {
	t.Parallel()
	helloFrom(t, true)
	helloFrom(t, false)
}
