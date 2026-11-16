package api

import (
	"context"
	"errors"
	"net/http"
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

// onHello makes the daemon match the database: servers it lacks are created.
func (a *App) onHello(ctx context.Context, nodeID int64, h proto.Hello) {
	have := map[string]bool{}
	for _, id := range h.Servers {
		have[id] = true
	}
	servers, err := a.Store.ServersOnNode(ctx, nodeID)
	if err != nil {
		return
	}
	for _, s := range servers {
		if have[s.UUID] {
			continue
		}
		if err := a.pushServer(ctx, s); err != nil {
			a.Log.Warn("resync create failed", "server", s.UUID, "err", err)
		}
	}
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
	image, startup := egg.DockerImage, egg.Startup
	if s.Image != "" {
		image = s.Image
	}
	if s.Startup != "" {
		startup = s.Startup
	}
	return proto.ServerSpec{
		Install: inst,
		UUID:    s.UUID, Image: image, Startup: shellStartup(startup), Stop: egg.Stop,
		Env: buildEnv(egg, s.Env, s.MemoryMB, s.Port), MemoryMB: s.MemoryMB, CPUPct: s.CPUPct,
		SwapMB: s.SwapMB, CPUSet: s.CPUSet, Pids: s.PidsLimit, IOWeight: s.IOWeight, DiskMB: s.DiskMB,
		Ports: []proto.PortMap{
			{Host: s.Port, Container: s.Port, Proto: "tcp"},
			{Host: s.Port, Container: s.Port, Proto: "udp"},
		},
	}, nil
}

func (a *App) pushServer(ctx context.Context, s store.Server) error {
	spec, err := a.specFor(ctx, s)
	if err != nil {
		return err
	}
	_, err = a.Hub.Request(ctx, s.NodeID, proto.ReqServerCreate, spec)
	return err
}

// nodeErr turns a failed node request into an HTTP error. A daemon error
// with a code was written for people and keeps its message; any other is
// logged and replaced with a generic one.
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
