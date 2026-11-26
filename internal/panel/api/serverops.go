package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// Server operations shared by GamePanel's own API and the Pterodactyl
// application API (application.go), so both have the same effects: the
// node is told, the activity log and webhooks see it.

// newServer is a server to create. Server.NodeID 0 lets the panel pick.
type newServer struct {
	store.Server
	// Candidates limits the nodes the panel may pick (nil = all).
	Candidates []int64
	// PortRanges limits the primary port (nil = any free port).
	PortRanges []store.PortRange
	// Extra are ports the server gets besides its primary one.
	Extra []int
	// Start starts the server once it is installed.
	Start bool
}

// createServer stores the server, gives it its extra ports and provisions it
// on its node. If any of that fails, nothing is left behind. A taken external
// id comes back as store.ErrExternalIDTaken.
func (a *App) createServer(ctx context.Context, u store.User, ns newServer) (store.Server, error) {
	s, err := a.placeServer(ctx, ns.Server, ns.Candidates, ns.PortRanges)
	if err != nil {
		return s, err
	}
	undo := func() {
		if derr := a.Store.DeleteServer(ctx, s.UUID); derr != nil {
			a.Log.Error("could not roll back a server that was not created", "server", s.UUID, "err", derr)
		}
		a.Hub.Forget(s.UUID)
	}
	for _, p := range ns.Extra {
		if _, err := a.Store.AddPort(ctx, s.UUID, p, "", true); err != nil {
			undo()
			return s, userErr(http.StatusUnprocessableEntity, fmt.Errorf("port %d: %w", p, err))
		}
	}
	// Provision on the node. If that fails the row goes again, so the panel
	// never lists a server that does not exist anywhere.
	if err := a.pushServer(ctx, s, ns.Start); err != nil {
		undo()
		return s, nodeErr(err)
	}
	a.audit(ctx, u, "server.create", s.UUID, s.Name)
	a.emit(ctx, EvServerCreated, serverData(s))
	return s, nil
}

// nodesIn returns the ids of the nodes in the given locations.
func (a *App) nodesIn(ctx context.Context, locations []int64) ([]int64, error) {
	nodes, err := a.Store.ListNodes(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("db error", err)
	}
	var ids []int64
	for _, n := range nodes {
		if n.LocationID != 0 && slices.Contains(locations, n.LocationID) {
			ids = append(ids, n.ID)
		}
	}
	return ids, nil
}

// deleteServer deletes a server from its node and then from the panel. With
// force, a server whose node is offline goes from the panel at once and the
// node removes the rest when it reconnects.
func (a *App) deleteServer(ctx context.Context, u store.User, s store.Server, force bool) error {
	// Node first: if it is unreachable the row stays so data is never
	// orphaned, unless forced, which queues the cleanup.
	forced := false
	if _, err := a.Hub.Request(ctx, s.NodeID, proto.ReqServerDelete, proto.UUIDOnly{UUID: s.UUID}); err != nil {
		if !force || !errors.Is(err, hub.ErrNodeOffline) {
			return nodeErr(err)
		}
		forced = true
	}
	a.removeRemoteBackups(ctx, s.UUID) // before the rows that list them go
	a.dropServerDatabases(ctx, s.UUID)
	detail := s.Name
	var err error
	if forced {
		err = a.Store.ForceDeleteServer(ctx, s)
		detail += " (node offline: it removes the files when it reconnects)"
	} else {
		err = a.Store.DeleteServer(ctx, s.UUID)
	}
	if err != nil {
		return huma.Error500InternalServerError("the panel could not delete the server", err)
	}
	a.Hub.Forget(s.UUID)
	a.audit(ctx, u, "server.delete", s.UUID, detail)
	a.emit(ctx, EvServerDeleted, serverData(s))
	return nil
}

// setLimits checks new limits against the node's capacity, applies them on
// the node (a running server at its next start) and saves them.
func (a *App) setLimits(ctx context.Context, u store.User, s store.Server, l store.Limits) error {
	if err := l.Normalize(); err != nil {
		return userErr(http.StatusUnprocessableEntity, err)
	}
	if err := a.Store.CheckLimits(ctx, s.UUID, l); err != nil {
		var ce store.CapacityError
		if errors.As(err, &ce) {
			return userErr(http.StatusConflict, err)
		}
		return userErr(http.StatusUnprocessableEntity, err)
	}
	next := s
	next.Limits = l
	spec, err := a.specFor(ctx, next)
	if err != nil {
		return huma.Error500InternalServerError("could not build the server spec", err)
	}
	if _, err := a.Hub.Request(ctx, s.NodeID, proto.ReqServerUpdate, spec); err != nil {
		return nodeErr(err)
	}
	if err := a.Store.UpdateLimits(ctx, s.UUID, l); err != nil {
		return huma.Error500InternalServerError("limits were applied on the node but could not be saved", err)
	}
	a.audit(ctx, u, "server.limits", s.UUID, fmt.Sprintf("mem=%d swap=%d cpu=%d disk=%d", l.MemoryMB, l.SwapMB, l.CPUPct, l.DiskMB))
	return nil
}

// setSuspended suspends a server (the node stops it and refuses to start
// it) or lifts the suspension. Doing what is already so changes nothing.
func (a *App) setSuspended(ctx context.Context, u store.User, s store.Server, to bool) (store.Server, error) {
	changed, err := a.Store.SetSuspended(ctx, s.UUID, to)
	if err != nil {
		return s, huma.Error500InternalServerError("db error", err)
	}
	s.Suspended = to
	if !changed {
		return s, nil
	}
	// The panel's record is what counts: a node that is offline now gets
	// the change when it reconnects.
	if spec, err := a.specFor(ctx, s); err == nil {
		if _, err := a.Hub.Request(ctx, s.NodeID, proto.ReqServerUpdate, spec); err != nil {
			a.Log.Warn("the node did not take the suspension yet", "server", s.UUID, "suspended", to, "err", err)
		}
	}
	action, ev := "server.unsuspend", EvServerUnsuspended
	if to {
		action, ev = "server.suspend", EvServerSuspended
	}
	a.audit(ctx, u, action, s.UUID, s.Name)
	a.emit(ctx, ev, serverData(s))
	return s, nil
}
