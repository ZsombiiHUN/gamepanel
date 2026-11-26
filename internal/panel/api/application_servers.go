package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// Servers in the application API. A server's numeric id is its number from
// server_ids; its identifier the first 8 characters of its uuid, as in
// Pterodactyl. GamePanel has no nests: every egg is in nest 1.

// appNest is the one nest every egg is in.
const appNest = 1

// appLookups caches the nodes and eggs a request shows, so a list does not
// load them once per server.
type appLookups struct {
	a     *App
	ctx   context.Context //nolint:containedctx // lives only for one request
	nodes map[int64]store.Node
	eggs  map[int64]store.Egg
}

func (a *App) lookups(ctx context.Context) *appLookups {
	return &appLookups{a: a, ctx: ctx}
}

func (l *appLookups) node(id int64) (store.Node, bool) {
	if l.nodes == nil {
		l.nodes = map[int64]store.Node{}
		if ns, err := l.a.Store.ListNodes(l.ctx); err == nil {
			for _, n := range ns {
				l.nodes[n.ID] = n
			}
		}
	}
	n, ok := l.nodes[id]
	return n, ok
}

func (l *appLookups) egg(id int64) (store.Egg, bool) {
	if l.eggs == nil {
		l.eggs = map[int64]store.Egg{}
		if es, err := l.a.Store.ListEggs(l.ctx); err == nil {
			for _, e := range es {
				l.eggs[e.ID] = e
			}
		}
	}
	e, ok := l.eggs[id]
	return e, ok
}

// allocID is the id an allocation (a port on a node) has in this API.
// GamePanel gives servers ports from each node's range instead of keeping
// allocation records, so the id is made from the node and the port.
func allocID(node int64, port int) int64 { return node<<16 | int64(port) }

// splitAllocID is the node and port of an allocation id.
func splitAllocID(id int64) (node int64, port int, ok bool) {
	node, port = id>>16, int(id&0xffff)
	return node, port, node > 0 && port > 0
}

// nodeAddress is where players reach a node: its SFTP host, else the
// panel's host, else 0.0.0.0.
func (a *App) nodeAddress(n store.Node) string {
	if n.SFTPHost != "" {
		return n.SFTPHost
	}
	if u, err := url.Parse(a.PublicURL); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return "0.0.0.0"
}

func (a *App) appAllocation(n store.Node, port int, notes string, assigned bool) map[string]any {
	return map[string]any{
		"id": allocID(n.ID, port), "ip": a.nodeAddress(n), "alias": nil, "port": port,
		"notes": nullable(notes), "assigned": assigned,
	}
}

// appStatus is Pterodactyl's server status: null when it is ready.
func appStatus(s store.Server) any {
	switch {
	case s.Suspended:
		return "suspended"
	case s.InstallStatus == store.InstallInstalling:
		return "installing"
	case s.InstallStatus == store.InstallFailed:
		return "install_failed"
	}
	return nil
}

// appServer is a server in Pterodactyl's application shape.
func (l *appLookups) appServer(s store.Server, m store.ServerMeta, inc map[string]bool) map[string]any {
	egg, _ := l.egg(s.EggID)
	image, startup := egg.DockerImage, egg.Startup
	if s.Image != "" {
		image = s.Image
	}
	if s.Startup != "" {
		startup = s.Startup
	}
	env := map[string]string{}
	for _, v := range egg.Variables {
		val, ok := s.Env[v.Env]
		if !ok {
			val = v.Default
		}
		env[v.Env] = val
	}
	installed := 0
	if s.InstallStatus == store.InstallInstalled {
		installed = 1
	}
	out := map[string]any{
		"id": m.ID, "external_id": nullable(s.ExternalID), "uuid": s.UUID, "identifier": s.UUID[:8],
		"name": s.Name, "description": s.Description, "status": appStatus(s), "suspended": s.Suspended,
		"limits": map[string]any{
			"memory": s.MemoryMB, "swap": s.SwapMB, "disk": s.DiskMB, "io": s.IOWeight, "cpu": s.CPUPct,
			"threads": nullable(s.CPUSet), "oom_disabled": false,
		},
		"feature_limits": map[string]any{"databases": s.DatabaseLimit, "allocations": s.PortLimit + 1, "backups": s.BackupLimit},
		"user":           s.OwnerID, "node": s.NodeID, "allocation": allocID(s.NodeID, s.Port), "nest": appNest, "egg": s.EggID,
		"container":  map[string]any{"startup_command": startup, "image": image, "installed": installed, "environment": env},
		"created_at": pteroTime(m.CreatedAt), "updated_at": pteroTime(m.CreatedAt),
	}
	if inc["allocations"] {
		var allocs []any
		if n, ok := l.node(s.NodeID); ok {
			ports, _ := l.a.Store.ListPorts(l.ctx, s.UUID)
			for _, p := range ports {
				allocs = append(allocs, pteroObj{"allocation", l.a.appAllocation(n, p.Port, p.Notes, true)})
			}
		}
		out["relationships"] = map[string]any{"allocations": map[string]any{"object": "list", "data": orEmpty(allocs)}}
	}
	return out
}

