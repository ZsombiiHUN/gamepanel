package api

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

type serverView struct {
	store.Server
	State      string       `json:"state" doc:"offline|starting|running|stopping|unknown"`
	NodeOnline bool         `json:"node_online"`
	Stats      *proto.Stats `json:"stats,omitempty"`
}

func (a *App) view(s store.Server) serverView {
	v := serverView{Server: s, State: a.Hub.State(s.UUID), NodeOnline: a.Hub.Online(s.NodeID)}
	if st, ok := a.Hub.Stats(s.UUID); ok && v.State == "running" {
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
		Name     string            `json:"name" minLength:"1" maxLength:"64"`
		OwnerID  int64             `json:"owner_id,omitempty" doc:"Defaults to you."`
		NodeID   int64             `json:"node_id"`
		EggID    int64             `json:"egg_id"`
		MemoryMB int64             `json:"memory_mb" minimum:"64" maximum:"1048576"`
		CPUPct   int64             `json:"cpu_pct,omitempty" minimum:"0" maximum:"6400" doc:"100 = one core, 0 = unlimited"`
		Port     int               `json:"port,omitempty" doc:"Leave 0 to auto-allocate."`
		Env      map[string]string `json:"env,omitempty"`
	}
}
type powerIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Action string `json:"action" enum:"start,stop,restart,kill"`
	}
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

func (a *App) nodeErr(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "offline") {
		return huma.Error503ServiceUnavailable("the node is offline")
	}
	return huma.Error502BadGateway(err.Error())
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
			out := &serverListOut{Body: []serverView{}}
			for _, s := range ss {
				out.Body = append(out.Body, a.view(s))
			}
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
				MemoryMB: b.MemoryMB, CPUPct: b.CPUPct, Port: b.Port, Env: b.Env,
			})
			if err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			// Provision on the node. If that fails the DB row is rolled back so the
			// panel never lists a server that does not exist anywhere.
			if err := a.pushServer(ctx, s); err != nil {
				a.Store.DeleteServer(ctx, s.UUID)
				return nil, a.nodeErr(err)
			}
			a.audit(ctx, u, "server.create", s.UUID, s.Name)
			return &serverOut{Body: a.view(s)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "getServer", Method: "GET", Path: "/servers/{uuid}", Summary: "Get a server"},
		func(ctx context.Context, in *uuidIn) (*serverOut, error) {
			_, s, err := a.authorize(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			return &serverOut{Body: a.view(s)}, nil
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
				return nil, a.nodeErr(err)
			}
			a.Store.DeleteServer(ctx, s.UUID)
			a.Hub.Forget(s.UUID)
			a.audit(ctx, mustUser(ctx), "server.delete", s.UUID, s.Name)
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "power", Method: "POST", Path: "/servers/{uuid}/power", Summary: "Start, stop, restart or kill", DefaultStatus: 202},
		func(ctx context.Context, in *powerIn) (*struct{}, error) {
			_, s, err := a.authorize(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			// Detached from the HTTP request: a graceful stop can outlive it, and the
			// browser follows progress through state events on the console socket.
			go func() {
				c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				if _, err := a.Hub.Request(c, s.NodeID, proto.ReqServerPower, proto.Power{UUID: s.UUID, Action: in.Body.Action}); err != nil {
					a.Log.Warn("power failed", "server", s.UUID, "action", in.Body.Action, "err", err)
				}
			}()
			if !a.Hub.Online(s.NodeID) {
				return nil, huma.Error503ServiceUnavailable("the node is offline")
			}
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "sendCommand", Method: "POST", Path: "/servers/{uuid}/command", Summary: "Send a console command", DefaultStatus: 204},
		func(ctx context.Context, in *commandIn) (*struct{}, error) {
			_, s, err := a.authorize(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			if strings.ContainsAny(in.Body.Command, "\r\n") {
				return nil, huma.Error422UnprocessableEntity("command must be a single line")
			}
			_, err = a.Hub.Request(ctx, s.NodeID, proto.ReqServerInput, proto.Input{UUID: s.UUID, Line: in.Body.Command})
			return nil, a.nodeErr(err)
		})

	huma.Register(api, huma.Operation{OperationID: "listFiles", Method: "GET", Path: "/servers/{uuid}/files", Summary: "List a directory"},
		func(ctx context.Context, in *filePathIn) (*fileListOut, error) {
			_, s, err := a.authorize(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			raw, err := a.Hub.Request(ctx, s.NodeID, proto.ReqFilesList, proto.FileReq{UUID: s.UUID, Path: in.Path})
			if err != nil {
				return nil, a.nodeErr(err)
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
			_, s, err := a.authorize(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			raw, err := a.Hub.Request(ctx, s.NodeID, proto.ReqFilesRead, proto.FileReq{UUID: s.UUID, Path: in.Path})
			if err != nil {
				return nil, a.nodeErr(err)
			}
			var f proto.FileReq
			json.Unmarshal(raw, &f)
			out := &fileContentOut{}
			out.Body.Path, out.Body.Content = in.Path, string(f.Content)
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "writeFile", Method: "PUT", Path: "/servers/{uuid}/files/content", Summary: "Write a text file", DefaultStatus: 204, MaxBodyBytes: 12 << 20},
		func(ctx context.Context, in *fileWriteIn) (*struct{}, error) {
			_, s, err := a.authorize(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			_, err = a.Hub.Request(ctx, s.NodeID, proto.ReqFilesWrite, proto.FileReq{UUID: s.UUID, Path: in.Body.Path, Content: []byte(in.Body.Content)})
			return nil, a.nodeErr(err)
		})

	huma.Register(api, huma.Operation{OperationID: "deleteFile", Method: "DELETE", Path: "/servers/{uuid}/files", Summary: "Delete a file or directory", DefaultStatus: 204},
		func(ctx context.Context, in *filePathIn) (*struct{}, error) {
			_, s, err := a.authorize(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			_, err = a.Hub.Request(ctx, s.NodeID, proto.ReqFilesDelete, proto.FileReq{UUID: s.UUID, Path: in.Path})
			return nil, a.nodeErr(err)
		})
}
