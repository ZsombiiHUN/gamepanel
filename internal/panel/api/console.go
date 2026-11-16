package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// Close codes the console websocket ends with when access is the problem.
// A browser cannot read the HTTP status of a refused upgrade, so the socket
// is accepted and closed with one of these instead; the UI stops reconnecting.
const (
	CloseUnauthorized websocket.StatusCode = 4401 // signed out, key revoked
	CloseForbidden    websocket.StatusCode = 4403 // no console permission
	CloseNotFound     websocket.StatusCode = 4404 // no such server, or no access
)

// consoleAccess checks again that the request's credential is valid and still
// grants console access to the server; 0 means it does.
func (a *App) consoleAccess(ctx context.Context, uuid string) (websocket.StatusCode, string) {
	u, err := recheckUser(ctx)
	if err != nil {
		return CloseUnauthorized, "sign in required"
	}
	srv, err := a.Store.GetServer(ctx, uuid)
	if err != nil {
		return CloseNotFound, "server not found"
	}
	perms, _, member := a.access(ctx, u, srv)
	if !member {
		return CloseNotFound, "server not found"
	}
	if !has(perms, PermConsoleRead) {
		return CloseForbidden, "you do not have permission to read the console"
	}
	return 0, ""
}

// consoleWS streams one server's console, state and stats to a browser. Access
// is checked when the socket opens and again every AuthRecheck, so signing
// out, revoking a key or losing the permission closes an open console.
func (a *App) consoleWS(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	// Accept enforces same-origin by default, blocking cross-site websocket hijacking.
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer ws.CloseNow()
	if code, why := a.consoleAccess(r.Context(), uuid); code != 0 {
		ws.Close(code, why)
		return
	}
	ctx := ws.CloseRead(r.Context())

	sub, backlog := a.Hub.Subscribe(uuid)
	defer a.Hub.Unsubscribe(sub)
	write := func(ev hub.Event) error {
		b, _ := json.Marshal(ev)
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return ws.Write(wctx, websocket.MessageText, b)
	}
	write(hub.Event{Type: "state", Data: proto.Marshal(proto.State{UUID: uuid, State: a.Hub.State(uuid)})})
	for _, l := range backlog {
		write(hub.Event{Type: "console", Data: proto.Marshal(proto.Console{UUID: uuid, Line: l})})
	}
	recheck := time.NewTicker(a.AuthRecheck)
	defer recheck.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-recheck.C:
			if code, why := a.consoleAccess(ctx, uuid); code != 0 {
				ws.Close(code, why)
				return
			}
		case ev := <-sub.C:
			if write(ev) != nil {
				return
			}
		}
	}
}
