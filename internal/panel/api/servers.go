package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

type serverView struct {
	store.Server
	State      string       `json:"state" doc:"offline|starting|running|stopping|installing|install_failed|crashed|unknown"`
	NodeOnline bool         `json:"node_online"`
	EggName    string       `json:"egg_name"`
	NodeName   string       `json:"node_name"`
	Stats      *proto.Stats `json:"stats,omitempty"`
	// Permissions are what the caller may do here; Owner is true for the
	// owner and admins, who can also manage who else has access.
	Permissions []string `json:"permissions" nullable:"false"`
	Owner       bool     `json:"owner"`
	// OwnerEmail is filled for admins viewing one server.
	OwnerEmail string `json:"owner_email,omitempty"`
}

// viewFor builds the server as the caller may see it. Only admins get the raw
// environment, which can hold values the egg hides from users (such as
// license keys); everyone else reads variables through GET .../startup,
// which applies the egg's visibility settings.
func (a *App) viewFor(ctx context.Context, u store.User, s store.Server) serverView {
	perms, owner, _ := a.access(ctx, u, s)
	return a.viewWith(s, perms, owner, u.IsAdmin)
}

func (a *App) viewWith(s store.Server, perms []string, owner, admin bool) serverView {
	v := a.view(s)
	v.Permissions, v.Owner = perms, owner
	if !admin {
		v.Env = map[string]string{}
		v.ExternalID = "" // the shop's business
	}
	return v
}

// label fills the egg and node names, which the UI searches and shows.
func (a *App) label(ctx context.Context, vs ...*serverView) {
	eggs, nodes, err := a.Store.Labels(ctx)
	if err != nil {
		return
	}
	for _, v := range vs {
		v.EggName, v.NodeName = eggs[v.EggID], nodes[v.NodeID]
	}
}

func (a *App) view(s store.Server) serverView {
	v := serverView{Server: s, State: a.Hub.State(s.UUID), NodeOnline: a.Hub.Online(s.NodeID)}
	if st, ok := a.Hub.Stats(s.UUID); ok {
		v.Stats = &st
	}
	return v
}

type uuidIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
}
type deleteServerIn struct {
	UUID  string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Force bool   `query:"force" doc:"delete even though the server's node is offline"`
}
type (
	serverOut       struct{ Body serverView }
	serverCreateOut struct {
		Status int
		Body   serverView
	}
)

type listServersIn struct {
	ExternalID string `query:"external_id" maxLength:"191" doc:"only the server with this external id (admin)"`
}
type (
	serverListOut  struct{ Body []serverView }
	serverCreateIn struct {
		Body struct {
			Name    string  `json:"name" minLength:"1" maxLength:"64"`
			OwnerID int64   `json:"owner_id,omitempty" doc:"Defaults to you."`
			NodeID  int64   `json:"node_id,omitempty" doc:"0 = the panel picks an online node with room, from node_ids if given"`
			NodeIDs []int64 `json:"node_ids,omitempty" maxItems:"100" doc:"nodes the panel may pick from when node_id is 0 (default: all)"`
			// ExternalID is the shop's id for this server. A create whose
			// external_id is already used returns that server (200) instead of
			// making another, so a shop can safely retry.
			ExternalID        string `json:"external_id,omitempty" maxLength:"191" doc:"your own id for the server, unique; a retried create with the same one returns the existing server with 200"`
			StartOnCompletion bool   `json:"start_on_completion,omitempty" doc:"start the server once it is installed"`
			EggID             int64  `json:"egg_id"`
			MemoryMB          int64  `json:"memory_mb" minimum:"64" maximum:"1048576"`
			CPUPct            int64  `json:"cpu_pct,omitempty" minimum:"0" maximum:"6400" doc:"100 = one core, 0 = unlimited"`
			// More limits, all optional. See PUT /servers/{uuid}/limits.
			SwapMB        int64             `json:"swap_mb,omitempty" minimum:"-1" maximum:"1048576" doc:"0 = none, -1 = unlimited"`
			CPUSet        string            `json:"cpuset,omitempty" maxLength:"64"`
			PidsLimit     int64             `json:"pids_limit,omitempty" minimum:"16" maximum:"65536" doc:"default 1024"`
			IOWeight      int64             `json:"io_weight,omitempty" minimum:"0" maximum:"1000" doc:"0 = default, else 10-1000"`
			DiskMB        int64             `json:"disk_mb,omitempty" minimum:"0" doc:"0 = unlimited"`
			BackupLimit   int64             `json:"backup_limit,omitempty" minimum:"1" maximum:"100" doc:"default 20"`
			PortLimit     int64             `json:"port_limit,omitempty" minimum:"0" maximum:"100" doc:"ports the owner may add beyond the primary one"`
			DatabaseLimit int64             `json:"database_limit,omitempty" minimum:"0" maximum:"100" doc:"databases the owner may create"`
			BandwidthMbit int64             `json:"bandwidth_mbit,omitempty" minimum:"0" maximum:"100000" doc:"network limit each way in Mbit/s, 0 = unlimited"`
			Port          int               `json:"port,omitempty" doc:"Leave 0 to auto-allocate."`
			Env           map[string]string `json:"env,omitempty"`
		}
	}
)

type powerIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Action string `json:"action" enum:"start,stop,restart,kill"`
	}
}
type limitsIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body store.Limits
}
type commandIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Command string `json:"command" minLength:"1" maxLength:"2048"`
	}
}
type filePathIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Path string `query:"path" default:"/" maxLength:"4096"`
}
type (
	fileListOut    struct{ Body []proto.FileInfo }
	fileContentOut struct {
		Body struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
	}
)

type fileWriteIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Path    string `json:"path" minLength:"1" maxLength:"4096"`
		Content string `json:"content" maxLength:"10485760"`
	}
}

func (a *App) registerServers(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "listServers", Method: "GET", Path: "/servers", Summary: "List servers you can access"},
		func(ctx context.Context, in *listServersIn) (*serverListOut, error) {
			u, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			if in.ExternalID != "" {
				if !u.IsAdmin {
					return nil, huma.Error403Forbidden("admin only")
				}
				out := &serverListOut{Body: []serverView{}}
				s, err := a.Store.ServerByExternalID(ctx, in.ExternalID)
				if errors.Is(err, store.ErrNotFound) {
					return out, nil
				} else if err != nil {
					return nil, huma.Error500InternalServerError("db error", err)
				}
				v := a.viewWith(s, allPermissions(), true, true)
				a.label(ctx, &v)
				out.Body = append(out.Body, v)
				return out, nil
			}
			ss, err := a.Store.ListServers(ctx, u)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			// One query for every server shared with the caller, not one per server.
			shared, err := a.Store.SubuserServers(ctx, u.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			out := &serverListOut{Body: []serverView{}}
			for _, s := range ss {
				switch {
				case u.IsAdmin:
					out.Body = append(out.Body, a.viewWith(s, allPermissions(), true, true))
				case s.Suspended: // only admins may use it
					out.Body = append(out.Body, a.viewWith(s, []string{}, s.OwnerID == u.ID, false))
				case s.OwnerID == u.ID:
					out.Body = append(out.Body, a.viewWith(s, allPermissions(), true, false))
				default:
					out.Body = append(out.Body, a.viewWith(s, shared[s.UUID], false, false))
				}
			}
			ptrs := make([]*serverView, len(out.Body))
			for i := range out.Body {
				ptrs[i] = &out.Body[i]
			}
			a.label(ctx, ptrs...)
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "createServer", Method: "POST", Path: "/servers", Summary: "Create a server", DefaultStatus: 201},
		func(ctx context.Context, in *serverCreateIn) (*serverCreateOut, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			b := in.Body
			b.ExternalID = strings.TrimSpace(b.ExternalID)
			// A retry of a create that went through: hand back the first one.
			if out, err := a.existingCreate(ctx, u, b.ExternalID); out != nil || err != nil {
				return out, err
			}
			name, err := serverName(b.Name)
			if err != nil {
				return nil, err
			}
			if b.OwnerID == 0 {
				b.OwnerID = u.ID
			}
			if _, err := a.Store.GetUser(ctx, b.OwnerID); errors.Is(err, store.ErrNotFound) {
				return nil, huma.Error422UnprocessableEntity("there is no account with that owner id")
			} else if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			egg, err := a.Store.GetEgg(ctx, b.EggID)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity("unknown egg")
			}
			if err := checkCreateEnv(egg, b.Env); err != nil {
				return nil, err
			}
			s, err := a.placeServer(ctx, store.Server{
				UUID: newUUID(), Name: name, OwnerID: b.OwnerID, NodeID: b.NodeID, EggID: b.EggID,
				Port: b.Port, Env: b.Env, ExternalID: b.ExternalID,
				Limits: store.Limits{
					MemoryMB: b.MemoryMB, SwapMB: b.SwapMB, CPUPct: b.CPUPct, CPUSet: b.CPUSet,
					PidsLimit: b.PidsLimit, IOWeight: b.IOWeight, DiskMB: b.DiskMB, BackupLimit: b.BackupLimit, PortLimit: b.PortLimit, DatabaseLimit: b.DatabaseLimit, BandwidthMbit: b.BandwidthMbit,
				},
			}, b.NodeIDs)
			if errors.Is(err, store.ErrExternalIDTaken) {
				// The same create, sent twice at once: the other one won.
				if out, err := a.existingCreate(ctx, u, b.ExternalID); out != nil || err != nil {
					return out, err
				}
				return nil, userErr(http.StatusConflict, err)
			}
			if err != nil {
				return nil, err
			}
			// Provision on the node. If that fails the DB row is rolled back so the
			// panel never lists a server that does not exist anywhere.
			if err := a.pushServer(ctx, s, b.StartOnCompletion); err != nil {
				if derr := a.Store.DeleteServer(ctx, s.UUID); derr != nil {
					a.Log.Error("could not roll back a server the node refused", "server", s.UUID, "err", derr)
				}
				a.Hub.Forget(s.UUID)
				return nil, nodeErr(err)
			}
			a.audit(ctx, u, "server.create", s.UUID, s.Name)
			a.emit(ctx, EvServerCreated, serverData(s))
			v := a.viewFor(ctx, u, s)
			a.label(ctx, &v)
			return &serverCreateOut{Status: http.StatusCreated, Body: v}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "getServer", Method: "GET", Path: "/servers/{uuid}", Summary: "Get a server"},
		func(ctx context.Context, in *uuidIn) (*serverOut, error) {
			u, s, err := a.authorize(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			v := a.viewFor(ctx, u, s)
			a.label(ctx, &v)
			if u.IsAdmin {
				if o, err := a.Store.GetUser(ctx, s.OwnerID); err == nil {
					v.OwnerEmail = o.Email
				}
			}
			return &serverOut{Body: v}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "deleteServer", Method: "DELETE", Path: "/servers/{uuid}", Summary: "Delete a server and its data", DefaultStatus: 204,
		Description: "The node deletes the container and files first. With `force`, a server whose node is offline is deleted from the panel at once, " +
			"and the node removes the rest when it next connects.",
	},
		func(ctx context.Context, in *deleteServerIn) (*struct{}, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			s, err := a.Store.GetServer(ctx, in.UUID)
			if err != nil {
				return nil, huma.Error404NotFound("server not found")
			}
			// Node first: if it is unreachable we keep the row so data is never
			// orphaned, unless an admin forces it, which queues the cleanup.
			forced := false
			if _, err := a.Hub.Request(ctx, s.NodeID, proto.ReqServerDelete, proto.UUIDOnly{UUID: s.UUID}); err != nil {
				if !in.Force || !errors.Is(err, hub.ErrNodeOffline) {
					return nil, nodeErr(err)
				}
				forced = true
			}
			a.removeRemoteBackups(ctx, s.UUID) // before the rows that list them go
			a.dropServerDatabases(ctx, s.UUID)
			detail := s.Name
			if forced {
				err = a.Store.ForceDeleteServer(ctx, s)
				detail += " (node offline: it removes the files when it reconnects)"
			} else {
				err = a.Store.DeleteServer(ctx, s.UUID)
			}
			if err != nil {
				return nil, huma.Error500InternalServerError("the panel could not delete the server", err)
			}
			a.Hub.Forget(s.UUID)
			a.audit(ctx, mustUser(ctx), "server.delete", s.UUID, detail)
			a.emit(ctx, EvServerDeleted, serverData(s))
			return nil, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "setLimits", Method: "PUT", Path: "/servers/{uuid}/limits",
		Summary: "Change a server's resource limits (admin; a running server applies them at its next start)", DefaultStatus: 204,
	},
		func(ctx context.Context, in *limitsIn) (*struct{}, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			s, err := a.Store.GetServer(ctx, in.UUID)
			if err != nil {
				return nil, huma.Error404NotFound("server not found")
			}
			l := in.Body
			if err := l.Normalize(); err != nil {
				return nil, userErr(http.StatusUnprocessableEntity, err)
			}
			// Validate against node capacity before touching the node.
			next := s
			next.Limits = l
			if err := a.Store.CheckLimits(ctx, s.UUID, l); err != nil {
				var ce store.ErrCapacity
				if errors.As(err, &ce) {
					return nil, userErr(http.StatusConflict, err)
				}
				return nil, userErr(http.StatusUnprocessableEntity, err)
			}
			spec, err := a.specFor(ctx, next)
			if err != nil {
				return nil, huma.Error500InternalServerError("could not build the server spec")
			}
			if _, err := a.Hub.Request(ctx, s.NodeID, proto.ReqServerUpdate, spec); err != nil {
				return nil, nodeErr(err)
			}
			if err := a.Store.UpdateLimits(ctx, s.UUID, l); err != nil {
				return nil, huma.Error500InternalServerError("limits were applied on the node but could not be saved", err)
			}
			a.audit(ctx, u, "server.limits", s.UUID, fmt.Sprintf("mem=%d swap=%d cpu=%d disk=%d", l.MemoryMB, l.SwapMB, l.CPUPct, l.DiskMB))
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "reinstall", Method: "POST", Path: "/servers/{uuid}/reinstall", Summary: "Re-run the egg install script (server must be offline)", DefaultStatus: 202},
		func(ctx context.Context, in *uuidIn) (*struct{}, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermReinstall)
			if err != nil {
				return nil, err
			}
			if _, err := a.Hub.Request(ctx, s.NodeID, proto.ReqServerReinstall, proto.UUIDOnly{UUID: s.UUID}); err != nil {
				return nil, nodeErr(err)
			}
			a.audit(ctx, u, "server.reinstall", s.UUID, s.Name)
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "power", Method: "POST", Path: "/servers/{uuid}/power", Summary: "Start, stop, restart or kill", DefaultStatus: 202},
		func(ctx context.Context, in *powerIn) (*struct{}, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermPower)
			if err != nil {
				return nil, err
			}
			return nil, a.sendPower(ctx, u, s, in.Body.Action)
		})

	huma.Register(api, huma.Operation{OperationID: "sendCommand", Method: "POST", Path: "/servers/{uuid}/command", Summary: "Send a console command", DefaultStatus: 204},
		func(ctx context.Context, in *commandIn) (*struct{}, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermConsoleSend)
			if err != nil {
				return nil, err
			}
			if strings.ContainsAny(in.Body.Command, "\r\n") {
				return nil, huma.Error422UnprocessableEntity("command must be a single line")
			}
			if _, err = a.Hub.Request(ctx, s.NodeID, proto.ReqServerInput, proto.Input{UUID: s.UUID, Line: in.Body.Command}); err != nil {
				return nil, nodeErr(err)
			}
			a.audit(ctx, u, "server.command", s.UUID, truncate(in.Body.Command, 200))
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listFiles", Method: "GET", Path: "/servers/{uuid}/files", Summary: "List a directory"},
		func(ctx context.Context, in *filePathIn) (*fileListOut, error) {
			_, s, err := a.authorizeP(ctx, in.UUID, PermFilesRead)
			if err != nil {
				return nil, err
			}
			raw, err := a.Hub.Request(ctx, s.NodeID, proto.ReqFilesList, proto.FileReq{UUID: s.UUID, Path: in.Path})
			if err != nil {
				return nil, nodeErr(err)
			}
			out := &fileListOut{}
			json.Unmarshal(raw, &out.Body)
			if out.Body == nil {
				out.Body = []proto.FileInfo{}
			}
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "readFile", Method: "GET", Path: "/servers/{uuid}/files/content", Summary: "Read a text file"},
		func(ctx context.Context, in *filePathIn) (*fileContentOut, error) {
			_, s, err := a.authorizeP(ctx, in.UUID, PermFilesRead)
			if err != nil {
				return nil, err
			}
			b, err := a.readSmall(ctx, s.NodeID, s.UUID, in.Path)
			if err != nil {
				return nil, err
			}
			out := &fileContentOut{}
			out.Body.Path, out.Body.Content = in.Path, string(b)
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "writeFile", Method: "PUT", Path: "/servers/{uuid}/files/content", Summary: "Write a text file", DefaultStatus: 204, MaxBodyBytes: 12 << 20},
		func(ctx context.Context, in *fileWriteIn) (*struct{}, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermFilesWrite)
			if err != nil {
				return nil, err
			}
			if len(in.Body.Content) > maxEditBytes {
				return nil, huma.Error422UnprocessableEntity("Files saved in the editor can be at most 10 MB; upload larger ones.")
			}
			if err := a.writeSmall(ctx, s.NodeID, s.UUID, in.Body.Path, []byte(in.Body.Content)); err != nil {
				return nil, err
			}
			a.audit(ctx, u, "file.write", s.UUID, fmt.Sprintf("%s (%d bytes)", in.Body.Path, len(in.Body.Content)))
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteFile", Method: "DELETE", Path: "/servers/{uuid}/files", Summary: "Delete a file or directory", DefaultStatus: 204},
		func(ctx context.Context, in *filePathIn) (*struct{}, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermFilesWrite)
			if err != nil {
				return nil, err
			}
			if _, err = a.Hub.Request(ctx, s.NodeID, proto.ReqFilesDelete, proto.FileReq{UUID: s.UUID, Path: in.Path}); err != nil {
				return nil, nodeErr(err)
			}
			a.audit(ctx, u, "file.delete", s.UUID, in.Path)
			return nil, nil
		})
}

