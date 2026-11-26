package api

import (
	"cmp"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// Nodes, allocations, locations and eggs in the application API: what a
// billing module lists to set up a product, and Paymenter's choice of node.

// maxListedAllocations bounds the allocations shown per node inside another
// object (nodes/deployable?include=allocations): a node's whole port range
// could be tens of thousands of ports.
const maxListedAllocations = 1000

func (a *App) appNode(n store.Node) map[string]any {
	var loc any
	if n.LocationID != 0 {
		loc = n.LocationID
	}
	return map[string]any{
		"id": n.ID, "uuid": nil, "public": true, "name": n.Name, "description": "",
		"location_id": loc, "fqdn": a.nodeAddress(n), "scheme": "https", "behind_proxy": false, "maintenance_mode": false,
		"memory": n.MemoryMB, "memory_overallocate": n.OverallocatePct, "disk": n.DiskMB, "disk_overallocate": n.OverallocatePct,
		"upload_size": 100, "daemon_listen": nil, "daemon_sftp": n.SFTPPort, "daemon_base": nil,
		"allocated_resources": map[string]any{"memory": n.AllocatedMemory, "disk": n.AllocatedDisk},
		"created_at":          nil, "updated_at": nil,
	}
}

// nodeAllocations lists a node's ports, those in use marked assigned, from
// the from-th, at most limit.
func (a *App) nodeAllocations(r *http.Request, n store.Node, from, limit int) ([]any, int, error) {
	used, err := a.Store.UsedPorts(r.Context(), n.ID)
	if err != nil {
		return nil, 0, err
	}
	// Ports a server was given outside the node's range are still listed.
	var ports []int
	for p := range used {
		if p < n.PortStart || p > n.PortEnd {
			ports = append(ports, p)
		}
	}
	slices.Sort(ports)
	total := max(n.PortEnd-n.PortStart+1, 0) + len(ports)
	var out []any
	for i := from; i < total && len(out) < limit; i++ {
		p := n.PortStart + i
		if i >= n.PortEnd-n.PortStart+1 {
			p = ports[i-(n.PortEnd-n.PortStart+1)]
		}
		_, assigned := used[p]
		out = append(out, a.appAllocation(n, p, "", assigned))
	}
	return out, total, nil
}

// hasRoom reports whether a node can take a server of that size, by the
// rules a create follows: on a node that tracks disk, a server needs a disk
// limit.
func hasRoom(n store.Node, memory, disk int64) bool {
	if n.MemoryMB > 0 && n.AllocatedMemory+memory > n.MemoryMB*(100+n.OverallocatePct)/100 {
		return false
	}
	return n.DiskMB == 0 || (disk > 0 && n.AllocatedDisk+disk <= n.DiskMB*(100+n.OverallocatePct)/100)
}

func (a *App) registerAppNodes(mux *http.ServeMux) {
	h := a.appRoute(mux)

	h("GET /api/application/nodes", func(w http.ResponseWriter, r *http.Request, _ store.User) error {
		nodes, err := a.Store.ListNodes(r.Context())
		if err != nil {
			return err
		}
		p := pageOf(r)
		items := []any{}
		for _, n := range pageSlice(nodes, p) {
			items = append(items, a.appNode(n))
		}
		pteroList(w, "node", items, len(nodes), p)
		return nil
	})

	// Nodes a server of the given size fits on, in the given locations, that
	// are online: the ones a create would pick from.
	h("GET /api/application/nodes/deployable", func(w http.ResponseWriter, r *http.Request, _ store.User) error {
		q := r.URL.Query()
		mem, _ := strconv.ParseInt(q.Get("memory"), 10, 64)
		disk, _ := strconv.ParseInt(q.Get("disk"), 10, 64)
		var locs []int64
		for _, v := range queryList(q, "location_ids") {
			if id, err := strconv.ParseInt(v, 10, 64); err == nil {
				locs = append(locs, id)
			}
		}
		nodes, err := a.Store.ListNodes(r.Context())
		if err != nil {
			return err
		}
		fit := slices.DeleteFunc(nodes, func(n store.Node) bool {
			return (len(locs) > 0 && !slices.Contains(locs, n.LocationID)) || !a.Hub.Online(n.ID) || !hasRoom(n, mem, disk)
		})
		p, inc := pageOf(r), includes(r)
		items := []any{}
		for _, n := range pageSlice(fit, p) {
			v := a.appNode(n)
			if inc["allocations"] {
				allocs, _, err := a.nodeAllocations(r, n, 0, maxListedAllocations)
				if err != nil {
					return err
				}
				v["relationships"] = map[string]any{"allocations": map[string]any{"object": "list", "data": wrapObjs("allocation", allocs)}}
			}
			items = append(items, v)
		}
		pteroList(w, "node", items, len(fit), p)
		return nil
	})

	h("GET /api/application/nodes/{id}", func(w http.ResponseWriter, r *http.Request, _ store.User) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		n, err := a.Store.GetNode(r.Context(), id)
		if err != nil {
			return notFound()
		}
		pteroJSON(w, http.StatusOK, pteroObj{"node", a.appNode(n)})
		return nil
	})

	h("GET /api/application/nodes/{id}/allocations", func(w http.ResponseWriter, r *http.Request, _ store.User) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		n, err := a.Store.GetNode(r.Context(), id)
		if err != nil {
			return notFound()
		}
		p := pageOf(r)
		allocs, total, err := a.nodeAllocations(r, n, p.offset(), p.per)
		if err != nil {
			return err
		}
		pteroList(w, "allocation", allocs, total, p)
		return nil
	})
}

