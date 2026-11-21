package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/coder/websocket"
	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// ---- daemon connection ----

func (a *App) daemonConnect(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	node, err := a.Store.NodeByToken(r.Context(), tok)
	if tok == "" || err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Daemons are not browsers: no Origin header is sent, so skip origin checks.
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	a.Hub.Serve(r.Context(), ws, node.ID)
}

// onHello brings a node in line with the database when it connects:
//   - servers it lacks are created;
//   - every server it has gets its current spec (server.update), which is a
//     no-op unless the node's copy drifted;
//   - servers deleted while it was offline are removed from it;
//   - servers it has that the panel does not know are reported to admins
//     (Nodes page), never deleted on their own.
func (a *App) onHello(ctx context.Context, nodeID int64, h proto.Hello) {
	have := map[string]bool{}
	for _, id := range h.Servers {
		have[id] = true
	}
	servers, err := a.Store.ServersOnNode(ctx, nodeID)
	if err != nil {
		return
	}
	known := map[string]bool{}
	for _, s := range servers {
		known[s.UUID] = true
		if !have[s.UUID] {
			if err := a.pushServer(ctx, s, false); err != nil {
				a.Log.Warn("resync create failed", "server", s.UUID, "err", err)
			}
			continue
		}
		if _, err := a.applySpec(ctx, s); err != nil {
			a.Log.Warn("resync update failed", "server", s.UUID, "err", err)
		}
	}

	removed := map[string]bool{}
	if pending, err := a.Store.NodeCleanup(ctx, nodeID); err == nil {
		for _, id := range pending {
			if known[id] { // never remove a server the panel still has
				_ = a.Store.NodeCleanupDone(ctx, nodeID, id)
				continue
			}
			if _, err := a.Hub.Request(ctx, nodeID, proto.ReqServerDelete, proto.UUIDOnly{UUID: id}); err != nil {
				a.Log.Warn("cleanup of a force-deleted server failed", "node", nodeID, "server", id, "err", err)
				continue
			}
			removed[id] = true
			_ = a.Store.NodeCleanupDone(ctx, nodeID, id)
			a.Log.Info("removed a server deleted while its node was offline", "node", nodeID, "server", id)
		}
	}

	unknown := []string{}
	seen := map[string]bool{}
	for _, id := range append(append([]string(nil), h.Servers...), h.Containers...) {
		if !known[id] && !removed[id] && !seen[id] {
			seen[id] = true
			unknown = append(unknown, id)
		}
	}
	sort.Strings(unknown)
	a.unknown.Store(nodeID, unknown)
	if len(unknown) > 0 {
		a.Log.Warn("node has servers the panel does not know", "node", nodeID, "servers", unknown)
	}
}

// unknownOn lists servers a node reported that the panel has no record of.
func (a *App) unknownOn(nodeID int64) []string {
	if v, ok := a.unknown.Load(nodeID); ok {
		if list, ok := v.([]string); ok {
			return list
		}
	}
	return []string{}
}

// onCrash records a crash in the server's activity. Nobody did it, so the
// entry has no actor.
func (a *App) onCrash(c proto.Crash) {
	detail := fmt.Sprintf("exit code %d", c.ExitCode)
	if c.OOM {
		detail += ", out of memory"
	}
	if c.Restarting {
		detail += "; restarted"
	} else if c.Reason != "" {
		detail += "; not restarted: " + c.Reason
	}
	a.auditEntry(context.Background(), store.AuditEntry{Action: "server.crash", Target: c.UUID, Detail: detail})
}

func (a *App) specFor(ctx context.Context, s store.Server) (proto.ServerSpec, error) {
	egg, err := a.Store.GetEgg(ctx, s.EggID)
	if err != nil {
		return proto.ServerSpec{}, err
	}
	var inst *proto.InstallSpec
	if egg.InstallScript != "" {
		inst = &proto.InstallSpec{Image: egg.InstallImage, Entrypoint: egg.InstallEntrypoint, Script: egg.InstallScript}
	}
	ports, err := a.Store.ListPorts(ctx, s.UUID)
	if err != nil {
		return proto.ServerSpec{}, err
	}
	if len(ports) == 0 { // never expected: every server has its primary port listed
		ports = []store.ServerPort{{Port: s.Port, Primary: true}}
	}
	var maps []proto.PortMap
	for _, p := range ports {
		// Each port is open for both protocols: games differ, and many use both.
		maps = append(maps, proto.PortMap{Host: p.Port, Container: p.Port, Proto: "tcp"}, proto.PortMap{Host: p.Port, Container: p.Port, Proto: "udp"})
	}
	image, startup := egg.DockerImage, egg.Startup
	if s.Image != "" {
		image = s.Image
	}
	if s.Startup != "" {
		startup = s.Startup
	}
	return proto.ServerSpec{
		Install: inst,
		UUID:    s.UUID, Image: image, Startup: shellStartup(startup), Stop: egg.Stop, Done: egg.Done,
		Env: buildEnv(egg, s.Env, s.MemoryMB, s.Port), MemoryMB: s.MemoryMB, CPUPct: s.CPUPct,
		SwapMB: s.SwapMB, CPUSet: s.CPUSet, Pids: s.PidsLimit, IOWeight: s.IOWeight, DiskMB: s.DiskMB, BandwidthMbit: s.BandwidthMbit,
		Ports: maps, NoCrashRestart: !s.CrashRestart, Suspended: s.Suspended,
	}, nil
}

// pushServer creates the server on its node; start makes the node start it
// once it is installed.
func (a *App) pushServer(ctx context.Context, s store.Server, start bool) error {
	spec, err := a.specFor(ctx, s)
	if err != nil {
		return err
	}
	spec.StartOnCompletion = start
	_, err = a.Hub.Request(ctx, s.NodeID, proto.ReqServerCreate, spec)
	return err
}

// nodeErr turns a failed node request into an HTTP error. A daemon error
// with a code was written for people and keeps its message; any other is
// logged and replaced with a generic one.
// decodeReply decodes a node's reply into v. A reply that does not decode is
// the node's fault (version skew or a bug), so it becomes a 502 rather than a
// silent zero value.
func decodeReply(raw []byte, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return huma.Error502BadGateway("the node sent a reply the panel could not read", err)
	}
	return nil
}

func nodeErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, hub.ErrNodeOffline):
		return huma.Error503ServiceUnavailable("the node is offline")
	case errors.Is(err, hub.ErrTimeout):
		return huma.Error504GatewayTimeout("the node did not reply in time")
	}
	msg := err.Error()
	switch proto.CodeOf(err) {
	case proto.CodeDiskFull:
		return huma.NewError(http.StatusInsufficientStorage, msg)
	case proto.CodeConflict:
		return huma.Error409Conflict(msg)
	case proto.CodeInvalid:
		return huma.Error422UnprocessableEntity(msg)
	case proto.CodeNotFound:
		return huma.Error404NotFound(msg)
	}
	return hidden(http.StatusBadGateway, "The node could not complete the request.", "node request failed", err)
}

// forgetUnknown drops one server from a node's unknown list once removed.
func (a *App) forgetUnknown(nodeID int64, uuid string) {
	a.unknown.Store(nodeID, slices.DeleteFunc(slices.Clone(a.unknownOn(nodeID)), func(id string) bool { return id == uuid }))
}
