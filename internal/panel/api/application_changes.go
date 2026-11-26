package api

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// Changes to existing servers through the application API: what a billing
// module does on an upgrade, a renamed service, an overdue invoice or a
// cancellation.

type appDetailsIn struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	User        flexInt `json:"user"`
	ExternalID  *string `json:"external_id"`
}

type appBuildIn struct {
	appLimitsIn
	Allocation        flexInt            `json:"allocation"`
	FeatureLimits     appFeatureLimitsIn `json:"feature_limits"`
	AddAllocations    []flexInt          `json:"add_allocations"`
	RemoveAllocations []flexInt          `json:"remove_allocations"`
}

type appStartupIn struct {
	Startup     *string               `json:"startup"`
	Environment map[string]flexString `json:"environment"`
	Egg         flexInt               `json:"egg"`
	Image       *string               `json:"image"`
}

func (a *App) registerAppServerChanges(mux *http.ServeMux) {
	h := a.appRoute(mux)

	h("PATCH /api/application/servers/{id}/details", func(w http.ResponseWriter, r *http.Request, u store.User) error {
		ctx := r.Context()
		s, _, err := a.appServerByID(r)
		if err != nil {
			return err
		}
		var in appDetailsIn
		if err := readJSON(r, &in); err != nil {
			return err
		}
		name, desc := s.Name, s.Description
		if in.Name != nil {
			if name, err = serverName(*in.Name); err != nil {
				return invalid("name", "required", "The name must be one line of plain text.")
			}
		}
		if in.Description != nil {
			desc = strings.TrimSpace(*in.Description)
		}
		var owner store.User
		if in.User.Set && in.User.V != s.OwnerID {
			if owner, err = a.Store.GetUser(ctx, in.User.V); err != nil {
				return invalid("user", "exists", "The selected user is invalid.")
			}
		}
		if in.ExternalID != nil {
			if ext := strings.TrimSpace(*in.ExternalID); ext != s.ExternalID {
				switch err := a.Store.SetServerExternalID(ctx, s.UUID, ext); {
				case errors.Is(err, store.ErrExternalIDTaken):
					return invalid("external_id", "unique", "The external id has already been taken.")
				case err != nil:
					return err
				}
				a.audit(ctx, u, "server.external_id", s.UUID, ext)
			}
		}
		if name != s.Name || desc != s.Description {
			if err := a.Store.RenameServer(ctx, s.UUID, name, desc); err != nil {
				return err
			}
			if name != s.Name {
				a.audit(ctx, u, "server.rename", s.UUID, s.Name+" -> "+name)
			}
		}
		if owner.ID != 0 {
			if _, err := a.Store.SetOwner(ctx, s.UUID, owner.Email); err != nil {
				return userErr(http.StatusUnprocessableEntity, err)
			}
			a.audit(ctx, u, "server.owner", s.UUID, owner.Email)
		}
		s, _ = a.Store.GetServer(ctx, s.UUID)
		return a.writeAppServer(w, r, http.StatusOK, s)
	})

	h("PATCH /api/application/servers/{id}/build", a.appBuild)
	h("PATCH /api/application/servers/{id}/startup", a.appStartup)

	for _, to := range []bool{true, false} {
		path := "/unsuspend"
		if to {
			path = "/suspend"
		}
		h("POST /api/application/servers/{id}"+path, func(w http.ResponseWriter, r *http.Request, u store.User) error {
			s, _, err := a.appServerByID(r)
			if err != nil {
				return err
			}
			if _, err := a.setSuspended(r.Context(), u, s, to); err != nil {
				return err
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		})
	}

	h("POST /api/application/servers/{id}/reinstall", func(w http.ResponseWriter, r *http.Request, u store.User) error {
		s, _, err := a.appServerByID(r)
		if err != nil {
			return err
		}
		if _, err := a.Hub.Request(r.Context(), s.NodeID, proto.ReqServerReinstall, proto.UUIDOnly{UUID: s.UUID}); err != nil {
			return nodeErr(err)
		}
		a.audit(r.Context(), u, "server.reinstall", s.UUID, s.Name)
		w.WriteHeader(http.StatusNoContent)
		return nil
	})

	del := func(force bool) func(w http.ResponseWriter, r *http.Request, u store.User) error {
		return func(w http.ResponseWriter, r *http.Request, u store.User) error {
			s, _, err := a.appServerByID(r)
			if err != nil {
				return err
			}
			if err := a.deleteServer(r.Context(), u, s, force); err != nil {
				return err
			}
			w.WriteHeader(http.StatusNoContent)
			return nil
		}
	}
	h("DELETE /api/application/servers/{id}", del(false))
	h("DELETE /api/application/servers/{id}/force", del(true))
}

// appBuild changes a server's limits and ports. Every port change is
// checked before anything is applied; the limits then go to the node, and
// the ports after them.
func (a *App) appBuild(w http.ResponseWriter, r *http.Request, u store.User) error {
	ctx := r.Context()
	s, _, err := a.appServerByID(r)
	if err != nil {
		return err
	}
	var in appBuildIn
	if err := readJSON(r, &in); err != nil {
		return err
	}
	l, err := in.apply(s.Limits)
	if err != nil {
		return err
	}
	l = in.FeatureLimits.apply(l)
	pc, err := a.planPorts(r, s, in)
	if err != nil {
		return err
	}
	if l != s.Limits {
		if err := a.setLimits(ctx, u, s, l); err != nil {
			return err
		}
	}
	if pc.changes(s) {
		if err := a.applyPorts(r, u, s, pc); err != nil {
			return err
		}
	}
	s, _ = a.Store.GetServer(ctx, s.UUID)
	return a.writeAppServer(w, r, http.StatusOK, s)
}

// portChange is what a build change does to a server's ports.
type portChange struct {
	primary     int
	add, remove []int
}

func (pc portChange) changes(s store.Server) bool {
	return len(pc.add)+len(pc.remove) > 0 || pc.primary != s.Port
}

// planPorts checks a build change's allocations against the server's ports.
func (a *App) planPorts(r *http.Request, s store.Server, in appBuildIn) (portChange, error) {
	have, err := a.Store.ListPorts(r.Context(), s.UUID)
	if err != nil {
		return portChange{}, err
	}
	holds := func(p int) bool {
		return slices.ContainsFunc(have, func(sp store.ServerPort) bool { return sp.Port == p })
	}
	onNode := func(field string, id flexInt) (int, error) {
		node, port, ok := splitAllocID(id.V)
		if !ok || node != s.NodeID {
			return 0, invalid(field, "exists", "That allocation is not on this server's node.")
		}
		return port, nil
	}
	pc := portChange{primary: s.Port}
	if in.Allocation.Set {
		if pc.primary, err = onNode("allocation", in.Allocation); err != nil {
			return pc, err
		}
	}
	for _, id := range in.AddAllocations {
		p, err := onNode("add_allocations", id)
		if err != nil {
			return pc, err
		}
		if !holds(p) && !slices.Contains(pc.add, p) {
			pc.add = append(pc.add, p)
		}
	}
	for _, id := range in.RemoveAllocations {
		p, err := onNode("remove_allocations", id)
		if err != nil {
			return pc, err
		}
		if holds(p) {
			pc.remove = append(pc.remove, p)
		}
	}
	if pc.primary != s.Port && !holds(pc.primary) && !slices.Contains(pc.add, pc.primary) {
		return pc, invalid("allocation", "exists", "The primary allocation must be one this server has.")
	}
	if slices.Contains(pc.remove, pc.primary) {
		return pc, invalid("remove_allocations", "different", "The primary allocation cannot be removed.")
	}
	return pc, nil
}

// applyPorts makes a planned port change and sends it to the node.
func (a *App) applyPorts(r *http.Request, u store.User, s store.Server, pc portChange) error {
	ctx := r.Context()
	for _, p := range pc.add {
		if _, err := a.Store.AddPort(ctx, s.UUID, p, "", true); err != nil {
			msg, _ := safeMessage(err)
			return invalid("add_allocations", "exists", fmt.Sprintf("Port %d cannot be added: %s.", p, msg))
		}
		a.audit(ctx, u, "server.port.add", s.UUID, strconv.Itoa(p))
	}
	if pc.primary != s.Port {
		if err := a.Store.UpdatePort(ctx, s.UUID, pc.primary, nil, true); err != nil {
			return portErr(err)
		}
		a.audit(ctx, u, "server.port.primary", s.UUID, strconv.Itoa(pc.primary))
	}
	for _, p := range pc.remove {
		if err := a.Store.RemovePort(ctx, s.UUID, p); err != nil {
			return portErr(err)
		}
		a.audit(ctx, u, "server.port.remove", s.UUID, strconv.Itoa(p))
	}
	_, err := a.applyServer(ctx, s.UUID)
	return err
}

// appStartup changes a server's startup command, image and variables.
// Changing its egg is not supported.
func (a *App) appStartup(w http.ResponseWriter, r *http.Request, u store.User) error {
	ctx := r.Context()
	s, _, err := a.appServerByID(r)
	if err != nil {
		return err
	}
	var in appStartupIn
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if in.Egg.Set && in.Egg.V != s.EggID {
		return invalid("egg", "in", "GamePanel cannot move a server to another egg; create a new server with that egg.")
	}
	egg, err := a.Store.GetEgg(ctx, s.EggID)
	if err != nil {
		return err
	}
	// Only what differs is a change: modules send every value back.
	env := map[string]string{}
	for k, v := range appEnv(egg, in.Environment) {
		if cur, ok := s.Env[k]; !ok || cur != v {
			env[k] = v
		}
	}
	image, startup := in.Image, in.Startup
	if image != nil && (*image == s.Image || (s.Image == "" && *image == egg.DockerImage)) {
		image = nil
	}
	if startup != nil && (strings.TrimSpace(*startup) == s.Startup || (s.Startup == "" && strings.TrimSpace(*startup) == strings.TrimSpace(egg.Startup))) {
		startup = nil
	}
	if len(env) > 0 || image != nil || startup != nil {
		c, err := startupChange(u, nil, s, egg, env, image, startup)
		if err != nil {
			return err
		}
		if _, _, err := a.changeStartup(ctx, u, s, egg, c); err != nil {
			return err
		}
	}
	s, _ = a.Store.GetServer(ctx, s.UUID)
	return a.writeAppServer(w, r, http.StatusOK, s)
}
