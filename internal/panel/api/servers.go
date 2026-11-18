package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

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
type serverOut struct{ Body serverView }
type serverListOut struct{ Body []serverView }
type serverCreateIn struct {
	Body struct {
		Name     string `json:"name" minLength:"1" maxLength:"64"`
		OwnerID  int64  `json:"owner_id,omitempty" doc:"Defaults to you."`
		NodeID   int64  `json:"node_id"`
		EggID    int64  `json:"egg_id"`
		MemoryMB int64  `json:"memory_mb" minimum:"64" maximum:"1048576"`
		CPUPct   int64  `json:"cpu_pct,omitempty" minimum:"0" maximum:"6400" doc:"100 = one core, 0 = unlimited"`
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
type fileListOut struct{ Body []proto.FileInfo }
type fileContentOut struct {
	Body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
}
type fileWriteIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Path    string `json:"path" minLength:"1" maxLength:"4096"`
		Content string `json:"content" maxLength:"10485760"`
	}
}

func (a *App) registerServers(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "listServers", Method: "GET", Path: "/servers", Summary: "List servers you can access"},
		func(ctx context.Context, _ *struct{}) (*serverListOut, error) {
			u, err := requireUser(ctx)
			if err != nil {
				return nil, err
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
				if u.IsAdmin || s.OwnerID == u.ID {
					out.Body = append(out.Body, a.viewWith(s, allPermissions(), true, u.IsAdmin))
				} else {
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
		func(ctx context.Context, in *serverCreateIn) (*serverOut, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			b := in.Body
			if b.OwnerID == 0 {
				b.OwnerID = u.ID
			}
			if _, err := a.Store.GetEgg(ctx, b.EggID); err != nil {
				return nil, huma.Error422UnprocessableEntity("unknown egg")
			}
			s, err := a.Store.CreateServer(ctx, store.Server{
				UUID: newUUID(), Name: b.Name, OwnerID: b.OwnerID, NodeID: b.NodeID, EggID: b.EggID,
				Port: b.Port, Env: b.Env,
				Limits: store.Limits{
					MemoryMB: b.MemoryMB, SwapMB: b.SwapMB, CPUPct: b.CPUPct, CPUSet: b.CPUSet,
					PidsLimit: b.PidsLimit, IOWeight: b.IOWeight, DiskMB: b.DiskMB, BackupLimit: b.BackupLimit, PortLimit: b.PortLimit, DatabaseLimit: b.DatabaseLimit, BandwidthMbit: b.BandwidthMbit,
				},
			})
			if err != nil {
				return nil, userErr(http.StatusUnprocessableEntity, err)
			}
			// Provision on the node. If that fails the DB row is rolled back so the
			// panel never lists a server that does not exist anywhere.
			if err := a.pushServer(ctx, s); err != nil {
				if derr := a.Store.DeleteServer(ctx, s.UUID); derr != nil {
					a.Log.Error("could not roll back a server the node refused", "server", s.UUID, "err", derr)
				}
				a.Hub.Forget(s.UUID)
				return nil, nodeErr(err)
			}
			a.audit(ctx, u, "server.create", s.UUID, s.Name)
			v := a.viewFor(ctx, u, s)
			a.label(ctx, &v)
			return &serverOut{Body: v}, nil
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

	huma.Register(api, huma.Operation{OperationID: "deleteServer", Method: "DELETE", Path: "/servers/{uuid}", Summary: "Delete a server and its data", DefaultStatus: 204},
		func(ctx context.Context, in *uuidIn) (*struct{}, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			s, err := a.Store.GetServer(ctx, in.UUID)
			if err != nil {
				return nil, huma.Error404NotFound("server not found")
			}
			// Node first: if it is unreachable we keep the row so data is never orphaned.
			if _, err := a.Hub.Request(ctx, s.NodeID, proto.ReqServerDelete, proto.UUIDOnly{UUID: s.UUID}); err != nil {
				return nil, nodeErr(err)
			}
			a.removeRemoteBackups(ctx, s.UUID) // before the rows that list them go
			a.dropServerDatabases(ctx, s.UUID)
			if err := a.Store.DeleteServer(ctx, s.UUID); err != nil {
				return nil, huma.Error500InternalServerError("the node deleted the server but the panel could not", err)
			}
			a.Hub.Forget(s.UUID)
			a.audit(ctx, mustUser(ctx), "server.delete", s.UUID, s.Name)
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "setLimits", Method: "PUT", Path: "/servers/{uuid}/limits",
		Summary: "Change a server's resource limits (admin; a running server applies them at its next start)", DefaultStatus: 204},
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
			if !a.Hub.Online(s.NodeID) {
				return nil, huma.Error503ServiceUnavailable("the node is offline")
			}
			action := in.Body.Action
			a.audit(ctx, u, "server.power", s.UUID, action)
			// Detached from the HTTP request: a graceful stop can outlive it, and the
			// browser follows progress through state events on the console socket.
			go func() {
				c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				if _, err := a.Hub.Request(c, s.NodeID, proto.ReqServerPower, proto.Power{UUID: s.UUID, Action: action}); err != nil {
					a.Log.Warn("power failed", "server", s.UUID, "action", action, "err", err)
					a.Hub.Notice(s.UUID, powerFailure(action, err))
				}
			}()
			return nil, nil
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
			name := strings.TrimSpace(in.Body.Name)
			if name == "" || strings.ContainsAny(name, "\r\n\t") {
				return nil, huma.Error422UnprocessableEntity("give the server a name on one line")
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

	huma.Register(api, huma.Operation{OperationID: "setServerOwner", Method: "PUT", Path: "/servers/{uuid}/owner",
		Summary: "Hand a server to another account (admin)", Description: "The previous owner loses access unless an admin adds them back as a subuser."},
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
