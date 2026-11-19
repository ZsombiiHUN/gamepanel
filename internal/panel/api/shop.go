package api

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// What a shop selling servers uses beyond creating them: safe retries
// (external ids), automatic node choice, and suspension. See
// docs/server-creation.md.

// existingCreate answers a create whose external id is already used with
// that server, as a 200. It returns nil, nil when there is none.
func (a *App) existingCreate(ctx context.Context, u store.User, externalID string) (*serverCreateOut, error) {
	if externalID == "" {
		return nil, nil
	}
	s, err := a.Store.ServerByExternalID(ctx, externalID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("db error", err)
	}
	v := a.viewFor(ctx, u, s)
	a.label(ctx, &v)
	return &serverCreateOut{Status: http.StatusOK, Body: v}, nil
}

// placeServer stores a new server on its node, or, when it names none, on
// the best online node that has room: candidates (all nodes if empty) are
// tried from the one with the most free memory down; nodes that do not track
// memory come last, the least used first.
func (a *App) placeServer(ctx context.Context, v store.Server, candidates []int64) (store.Server, error) {
	if err := v.Normalize(); err != nil {
		return v, userErr(http.StatusUnprocessableEntity, err)
	}
	if v.NodeID != 0 {
		s, err := a.Store.CreateServer(ctx, v)
		return s, createErr(err)
	}
	nodes, err := a.Store.ListNodes(ctx)
	if err != nil {
		return v, huma.Error500InternalServerError("db error", err)
	}
	var pool []store.Node
	for _, n := range nodes {
		if (len(candidates) == 0 || slices.Contains(candidates, n.ID)) && a.Hub.Online(n.ID) {
			pool = append(pool, n)
		}
	}
	if len(pool) == 0 {
		return v, huma.Error503ServiceUnavailable("none of the nodes the server may go on is online")
	}
	free := func(n store.Node) int64 {
		return n.MemoryMB*(100+n.OverallocatePct)/100 - n.AllocatedMemory
	}
	slices.SortStableFunc(pool, func(x, y store.Node) int {
		if (x.MemoryMB == 0) != (y.MemoryMB == 0) {
			if x.MemoryMB == 0 {
				return 1
			}
			return -1
		}
		if x.MemoryMB == 0 {
			return cmp.Compare(x.AllocatedMemory, y.AllocatedMemory)
		}
		return cmp.Compare(free(y), free(x))
	})
	for _, n := range pool {
		v.NodeID = n.ID
		s, err := a.Store.CreateServer(ctx, v)
		if err == nil {
			return s, nil
		}
		var ce store.CapacityError
		var ue *store.UserError
		if errors.Is(err, store.ErrExternalIDTaken) || (!errors.As(err, &ce) && !errors.As(err, &ue)) {
			return s, createErr(err)
		}
		// This node is full (memory, disk or ports): try the next.
	}
	return v, huma.Error422UnprocessableEntity("no node has room for this server")
}

// createErr maps a failed CreateServer to an HTTP error.
func createErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrExternalIDTaken):
		return err // the caller looks the other server up
	}
	// A full node, a taken port, bad limits: all 422, as creates always were.
	return userErr(http.StatusUnprocessableEntity, err)
}

type externalIDIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		ExternalID string `json:"external_id" maxLength:"191" doc:"\"\" clears it"`
	}
}

func (a *App) registerShop(api huma.API) {
	suspend := func(to bool) func(context.Context, *uuidIn) (*serverOut, error) {
		return func(ctx context.Context, in *uuidIn) (*serverOut, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			s, err := a.Store.GetServer(ctx, in.UUID)
			if err != nil {
				return nil, huma.Error404NotFound("server not found")
			}
			changed, err := a.Store.SetSuspended(ctx, s.UUID, to)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			s.Suspended = to
			if changed {
				// The node stops the server and refuses to start it. The panel's
				// record is what counts: a node that is offline now gets the
				// change when it reconnects.
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
			}
			v := a.viewFor(ctx, u, s)
			a.label(ctx, &v)
			return &serverOut{Body: v}, nil
		}
	}
	huma.Register(api, huma.Operation{
		OperationID: "suspendServer", Method: "POST", Path: "/servers/{uuid}/suspend", Summary: "Suspend a server (admin)",
		Description: "Stops the server and keeps it stopped: it cannot be started, and its owner and subusers can see it but do nothing with it (console, files, SFTP, backups, schedules) until it is unsuspended. Nothing is deleted. Suspending a suspended server changes nothing.",
	},
		suspend(true))
	huma.Register(api, huma.Operation{
		OperationID: "unsuspendServer", Method: "POST", Path: "/servers/{uuid}/unsuspend", Summary: "Lift a suspension (admin)",
		Description: "The server can be used and started again; it is not started by this.",
	},
		suspend(false))

	huma.Register(api, huma.Operation{OperationID: "setServerExternalID", Method: "PUT", Path: "/servers/{uuid}/external-id", Summary: "Set or clear a server's external id (admin)"},
		func(ctx context.Context, in *externalIDIn) (*serverOut, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			id := strings.TrimSpace(in.Body.ExternalID)
			switch err := a.Store.SetServerExternalID(ctx, in.UUID, id); {
			case errors.Is(err, store.ErrNotFound):
				return nil, huma.Error404NotFound("server not found")
			case errors.Is(err, store.ErrExternalIDTaken):
				return nil, userErr(http.StatusConflict, err)
			case err != nil:
				return nil, huma.Error500InternalServerError("db error", err)
			}
			a.audit(ctx, u, "server.external_id", in.UUID, id)
			s, _ := a.Store.GetServer(ctx, in.UUID)
			v := a.viewFor(ctx, u, s)
			a.label(ctx, &v)
			return &serverOut{Body: v}, nil
		})
}
