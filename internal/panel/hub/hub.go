// Package hub tracks connected node daemons, correlates request/reply
// frames, and fans console/state/stats events out to browser subscribers.
package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

const backlogLines = 500

// DefaultRequestTimeout bounds a request whose context has no deadline of its
// own. Long operations (backups, restores) pass a context with a longer one.
const DefaultRequestTimeout = 5 * time.Minute

var ErrNodeOffline = errors.New("node is offline")

// ErrTimeout means the node took longer than the request's deadline.
var ErrTimeout = errors.New("node did not reply in time")

type Event struct {
	Type string          `json:"type"` // console | state | stats
	Data json.RawMessage `json:"data"`
}

type Sub struct {
	C    chan Event
	uuid string
}

type nodeConn struct {
	id      int64
	ws      *websocket.Conn
	wmu     sync.Mutex
	pmu     sync.Mutex
	pending map[string]chan proto.Envelope
	seq     atomic.Uint64
}

type Hub struct {
	log *slog.Logger

	// OnHello runs (in its own goroutine) after a daemon connects, so the
	// panel can push any servers the daemon is missing.
	OnHello func(ctx context.Context, nodeID int64, h proto.Hello)
	// OnStats, when set, sees every stats sample after it was checked (the
	// panel keeps a history from them). It must not block.
	OnStats func(s proto.Stats)
	// Locate returns the node a server lives on. When set, events a node sends
	// about a server on another node are dropped, so one compromised node
	// cannot write into the consoles or state of servers it does not host.
	Locate func(ctx context.Context, uuid string) (int64, error)
	// RequestTimeout bounds a request whose context has no deadline of its
	// own (default DefaultRequestTimeout).
	RequestTimeout time.Duration

	mu      sync.RWMutex
	nodes   map[int64]*nodeConn
	sftp    map[int64]int    // node -> the SFTP port its daemon reported (0 = off)
	srvNode map[string]int64 // server uuid -> node, cached results of Locate
	states  map[string]string
	stats   map[string]proto.Stats
	backlog map[string][]string
	subs    map[string]map[*Sub]struct{}
}

func New(log *slog.Logger) *Hub {
	return &Hub{
		log: log, nodes: map[int64]*nodeConn{}, sftp: map[int64]int{}, srvNode: map[string]int64{}, RequestTimeout: DefaultRequestTimeout,
		states: map[string]string{}, stats: map[string]proto.Stats{},
		backlog: map[string][]string{}, subs: map[string]map[*Sub]struct{}{},
	}
}

func (h *Hub) Online(nodeID int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.nodes[nodeID]
	return ok
}

// SFTPPort is the port a connected node's daemon serves SFTP on. ok is false
// while the node is offline; a port of 0 means its SFTP server is off.
func (h *Hub) SFTPPort(nodeID int64) (port int, ok bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if _, online := h.nodes[nodeID]; !online {
		return 0, false
	}
	return h.sftp[nodeID], true
}

func (h *Hub) State(uuid string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if s, ok := h.states[uuid]; ok {
		return s
	}
	return "unknown"
}

func (h *Hub) Stats(uuid string) (proto.Stats, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s, ok := h.stats[uuid]
	return s, ok
}

// Request sends a request to a node and waits for its reply. A failed reply
// is returned as a *proto.Error carrying the daemon's message and code. If ctx
// has no deadline, RequestTimeout applies; long operations (backups,
// restores) pass a context with their own, longer deadline.
func (h *Hub) Request(ctx context.Context, nodeID int64, typ string, payload any) (json.RawMessage, error) {
	h.mu.RLock()
	nc := h.nodes[nodeID]
	h.mu.RUnlock()
	if nc == nil {
		return nil, ErrNodeOffline
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.RequestTimeout)
		defer cancel()
	}
	id := fmt.Sprintf("%d", nc.seq.Add(1))
	ch := make(chan proto.Envelope, 1)
	nc.pmu.Lock()
	nc.pending[id] = ch
	nc.pmu.Unlock()
	defer func() {
		nc.pmu.Lock()
		delete(nc.pending, id)
		nc.pmu.Unlock()
	}()

	b, _ := json.Marshal(proto.Envelope{Type: typ, ID: id, Data: proto.Marshal(payload)})
	nc.wmu.Lock()
	err := nc.ws.Write(ctx, websocket.MessageText, b)
	nc.wmu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.Err != "" {
			return nil, &proto.Error{Code: r.Code, Msg: r.Err}
		}
		return r.Data, nil
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrTimeout
		}
		return nil, ctx.Err()
	}
}

