package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// Locations group nodes, so a shop can sell a server "in Frankfurt" and the
// panel picks a node there (location_ids on POST /servers, deploy.locations
// in the Pterodactyl application API).

type (
	locationOut     struct{ Body store.Location }
	locationListOut struct{ Body []store.Location }
	locationIn      struct {
		Body struct {
			Short string `json:"short" minLength:"1" maxLength:"60" doc:"a short code, unique, e.g. eu-west"`
			Long  string `json:"long,omitempty" maxLength:"191" doc:"a description, e.g. Frankfurt, Germany"`
		}
	}
	locationUpdateIn struct {
		ID   int64 `path:"id"`
		Body struct {
			Short *string `json:"short,omitempty" minLength:"1" maxLength:"60"`
			Long  *string `json:"long,omitempty" maxLength:"191"`
		}
	}
	nodeLocationIn struct {
		ID   int64 `path:"id"`
		Body struct {
			LocationID int64 `json:"location_id" minimum:"0" doc:"0 takes the node out of any location"`
		}
	}
)

func locationStoreErr(err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return huma.Error404NotFound("location not found")
	case errors.Is(err, store.ErrLocationTaken), errors.Is(err, store.ErrLocationInUse):
		return userErr(http.StatusConflict, err)
	}
	return userErr(http.StatusUnprocessableEntity, err)
}

func (a *App) registerLocations(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "listLocations", Method: "GET", Path: "/locations", Summary: "List locations (admin)"},
		func(ctx context.Context, _ *struct{}) (*locationListOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			l, err := a.Store.ListLocations(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			return &locationListOut{Body: l}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "createLocation", Method: "POST", Path: "/locations", Summary: "Create a location (admin)", DefaultStatus: 201},
		func(ctx context.Context, in *locationIn) (*locationOut, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			l, err := a.Store.CreateLocation(ctx, in.Body.Short, in.Body.Long)
			if err != nil {
				return nil, locationStoreErr(err)
			}
			a.audit(ctx, u, "location.create", l.Short, l.Long)
			return &locationOut{Body: l}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "updateLocation", Method: "PATCH", Path: "/locations/{id}", Summary: "Rename a location (admin)"},
		func(ctx context.Context, in *locationUpdateIn) (*locationOut, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			l, err := a.Store.UpdateLocation(ctx, in.ID, in.Body.Short, in.Body.Long)
			if err != nil {
				return nil, locationStoreErr(err)
			}
			a.audit(ctx, u, "location.update", l.Short, l.Long)
			return &locationOut{Body: l}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteLocation", Method: "DELETE", Path: "/locations/{id}", Summary: "Delete a location that has no nodes (admin)", DefaultStatus: 204},
		func(ctx context.Context, in *idIn) (*struct{}, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			l, err := a.Store.GetLocation(ctx, in.ID)
			if err != nil {
				return nil, locationStoreErr(err)
			}
			if err := a.Store.DeleteLocation(ctx, in.ID); err != nil {
				return nil, locationStoreErr(err)
			}
			a.audit(ctx, u, "location.delete", l.Short, "")
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "setNodeLocation", Method: "PUT", Path: "/nodes/{id}/location", Summary: "Put a node in a location (admin)", DefaultStatus: 204},
		func(ctx context.Context, in *nodeLocationIn) (*struct{}, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			if err := a.Store.SetNodeLocation(ctx, in.ID, in.Body.LocationID); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return nil, huma.Error404NotFound("node not found")
				}
				return nil, userErr(http.StatusUnprocessableEntity, err)
			}
			a.audit(ctx, u, "node.location", strconv.FormatInt(in.ID, 10), strconv.FormatInt(in.Body.LocationID, 10))
			return nil, nil
		})
}
