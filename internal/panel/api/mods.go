package api

import (
	"context"
	"errors"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/modrinth"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// The mod manager: find mods, plugins and datapacks on Modrinth and install
// them with one click. The panel resolves the version itself (only files on
// Modrinth's CDN), and the node checks the file's SHA-512 against
// Modrinth's before it is kept.

type modSearchIn struct {
	UUID    string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Q       string `query:"q" maxLength:"100"`
	Type    string `query:"type" enum:"mod,plugin,datapack" default:"mod"`
	Loader  string `query:"loader" maxLength:"32" doc:"fabric, forge, neoforge, quilt, paper, spigot, purpur, ..."`
	Version string `query:"version" maxLength:"32" doc:"game version, e.g. 1.21.1"`
	Offset  int    `query:"offset" minimum:"0" maximum:"1000"`
}
type (
	modSearchOut  struct{ Body modrinth.SearchResult }
	modVersionsIn struct {
		UUID    string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
		Project string `path:"project" maxLength:"64"`
		Loader  string `query:"loader" maxLength:"32"`
		Version string `query:"version" maxLength:"32"`
	}
)

type (
	modVersionsOut struct{ Body []modrinth.Version }
	modInstallIn   struct {
		UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
		Body struct {
			VersionID string `json:"version_id" pattern:"^[A-Za-z0-9]{1,32}$"`
			Type      string `json:"type" enum:"mod,plugin,datapack" doc:"decides the folder unless one is given"`
			Folder    string `json:"folder,omitempty" maxLength:"4096"`
		}
	}
)

func (a *App) modrinth() *modrinth.Client {
	if a.Modrinth != nil {
		return a.Modrinth
	}
	return defaultModrinth
}

var defaultModrinth = modrinth.New()

func modErr(err error) error {
	if errors.Is(err, modrinth.ErrNotFound) {
		return huma.Error404NotFound("not found on Modrinth")
	}
	return hidden(502, "Modrinth could not be reached. Try again in a moment.", "modrinth request failed", err)
}

func (a *App) registerMods(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "searchMods", Method: "GET", Path: "/servers/{uuid}/mods/search", Summary: "Search Modrinth"},
		func(ctx context.Context, in *modSearchIn) (*modSearchOut, error) {
			if _, _, err := a.authorizeP(ctx, in.UUID, PermFilesWrite); err != nil {
				return nil, err
			}
			r, err := a.modrinth().Search(ctx, in.Q, in.Type, strings.ToLower(in.Loader), in.Version, in.Offset)
			if err != nil {
				return nil, modErr(err)
			}
			return &modSearchOut{Body: r}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "modVersions", Method: "GET", Path: "/servers/{uuid}/mods/{project}/versions", Summary: "A project's versions on Modrinth"},
		func(ctx context.Context, in *modVersionsIn) (*modVersionsOut, error) {
			if _, _, err := a.authorizeP(ctx, in.UUID, PermFilesWrite); err != nil {
				return nil, err
			}
			vs, err := a.modrinth().Versions(ctx, in.Project, strings.ToLower(in.Loader), in.Version)
			if err != nil {
				return nil, modErr(err)
			}
			return &modVersionsOut{Body: vs}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "installMod", Method: "POST", Path: "/servers/{uuid}/mods", Summary: "Install a Modrinth version into the server", DefaultStatus: 201},
		func(ctx context.Context, in *modInstallIn) (*transferOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermFilesWrite)
			if err != nil {
				return nil, err
			}
			v, f, err := a.modrinth().Download(ctx, in.Body.VersionID)
			if err != nil {
				if errors.Is(err, modrinth.ErrNotFound) {
					return nil, huma.Error404NotFound("that version is not on Modrinth")
				}
				if strings.Contains(err.Error(), "modrinth answered") {
					return nil, modErr(err)
				}
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			folder := in.Body.Folder
			if folder == "" {
				folder = modrinth.Folders[in.Body.Type]
			}
			name := strings.ReplaceAll(strings.ReplaceAll(f.Filename, "/", "_"), "\\", "_")
			c, cancel := context.WithTimeout(ctx, pullRequestTimeout)
			defer cancel()
			raw, err := a.Hub.Request(c, s.NodeID, proto.ReqFilesPull, proto.Transfer{
				UUID: s.UUID, URL: f.URL, Path: strings.TrimRight(folder, "/") + "/" + name, SHA512: f.Hashes.SHA512,
			})
			if err != nil {
				return nil, nodeErr(err)
			}
			out := &transferOut{}
			if err := decodeReply(raw, &out.Body); err != nil {
				return nil, err
			}
			a.audit(ctx, u, "mod.install", s.UUID, v.ProjectID+" "+v.Number+" -> "+out.Body.Path)
			return out, nil
		})
}