func orEmpty(l []any) []any {
	if l == nil {
		return []any{}
	}
	return l
}

// appServerByID loads a server by its number.
func (a *App) appServerByID(r *http.Request) (store.Server, store.ServerMeta, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return store.Server{}, store.ServerMeta{}, err
	}
	s, err := a.Store.ServerByNumber(r.Context(), id)
	if err != nil {
		return s, store.ServerMeta{}, notFound()
	}
	return s, store.ServerMeta{ID: id, CreatedAt: a.createdAt(r.Context(), s.UUID)}, nil
}

func (a *App) createdAt(ctx context.Context, uuid string) int64 {
	m, _ := a.Store.GetServerMeta(ctx, uuid)
	return m.CreatedAt
}

// writeAppServer answers with one server.
func (a *App) writeAppServer(w http.ResponseWriter, r *http.Request, status int, s store.Server) error {
	m, err := a.Store.GetServerMeta(r.Context(), s.UUID)
	if err != nil {
		return err
	}
	pteroJSON(w, status, pteroObj{"server", a.lookups(r.Context()).appServer(s, m, includes(r))})
	return nil
}

func (a *App) registerAppServers(mux *http.ServeMux) {
	h := a.appRoute(mux)

	h("GET /api/application/servers", func(w http.ResponseWriter, r *http.Request, u store.User) error {
		ctx := r.Context()
		all, err := a.Store.ListServers(ctx, u)
		if err != nil {
			return err
		}
		metas, err := a.Store.ServerMetas(ctx)
		if err != nil {
			return err
		}
		q := r.URL.Query()
		match := func(s store.Server) bool {
			if v := q.Get("filter[external_id]"); v != "" && s.ExternalID != v {
				return false
			}
			if v := q.Get("filter[uuid]"); v != "" && s.UUID != v {
				return false
			}
			if v := q.Get("filter[uuidShort]"); v != "" && s.UUID[:8] != v {
				return false
			}
			if v := q.Get("filter[name]"); v != "" && !strings.Contains(strings.ToLower(s.Name), strings.ToLower(v)) {
				return false
			}
			return true
		}
		var list []store.Server
		for _, s := range all {
			if match(s) {
				list = append(list, s)
			}
		}
		// By id, as Pterodactyl lists them.
		slices.SortFunc(list, func(x, y store.Server) int { return int(metas[x.UUID].ID - metas[y.UUID].ID) })
		p := pageOf(r)
		l, inc := a.lookups(ctx), includes(r)
		items := []any{}
		for _, s := range pageSlice(list, p) {
			items = append(items, l.appServer(s, metas[s.UUID], inc))
		}
		pteroList(w, "server", items, len(list), p)
		return nil
	})

	h("GET /api/application/servers/{id}", func(w http.ResponseWriter, r *http.Request, _ store.User) error {
		s, m, err := a.appServerByID(r)
		if err != nil {
			return err
		}
		pteroJSON(w, http.StatusOK, pteroObj{"server", a.lookups(r.Context()).appServer(s, m, includes(r))})
		return nil
	})

	h("GET /api/application/servers/external/{external}", func(w http.ResponseWriter, r *http.Request, _ store.User) error {
		s, err := a.Store.ServerByExternalID(r.Context(), r.PathValue("external"))
		if err != nil {
			return notFound()
		}
		return a.writeAppServer(w, r, http.StatusOK, s)
	})

	h("POST /api/application/servers", a.appCreateServer)
}

