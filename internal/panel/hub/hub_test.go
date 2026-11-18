package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// fakeNode is a daemon on the other end of a hub connection.
type fakeNode struct {
	t  *testing.T
	ws *websocket.Conn
}

// connect serves the hub over HTTP (node id from the URL) and dials it as a node.
func connect(t *testing.T, h *Hub, node int64) *fakeNode {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		id, _ := strconv.ParseInt(r.URL.Query().Get("node"), 10, 64)
		h.Serve(r.Context(), ws, id)
	}))
	t.Cleanup(ts.Close)
	ws, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(ts.URL, "http")+"/?node="+strconv.FormatInt(node, 10), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.CloseNow() })
	deadline := time.Now().Add(2 * time.Second)
	for !h.Online(node) {
		if time.Now().After(deadline) {
			t.Fatal("node never came online")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return &fakeNode{t, ws}
}

func (n *fakeNode) send(env proto.Envelope) {
	if err := n.write(env); err != nil {
		n.t.Fatal(err)
	}
}

// write is send for background goroutines, which may outlive the test.
func (n *fakeNode) write(env proto.Envelope) error {
	b, _ := json.Marshal(env)
	return n.ws.Write(context.Background(), websocket.MessageText, b)
}

// serve answers every request after delay, until the connection closes.
func (n *fakeNode) serve(delay time.Duration, reply func(proto.Envelope) proto.Envelope) {
	go func() {
		for {
			_, b, err := n.ws.Read(context.Background())
			if err != nil {
				return
			}
			var env proto.Envelope
			json.Unmarshal(b, &env)
			time.Sleep(delay)
			r := reply(env)
			r.Type, r.ID = proto.EvtReply, env.ID
			if n.write(r) != nil {
				return
			}
		}
	}()
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// A backup or restore passes its own (hours long) deadline; the default
// request timeout must not cut it off. Requests without a deadline still get one.
func TestRequestHonoursTheCallersDeadline(t *testing.T) {
	h := New(quiet())
	h.RequestTimeout = 50 * time.Millisecond
	n := connect(t, h, 1)
	n.serve(200*time.Millisecond, func(proto.Envelope) proto.Envelope { return proto.Envelope{Data: proto.Marshal("done")} })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := h.Request(ctx, 1, proto.ReqBackupCreate, nil); err != nil {
		t.Fatalf("a request with a long deadline was cut off: %v", err)
	}
	if _, err := h.Request(context.Background(), 1, proto.ReqBackupCreate, nil); err == nil || !strings.Contains(err.Error(), "did not reply in time") {
		t.Fatalf("a request without a deadline should time out by default, got %v", err)
	}
}

func TestFailedRepliesKeepTheirCode(t *testing.T) {
	h := New(quiet())
	n := connect(t, h, 1)
	n.serve(0, func(proto.Envelope) proto.Envelope {
		return proto.Envelope{Err: "stop the server first", Code: proto.CodeConflict}
	})
	_, err := h.Request(context.Background(), 1, proto.ReqServerUpdate, nil)
	if proto.CodeOf(err) != proto.CodeConflict || err.Error() != "stop the server first" {
		t.Fatalf("got %v (code %q)", err, proto.CodeOf(err))
	}
	if _, err := h.Request(context.Background(), 2, proto.ReqServerUpdate, nil); !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("offline node: %v", err)
	}
}

// A compromised node must not be able to write into the console, state or
// stats of a server that lives on another node.
func TestEventsFromTheWrongNodeAreDropped(t *testing.T) {
	const srv = "11111111-2222-4333-8444-555555555555"
	h := New(quiet())
	h.Locate = func(_ context.Context, uuid string) (int64, error) {
		if uuid == srv {
			return 1, nil
		}
		return 0, errors.New("no such server")
	}
	sub, _ := h.Subscribe(srv)
	defer h.Unsubscribe(sub)
	evil, good := connect(t, h, 2), connect(t, h, 1)

	evil.send(proto.Envelope{Type: proto.EvtConsole, Data: proto.Marshal(proto.Console{UUID: srv, Line: "injected"})})
	evil.send(proto.Envelope{Type: proto.EvtState, Data: proto.Marshal(proto.State{UUID: srv, State: "running"})})
	evil.send(proto.Envelope{Type: proto.EvtStats, Data: proto.Marshal(proto.Stats{UUID: srv, CPUPct: 99})})
	good.send(proto.Envelope{Type: proto.EvtConsole, Data: proto.Marshal(proto.Console{UUID: srv, Line: "genuine"})})

	select {
	case ev := <-sub.C:
		if !strings.Contains(string(ev.Data), "genuine") {
			t.Fatalf("an event from the wrong node got through: %s", ev.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the genuine event never arrived")
	}
	if st := h.State(srv); st != "unknown" {
		t.Fatalf("the wrong node changed the state to %q", st)
	}
	if _, ok := h.Stats(srv); ok {
		t.Fatal("the wrong node reported stats")
	}
	if _, backlog := h.Subscribe(srv); len(backlog) != 1 || backlog[0] != "genuine" {
		t.Fatalf("backlog: %q", backlog)
	}
}

// A duplicate reply must not stall the read loop (which would freeze every
// console on the node).
func TestDuplicateRepliesDoNotStallTheNode(t *testing.T) {
	h := New(quiet())
	n := connect(t, h, 1)
	go func() {
		_, b, err := n.ws.Read(context.Background())
		if err != nil {
			return
		}
		var env proto.Envelope
		json.Unmarshal(b, &env)
		for range 3 {
			n.write(proto.Envelope{Type: proto.EvtReply, ID: env.ID})
		}
		n.write(proto.Envelope{Type: proto.EvtState, Data: proto.Marshal(proto.State{UUID: "s", State: "running"})})
	}()
	if _, err := h.Request(context.Background(), 1, proto.ReqServerInput, nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for h.State("s") != "running" {
		if time.Now().After(deadline) {
			t.Fatal("events after a duplicate reply were never read")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDisconnectDropsTheNode(t *testing.T) {
	h := New(quiet())
	n := connect(t, h, 1)
	go n.ws.Read(context.Background()) // a real daemon is always reading
	h.Disconnect(1)
	deadline := time.Now().Add(2 * time.Second)
	for h.Online(1) {
		if time.Now().After(deadline) {
			t.Fatal("node still online after Disconnect")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Panel notices reach live viewers and the backlog a new viewer gets.
func TestNoticeReachesTheConsole(t *testing.T) {
	h := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	id := "00000000-0000-4000-8000-000000000001"
	sub, _ := h.Subscribe(id)
	defer h.Unsubscribe(sub)
	h.Notice(id, "start failed: disk limit exceeded")
	select {
	case ev := <-sub.C:
		if ev.Type != "console" || !strings.Contains(string(ev.Data), "disk limit exceeded") {
			t.Fatalf("event %s %s", ev.Type, ev.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("no console event")
	}
	if _, bl := h.Subscribe(id); len(bl) != 1 || !strings.Contains(bl[0], "disk limit exceeded") {
		t.Fatalf("backlog %q", bl)
	}
}

// The SFTP port comes from the daemon's hello and goes with the connection.
func TestSFTPPortFollowsTheConnection(t *testing.T) {
	h := New(quiet())
	if _, ok := h.SFTPPort(1); ok {
		t.Fatal("an offline node has a port")
	}
	n := connect(t, h, 1)
	if p, ok := h.SFTPPort(1); !ok || p != 0 {
		t.Fatalf("before hello: %d %v", p, ok)
	}
	n.send(proto.Envelope{Type: proto.EvtHello, Data: proto.Marshal(proto.Hello{SFTPPort: 2022})})
	waitFor(t, func() bool { p, _ := h.SFTPPort(1); return p == 2022 })
	n.send(proto.Envelope{Type: proto.EvtHello, Data: proto.Marshal(proto.Hello{SFTPPort: 99999})})
	waitFor(t, func() bool { p, _ := h.SFTPPort(1); return p == 65535 })
	n.ws.Close(websocket.StatusNormalClosure, "")
	waitFor(t, func() bool { _, ok := h.SFTPPort(1); return !ok })
	h.mu.RLock()
	_, kept := h.sftp[1]
	h.mu.RUnlock()
	if kept {
		t.Fatal("the port outlived the connection")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
