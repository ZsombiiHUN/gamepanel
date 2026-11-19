package api

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

type uaKey struct{}

func userAgentFrom(ctx context.Context) string {
	s, _ := ctx.Value(uaKey{}).(string)
	return s
}

// newSession starts a session for a completed sign-in, recording where it
// came from so the account owner can recognise it later.
func (a *App) newSession(ctx context.Context, userID int64, method string) (string, error) {
	return a.Store.NewSession(ctx, userID, store.SessionMeta{IP: ipFrom(ctx), UserAgent: userAgentFrom(ctx), Method: method})
}

type (
	sessionsOut struct{ Body []store.Session }
	sessionIDIn struct {
		ID string `path:"id" minLength:"32" maxLength:"32" pattern:"^[0-9a-f]{32}$"`
	}
)

// registerSessions: the account's session list (ASVS v5.0.0-7.5.2). Both
// need a browser session: an API key cannot see or end sessions.
func (a *App) registerSessions(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listSessions", Method: "GET", Path: "/auth/sessions",
		Summary: "Your signed-in sessions",
	},
		func(ctx context.Context, _ *struct{}) (*sessionsOut, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			l, err := a.Store.ListSessions(ctx, u.ID, sessionToken(ctx))
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			return &sessionsOut{Body: l}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "revokeSession", Method: "DELETE", Path: "/auth/sessions/{id}",
		Summary: "Sign out one of your other sessions", DefaultStatus: 204,
	},
		func(ctx context.Context, in *sessionIDIn) (*struct{}, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			err = a.Store.DeleteSessionByID(ctx, u.ID, in.ID, sessionToken(ctx))
			switch {
			case errors.Is(err, store.ErrNotFound):
				return nil, huma.Error404NotFound("session not found (it may have ended already)")
			case errors.Is(err, store.ErrCurrentSession):
				return nil, huma.Error409Conflict(err.Error())
			case err != nil:
				return nil, huma.Error500InternalServerError("db error", err)
			}
			a.audit(ctx, u, "sessions.revoke", u.Email, "session "+in.ID[:8])
			return nil, nil
		})
}