type appLimitsIn struct {
	Memory  flexInt   `json:"memory"`
	Swap    flexInt   `json:"swap"`
	Disk    flexInt   `json:"disk"`
	IO      flexInt   `json:"io"`
	CPU     flexInt   `json:"cpu"`
	Threads optString `json:"threads"`
}

type appFeatureLimitsIn struct {
	Databases   flexInt `json:"databases"`
	Allocations flexInt `json:"allocations"`
	Backups     flexInt `json:"backups"`
}

type appCreateIn struct {
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	ExternalID    *string               `json:"external_id"`
	User          flexInt               `json:"user"`
	Egg           flexInt               `json:"egg"`
	DockerImage   string                `json:"docker_image"`
	Startup       string                `json:"startup"`
	Environment   map[string]flexString `json:"environment"`
	Limits        appLimitsIn           `json:"limits"`
	FeatureLimits appFeatureLimitsIn    `json:"feature_limits"`
	Allocation    *struct {
		Default    flexInt   `json:"default"`
		Additional []flexInt `json:"additional"`
	} `json:"allocation"`
	Deploy *struct {
		Locations []flexInt    `json:"locations"`
		PortRange []flexString `json:"port_range"`
		// dedicated_ip is accepted: GamePanel binds every address.
	} `json:"deploy"`
	StartOnCompletion looseBool `json:"start_on_completion"`
	// skip_scripts and oom_disabled are accepted and not used: the egg's
	// install script always runs, and the OOM killer is always on.
}

// limits maps Pterodactyl's limits onto GamePanel's, starting from cur.
// Block IO weight is not taken: Wings no longer applies it either, and a
// node without the io.weight controller would refuse to start the server.
func (in appLimitsIn) apply(cur store.Limits) (store.Limits, error) {
	l := cur
	if in.Memory.Set {
		switch {
		case in.Memory.V == 0:
			return l, invalid("limits.memory", "min", "GamePanel needs a memory limit: 0 (unlimited) is not supported.")
		case in.Memory.V < 64:
			return l, invalid("limits.memory", "min", "The memory limit must be at least 64 MB.")
		}
		l.MemoryMB = in.Memory.V
	}
	l.SwapMB = in.Swap.or(l.SwapMB)
	l.DiskMB = in.Disk.or(l.DiskMB)
	l.CPUPct = in.CPU.or(l.CPUPct)
	if in.Threads.Set { // null or "" unpins; left out keeps it
		l.CPUSet = strings.ReplaceAll(in.Threads.V, " ", "")
	}
	return l, nil
}

// apply maps feature limits: allocations counts the primary port, and
// GamePanel keeps at least one backup.
func (in appFeatureLimitsIn) apply(cur store.Limits) store.Limits {
	l := cur
	if in.Databases.Set || in.Databases.Null {
		l.DatabaseLimit = min(max(in.Databases.V, 0), 100)
	}
	if in.Allocations.Set || in.Allocations.Null {
		l.PortLimit = min(max(in.Allocations.V-1, 0), 100)
	}
	if in.Backups.Set || in.Backups.Null {
		l.BackupLimit = min(max(in.Backups.V, 1), 100)
	}
	return l
}

// portRanges parses deploy.port_range: "25565" or "25565-25575" each.
func portRanges(in []flexString) ([]store.PortRange, error) {
	var out []store.PortRange
	for _, raw := range in {
		s := strings.TrimSpace(string(raw))
		if s == "" {
			continue
		}
		from, to, isRange := strings.Cut(s, "-")
		a, err1 := strconv.Atoi(strings.TrimSpace(from))
		b := a
		var err2 error
		if isRange {
			b, err2 = strconv.Atoi(strings.TrimSpace(to))
		}
		if err1 != nil || err2 != nil || a < 1 || b > 65535 || a > b {
			return nil, invalid("deploy.port_range", "regex", fmt.Sprintf("The port range %q must be a port or two ports joined by a dash.", s))
		}
		out = append(out, store.PortRange{From: a, To: b})
	}
	return out, nil
}

// appEnv keeps the given values of the egg's variables; values for other
// names are ignored, as Pterodactyl does.
func appEnv(egg store.Egg, in map[string]flexString) map[string]string {
	env := map[string]string{}
	for _, v := range egg.Variables {
		if val, ok := in[v.Env]; ok {
			env[v.Env] = string(val)
		}
	}
	return env
}

