package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// A subset of Pterodactyl's client API (/api/client), in Pterodactyl's JSON
// shapes, so Discord bots, status pages and apps written for Pterodactyl
// work by changing the panel address and the key. Authentication is a
// GamePanel API key as the bearer token; every route goes through the same
// permission checks as the rest of the API.

type pteroObj struct {
	Object     string `json:"object"`
	Attributes any    `json:"attributes"`
}

func pteroJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeBody(w, v)
}

// pteroServer finds a server by Pterodactyl's identifier (the first 8
// characters of the uuid) or the full uuid, among those the caller can see.
func (a *App) pteroServer(ctx context.Context, id, perm string) (store.User, store.Server, error) {
	u, err := requireUser(ctx)
	if err != nil {
		return u, store.Server{}, err
	}
	uuid := id
	if len(id) == 8 {
		ss, err := a.Store.ListServers(ctx, u)
		if err != nil {
			return u, store.Server{}, huma.Error500InternalServerError("db error", err)
		}
		uuid = ""
		matches := 0
		for _, s := range ss {
			if strings.HasPrefix(s.UUID, strings.ToLower(id)) {
				uuid = s.UUID
				matches++
			}
		}
		// Two servers sharing a prefix: refuse rather than guess (use the full uuid).
		if matches != 1 {
			return u, store.Server{}, huma.Error404NotFound("The requested resource could not be found on the server.")
		}
	}
	return a.authorizeP(ctx, uuid, perm)
}

func (a *App) pteroServerAttrs(ctx context.Context, u store.User, s store.Server) map[string]any {
	ports, _ := a.Store.ListPorts(ctx, s.UUID)
	allocs := []any{}
	for _, p := range ports {
		allocs = append(allocs, pteroObj{"allocation", map[string]any{"ip": "0.0.0.0", "ip_alias": nil, "port": p.Port, "notes": nullable(p.Notes), "is_default": p.Primary}})
	}
	nodeName := ""
	if n, err := a.Store.GetNode(ctx, s.NodeID); err == nil {
		nodeName = n.Name
	}
	return map[string]any{
		"server_owner": s.OwnerID == u.ID, "identifier": s.UUID[:8], "uuid": s.UUID, "internal_id": nil,
		"name": s.Name, "node": nodeName, "description": s.Description,
		"limits":         map[string]any{"memory": s.MemoryMB, "swap": s.SwapMB, "disk": s.DiskMB, "io": s.IOWeight, "cpu": s.CPUPct, "threads": nullable(s.CPUSet)},
		"feature_limits": map[string]any{"databases": s.DatabaseLimit, "allocations": s.PortLimit + 1, "backups": s.BackupLimit},
		"is_suspended":   s.Suspended, "is_installing": a.Hub.State(s.UUID) == "installing",
		"relationships": map[string]any{"allocations": map[string]any{"object": "list", "data": allocs}},
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// pteroState maps our states to Pterodactyl's (offline, starting, running, stopping).
func pteroState(st string) string {
	switch st {
	case "running", "starting", "stopping":
		return st
	}
	return "offline"
}

// pteroRoutes returns a function that registers a handler on mux whose
// errors are answered in Pterodactyl's format.
func pteroRoutes(mux *http.ServeMux) func(pattern string, fn func(w http.ResponseWriter, r *http.Request) error) {
	return func(pattern string, fn func(w http.ResponseWriter, r *http.Request) error) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if err := fn(w, r); err != nil {
				pteroError(w, err)
			}
		})
	}
}