// truncate shortens s to at most n bytes for log details.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

type renameIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Name        string `json:"name" minLength:"1" maxLength:"64"`
		Description string `json:"description,omitempty" maxLength:"500"`
	}
}
type ownerIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Email string `json:"email" format:"email" maxLength:"254"`
	}
}

func (a *App) registerServerDetails(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "renameServer", Method: "PUT", Path: "/servers/{uuid}/details", Summary: "Rename a server and set its description"},
		func(ctx context.Context, in *renameIn) (*serverOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermRename)
			if err != nil {
				return nil, err
			}
			name, err := serverName(in.Body.Name)
			if err != nil {
				return nil, err
			}
			if err := a.Store.RenameServer(ctx, s.UUID, name, strings.TrimSpace(in.Body.Description)); err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			if name != s.Name {
				a.audit(ctx, u, "server.rename", s.UUID, s.Name+" -> "+name)
			}
			s, _ = a.Store.GetServer(ctx, s.UUID)
			v := a.viewFor(ctx, u, s)
			a.label(ctx, &v)
			return &serverOut{Body: v}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "setServerOwner", Method: "PUT", Path: "/servers/{uuid}/owner",
		Summary: "Hand a server to another account (admin)", Description: "The previous owner loses access unless an admin adds them back as a subuser.",
	},
		func(ctx context.Context, in *ownerIn) (*serverOut, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			s, err := a.Store.GetServer(ctx, in.UUID)
			if err != nil {
				return nil, huma.Error404NotFound("server not found")
			}
			owner, err := a.Store.SetOwner(ctx, s.UUID, in.Body.Email)
			if err != nil {
				return nil, userErr(http.StatusUnprocessableEntity, err)
			}
			a.audit(ctx, u, "server.owner", s.UUID, owner.Email)
			s, _ = a.Store.GetServer(ctx, s.UUID)
			v := a.viewFor(ctx, u, s)
			a.label(ctx, &v)
			return &serverOut{Body: v}, nil
		})
}

