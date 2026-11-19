package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

type networkView struct {
	Ports []store.ServerPort `json:"ports" nullable:"false"`
	// Limit is how many ports may be added beyond the primary one.
	Limit     int64 `json:"limit"`
	CanManage bool  `json:"can_manage"`
	// CanPick: the caller may choose a specific port (admins); others get
	// the next free one.
	CanPick   bool `json:"can_pick"`
	RangeFrom int  `json:"range_from"`
	RangeTo   int  `json:"range_to"`
	Pending   bool `json:"pending" doc:"the server must restart to use the change"`
}
type networkOut struct{ Body networkView }

type portAddIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Port  int    `json:"port,omitempty" minimum:"0" maximum:"65535" doc:"admins only; 0 = the next free port"`
		Notes string `json:"notes,omitempty" maxLength:"255"`
	}
}
type portIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Port int    `path:"port" minimum:"1" maximum:"65535"`
}
type portUpdateIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Port int    `path:"port" minimum:"1" maximum:"65535"`
	Body struct {
		Notes   *string `json:"notes,omitempty" maxLength:"255"`
		Primary bool    `json:"primary,omitempty" doc:"make this the primary port"`
	}
}

func (a *App) networkFor(ctx context.Context, u store.User, s store.Server, pending bool) (*networkOut, error) {
	ports, err := a.Store.ListPorts(ctx, s.UUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("db error", err)
	}
	perms, _, _ := a.access(ctx, u, s)
	v := networkView{
		Ports: ports, Limit: s.PortLimit, CanManage: has(perms, PermNetworkManage), CanPick: u.IsAdmin,
		Pending: pending || a.pendingStartup(s.UUID),
	}
	if n, err := a.Store.GetNode(ctx, s.NodeID); err == nil {
		v.RangeFrom, v.RangeTo = n.PortStart, n.PortEnd
	}
	return &networkOut{Body: v}, nil
}

func (a *App) registerNetwork(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "getNetwork", Method: "GET", Path: "/servers/{uuid}/network", Summary: "List a server's ports"},
		func(ctx context.Context, in *uuidIn) (*networkOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermNetworkRead)
			if err != nil {
				return nil, err
			}
			return a.networkFor(ctx, u, s, false)
		})

	huma.Register(api, huma.Operation{OperationID: "addPort", Method: "POST", Path: "/servers/{uuid}/network", Summary: "Give the server another port", DefaultStatus: 201},
		func(ctx context.Context, in *portAddIn) (*networkOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermNetworkManage)
			if err != nil {
				return nil, err
			}
			if in.Body.Port != 0 && !u.IsAdmin {
				return nil, huma.Error403Forbidden("only administrators can choose a port; leave it empty to get the next free one")
			}
			p, err := a.Store.AddPort(ctx, s.UUID, in.Body.Port, in.Body.Notes, u.IsAdmin)
			if err != nil {
				return nil, portErr(err)
			}
			pending, err := a.applyServer(ctx, s.UUID)
			if err != nil {
				if rerr := a.Store.RemovePort(ctx, s.UUID, p.Port); rerr != nil {
					a.Log.Error("could not undo a port the node did not take", "server", s.UUID, "port", p.Port, "err", rerr)
				}
				return nil, err
			}
			a.audit(ctx, u, "server.port.add", s.UUID, strconv.Itoa(p.Port))
			return a.networkFor(ctx, u, s, pending)
		})

	huma.Register(api, huma.Operation{OperationID: "updatePort", Method: "PUT", Path: "/servers/{uuid}/network/{port}", Summary: "Label a port, or make it the primary one"},
		func(ctx context.Context, in *portUpdateIn) (*networkOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermNetworkManage)
			if err != nil {
				return nil, err
			}
			if err := a.Store.UpdatePort(ctx, s.UUID, in.Port, in.Body.Notes, in.Body.Primary); err != nil {
				return nil, portErr(err)
			}
			pending := false
			if in.Body.Primary && in.Port != s.Port {
				if pending, err = a.applyServer(ctx, s.UUID); err != nil {
					if rerr := a.Store.UpdatePort(ctx, s.UUID, s.Port, nil, true); rerr != nil {
						a.Log.Error("could not undo a primary port change", "server", s.UUID, "err", rerr)
					}
					return nil, err
				}
				a.audit(ctx, u, "server.port.primary", s.UUID, strconv.Itoa(in.Port))
			}
			s, _ = a.Store.GetServer(ctx, s.UUID)
			return a.networkFor(ctx, u, s, pending)
		})

	huma.Register(api, huma.Operation{OperationID: "removePort", Method: "DELETE", Path: "/servers/{uuid}/network/{port}", Summary: "Take a port away from the server"},
		func(ctx context.Context, in *portIn) (*networkOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermNetworkManage)
			if err != nil {
				return nil, err
			}
			ports, _ := a.Store.ListPorts(ctx, s.UUID)
			var notes string
			for _, p := range ports {
				if p.Port == in.Port {
					notes = p.Notes
				}
			}
			if err := a.Store.RemovePort(ctx, s.UUID, in.Port); err != nil {
				return nil, portErr(err)
			}
			pending, err := a.applyServer(ctx, s.UUID)
			if err != nil {
				if _, rerr := a.Store.AddPort(ctx, s.UUID, in.Port, notes, true); rerr != nil {
					a.Log.Error("could not give back a port after the node refused", "server", s.UUID, "port", in.Port, "err", rerr)
				}
				return nil, err
			}
			a.audit(ctx, u, "server.port.remove", s.UUID, strconv.Itoa(in.Port))
			return a.networkFor(ctx, u, s, pending)
		})
}

// applyServer sends the server's saved settings to its node.
func (a *App) applyServer(ctx context.Context, uuid string) (bool, error) {
	s, err := a.Store.GetServer(ctx, uuid)
	if err != nil {
		return false, huma.Error404NotFound("server not found")
	}
	return a.applySpec(ctx, s)
}

func portErr(err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return huma.Error404NotFound("this server has no such port")
	case errors.Is(err, store.ErrPortLimit), errors.Is(err, store.ErrPrimaryPort):
		return userErr(http.StatusConflict, err)
	}
	return userErr(http.StatusUnprocessableEntity, err)
}