func (a *App) registerPtero(mux *http.ServeMux) {
	h := pteroRoutes(mux)

	h("GET /api/client/account", func(w http.ResponseWriter, r *http.Request) error {
		u, err := requireUser(r.Context())
		if err != nil {
			return err
		}
		pteroJSON(w, 200, pteroObj{"user", map[string]any{
			"id": u.ID, "admin": u.IsAdmin, "username": strings.Split(u.Email, "@")[0], "email": u.Email,
			"first_name": "", "last_name": "", "language": "en",
		}})
		return nil
	})

	h("GET /api/client", func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		u, err := requireUser(ctx)
		if err != nil {
			return err
		}
		ss, err := a.Store.ListServers(ctx, u)
		if err != nil {
			return huma.Error500InternalServerError("db error", err)
		}
		data := []any{}
		for _, s := range ss {
			data = append(data, pteroObj{"server", a.pteroServerAttrs(ctx, u, s)})
		}
		pteroJSON(w, 200, map[string]any{
			"object": "list", "data": data,
			"meta": map[string]any{"pagination": map[string]any{"total": len(data), "count": len(data), "per_page": max(len(data), 50), "current_page": 1, "total_pages": 1, "links": map[string]any{}}},
		})
		return nil
	})

	h("GET /api/client/servers/{id}", func(w http.ResponseWriter, r *http.Request) error {
		u, s, err := a.pteroServer(r.Context(), r.PathValue("id"), "")
		if err != nil {
			return err
		}
		pteroJSON(w, 200, pteroObj{"server", a.pteroServerAttrs(r.Context(), u, s)})
		return nil
	})

	h("GET /api/client/servers/{id}/resources", func(w http.ResponseWriter, r *http.Request) error {
		_, s, err := a.pteroServer(r.Context(), r.PathValue("id"), PermConsoleRead)
		if err != nil {
			return err
		}
		st, _ := a.Hub.Stats(s.UUID)
		pteroJSON(w, 200, pteroObj{"stats", map[string]any{
			"current_state": pteroState(a.Hub.State(s.UUID)), "is_suspended": s.Suspended,
			"resources": map[string]any{"memory_bytes": st.MemBytes, "cpu_absolute": st.CPUPct, "disk_bytes": st.DiskBytes, "network_rx_bytes": 0, "network_tx_bytes": 0, "uptime": 0},
		}})
		return nil
	})

	a.registerPteroControl(mux)
	a.registerPteroFiles(mux)
}