// Power actions wait this long for the node's answer. Most refusals (busy,
// suspended, out of disk) come back at once and are returned to the caller;
// a graceful stop can take far longer, so after this the reply goes out and
// the outcome arrives as state events, or as a console line if it fails.
var powerGrace = 2 * time.Second

// powerTimeout bounds the node's work on one power action. A restart may stop
// the server gracefully, then rebuild its container and pull an image.
const powerTimeout = 15 * time.Minute

// sendPower checks that a power action can be sent now, sends it, and returns
// the node's answer if it comes within powerGrace. What can be known up front
// is refused here, so an API caller learns of it.
func (a *App) sendPower(ctx context.Context, u store.User, s store.Server, action string) error {
	if !a.Hub.Online(s.NodeID) {
		return huma.Error503ServiceUnavailable("the node is offline")
	}
	starts := action == "start" || action == "restart"
	switch st := a.Hub.State(s.UUID); {
	case st == "installing":
		return huma.Error409Conflict("the server is still installing; try again once it is done")
	case st == "install_failed" && starts:
		return huma.Error409Conflict("the install failed; reinstall the server before starting it")
	}
	if s.Suspended && starts {
		return huma.Error409Conflict("this server is suspended; unsuspend it first")
	}
	a.audit(ctx, u, "server.power", s.UUID, action)
	res := make(chan error, 1)
	go func() {
		c, cancel := context.WithTimeout(context.Background(), powerTimeout)
		defer cancel()
		_, err := a.Hub.Request(c, s.NodeID, proto.ReqServerPower, proto.Power{UUID: s.UUID, Action: action})
		res <- err
	}()
	t := time.NewTimer(powerGrace)
	defer t.Stop()
	select {
	case err := <-res:
		return nodeErr(err)
	case <-t.C:
	case <-ctx.Done():
	}
	go func() {
		if err := <-res; err != nil {
			a.Log.Warn("power failed", "server", s.UUID, "action", action, "err", err)
			a.Hub.Notice(s.UUID, powerFailure(action, err))
		}
	}()
	return nil
}