func wrapObjs(kind string, items []any) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		out = append(out, pteroObj{kind, it})
	}
	return out
}

func appLocation(l store.Location) map[string]any {
	return map[string]any{"id": l.ID, "short": l.Short, "long": l.Long, "created_at": pteroTime(l.CreatedAt), "updated_at": pteroTime(l.CreatedAt)}
}

type appLocationIn struct {
	Short *string `json:"short"`
	Long  *string `json:"long"`
}

func locationErr(err error) error {
	switch {
	case errors.Is(err, store.ErrLocationTaken):
		return invalid("short", "unique", "The short has already been taken.")
	case errors.Is(err, store.ErrNotFound):
		return notFound()
	}
	var ue *store.UserError
	if errors.As(err, &ue) {
		return invalid("short", "between", ue.Msg)
	}
	return err
}

func (a *App) registerAppLocations(mux *http.ServeMux) {
	h := a.appRoute(mux)

	h("GET /api/application/locations", func(w http.ResponseWriter, r *http.Request, _ store.User) error {
		ls, err := a.Store.ListLocations(r.Context())
		if err != nil {
			return err
		}
		p := pageOf(r)
		items := []any{}
		for _, l := range pageSlice(ls, p) {
			items = append(items, appLocation(l))
		}
		pteroList(w, "location", items, len(ls), p)
		return nil
	})

	h("GET /api/application/locations/{id}", func(w http.ResponseWriter, r *http.Request, _ store.User) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		l, err := a.Store.GetLocation(r.Context(), id)
		if err != nil {
			return notFound()
		}
		pteroJSON(w, http.StatusOK, pteroObj{"location", appLocation(l)})
		return nil
	})

	h("POST /api/application/locations", func(w http.ResponseWriter, r *http.Request, u store.User) error {
		var in appLocationIn
		if err := readJSON(r, &in); err != nil {
			return err
		}
		if in.Short == nil {
			return invalid("short", "required", "The short field is required.")
		}
		l, err := a.Store.CreateLocation(r.Context(), *in.Short, cmp.Or(ptrVal(in.Long), ""))
		if err != nil {
			return locationErr(err)
		}
		a.audit(r.Context(), u, "location.create", l.Short, l.Long)
		pteroJSON(w, http.StatusCreated, pteroObj{"location", appLocation(l)})
		return nil
	})

	h("PATCH /api/application/locations/{id}", func(w http.ResponseWriter, r *http.Request, u store.User) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		var in appLocationIn
		if err := readJSON(r, &in); err != nil {
			return err
		}
		l, err := a.Store.UpdateLocation(r.Context(), id, in.Short, in.Long)
		if err != nil {
			return locationErr(err)
		}
		a.audit(r.Context(), u, "location.update", l.Short, l.Long)
		pteroJSON(w, http.StatusOK, pteroObj{"location", appLocation(l)})
		return nil
	})

	h("DELETE /api/application/locations/{id}", func(w http.ResponseWriter, r *http.Request, u store.User) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		l, err := a.Store.GetLocation(r.Context(), id)
		if err != nil {
			return notFound()
		}
		if err := a.Store.DeleteLocation(r.Context(), id); err != nil {
			if errors.Is(err, store.ErrLocationInUse) {
				return userErr(http.StatusBadRequest, err)
			}
			return locationErr(err)
		}
		a.audit(r.Context(), u, "location.delete", l.Short, "")
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
}

func ptrVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func appNestObj() map[string]any {
	return map[string]any{
		"id": appNest, "uuid": nil, "author": "", "name": "GamePanel", "description": "Every egg on this panel. GamePanel has no nests.",
		"created_at": nil, "updated_at": nil,
	}
}

func appEgg(e store.Egg, inc map[string]bool) map[string]any {
	images := map[string]string{}
	for _, img := range e.DockerImages {
		images[cmp.Or(img.Label, img.Image)] = img.Image
	}
	out := map[string]any{
		"id": e.ID, "uuid": nil, "name": e.Name, "nest": appNest, "author": "", "description": e.Description,
		"docker_image": e.DockerImage, "docker_images": images, "startup": e.Startup,
		"config":     map[string]any{"files": map[string]any{}, "startup": map[string]any{"done": e.Done}, "stop": e.Stop, "logs": map[string]any{}, "file_denylist": []string{}, "extends": nil},
		"script":     map[string]any{"privileged": true, "install": e.InstallScript, "entry": e.InstallEntrypoint, "container": e.InstallImage, "extends": nil},
		"created_at": nil, "updated_at": nil,
	}
	if inc["variables"] {
		vars := []any{}
		for i, v := range e.Variables {
			vars = append(vars, pteroObj{"egg_variable", map[string]any{
				"id": i + 1, "egg_id": e.ID, "name": v.Name, "description": v.Description, "env_variable": v.Env,
				"default_value": v.Default, "user_viewable": v.UserViewable, "user_editable": v.UserEditable, "rules": v.Rules,
				"created_at": nil, "updated_at": nil,
			}})
		}
		out["relationships"] = map[string]any{"variables": map[string]any{"object": "list", "data": vars}}
	}
	return out
}

func (a *App) registerAppEggs(mux *http.ServeMux) {
	h := a.appRoute(mux)

	h("GET /api/application/nests", func(w http.ResponseWriter, r *http.Request, _ store.User) error {
		pteroList(w, "nest", []any{appNestObj()}, 1, pageOf(r))
		return nil
	})
	h("GET /api/application/nests/{nest}", func(w http.ResponseWriter, r *http.Request, _ store.User) error {
		if r.PathValue("nest") != strconv.Itoa(appNest) {
			return notFound()
		}
		pteroJSON(w, http.StatusOK, pteroObj{"nest", appNestObj()})
		return nil
	})

	// The nest in the path is not checked: products set up on Pterodactyl
	// name their own nest ids, and every egg here is in every nest.
	listEggs := func(w http.ResponseWriter, r *http.Request, _ store.User) error {
		eggs, err := a.Store.ListEggs(r.Context())
		if err != nil {
			return err
		}
		slices.SortFunc(eggs, func(x, y store.Egg) int { return cmp.Compare(x.ID, y.ID) })
		p, inc := pageOf(r), includes(r)
		items := []any{}
		for _, e := range pageSlice(eggs, p) {
			items = append(items, appEgg(e, inc))
		}
		pteroList(w, "egg", items, len(eggs), p)
		return nil
	}
	getEgg := func(w http.ResponseWriter, r *http.Request, _ store.User) error {
		id, err := pathID(r, "egg")
		if err != nil {
			return err
		}
		e, err := a.Store.GetEgg(r.Context(), id)
		if err != nil {
			return notFound()
		}
		pteroJSON(w, http.StatusOK, pteroObj{"egg", appEgg(e, includes(r))})
		return nil
	}
	h("GET /api/application/nests/{nest}/eggs", listEggs)
	h("GET /api/application/nests/{nest}/eggs/{egg}", getEgg)
	// Without the nest, as WHMCS asks first (and Pelican's panel answers).
	h("GET /api/application/eggs", listEggs)
	h("GET /api/application/eggs/{egg}", getEgg)
}
