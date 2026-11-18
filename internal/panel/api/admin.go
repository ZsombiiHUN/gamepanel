package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// ---- admin: nodes & eggs ----

type nodeCreateIn struct {
	Body struct {
		Name      string `json:"name" minLength:"1" maxLength:"64"`
		PortStart int    `json:"port_start,omitempty" minimum:"1024" maximum:"65535" default:"25565"`
		PortEnd   int    `json:"port_end,omitempty" minimum:"1024" maximum:"65535" default:"25665"`
		// Optional capacity, so the panel can refuse to over-allocate this node.
		MemoryMB        int64 `json:"memory_mb,omitempty" minimum:"0" doc:"memory servers may use in total, 0 = not tracked"`
		DiskMB          int64 `json:"disk_mb,omitempty" minimum:"0" doc:"disk servers may use in total, 0 = not tracked"`
		OverallocatePct int64 `json:"overallocate_pct,omitempty" minimum:"0" maximum:"1000"`
	}
}
type nodeCapacityIn struct {
	ID   int64 `path:"id"`
	Body struct {
		MemoryMB        int64 `json:"memory_mb" minimum:"0"`
		DiskMB          int64 `json:"disk_mb" minimum:"0"`
		OverallocatePct int64 `json:"overallocate_pct" minimum:"0" maximum:"1000"`
	}
}
type nodeCreateOut struct {
	Body struct {
		Node  store.Node `json:"node"`
		Token string     `json:"token" doc:"Shown once. Set as GAMEPANEL_NODE_TOKEN on the daemon."`
	}
}
type nodeView struct {
	store.Node
	Online bool `json:"online"`
	// UnknownServers are on the node but not in the panel, as the node last
	// reported when it connected.
	UnknownServers []string `json:"unknown_servers" nullable:"false"`
}
type nodeServerIn struct {
	ID   int64  `path:"id"`
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
}
type nodeListOut struct{ Body []nodeView }
type idIn struct {
	ID int64 `path:"id"`
}

type eggImportIn struct {
	RawBody []byte
}
type eggOut struct{ Body store.Egg }
type eggListOut struct{ Body []store.Egg }

func (a *App) registerAdmin(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "createNode", Method: "POST", Path: "/nodes", Summary: "Register a node", DefaultStatus: 201},
		func(ctx context.Context, in *nodeCreateIn) (*nodeCreateOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			if in.Body.PortEnd < in.Body.PortStart {
				return nil, huma.Error422UnprocessableEntity("port_end must be >= port_start")
			}
			n, tok, err := a.Store.CreateNode(ctx, in.Body.Name, in.Body.PortStart, in.Body.PortEnd)
			if err != nil {
				return nil, huma.Error409Conflict("could not create node (name taken?)")
			}
			if b := in.Body; b.MemoryMB > 0 || b.DiskMB > 0 || b.OverallocatePct > 0 {
				if err := a.Store.SetNodeCapacity(ctx, n.ID, b.MemoryMB, b.DiskMB, b.OverallocatePct); err != nil {
					return nil, userErr(http.StatusUnprocessableEntity, err)
				}
				n.MemoryMB, n.DiskMB, n.OverallocatePct = b.MemoryMB, b.DiskMB, b.OverallocatePct
			}
			a.audit(ctx, mustUser(ctx), "node.create", n.Name, "")
			out := &nodeCreateOut{}
			out.Body.Node, out.Body.Token = n, tok
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "setNodeCapacity", Method: "PUT", Path: "/nodes/{id}/capacity",
		Summary: "Set what a node can hold (0 = not tracked); existing servers are not touched", DefaultStatus: 204},
		func(ctx context.Context, in *nodeCapacityIn) (*struct{}, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			b := in.Body
			if err := a.Store.SetNodeCapacity(ctx, in.ID, b.MemoryMB, b.DiskMB, b.OverallocatePct); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return nil, huma.Error404NotFound("node not found")
				}
				return nil, userErr(http.StatusUnprocessableEntity, err)
			}
			a.audit(ctx, u, "node.capacity", strconv.FormatInt(in.ID, 10), fmt.Sprintf("mem=%d disk=%d over=%d%%", b.MemoryMB, b.DiskMB, b.OverallocatePct))
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listNodes", Method: "GET", Path: "/nodes", Summary: "List nodes"},
		func(ctx context.Context, _ *struct{}) (*nodeListOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			ns, err := a.Store.ListNodes(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			out := &nodeListOut{Body: []nodeView{}}
			for _, n := range ns {
				out.Body = append(out.Body, nodeView{Node: n, Online: a.Hub.Online(n.ID), UnknownServers: a.unknownOn(n.ID)})
			}
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteNode", Method: "DELETE", Path: "/nodes/{id}", Summary: "Delete a node", DefaultStatus: 204},
		func(ctx context.Context, in *idIn) (*struct{}, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			switch err := a.Store.DeleteNode(ctx, in.ID); {
			case errors.Is(err, store.ErrNodeInUse):
				return nil, huma.Error409Conflict("move or delete this node's servers first")
			case errors.Is(err, store.ErrNotFound):
				return nil, huma.Error404NotFound("node not found")
			case err != nil:
				return nil, huma.Error500InternalServerError("db error")
			}
			// Its token is gone; do not keep talking to it.
			a.Hub.Disconnect(in.ID)
			a.unknown.Delete(in.ID)
			a.audit(ctx, mustUser(ctx), "node.delete", strconv.FormatInt(in.ID, 10), "")
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "removeUnknownServer", Method: "DELETE", Path: "/nodes/{id}/unknown/{uuid}",
		Summary:     "Remove a server the panel does not know from a node",
		Description: "Deletes the container and files of a server the node reported but the panel has no record of.", DefaultStatus: 204},
		func(ctx context.Context, in *nodeServerIn) (*struct{}, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			if !slices.Contains(a.unknownOn(in.ID), in.UUID) {
				return nil, huma.Error404NotFound("the node did not report that server")
			}
			if _, err := a.Store.GetServer(ctx, in.UUID); err == nil {
				return nil, huma.Error409Conflict("the panel knows this server; delete it from the server page")
			}
			if _, err := a.Hub.Request(ctx, in.ID, proto.ReqServerDelete, proto.UUIDOnly{UUID: in.UUID}); err != nil {
				return nil, nodeErr(err)
			}
			a.forgetUnknown(in.ID, in.UUID)
			a.audit(ctx, u, "node.unknown.remove", strconv.FormatInt(in.ID, 10), in.UUID)
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "importEgg", Method: "POST", Path: "/eggs/import", Summary: "Import a Pterodactyl (PTDL) egg", DefaultStatus: 201,
		RequestBody:  &huma.RequestBody{Content: map[string]*huma.MediaType{"application/json": {Schema: &huma.Schema{Type: "object"}}}},
		MaxBodyBytes: 1 << 20},
		func(ctx context.Context, in *eggImportIn) (*eggOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			e, err := importPTDL(in.RawBody)
			if err != nil {
				return nil, userErr(http.StatusUnprocessableEntity, err)
			}
			e, err = a.Store.CreateEgg(ctx, e)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			a.audit(ctx, mustUser(ctx), "egg.import", e.Name, "")
			return &eggOut{Body: e}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listEggs", Method: "GET", Path: "/eggs", Summary: "List eggs"},
		func(ctx context.Context, _ *struct{}) (*eggListOut, error) {
			if _, err := requireUser(ctx); err != nil {
				return nil, err
			}
			es, err := a.Store.ListEggs(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			return &eggListOut{Body: es}, nil
		})
}
