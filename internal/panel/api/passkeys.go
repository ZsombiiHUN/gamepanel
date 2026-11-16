package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// Passkeys (WebAuthn). They sign in without a password and need no second
// factor: a passkey is something you have, unlocked by something you know or
// are (user verification is required).

const passkeyCeremonyTTL = 5 * time.Minute

// passkeysOn: WebAuthn binds credentials to the panel's domain, so it needs
// the public address.
func (a *App) passkeysOn() bool { return a.webauthn() != nil }

func (a *App) webauthn() *webauthn.WebAuthn {
	a.waOnce.Do(func() {
		if a.PublicURL == "" {
			return
		}
		u, err := url.Parse(a.PublicURL)
		if err != nil || u.Hostname() == "" {
			return
		}
		w, err := webauthn.New(&webauthn.Config{RPID: u.Hostname(), RPDisplayName: "GamePanel", RPOrigins: []string{u.Scheme + "://" + u.Host}})
		if err != nil {
			a.Log.Warn("passkeys are off: could not set up WebAuthn", "err", err)
			return
		}
		a.wa = w
	})
	return a.wa
}

// waUser adapts an account to the WebAuthn library.
type waUser struct {
	u      store.User
	handle []byte
	creds  []webauthn.Credential
}

func (w *waUser) WebAuthnID() []byte                         { return w.handle }
func (w *waUser) WebAuthnName() string                       { return w.u.Email }
func (w *waUser) WebAuthnDisplayName() string                { return w.u.Email }
func (w *waUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

func (a *App) loadWAUser(ctx context.Context, u store.User) (*waUser, error) {
	h, err := a.Store.WebAuthnHandle(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	data, err := a.Store.PasskeyData(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	w := &waUser{u: u, handle: h}
	for _, d := range data {
		var c webauthn.Credential
		if json.Unmarshal(d, &c) == nil {
			w.creds = append(w.creds, c)
		}
	}
	return w, nil
}

type waCeremony struct {
	session webauthn.SessionData
	userID  int64 // registration: who is registering
	expires time.Time
}

func (a *App) keepCeremony(key string, c waCeremony) {
	c.expires = time.Now().Add(passkeyCeremonyTTL)
	a.waCeremonies.Store(key, c)
	time.AfterFunc(passkeyCeremonyTTL, func() { a.waCeremonies.Delete(key) })
}

func (a *App) takeCeremony(key string) (waCeremony, bool) {
	v, ok := a.waCeremonies.LoadAndDelete(key)
	c, _ := v.(waCeremony)
	return c, ok && time.Now().Before(c.expires)
}

type passkeyBeginIn struct {
	Body struct {
		Password string `json:"password,omitempty" maxLength:"256" doc:"required when the account has a password"`
	}
}
type passkeyOptionsOut struct {
	Body struct {
		// Options go to navigator.credentials.create() or .get() as they are
		// (after decoding the base64url fields).
		Options json.RawMessage `json:"options"`
		// Ticket ties the answer to this sign-in attempt.
		Ticket string `json:"ticket,omitempty"`
	}
}
type passkeyFinishIn struct {
	Body struct {
		Name       string          `json:"name,omitempty" maxLength:"64"`
		Ticket     string          `json:"ticket,omitempty" maxLength:"100"`
		Credential json.RawMessage `json:"credential" doc:"the PublicKeyCredential, JSON-encoded"`
	}
}
type passkeyOut struct{ Body store.Passkey }
type passkeysOut struct{ Body []store.Passkey }
type passkeyIDIn struct {
	ID string `path:"id" maxLength:"512"`
}
type passkeyRenameIn struct {
	ID   string `path:"id" maxLength:"512"`
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"64"`
	}
}

func (a *App) registerPasskeys(api huma.API) {
	off := func() error {
		return huma.Error404NotFound("passkeys need the panel's public address (GAMEPANEL_PUBLIC_URL)")
	}

	huma.Register(api, huma.Operation{OperationID: "passkeyRegisterBegin", Method: "POST", Path: "/auth/passkeys/register/begin", Summary: "Start adding a passkey"},
		func(ctx context.Context, in *passkeyBeginIn) (*passkeyOptionsOut, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			wa := a.webauthn()
			if wa == nil {
				return nil, off()
			}
			// A new way into the account: prove it is you, as for API keys.
			if a.Store.HasPassword(ctx, u.ID) {
				if err := a.confirm(ctx, u, in.Body.Password); err != nil {
					return nil, err
				}
			}
			w, err := a.loadWAUser(ctx, u)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			exclude := make([]protocol.CredentialDescriptor, len(w.creds))
			for i := range w.creds {
				exclude[i] = w.creds[i].Descriptor()
			}
			opts, session, err := wa.BeginRegistration(w,
				webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
				webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementRequired, UserVerification: protocol.VerificationRequired}),
				webauthn.WithExclusions(exclude))
			if err != nil {
				return nil, huma.Error500InternalServerError("could not start", err)
			}
			a.keepCeremony("reg:"+itoa64(u.ID), waCeremony{session: *session, userID: u.ID})
			out := &passkeyOptionsOut{}
			out.Body.Options, _ = json.Marshal(opts)
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "passkeyRegisterFinish", Method: "POST", Path: "/auth/passkeys/register/finish", Summary: "Save a new passkey", DefaultStatus: 201},
		func(ctx context.Context, in *passkeyFinishIn) (*passkeyOut, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			wa := a.webauthn()
			if wa == nil {
				return nil, off()
			}
			c, ok := a.takeCeremony("reg:" + itoa64(u.ID))
			if !ok || c.userID != u.ID {
				return nil, huma.Error409Conflict("that took too long; start again")
			}
			parsed, err := protocol.ParseCredentialCreationResponseBytes(in.Body.Credential)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity("the browser's answer could not be read")
			}
			w, err := a.loadWAUser(ctx, u)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			cred, err := wa.CreateCredential(w, c.session, parsed)
			if err != nil {
				a.Log.Info("passkey registration refused", "user", u.ID, "err", err)
				return nil, huma.Error422UnprocessableEntity("the passkey could not be verified")
			}
			data, _ := json.Marshal(cred)
			p, err := a.Store.AddPasskey(ctx, u.ID, cred.ID, in.Body.Name, data)
			if err != nil {
				return nil, userErr(http.StatusConflict, err)
			}
			a.audit(ctx, u, "passkey.add", u.Email, p.Name)
			return &passkeyOut{Body: p}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "passkeyLoginBegin", Method: "POST", Path: "/auth/passkeys/login/begin", Summary: "Start signing in with a passkey"},
		func(ctx context.Context, _ *struct{}) (*passkeyOptionsOut, error) {
			wa := a.webauthn()
			if wa == nil {
				return nil, off()
			}
			if !a.limiter.allow("passkey:" + ipFrom(ctx)) {
				return nil, huma.Error429TooManyRequests("too many sign-in attempts, try again in a minute")
			}
			opts, session, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
			if err != nil {
				return nil, huma.Error500InternalServerError("could not start", err)
			}
			ticket := randomToken()
			a.keepCeremony("login:"+ticket, waCeremony{session: *session})
			out := &passkeyOptionsOut{}
			out.Body.Options, _ = json.Marshal(opts)
			out.Body.Ticket = ticket
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "passkeyLoginFinish", Method: "POST", Path: "/auth/passkeys/login/finish", Summary: "Sign in with a passkey"},
		func(ctx context.Context, in *passkeyFinishIn) (*loginOut, error) {
			wa := a.webauthn()
			if wa == nil {
				return nil, off()
			}
			c, ok := a.takeCeremony("login:" + in.Body.Ticket)
			if in.Body.Ticket == "" || !ok {
				return nil, huma.Error401Unauthorized("that took too long; try again")
			}
			parsed, err := protocol.ParseCredentialRequestResponseBytes(in.Body.Credential)
			if err != nil {
				return nil, huma.Error401Unauthorized("the passkey could not be verified")
			}
			var who *waUser
			lookup := func(rawID, handle []byte) (webauthn.User, error) {
				u, err := a.Store.UserByWebAuthnHandle(ctx, handle)
				if err != nil {
					return nil, err
				}
				who, err = a.loadWAUser(ctx, u)
				return who, err
			}
			_, cred, err := wa.ValidatePasskeyLogin(lookup, c.session, parsed)
			if err != nil || who == nil {
				a.audit(ctx, store.User{}, "login.fail", "", "passkey not accepted")
				return nil, huma.Error401Unauthorized("the passkey could not be verified")
			}
			if cred.Authenticator.CloneWarning {
				// The signature counter went backwards: the key may have been copied.
				a.audit(ctx, who.u, "login.blocked", who.u.Email, "passkey sign counter went backwards (possible clone)")
				return nil, huma.Error401Unauthorized("this passkey looks copied, so it was refused; use another way to sign in")
			}
			data, _ := json.Marshal(cred)
			if err := a.Store.UsePasskey(ctx, who.u.ID, cred.ID, data); err != nil {
				return nil, huma.Error401Unauthorized("the passkey could not be verified")
			}
			tok, err := a.Store.NewSession(ctx, who.u.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("could not create session")
			}
			a.fails.reset(who.u.Email)
			a.audit(ctx, who.u, "login.ok", who.u.Email, "passkey")
			return &loginOut{Body: who.u, SetCookie: sessionCookie(ctx, tok, a.Store.SessionMax)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listPasskeys", Method: "GET", Path: "/auth/passkeys", Summary: "Your passkeys"},
		func(ctx context.Context, _ *struct{}) (*passkeysOut, error) {
			u, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			l, err := a.Store.ListPasskeys(ctx, u.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			return &passkeysOut{Body: l}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "renamePasskey", Method: "PUT", Path: "/auth/passkeys/{id}", Summary: "Rename a passkey", DefaultStatus: 204},
		func(ctx context.Context, in *passkeyRenameIn) (*struct{}, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			if err := a.Store.RenamePasskey(ctx, u.ID, in.ID, in.Body.Name); err != nil {
				return nil, huma.Error404NotFound("passkey not found")
			}
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deletePasskey", Method: "DELETE", Path: "/auth/passkeys/{id}", Summary: "Remove a passkey", DefaultStatus: 204},
		func(ctx context.Context, in *passkeyIDIn) (*struct{}, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			if err := a.Store.DeletePasskey(ctx, u.ID, in.ID); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return nil, huma.Error404NotFound("passkey not found")
				}
				return nil, userErr(http.StatusConflict, err)
			}
			a.audit(ctx, u, "passkey.delete", u.Email, in.ID[:min(len(in.ID), 16)])
			return nil, nil
		})
}

func itoa64(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