// Serve runs the read loop for an authenticated daemon connection.
func (h *Hub) Serve(ctx context.Context, ws *websocket.Conn, nodeID int64) {
	ws.SetReadLimit(32 << 20)
	nc := &nodeConn{id: nodeID, ws: ws, pending: map[string]chan proto.Envelope{}}
	h.mu.Lock()
	if old := h.nodes[nodeID]; old != nil {
		old.ws.Close(websocket.StatusPolicyViolation, "replaced by a newer connection")
	}
	h.nodes[nodeID] = nc
	h.mu.Unlock()
	h.log.Info("node connected", "node", nodeID)
	defer func() {
		h.mu.Lock()
		if h.nodes[nodeID] == nc {
			delete(h.nodes, nodeID)
			delete(h.sftp, nodeID)
		}
		h.mu.Unlock()
		nc.pmu.Lock()
		for _, ch := range nc.pending {
			select {
			case ch <- proto.Envelope{Err: "node disconnected"}:
			default:
			}
		}
		nc.pmu.Unlock()
		h.log.Info("node disconnected", "node", nodeID)
	}()

	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var env proto.Envelope
		if json.Unmarshal(data, &env) != nil {
			continue
		}
		h.route(ctx, nc, env)
	}
}

func (h *Hub) route(ctx context.Context, nc *nodeConn, env proto.Envelope) {
	switch env.Type {
	case proto.EvtReply:
		nc.pmu.Lock()
		ch := nc.pending[env.ID]
		nc.pmu.Unlock()
		if ch != nil {
			select { // a duplicate reply must never stall the read loop
			case ch <- env:
			default:
			}
		}
	case proto.EvtHello:
		var hello proto.Hello
		json.Unmarshal(env.Data, &hello)
		h.mu.Lock()
		if h.nodes[nc.id] == nc {
			h.sftp[nc.id] = max(0, min(hello.SFTPPort, 65535))
		}
		h.mu.Unlock()
		if h.OnHello != nil {
			go h.OnHello(ctx, nc.id, hello)
		}
	case proto.EvtConsole:
		var c proto.Console
		if json.Unmarshal(env.Data, &c) != nil || !h.hosts(ctx, nc.id, c.UUID) {
			return
		}
		h.console(c)
	case proto.EvtState:
		var s proto.State
		if json.Unmarshal(env.Data, &s) != nil || !h.hosts(ctx, nc.id, s.UUID) {
			return
		}
		h.mu.Lock()
		h.states[s.UUID] = s.State
		h.mu.Unlock()
		h.publish(s.UUID, Event{Type: "state", Data: proto.Marshal(s)})
	case proto.EvtStats:
		var s proto.Stats
		if json.Unmarshal(env.Data, &s) != nil || !h.hosts(ctx, nc.id, s.UUID) {
			return
		}
		h.mu.Lock()
		h.stats[s.UUID] = s
		h.mu.Unlock()
		h.publish(s.UUID, Event{Type: "stats", Data: proto.Marshal(s)})
		if h.OnStats != nil {
			h.OnStats(s)
		}
	}
}

// hosts reports whether server uuid lives on node. Servers never move between
// nodes, so a positive answer is cached until Forget.
func (h *Hub) hosts(ctx context.Context, node int64, uuid string) bool {
	if h.Locate == nil {
		return true
	}
	h.mu.RLock()
	n, ok := h.srvNode[uuid]
	h.mu.RUnlock()
	if !ok {
		var err error
		if n, err = h.Locate(ctx, uuid); err != nil {
			return false
		}
		h.mu.Lock()
		h.srvNode[uuid] = n
		h.mu.Unlock()
	}
	if n != node {
		h.log.Warn("dropped an event for a server on another node", "node", node, "server", uuid)
		return false
	}
	return true
}

// Disconnect drops a node's connection, e.g. after the node was deleted and
// its token no longer exists.
func (h *Hub) Disconnect(nodeID int64) {
	h.mu.RLock()
	nc := h.nodes[nodeID]
	h.mu.RUnlock()
	if nc != nil {
		nc.ws.Close(websocket.StatusPolicyViolation, "node deleted")
	}
}

// Notice prints a line from the panel itself to a server's console, e.g. why
// a power action failed. Viewers see it live and in the backlog.
func (h *Hub) Notice(uuid, line string) {
	h.console(proto.Console{UUID: uuid, Line: line})
}

func (h *Hub) console(c proto.Console) {
	h.mu.Lock()
	bl := append(h.backlog[c.UUID], c.Line)
	if len(bl) > backlogLines {
		bl = bl[len(bl)-backlogLines:]
	}
	h.backlog[c.UUID] = bl
	h.mu.Unlock()
	h.publish(c.UUID, Event{Type: "console", Data: proto.Marshal(c)})
}

func (h *Hub) publish(uuid string, ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs[uuid] {
		select {
		case s.C <- ev:
		default: // slow browser: drop rather than stall the daemon read loop
		}
	}
}

// Subscribe returns a subscription plus the recent console backlog.
func (h *Hub) Subscribe(uuid string) (*Sub, []string) {
	s := &Sub{C: make(chan Event, 256), uuid: uuid}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[uuid] == nil {
		h.subs[uuid] = map[*Sub]struct{}{}
	}
	h.subs[uuid][s] = struct{}{}
	return s, append([]string(nil), h.backlog[uuid]...)
}

func (h *Hub) Unsubscribe(s *Sub) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs[s.uuid], s)
}

// Forget drops cached state for a deleted server.
func (h *Hub) Forget(uuid string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.states, uuid)
	delete(h.stats, uuid)
	delete(h.backlog, uuid)
	delete(h.srvNode, uuid)
}
