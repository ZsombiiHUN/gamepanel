package api

import (
	"context"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

const totpIssuer = "GamePanel"

type viaKeyCtx struct{}

// requireSession is requireUser for sensitive account actions: a leaked API
// key must not be able to mint more keys, change the password or touch 2FA.
func requireSession(ctx context.Context) (store.User, error) {
	u, err := requireUser(ctx)
	if err != nil {
		return u, err
	}
	if v, _ := ctx.Value(viaKeyCtx{}).(bool); v {
		return u, huma.Error403Forbidden("this action needs a browser session, not an API key")
	}
	return u, nil
}

// confirm checks the account password (and counts failures like login does).
func (a *App) confirm(ctx context.Context, u store.User, password string) error {
	if a.fails.over(u.Email) {
		return huma.Error429TooManyRequests("too many failed attempts, try again later")
	}
	if !a.Store.CheckPassword(ctx, u.ID, password) {
		a.fails.add(u.Email)
		a.audit(ctx, u, "reauth.fail", u.Email, "")
		return huma.Error403Forbidden("password is incorrect")
	}
	return nil
}

// confirmCode checks a TOTP or recovery code for an already-enrolled user.
func (a *App) confirmCode(ctx context.Context, u store.User, code string) error {
	if a.fails.over(u.Email) {
		return huma.Error429TooManyRequests("too many failed attempts, try again later")
	}
	if _, err := a.Store.VerifySecondFactor(ctx, u.ID, code); err != nil {
		a.fails.add(u.Email)
		a.audit(ctx, u, "reauth.fail", u.Email, "bad code")
		return huma.Error403Forbidden("invalid code")
	}
	return nil
}

type totpSetupIn struct {
	Body struct {
		Password string `json:"password" minLength:"1"`
	}
}
type totpSetupOut struct {
	Body struct {
		Secret string `json:"secret" doc:"base32 secret to type into an authenticator app"`
		URI    string `json:"uri" doc:"otpauth:// URI, render as a QR code"`
	}
}
type totpEnableIn struct {
	Body struct {
		Code string `json:"code" minLength:"6" maxLength:"6"`
	}
}
type recoveryOut struct {
	Body struct {
		Codes []string `json:"codes" doc:"single-use recovery codes, shown once"`
	}
}
type totpOffIn struct {
	Session string `cookie:"gp_session"`
	Body    struct {
		Password string `json:"password" minLength:"1"`
		Code     string `json:"code" minLength:"6" maxLength:"20" doc:"authenticator or recovery code"`
	}
}
type regenIn struct {
	Body struct {
		Password string `json:"password" minLength:"1"`
		Code     string `json:"code" minLength:"6" maxLength:"20"`
	}
}

type keyCreateIn struct {
	Body struct {
		Name       string `json:"name" minLength:"1" maxLength:"64"`
		ExpiresInD int    `json:"expires_in_days,omitempty" minimum:"0" maximum:"3650" doc:"0 = never expires"`
	}
}
type keyCreateOut struct {
	Body struct {
		Key   store.APIKey `json:"key"`
		Token string       `json:"token" doc:"the secret; shown only once"`
	}
}
type keyListOut struct{ Body []store.APIKey }
type keyIDIn struct {
	ID int64 `path:"id"`
}

func (a *App) registerMFA(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "totpSetup", Method: "POST", Path: "/auth/totp/setup",
		Summary: "Start two-factor setup (returns a secret and QR URI)"},
		func(ctx context.Context, in *totpSetupIn) (*totpSetupOut, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			if err := a.confirm(ctx, u, in.Body.Password); err != nil {
				return nil, err
			}
			secret, err := a.Store.BeginTOTP(ctx, u.ID)
			if err != nil {
				return nil, huma.Error409Conflict(err.Error())
			}
			out := &totpSetupOut{}
			out.Body.Secret, out.Body.URI = secret, store.TOTPURI(totpIssuer, u.Email, secret)
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "totpEnable", Method: "POST", Path: "/auth/totp/enable",
		Summary: "Confirm setup with a code and turn two-factor on"},
		func(ctx context.Context, in *totpEnableIn) (*recoveryOut, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			if a.fails.over(u.Email) {
				return nil, huma.Error429TooManyRequests("too many failed attempts, try again later")
			}
			codes, err := a.Store.EnableTOTP(ctx, u.ID, in.Body.Code)
			if err != nil {
				a.fails.add(u.Email)
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			a.audit(ctx, u, "totp.enable", u.Email, "")
			out := &recoveryOut{}
			out.Body.Codes = codes
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "totpDisable", Method: "POST", Path: "/auth/totp/disable",
		Summary: "Turn two-factor off (password and a code required)", DefaultStatus: 204},
		func(ctx context.Context, in *totpOffIn) (*struct{}, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			if !u.TOTPEnabled {
				return nil, huma.Error409Conflict("two-factor authentication is not enabled")
			}
			if err := a.confirm(ctx, u, in.Body.Password); err != nil {
				return nil, err
			}
			if err := a.confirmCode(ctx, u, in.Body.Code); err != nil {
				return nil, err
			}
			if err := a.Store.DisableTOTP(ctx, u.ID); err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			a.Store.DeleteOtherSessions(ctx, u.ID, in.Session)
			a.audit(ctx, u, "totp.disable", u.Email, "other sessions revoked")
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "regenerateRecoveryCodes", Method: "POST", Path: "/auth/totp/recovery-codes",
		Summary: "Replace recovery codes (password and a code required)"},
		func(ctx context.Context, in *regenIn) (*recoveryOut, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			if !u.TOTPEnabled {
				return nil, huma.Error409Conflict("two-factor authentication is not enabled")
			}
			if err := a.confirm(ctx, u, in.Body.Password); err != nil {
				return nil, err
			}
			if err := a.confirmCode(ctx, u, in.Body.Code); err != nil {
				return nil, err
			}
			codes, err := a.Store.NewRecoveryCodes(ctx, u.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			a.audit(ctx, u, "totp.recovery_regenerated", u.Email, "")
			out := &recoveryOut{}
			out.Body.Codes = codes
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "createApiKey", Method: "POST", Path: "/auth/keys",
		Summary: "Create an API key (the token is shown once)", DefaultStatus: 201},
		func(ctx context.Context, in *keyCreateIn) (*keyCreateOut, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			ttl := time.Duration(in.Body.ExpiresInD) * 24 * time.Hour
			k, tok, err := a.Store.CreateAPIKey(ctx, u.ID, in.Body.Name, ttl)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			a.audit(ctx, u, "apikey.create", strconv.FormatInt(k.ID, 10), k.Name)
			out := &keyCreateOut{}
			out.Body.Key, out.Body.Token = k, tok
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listApiKeys", Method: "GET", Path: "/auth/keys", Summary: "List your API keys"},
		func(ctx context.Context, _ *struct{}) (*keyListOut, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			ks, err := a.Store.ListAPIKeys(ctx, u.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			return &keyListOut{Body: ks}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteApiKey", Method: "DELETE", Path: "/auth/keys/{id}",
		Summary: "Revoke an API key", DefaultStatus: 204},
		func(ctx context.Context, in *keyIDIn) (*struct{}, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			if err := a.Store.DeleteAPIKey(ctx, u.ID, in.ID); err != nil {
				return nil, huma.Error404NotFound("key not found")
			}
			a.audit(ctx, u, "apikey.delete", strconv.FormatInt(in.ID, 10), "")
			return nil, nil
		})
}
