package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// ---- sign-in ----

type loginIn struct {
	Body struct {
		Email    string `json:"email" format:"email" maxLength:"254"`
		Password string `json:"password" minLength:"1" maxLength:"256"`
		// TOTPCode is an authenticator or recovery code, required when the
		// account has two-factor enabled.
		TOTPCode string `json:"totp_code,omitempty" maxLength:"64"`
	}
}
type (
	userOut  struct{ Body store.User }
	loginOut struct {
		SetCookie []http.Cookie `header:"Set-Cookie"`
		Body      store.User
	}
)

type logoutIn struct {
	Session     string `cookie:"gp_session"`
	HostSession string `cookie:"__Host-gp_session"`
}
type logoutOut struct {
	SetCookie []http.Cookie `header:"Set-Cookie"`
}

func (a *App) registerAuth(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "login", Method: "POST", Path: "/auth/login", Summary: "Sign in"},
		func(ctx context.Context, in *loginIn) (*loginOut, error) {
			email := strings.ToLower(strings.TrimSpace(in.Body.Email))
			if a.fails.over(email) {
				a.audit(ctx, store.User{Email: email}, "login.blocked", email, "too many failures for this account")
				return nil, huma.Error429TooManyRequests("too many failed attempts for this account, try again later")
			}
			u, err := a.Store.Login(ctx, email, in.Body.Password)
			if err != nil {
				a.fails.add(email)
				a.audit(ctx, store.User{Email: email}, "login.fail", email, "")
				return nil, huma.Error401Unauthorized("invalid email or password")
			}
			method := store.MethodPassword
			if u.TOTPEnabled {
				if in.Body.TOTPCode == "" {
					// Password was right; tell the client to ask for the code.
					// Not a failure, so it does not count against the account.
					return nil, huma.Error401Unauthorized("totp_required")
				}
				kind, err := a.Store.VerifySecondFactor(ctx, u.ID, in.Body.TOTPCode)
				if errors.Is(err, store.ErrSecondFactorLocked) {
					a.audit(ctx, u, "login.blocked", email, "too many wrong two-factor codes in a row")
					return nil, huma.Error429TooManyRequests(err.Error())
				}
				if err != nil {
					a.fails.add(email)
					a.audit(ctx, u, "login.fail", email, "bad second factor")
					return nil, huma.Error401Unauthorized("invalid code")
				}
				method = store.MethodTOTP
				if kind == "recovery" {
					method = store.MethodRecovery
					a.audit(ctx, u, "login.recovery_code", email, "a recovery code was used")
				}
			}
			a.fails.reset(email)
			a.audit(ctx, u, "login.ok", email, "")
			tok, err := a.newSession(ctx, u.ID, method)
			if err != nil {
				return nil, huma.Error500InternalServerError("could not create session")
			}
			return &loginOut{Body: a.loginUser(u, method), SetCookie: sessionCookies(ctx, tok, a.Store.SessionMax)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "logout", Method: "POST", Path: "/auth/logout", Summary: "Sign out"},
		func(ctx context.Context, in *logoutIn) (*logoutOut, error) {
			// Revoke server-side, not just in the browser, under either cookie name.
			for _, s := range []string{in.HostSession, in.Session} {
				if s == "" {
					continue
				}
				if err := a.Store.DeleteSession(ctx, s); err != nil {
					return nil, huma.Error500InternalServerError("could not sign out; try again", err)
				}
			}
			if u, ok := userFrom(ctx); ok {
				a.audit(ctx, u, "logout", u.Email, "")
			}
			return &logoutOut{SetCookie: sessionCookies(ctx, "", 0)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "me", Method: "GET", Path: "/auth/me", Summary: "Current user"},
		func(ctx context.Context, _ *struct{}) (*userOut, error) {
			u, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			return &userOut{Body: u}, nil
		})
}

// ---- account ----

type passwordIn struct {
	Body struct {
		Current string `json:"current_password" minLength:"1" maxLength:"256"`
		New     string `json:"new_password" minLength:"12" maxLength:"256"`
		Code    string `json:"code,omitempty" maxLength:"64" doc:"authenticator or recovery code; required when two-factor is on"`
	}
}
type auditIn struct {
	Limit  int    `query:"limit" default:"100" minimum:"1" maximum:"500"`
	Offset int    `query:"offset" default:"0" minimum:"0"`
	Q      string `query:"q" maxLength:"200" doc:"Only entries containing every word, in the action, user, target, detail or IP."`
}
type auditOut struct{ Body []store.AuditEntry }

func (a *App) registerAccount(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "changePassword", Method: "POST", Path: "/auth/password",
		Summary: "Change your password (signs out every other session)", DefaultStatus: 204,
	},
		func(ctx context.Context, in *passwordIn) (*struct{}, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			// Same brute-force guard as login: the current password is a guess target too.
			if a.fails.over(u.Email) {
				return nil, huma.Error429TooManyRequests("too many failed attempts, try again later")
			}
			if u.TOTPEnabled {
				// With two-factor on, the password alone does not prove who
				// is at the keyboard: ask for a code too, like creating an API
				// key. The password and the new password's policy are checked
				// first, so a code is not spent on a request that would fail.
				if in.Body.Code == "" {
					return nil, huma.Error403Forbidden("totp_required")
				}
				if !a.Store.CheckPassword(ctx, u.ID, in.Body.Current) {
					a.fails.add(u.Email)
					a.audit(ctx, u, "password.change.fail", u.Email, "")
					return nil, huma.Error403Forbidden("current password is incorrect")
				}
				if err := store.ValidatePassword(in.Body.New, u.Email); err != nil {
					return nil, userErr(http.StatusUnprocessableEntity, err)
				}
				if err := a.confirmCode(ctx, u, in.Body.Code); err != nil {
					return nil, err
				}
			}
			if err := a.Store.ChangePassword(ctx, u.ID, in.Body.Current, in.Body.New, sessionToken(ctx)); err != nil {
				if errors.Is(err, store.ErrBadPassword) {
					a.fails.add(u.Email)
					a.audit(ctx, u, "password.change.fail", u.Email, "")
					return nil, huma.Error403Forbidden("current password is incorrect")
				}
				return nil, userErr(http.StatusUnprocessableEntity, err)
			}
			a.audit(ctx, u, "password.change", u.Email, "other sessions revoked")
			return nil, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "revokeOtherSessions", Method: "POST", Path: "/auth/sessions/revoke-others",
		Summary: "Sign out everywhere except this browser", DefaultStatus: 204,
	},
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			if err := a.Store.DeleteOtherSessions(ctx, u.ID, sessionToken(ctx)); err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			a.audit(ctx, u, "sessions.revoke_others", u.Email, "")
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listAudit", Method: "GET", Path: "/audit", Summary: "Audit log (admin)"},
		func(ctx context.Context, in *auditIn) (*auditOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			es, err := a.Store.ListAudit(ctx, in.Q, in.Limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			return &auditOut{Body: es}, nil
		})
}