// powerFailure is the console line telling the user why a power action
// failed. Coded node errors are written for users and shown as they are;
// anything else could leak internals and gets a generic reason, as in nodeErr.
func powerFailure(action string, err error) string {
	reason := "the node could not complete the request"
	switch {
	case errors.Is(err, hub.ErrNodeOffline):
		reason = "the node is offline"
	case errors.Is(err, hub.ErrTimeout):
		reason = "the node did not reply in time"
	case proto.CodeOf(err) != "":
		reason = err.Error()
	}
	return fmt.Sprintf("\x1b[31m[gamepanel] %s failed: %s\x1b[0m", action, reason)
}

// serverName trims a server name and refuses one that is empty, spans lines,
// or holds control or text-direction characters. Those last can make one
// server's name read like another's in lists and in the activity log.
func serverName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || strings.ContainsAny(name, "\r\n\t") {
		return "", huma.Error422UnprocessableEntity("give the server a name on one line")
	}
	for _, r := range name {
		if unicode.IsControl(r) || isBidiControl(r) {
			return "", huma.Error422UnprocessableEntity("the name has a control or text-direction character; use plain text")
		}
	}
	return name, nil
}

// isBidiControl reports the invisible characters that change text direction.
func isBidiControl(r rune) bool {
	return r == '\u061c' || r == '\u200e' || r == '\u200f' || (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069')
}

// checkCreateEnv checks the variables given for a new server the way the
// Startup tab checks a change: each must be one of the egg's variables, and
// every variable's value, the one given or else the egg's default, must pass
// the egg's rules. Without this an empty required variable would only show up
// as a failed install.
func checkCreateEnv(egg store.Egg, given map[string]string) error {
	byEnv := map[string]bool{}
	for _, v := range egg.Variables {
		byEnv[v.Env] = true
	}
	keys := make([]string, 0, len(given))
	for k := range given {
		keys = append(keys, k)
	}
	sort.Strings(keys) // errors come out in a stable order
	for _, k := range keys {
		if !byEnv[k] {
			return huma.Error422UnprocessableEntity("this egg has no variable " + truncate(k, 64))
		}
	}
	for _, v := range egg.Variables {
		val, ok := given[v.Env]
		if !ok {
			val = v.Default
		}
		if err := store.ValidateVar(v, val); err != nil {
			return userErr(http.StatusUnprocessableEntity, err)
		}
	}
	return nil
}
