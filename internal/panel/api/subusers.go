package api

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

type subuserListOut struct{ Body []store.Subuser }
type subuserOut struct{ Body store.Subuser }
type permListOut struct{ Body []store.Permission }
type subuserAddIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Email       string   `json:"email" format:"email"`
		Permissions []string `json:"permissions" minItems:"1"`
	}
}
type subuserIn struct {
	UUID   string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	UserID int64  `path:"user_id"`
}
type subuserSetIn struct {
	UUID   string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	UserID int64  `path:"user_id"`
	Body   struct {
		Permissions []string `json:"permissions" minItems:"1"`
	}
}

func (a *App) registerSubusers(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "listPermissions", Method: "GET", Path: "/permissions", Summary: "Permissions that can be granted to a subuser"},
		func(ctx context.Context, _ *struct{}) (*permListOut, error) {
			if _, err := requireUser(ctx); err != nil {
				return nil, err
			}
			return &permListOut{Body: store.AllPermissions}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listSubusers", Method: "GET", Path: "/servers/{uuid}/users", Summary: "Who else has access (owner only)"},
		func(ctx context.Context, in *uuidIn) (*subuserListOut, error) {
			_, s, err := a.authorizeOwner(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			l, err := a.Store.ListSubusers(ctx, s.UUID)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			return &subuserListOut{Body: l}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "addSubuser", Method: "POST", Path: "/servers/{uuid}/users", Summary: "Give an existing account access (owner only)", DefaultStatus: 201},
		func(ctx context.Context, in *subuserAddIn) (*subuserOut, error) {
			u, s, err := a.authorizeOwner(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			v, err := a.Store.AddSubuser(ctx, s.UUID, in.Body.Email, in.Body.Permissions)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			a.audit(ctx, u, "subuser.add", s.UUID, v.Email+": "+strings.Join(v.Permissions, ","))
			return &subuserOut{Body: v}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "setSubuserPermissions", Method: "PUT", Path: "/servers/{uuid}/users/{user_id}", Summary: "Change a subuser's permissions (owner only)", DefaultStatus: 204},
		func(ctx context.Context, in *subuserSetIn) (*struct{}, error) {
			u, s, err := a.authorizeOwner(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			if err := a.Store.SetSubuserPermissions(ctx, s.UUID, in.UserID, in.Body.Permissions); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return nil, huma.Error404NotFound("that user has no access to this server")
				}
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			a.audit(ctx, u, "subuser.update", s.UUID, strconv.FormatInt(in.UserID, 10)+": "+strings.Join(in.Body.Permissions, ","))
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "removeSubuser", Method: "DELETE", Path: "/servers/{uuid}/users/{user_id}", Summary: "Remove a subuser's access (owner only)", DefaultStatus: 204},
		func(ctx context.Context, in *subuserIn) (*struct{}, error) {
			u, s, err := a.authorizeOwner(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			if err := a.Store.RemoveSubuser(ctx, s.UUID, in.UserID); err != nil {
				return nil, huma.Error404NotFound("that user has no access to this server")
			}
			a.audit(ctx, u, "subuser.remove", s.UUID, strconv.FormatInt(in.UserID, 10))
			return nil, nil
		})
}
