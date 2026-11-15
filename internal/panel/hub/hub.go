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

var ErrNodeOffline = errors.New("node is offline")

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

	mu      sync.RWMutex
	nodes   map[int64]*nodeConn
	srvNode map[string]int64 // server uuid -> node, learned from DB lookups by callers
	states  map[string]string
	stats   map[string]proto.Stats
	backlog map[string][]string
	subs    map[string]map[*Sub]struct{}
}

func New(log *slog.Logger) *Hub {
	return &Hub{
		log: log, nodes: map[int64]*nodeConn{},
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

// Request sends a request to a node and waits for its reply.
func (h *Hub) Request(ctx context.Context, nodeID int64, typ string, payload any) (json.RawMessage, error) {
	h.mu.RLock()
	nc := h.nodes[nodeID]
	h.mu.RUnlock()
	if nc == nil {
		return nil, ErrNodeOffline
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
			return nil, errors.New(r.Err)
		}
		return r.Data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Minute): // image pulls can be slow
		return nil, errors.New("node did not reply in time")
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
			ch <- env
		}
	case proto.EvtHello:
		var hello proto.Hello
		json.Unmarshal(env.Data, &hello)
		if h.OnHello != nil {
			go h.OnHello(ctx, nc.id, hello)
		}
	case proto.EvtConsole:
		var c proto.Console
		if json.Unmarshal(env.Data, &c) != nil {
			return
		}
		h.mu.Lock()
		bl := append(h.backlog[c.UUID], c.Line)
		if len(bl) > backlogLines {
			bl = bl[len(bl)-backlogLines:]
		}
		h.backlog[c.UUID] = bl
		h.mu.Unlock()
		h.publish(c.UUID, Event{Type: "console", Data: proto.Marshal(c)})
	case proto.EvtState:
		var s proto.State
		if json.Unmarshal(env.Data, &s) != nil {
			return
		}
		h.mu.Lock()
		h.states[s.UUID] = s.State
		h.mu.Unlock()
		h.publish(s.UUID, Event{Type: "state", Data: proto.Marshal(s)})
	case proto.EvtStats:
		var s proto.Stats
		if json.Unmarshal(env.Data, &s) != nil {
			return
		}
		h.mu.Lock()
		h.stats[s.UUID] = s
		h.mu.Unlock()
		h.publish(s.UUID, Event{Type: "stats", Data: proto.Marshal(s)})
	}
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
}
