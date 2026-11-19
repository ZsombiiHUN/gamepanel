package api

import (
	"context"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

type startupVar struct {
	Env         string `json:"env"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Value       string `json:"value"`
	Default     string `json:"default"`
	// Editable: the caller may change this value.
	Editable bool `json:"editable"`
	// Rules as written in the egg, and a short version for people.
	Rules        string `json:"rules"`
	RulesSummary string `json:"rules_summary"`
	// Hidden from users by the egg (only admins see these).
	AdminOnly bool `json:"admin_only"`
}

type startupView struct {
	// Command is the startup command in egg form ({{VAR}} placeholders);
	// EggCommand is the egg's own, which Command differs from when an admin
	// set a custom one.
	Command       string `json:"command"`
	EggCommand    string `json:"egg_command"`
	CustomCommand bool   `json:"custom_command"`
	// Builtins are the variables every server gets.
	Builtins map[string]string `json:"builtins"`

	Image       string           `json:"image"`
	Images      []store.EggImage `json:"images" nullable:"false"`
	CustomImage bool             `json:"custom_image" doc:"an admin set an image the egg does not list"`

	Variables []startupVar `json:"variables" nullable:"false"`

	CanEditCommand   bool `json:"can_edit_command"`
	CanChangeImage   bool `json:"can_change_image"`
	CanEditVariables bool `json:"can_edit_variables"`
	// Pending: the server was running when the last change was made, so it
	// takes effect at the next start.
	Pending bool `json:"pending"`
	// CrashRestart: the node restarts the server when it crashes.
	CrashRestart        bool `json:"crash_restart"`
	CanEditCrashRestart bool `json:"can_edit_crash_restart"`
}

type startupOut struct{ Body startupView }

type startupIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Env     map[string]string `json:"env,omitempty" doc:"variables to change; others keep their values"`
		Image   *string           `json:"image,omitempty" maxLength:"255" doc:"one of the egg's images; admins may use any, \"\" = the egg's default"`
		Startup *string           `json:"startup,omitempty" maxLength:"4096" doc:"admins only; \"\" = the egg's command"`
		// Takes effect at once, even while the server runs.
		CrashRestart *bool `json:"crash_restart,omitempty" doc:"restart the server when it crashes"`
	}
}

// startupFor describes a server's startup settings as the caller may see them.
func startupFor(u store.User, perms []string, s store.Server, egg store.Egg) startupView {
	admin := u.IsAdmin
	v := startupView{
		Command: egg.Startup, EggCommand: egg.Startup, CustomCommand: s.Startup != "",
		Builtins: map[string]string{"SERVER_MEMORY": strconv.FormatInt(s.MemoryMB, 10), "SERVER_PORT": strconv.Itoa(s.Port)},
		Image:    egg.DockerImage, Images: egg.DockerImages,
		CanEditCommand: admin,
		Variables:      []startupVar{},
		CrashRestart:   s.CrashRestart, CanEditCrashRestart: admin || has(perms, PermStartupUpdate),
	}
	if s.Startup != "" {
		v.Command = s.Startup
	}
	if s.Image != "" {
		v.Image = s.Image
	}
	v.CustomImage = !slices.ContainsFunc(egg.DockerImages, func(i store.EggImage) bool { return i.Image == v.Image })
	v.CanChangeImage = admin || (has(perms, PermStartupImage) && !v.CustomImage && len(egg.DockerImages) > 1)
	canEdit := admin || has(perms, PermStartupUpdate)
	for _, ev := range egg.Variables {
		if !admin && !ev.UserViewable {
			continue
		}
		val, ok := s.Env[ev.Env]
		if !ok {
			val = ev.Default
		}
		sv := startupVar{
			Env: ev.Env, Name: ev.Name, Description: ev.Description, Value: val, Default: ev.Default,
			Editable: canEdit && (admin || ev.UserEditable), Rules: ev.Rules, RulesSummary: store.VarRulesSummary(ev.Rules),
			AdminOnly: !ev.UserViewable,
		}
		if sv.Editable {
			v.CanEditVariables = true
		}
		v.Variables = append(v.Variables, sv)
	}
	return v
}

// startupChange checks a requested change against the caller's rights and
// the egg's rules, and returns what to store.
func startupChange(u store.User, perms []string, s store.Server, egg store.Egg, env map[string]string, image, startup *string) (store.StartupChange, error) {
	admin := u.IsAdmin
	c := store.StartupChange{Env: map[string]string{}}
	byEnv := map[string]store.EggVar{}
	for _, v := range egg.Variables {
		byEnv[v.Env] = v
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys) // errors come out in a stable order
	for _, k := range keys {
		val := env[k]
		ev, ok := byEnv[k]
		if !ok || (!admin && !ev.UserViewable) {
			return c, huma.Error422UnprocessableEntity("this egg has no variable " + truncate(k, 64))
		}
		if !admin && (!has(perms, PermStartupUpdate) || !ev.UserEditable) {
			return c, huma.Error403Forbidden("you cannot change " + ev.Name)
		}
		if err := store.ValidateVar(ev, val); err != nil {
			return c, userErr(http.StatusUnprocessableEntity, err)
		}
		c.Env[k] = val
	}
	if image != nil {
		img := strings.TrimSpace(*image)
		listed := img == "" || slices.ContainsFunc(egg.DockerImages, func(i store.EggImage) bool { return i.Image == img })
		cur := s.Image
		if cur == "" {
			cur = egg.DockerImage
		}
		custom := !slices.ContainsFunc(egg.DockerImages, func(i store.EggImage) bool { return i.Image == cur })
		switch {
		case admin:
			if img != "" && !validImage(img) {
				return c, huma.Error422UnprocessableEntity("that is not a valid Docker image name")
			}
		case !has(perms, PermStartupImage):
			return c, huma.Error403Forbidden("you do not have permission to do that (" + PermStartupImage + ")")
		case custom:
			return c, huma.Error403Forbidden("an administrator chose this server's image; ask them to change it")
		case !listed:
			return c, huma.Error422UnprocessableEntity("pick one of the egg's images")
		}
		if img == egg.DockerImage {
			img = "" // follow the egg's default
		}
		c.Image = &img
	}
	if startup != nil {
		if !admin {
			return c, huma.Error403Forbidden("only administrators can change the startup command")
		}
		cmd := strings.TrimSpace(*startup)
		if strings.ContainsAny(cmd, "\x00\r\n") {
			return c, huma.Error422UnprocessableEntity("the startup command must be a single line")
		}
		if cmd == strings.TrimSpace(egg.Startup) {
			cmd = ""
		}
		c.Startup = &cmd
	}
	return c, nil
}

// crashRestartChange checks that the caller may switch automatic restarts.
func crashRestartChange(u store.User, perms []string, v *bool) error {
	if v != nil && !u.IsAdmin && !has(perms, PermStartupUpdate) {
		return huma.Error403Forbidden("you do not have permission to do that (" + PermStartupUpdate + ")")
	}
	return nil
}

func (a *App) registerStartup(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getStartup", Method: "GET", Path: "/servers/{uuid}/startup",
		Summary: "Startup command, Docker image and variables",
	},
		func(ctx context.Context, in *uuidIn) (*startupOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermStartupRead)
			if err != nil {
				return nil, err
			}
			egg, err := a.Store.GetEgg(ctx, s.EggID)
			if err != nil {
				return nil, huma.Error500InternalServerError("could not load the egg", err)
			}
			perms, _, _ := a.access(ctx, u, s)
			v := startupFor(u, perms, s, egg)
			v.Pending = a.pendingStartup(s.UUID)
			return &startupOut{Body: v}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "updateStartup", Method: "PUT", Path: "/servers/{uuid}/startup",
		Summary:     "Change variables, the Docker image or the startup command",
		Description: "Applies at once to a stopped server, otherwise at its next start (`pending` in the reply).",
	},
		func(ctx context.Context, in *startupIn) (*startupOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermStartupRead)
			if err != nil {
				return nil, err
			}
			egg, err := a.Store.GetEgg(ctx, s.EggID)
			if err != nil {
				return nil, huma.Error500InternalServerError("could not load the egg", err)
			}
			perms, _, _ := a.access(ctx, u, s)
			b := in.Body
			c, err := startupChange(u, perms, s, egg, b.Env, b.Image, b.Startup)
			if err != nil {
				return nil, err
			}
			if err := crashRestartChange(u, perms, b.CrashRestart); err != nil {
				return nil, err
			}
			c.CrashRestart = b.CrashRestart
			next := s
			next.Env = map[string]string{}
			for k, v := range s.Env {
				next.Env[k] = v
			}
			for k, v := range c.Env {
				next.Env[k] = v
			}
			if c.Image != nil {
				next.Image = *c.Image
			}
			if c.Startup != nil {
				next.Startup = *c.Startup
			}
			if c.CrashRestart != nil {
				next.CrashRestart = *c.CrashRestart
			}
			pending, err := a.applySpec(ctx, next)
			if err != nil {
				return nil, err
			}
			saved, err := a.Store.UpdateStartup(ctx, s.UUID, c)
			if err != nil {
				return nil, huma.Error500InternalServerError("the node took the change but the panel could not save it", err)
			}
			a.audit(ctx, u, "server.startup", s.UUID, startupAuditText(c, egg))
			v := startupFor(u, perms, saved, egg)
			v.Pending = pending || a.pendingStartup(s.UUID)
			return &startupOut{Body: v}, nil
		})
}

// applySpec sends a server's spec to its node. pending reports that the
// server was running, so the change waits for its next start.
func (a *App) applySpec(ctx context.Context, s store.Server) (pending bool, err error) {
	spec, err := a.specFor(ctx, s)
	if err != nil {
		return false, huma.Error500InternalServerError("could not build the server spec", err)
	}
	raw, err := a.Hub.Request(ctx, s.NodeID, proto.ReqServerUpdate, spec)
	if err != nil {
		return false, nodeErr(err)
	}
	var res proto.UpdateResult
	if err := decodeReply(raw, &res); err != nil {
		return false, err
	}
	return res.Pending, nil
}

// pendingStartup reports whether a change is waiting for the server's next
// start, as the node last reported it.
func (a *App) pendingStartup(uuid string) bool {
	st, ok := a.Hub.Stats(uuid)
	return ok && st.Pending
}

// startupAuditText lists what changed, never the values: variables often
// hold passwords and license keys.
func startupAuditText(c store.StartupChange, egg store.Egg) string {
	var parts []string
	if len(c.Env) > 0 {
		names := make([]string, 0, len(c.Env))
		for k := range c.Env {
			names = append(names, k)
		}
		sort.Strings(names)
		parts = append(parts, "variables "+strings.Join(names, ", "))
	}
	if c.Image != nil {
		img := *c.Image
		if img == "" {
			img = egg.DockerImage + " (egg default)"
		}
		parts = append(parts, "image "+img)
	}
	if c.CrashRestart != nil {
		if *c.CrashRestart {
			parts = append(parts, "automatic restart after a crash on")
		} else {
			parts = append(parts, "automatic restart after a crash off")
		}
	}
	if c.Startup != nil {
		if *c.Startup == "" {
			parts = append(parts, "startup command reset to the egg's")
		} else {
			parts = append(parts, "startup command changed")
		}
	}
	return strings.Join(parts, "; ")
}
