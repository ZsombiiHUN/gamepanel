package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

type activityIn struct {
	UUID   string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Limit  int    `query:"limit" default:"50" minimum:"1" maximum:"200"`
	Offset int    `query:"offset" default:"0" minimum:"0"`
	Q      string `query:"q" maxLength:"200" doc:"Only entries containing every word."`
}
type activityOut struct{ Body []store.AuditEntry }

func (a *App) registerActivity(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "serverActivity", Method: "GET", Path: "/servers/{uuid}/activity",
		Summary: "What happened on a server, and who did it", Description: "Addresses are shown to the owner and admins only.",
	},
		func(ctx context.Context, in *activityIn) (*activityOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermActivityRead)
			if err != nil {
				return nil, err
			}
			es, err := a.Store.ListServerActivity(ctx, s.UUID, in.Q, in.Limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			if _, owner, _ := a.access(ctx, u, s); !owner {
				// A subuser sees who did what, not where everyone signs in from.
				for i := range es {
					es[i].IP = ""
				}
			}
			return &activityOut{Body: es}, nil
		})
}
