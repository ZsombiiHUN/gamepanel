package api

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/minecraft"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

var defaultMinecraft = minecraft.NewClient(minecraft.DefaultSources)

func (a *App) minecraft() *minecraft.Client {
	if a.Minecraft != nil {
		return a.Minecraft
	}
	return defaultMinecraft
}

var gpInstallRe = regexp.MustCompile(`\bgp-install\s+([a-z]+)\b`)

// minecraftFlavor names the server project an egg installs: the argument of
// gp-install for GamePanel's own eggs, otherwise a guess from the egg's name
// (community eggs). "" = not one the panel knows.
func minecraftFlavor(egg store.Egg) string {
	if m := gpInstallRe.FindStringSubmatch(egg.InstallScript); len(m) == 2 && slices.Contains(minecraft.Flavors, m[1]) {
		return m[1]
	}
	name := strings.ToLower(egg.Name)
	for _, f := range []string{"neoforge", "forge", "fabric", "folia", "purpur", "paper", "velocity", "bungeecord", "vanilla"} {
		if strings.Contains(name, f) {
			return f
		}
	}
	return ""
}

// versionVars are the variables eggs keep the game version in, ours first,
// then those of the common Pterodactyl eggs.
var versionVars = []string{"MINECRAFT_VERSION", "VANILLA_VERSION", "MC_VERSION", "VELOCITY_VERSION"}

// resetVars name a build of one game version; switching version sets them
// back to their defaults, as a build of one version does not exist for another.
var resetVars = []string{"BUILD_NUMBER", "LOADER_VERSION", "FORGE_VERSION", "FABRIC_LOADER_VERSION"}

type mcVersionsView struct {
	Flavor string `json:"flavor" doc:"paper, purpur, folia, vanilla, fabric, forge, neoforge, velocity or bungeecord; \"\" if unknown"`
	// Variable is the startup variable that holds the version ("" if the
	// egg has none); Current its value.
	Variable string `json:"variable"`
	Current  string `json:"current"`
	// Reset lists the variables to set back to these defaults when the
	// version changes (a build number belongs to one version).
	Reset    map[string]string `json:"reset"`
	Versions []string          `json:"versions" nullable:"false" doc:"newest first; empty when only the latest can be installed"`
}

type mcProfileIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Name string `path:"name" pattern:"^[A-Za-z0-9_]{1,16}$"`
	// Offline skips Mojang: the server runs with online-mode=false, so its
	// players have the UUIDs it derives from their names.
	Offline bool `query:"offline" doc:"give the offline-mode UUID without asking Mojang"`
}

func (a *App) registerMinecraft(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "minecraftStatus", Method: "GET", Path: "/servers/{uuid}/minecraft/status", Summary: "Ask a running Minecraft server for its status",
		Description: "The node sends the Server List Ping to the server's primary port: players online, the version and the MOTD.",
	}, func(ctx context.Context, in *uuidIn) (*struct{ Body minecraft.ServerStatus }, error) {
		_, s, err := a.authorizeP(ctx, in.UUID, PermConsoleRead)
		if err != nil {
			return nil, err
		}
		raw, err := a.Hub.Request(ctx, s.NodeID, proto.ReqServerPing, proto.UUIDOnly{UUID: s.UUID})
		if err != nil {
			return nil, nodeErr(err)
		}
		out := &struct{ Body minecraft.ServerStatus }{}
		if err := decodeReply(raw, &out.Body); err != nil {
			return nil, err
		}
		if out.Body.Players == nil {
			out.Body.Players = []minecraft.Player{}
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "minecraftVersions", Method: "GET", Path: "/servers/{uuid}/minecraft/versions", Summary: "Minecraft versions this server can switch to",
	}, func(ctx context.Context, in *uuidIn) (*struct{ Body mcVersionsView }, error) {
		_, s, err := a.authorizeP(ctx, in.UUID, PermStartupRead)
		if err != nil {
			return nil, err
		}
		egg, err := a.Store.GetEgg(ctx, s.EggID)
		if err != nil {
			return nil, err
		}
		v := mcVersionsView{Flavor: minecraftFlavor(egg), Reset: map[string]string{}, Versions: []string{}}
		for _, ev := range egg.Variables {
			if v.Variable == "" && slices.Contains(versionVars, ev.Env) && ev.UserViewable {
				v.Variable, v.Current = ev.Env, ev.Default
				if cur, ok := s.Env[ev.Env]; ok {
					v.Current = cur
				}
			}
			if slices.Contains(resetVars, ev.Env) {
				v.Reset[ev.Env] = ev.Default
			}
		}
		if v.Flavor != "" {
			list, err := a.minecraft().Versions(ctx, v.Flavor)
			if err != nil {
				return nil, hidden(502, "The version list could not be fetched. Try again in a moment.", "minecraft versions", err)
			}
			v.Versions = list
		}
		return &struct{ Body mcVersionsView }{Body: v}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "minecraftProfile", Method: "GET", Path: "/servers/{uuid}/minecraft/players/{name}", Summary: "Look up a Minecraft player by name",
		Description: "Finds the account's UUID, for whitelists and operator lists edited while the server is stopped. " +
			"A name with no Mojang account gets the UUID an offline-mode server would give it, marked offline.",
	}, func(ctx context.Context, in *mcProfileIn) (*struct{ Body minecraft.PlayerProfile }, error) {
		if _, _, err := a.authorizeP(ctx, in.UUID, PermFilesRead); err != nil {
			return nil, err
		}
		if in.Offline {
			return &struct{ Body minecraft.PlayerProfile }{Body: minecraft.PlayerProfile{Name: in.Name, UUID: minecraft.OfflineUUID(in.Name), Offline: true}}, nil
		}
		p, err := a.minecraft().Lookup(ctx, in.Name)
		switch {
		case errors.Is(err, minecraft.ErrNoSuchPlayer):
			p = minecraft.PlayerProfile{Name: in.Name, UUID: minecraft.OfflineUUID(in.Name), Offline: true}
		case err != nil:
			return nil, hidden(502, "Mojang could not be reached to look up the player. Try again in a moment.", "minecraft profile", err)
		}
		return &struct{ Body minecraft.PlayerProfile }{Body: p}, nil
	})
}