func (a *App) appCreateServer(w http.ResponseWriter, r *http.Request, u store.User) error {
	ctx := r.Context()
	var in appCreateIn
	if err := readJSON(r, &in); err != nil {
		return err
	}
	name, err := serverName(in.Name)
	if err != nil {
		return invalid("name", "required", "The name field is required and must be one line of plain text.")
	}
	if !in.User.Set {
		return invalid("user", "required", "The user field is required.")
	}
	if _, err := a.Store.GetUser(ctx, in.User.V); err != nil {
		return invalid("user", "exists", "The selected user is invalid.")
	}
	if !in.Egg.Set {
		return invalid("egg", "required", "The egg field is required.")
	}
	egg, err := a.Store.GetEgg(ctx, in.Egg.V)
	if err != nil {
		return invalid("egg", "exists", "The selected egg is invalid.")
	}
	if !in.Limits.Memory.Set {
		return invalid("limits.memory", "required", "The limits.memory field is required.")
	}
	l, err := in.Limits.apply(store.Limits{})
	if err != nil {
		return err
	}
	l = in.FeatureLimits.apply(l)
	l.BackupLimit = max(l.BackupLimit, 1)
	env := appEnv(egg, in.Environment)
	if err := checkCreateEnv(egg, env); err != nil {
		return err
	}
	v := store.Server{UUID: newUUID(), Name: name, Description: strings.TrimSpace(in.Description), OwnerID: in.User.V, EggID: egg.ID, Env: env, Limits: l}
	if in.ExternalID != nil {
		v.ExternalID = strings.TrimSpace(*in.ExternalID)
		if len(v.ExternalID) > 191 {
			return invalid("external_id", "max", "The external id may not be greater than 191 characters.")
		}
	}
	if img := strings.TrimSpace(in.DockerImage); img != "" && img != egg.DockerImage {
		if !validImage(img) {
			return invalid("docker_image", "string", "That is not a valid Docker image name.")
		}
		v.Image = img
	}
	if cmd := strings.TrimSpace(in.Startup); cmd != "" && cmd != strings.TrimSpace(egg.Startup) {
		if strings.ContainsAny(cmd, "\x00\r\n") {
			return invalid("startup", "string", "The startup command must be a single line.")
		}
		v.Startup = cmd
	}
	ns := newServer{Server: v, Start: in.StartOnCompletion.V}
	if err := a.appPlacement(r, in, &ns); err != nil {
		return err
	}
	s, err := a.createServer(ctx, u, ns)
	switch {
	case errors.Is(err, store.ErrExternalIDTaken):
		return invalid("external_id", "unique", "The external id has already been taken.")
	case err != nil:
		return err
	}
	return a.writeAppServer(w, r, http.StatusCreated, s)
}

// appPlacement sets where a new server goes: the allocations it was given,
// or the nodes and ports deploy allows.
func (a *App) appPlacement(r *http.Request, in appCreateIn, ns *newServer) error {
	ctx := r.Context()
	var err error
	switch {
	case in.Allocation != nil && in.Allocation.Default.Set:
		node, port, ok := splitAllocID(in.Allocation.Default.V)
		if !ok {
			return invalid("allocation.default", "exists", "The selected allocation is invalid.")
		}
		ns.NodeID, ns.Port = node, port
		for _, x := range in.Allocation.Additional {
			xn, xp, ok := splitAllocID(x.V)
			if !ok || xn != node {
				return invalid("allocation.additional", "exists", "Additional allocations must be on the same node as the default one.")
			}
			ns.Extra = append(ns.Extra, xp)
		}
		if _, err := a.Store.GetNode(ctx, node); err != nil {
			return invalid("allocation.default", "exists", "The selected allocation is invalid.")
		}
	case in.Deploy != nil:
		if ns.PortRanges, err = portRanges(in.Deploy.PortRange); err != nil {
			return err
		}
		var locs []int64
		for _, l := range in.Deploy.Locations {
			if l.Set {
				locs = append(locs, l.V)
			}
		}
		if len(locs) > 0 {
			if ns.Candidates, err = a.nodesIn(ctx, locs); err != nil {
				return err
			}
			if len(ns.Candidates) == 0 {
				return noNode(http.StatusBadRequest, "No node is in the requested locations.")
			}
		}
	default:
		return invalid("deploy", "required_without", "Give either allocation.default or deploy.")
	}
	return nil
}