// registerPteroControl registers Pterodactyl-style power and console commands.
func (a *App) registerPteroControl(mux *http.ServeMux) {
	h := pteroRoutes(mux)
	h("POST /api/client/servers/{id}/power", func(w http.ResponseWriter, r *http.Request) error {
		u, s, err := a.pteroServer(r.Context(), r.PathValue("id"), PermPower)
		if err != nil {
			return err
		}
		var in struct {
			Signal string `json:"signal"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
			return huma.Error422UnprocessableEntity("The request body must be JSON.")
		}
		switch in.Signal {
		case "start", "stop", "restart", "kill":
		default:
			return huma.Error422UnprocessableEntity("The signal must be start, stop, restart or kill.")
		}
		if err := a.sendPower(r.Context(), u, s, in.Signal); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})

	h("POST /api/client/servers/{id}/command", func(w http.ResponseWriter, r *http.Request) error {
		u, s, err := a.pteroServer(r.Context(), r.PathValue("id"), PermConsoleSend)
		if err != nil {
			return err
		}
		var in struct {
			Command string `json:"command"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&in); err != nil {
			return huma.Error422UnprocessableEntity("The request body must be JSON.")
		}
		if in.Command == "" || len(in.Command) > 2048 || strings.ContainsAny(in.Command, "\r\n") {
			return huma.Error422UnprocessableEntity("The command must be one line.")
		}
		if _, err := a.Hub.Request(r.Context(), s.NodeID, proto.ReqServerInput, proto.Input{UUID: s.UUID, Line: in.Command}); err != nil {
			if proto.CodeOf(err) == proto.CodeConflict {
				// Pterodactyl says 502 when the server is not running.
				return huma.NewError(http.StatusBadGateway, "The server is not running.")
			}
			return nodeErr(err)
		}
		a.audit(r.Context(), u, "server.command", s.UUID, truncate(in.Command, 200))
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
}

// registerPteroFiles registers Pterodactyl-style file and backup routes.
func (a *App) registerPteroFiles(mux *http.ServeMux) {
	h := pteroRoutes(mux)
	h("GET /api/client/servers/{id}/files/list", func(w http.ResponseWriter, r *http.Request) error {
		_, s, err := a.pteroServer(r.Context(), r.PathValue("id"), PermFilesRead)
		if err != nil {
			return err
		}
		dir := r.URL.Query().Get("directory")
		if dir == "" {
			dir = "/"
		}
		raw, err := a.Hub.Request(r.Context(), s.NodeID, proto.ReqFilesList, proto.FileReq{UUID: s.UUID, Path: dir})
		if err != nil {
			return nodeErr(err)
		}
		var list []proto.FileInfo
		if err := decodeReply(raw, &list); err != nil {
			return err
		}
		data := []any{}
		for _, f := range list {
			mod := time.Unix(f.ModUnix, 0).UTC().Format(time.RFC3339)
			data = append(data, pteroObj{"file_object", map[string]any{
				"name": f.Name, "mode": modeString(f.Mode, f.Dir), "mode_bits": modeBits(f.Mode),
				"size": f.Size, "is_file": !f.Dir, "is_symlink": f.Symlink, "mimetype": map[bool]string{true: "inode/directory", false: "application/octet-stream"}[f.Dir],
				"created_at": mod, "modified_at": mod,
			}})
		}
		pteroJSON(w, 200, map[string]any{"object": "list", "data": data})
		return nil
	})

	h("GET /api/client/servers/{id}/files/contents", func(w http.ResponseWriter, r *http.Request) error {
		_, s, err := a.pteroServer(r.Context(), r.PathValue("id"), PermFilesRead)
		if err != nil {
			return err
		}
		b, err := a.readSmall(r.Context(), s.NodeID, s.UUID, r.URL.Query().Get("file"))
		if err != nil {
			return err
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// text/plain with nosniff (set for every response in api.go): never rendered as HTML.
		_, _ = w.Write(b) //nolint:gosec // G705: see above; a failed write means the client went away
		return nil
	})

	h("POST /api/client/servers/{id}/files/write", func(w http.ResponseWriter, r *http.Request) error {
		u, s, err := a.pteroServer(r.Context(), r.PathValue("id"), PermFilesWrite)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20+1))
		if err != nil || len(body) > 10<<20 {
			return huma.Error422UnprocessableEntity("Files written this way can be at most 10 MB; use the upload endpoint for larger ones.")
		}
		p := r.URL.Query().Get("file")
		if p == "" {
			return huma.Error422UnprocessableEntity("Name the file.")
		}
		if err := a.writeSmall(r.Context(), s.NodeID, s.UUID, p, body); err != nil {
			return err
		}
		a.audit(r.Context(), u, "file.write", s.UUID, p)
		w.WriteHeader(http.StatusNoContent)
		return nil
	})

	h("GET /api/client/servers/{id}/backups", func(w http.ResponseWriter, r *http.Request) error {
		_, s, err := a.pteroServer(r.Context(), r.PathValue("id"), PermBackupsRead)
		if err != nil {
			return err
		}
		bs, err := a.Store.ListBackups(r.Context(), s.UUID)
		if err != nil {
			return huma.Error500InternalServerError("db error", err)
		}
		data := []any{}
		for _, b := range bs {
			var done any
			if b.CompletedAt > 0 {
				done = time.Unix(b.CompletedAt, 0).UTC().Format(time.RFC3339)
			}
			data = append(data, pteroObj{"backup", map[string]any{
				"uuid": b.UUID, "is_successful": b.Status == store.BackupOK, "is_locked": b.Locked, "name": b.Name,
				"ignored_files": store.IgnoreList(b.Ignore), "checksum": "sha256:" + b.SHA256, "bytes": b.Size,
				"created_at": time.Unix(b.CreatedAt, 0).UTC().Format(time.RFC3339), "completed_at": done,
			}})
		}
		pteroJSON(w, 200, map[string]any{"object": "list", "data": data})
		return nil
	})
}

// modeString is "-rw-r--r--" style, as Pterodactyl shows it.
func modeString(mode uint32, dir bool) string {
	b := []byte("-rwxrwxrwx")
	if dir {
		b[0] = 'd'
	}
	for i := range 9 {
		if mode&(1<<(8-i)) == 0 {
			b[i+1] = '-'
		}
	}
	return string(b)
}

func modeBits(mode uint32) string {
	const digits = "01234567"
	m := mode & 0o777
	return string([]byte{digits[m>>6&7], digits[m>>3&7], digits[m&7]})
}
