package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/mail"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// Password resets and invitations. Links carry their token in the URL
// fragment (#token=...), which browsers never send to a server or put in a
// Referer, so it cannot end up in access logs.

const (
	SignupInvite = "invite" // accounts only through an invitation (default)
	SignupOpen   = "open"   // anyone may create an account
)

type optionsOut struct {
	Body struct {
		// PasswordReset: "forgot password" works (email is set up).
		PasswordReset bool          `json:"password_reset"`
		Signup        string        `json:"signup" enum:"invite,open"`
		SSO           []ssoProvider `json:"sso" nullable:"false"`
		Passkeys      bool          `json:"passkeys"`
	}
}

type forgotIn struct {
	Body struct {
		Email string `json:"email" format:"email" maxLength:"254"`
	}
}
type resetIn struct {
	Body struct {
		Token    string `json:"token" minLength:"20" maxLength:"100"`
		Password string `json:"password" minLength:"1" maxLength:"256"`
	}
}
type tokenIn struct {
	Body struct {
		Token string `json:"token" minLength:"20" maxLength:"100"`
	}
}
type inviteOut struct{ Body store.Invite }
type registerIn struct {
	Body struct {
		Token    string `json:"token,omitempty" maxLength:"100" doc:"invitation token; required unless sign-up is open"`
		Email    string `json:"email,omitempty" maxLength:"254" doc:"ignored with a token: the invited address is used"`
		Password string `json:"password" minLength:"1" maxLength:"256"`
	}
}
type inviteCreateIn struct {
	Body struct {
		Email string `json:"email" format:"email" maxLength:"254"`
		Admin bool   `json:"admin,omitempty"`
	}
}
type serverInviteIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Email       string   `json:"email" format:"email" maxLength:"254"`
		Permissions []string `json:"permissions" minItems:"1"`
	}
}
type invitationResult struct {
	// Added: the email already had an account, so it was given access
	// straight away and no invitation was needed.
	Added  bool          `json:"added"`
	Invite *store.Invite `json:"invite,omitempty"`
	// Link to pass on yourself (shown once). Emailed: the panel also sent it.
	Link    string `json:"link,omitempty"`
	Emailed bool   `json:"emailed"`
}
type invitationOut struct{ Body invitationResult }
type invitesOut struct{ Body []store.Invite }
type inviteIDIn struct {
	ID int64 `path:"id"`
}
type serverInviteIDIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	ID   int64  `path:"id"`
}

// link builds a link into the panel. Without a configured public URL it is
// relative, and the browser that shows it completes it.
func (a *App) link(path, token string) string {
	return strings.TrimRight(a.PublicURL, "/") + path + "#token=" + token
}

// mailOn reports whether the panel can email links: SMTP and a public URL
// (links in mail must never be built from a request's Host header).
func (a *App) mailOn() bool { return a.Mail != nil && a.PublicURL != "" }

// sendMail sends in the background, so how long a request takes never shows
// whether an email went out.
func (a *App) sendMail(m mail.Message) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := a.Mail.Send(ctx, m); err != nil {
			a.Log.Warn("could not send email", "subject", m.Subject, "err", err)
		}
	}()
}

func (a *App) registerLinks(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "authOptions", Method: "GET", Path: "/auth/options", Summary: "Ways to sign in on this panel (public)"},
		func(ctx context.Context, _ *struct{}) (*optionsOut, error) {
			out := &optionsOut{}
			out.Body.PasswordReset = a.mailOn()
			out.Body.Signup = a.signup()
			out.Body.SSO = a.ssoList()
			out.Body.Passkeys = a.passkeysOn()
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "forgotPassword", Method: "POST", Path: "/auth/forgot",
		Summary: "Email a password reset link", Description: "Always answers the same, whether or not the email has an account.", DefaultStatus: 202},
		func(ctx context.Context, in *forgotIn) (*struct{}, error) {
			if !a.mailOn() {
				return nil, huma.Error404NotFound("password reset by email is not set up on this panel; ask an administrator")
			}
			email := strings.ToLower(strings.TrimSpace(in.Body.Email))
			if !a.mailLimit.allow("ip:"+ipFrom(ctx)) || !a.mailLimit.allow("email:"+email) {
				return nil, huma.Error429TooManyRequests("too many reset requests, try again later")
			}
			u, err := a.Store.UserByEmail(ctx, email)
			if err != nil {
				a.audit(ctx, store.User{Email: email}, "password.reset.request", email, "no such account")
				return nil, nil
			}
			tok, err := a.Store.NewResetToken(ctx, u.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("could not make a link", err)
			}
			a.audit(ctx, u, "password.reset.request", email, "")
			a.sendMail(mail.Message{To: u.Email, Subject: "Reset your GamePanel password", Text: fmt.Sprintf(
				"Someone (hopefully you) asked to reset the password of %s.\n\nChoose a new password here within the next hour:\n%s\n\nIf it was not you, ignore this email: your password stays as it is.\n",
				u.Email, a.link("/reset", tok))})
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "resetPassword", Method: "POST", Path: "/auth/reset",
		Summary: "Set a new password with a reset link (signs out everywhere)", DefaultStatus: 204},
		func(ctx context.Context, in *resetIn) (*struct{}, error) {
			if !a.mailLimit.allow("ip:" + ipFrom(ctx)) {
				return nil, huma.Error429TooManyRequests("too many attempts, try again later")
			}
			u, err := a.Store.ResetPassword(ctx, in.Body.Token, in.Body.Password)
			if err != nil {
				return nil, userErr(http.StatusUnprocessableEntity, err)
			}
			a.fails.reset(u.Email)
			a.audit(ctx, u, "password.reset", u.Email, "all sessions signed out")
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "lookupInvite", Method: "POST", Path: "/auth/invite", Summary: "What an invitation is for (public)"},
		func(ctx context.Context, in *tokenIn) (*inviteOut, error) {
			if !a.mailLimit.allow("ip:" + ipFrom(ctx)) {
				return nil, huma.Error429TooManyRequests("too many attempts, try again later")
			}
			inv, err := a.Store.GetInvite(ctx, in.Body.Token)
			if err != nil {
				return nil, userErr(http.StatusNotFound, err)
			}
			return &inviteOut{Body: inv}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "register", Method: "POST", Path: "/auth/register", Summary: "Create an account (signs you in)", DefaultStatus: 201},
		func(ctx context.Context, in *registerIn) (*loginOut, error) {
			if !a.mailLimit.allow("ip:" + ipFrom(ctx)) {
				return nil, huma.Error429TooManyRequests("too many attempts, try again later")
			}
			b := in.Body
			if b.Token == "" && a.signup() != SignupOpen {
				return nil, huma.Error403Forbidden("this panel is invitation only")
			}
			if b.Token == "" && !strings.Contains(b.Email, "@") {
				return nil, huma.Error422UnprocessableEntity("enter your email address")
			}
			u, err := a.Store.Register(ctx, b.Token, b.Email, b.Password)
			if err != nil {
				return nil, userErr(http.StatusUnprocessableEntity, err)
			}
			a.audit(ctx, u, "account.register", u.Email, map[bool]string{true: "invited", false: "open sign-up"}[b.Token != ""])
			tok, err := a.Store.NewSession(ctx, u.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("could not create session")
			}
			return &loginOut{Body: u, SetCookie: sessionCookie(ctx, tok, a.Store.SessionMax)}, nil
		})

	// ---- admin: account invitations ----
	huma.Register(api, huma.Operation{OperationID: "createInvite", Method: "POST", Path: "/invites", Summary: "Invite someone to make an account (admin)", DefaultStatus: 201},
		func(ctx context.Context, in *inviteCreateIn) (*invitationOut, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			tok, inv, err := a.Store.NewInvite(ctx, in.Body.Email, u.ID, "", nil, in.Body.Admin)
			if err != nil {
				return nil, userErr(http.StatusConflict, err)
			}
			a.audit(ctx, u, "invite.create", inv.Email, map[bool]string{true: "admin", false: ""}[inv.IsAdmin])
			return a.deliverInvite(u, inv, tok, "")
		})
	huma.Register(api, huma.Operation{OperationID: "listInvites", Method: "GET", Path: "/invites", Summary: "Pending account invitations (admin)"},
		func(ctx context.Context, _ *struct{}) (*invitesOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			l, err := a.Store.ListInvites(ctx, "")
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			return &invitesOut{Body: l}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "deleteInvite", Method: "DELETE", Path: "/invites/{id}", Summary: "Withdraw an account invitation (admin)", DefaultStatus: 204},
		func(ctx context.Context, in *inviteIDIn) (*struct{}, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			if err := a.Store.DeleteInvite(ctx, "", in.ID); err != nil {
				return nil, huma.Error404NotFound("invitation not found")
			}
			a.audit(ctx, u, "invite.delete", strconv.FormatInt(in.ID, 10), "")
			return nil, nil
		})

	// ---- owners: invite onto a server ----
	huma.Register(api, huma.Operation{OperationID: "inviteToServer", Method: "POST", Path: "/servers/{uuid}/invites",
		Summary:     "Give someone access by email, inviting them if they have no account",
		Description: "If the email has an account it is added as a subuser at once (added: true). Otherwise an invitation is made: on sign-up the account gets these permissions."},
		func(ctx context.Context, in *serverInviteIn) (*invitationOut, error) {
			u, s, err := a.authorizeOwner(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			if _, err := a.Store.UserByEmail(ctx, in.Body.Email); err == nil {
				sub, err := a.Store.AddSubuser(ctx, s.UUID, in.Body.Email, in.Body.Permissions)
				if err != nil {
					return nil, userErr(http.StatusUnprocessableEntity, err)
				}
				a.audit(ctx, u, "subuser.add", s.UUID, sub.Email+" "+strings.Join(sub.Permissions, ","))
				return &invitationOut{Body: invitationResult{Added: true}}, nil
			}
			tok, inv, err := a.Store.NewInvite(ctx, in.Body.Email, u.ID, s.UUID, in.Body.Permissions, false)
			if err != nil {
				if errors.Is(err, store.ErrAccountExists) {
					return nil, userErr(http.StatusConflict, err)
				}
				return nil, userErr(http.StatusUnprocessableEntity, err)
			}
			inv.ServerName = s.Name
			a.audit(ctx, u, "invite.create", s.UUID, inv.Email+" "+strings.Join(inv.Permissions, ","))
			return a.deliverInvite(u, inv, tok, s.Name)
		})
	huma.Register(api, huma.Operation{OperationID: "listServerInvites", Method: "GET", Path: "/servers/{uuid}/invites", Summary: "Pending invitations to a server"},
		func(ctx context.Context, in *uuidIn) (*invitesOut, error) {
			_, s, err := a.authorizeOwner(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			l, err := a.Store.ListInvites(ctx, s.UUID)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			return &invitesOut{Body: l}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "deleteServerInvite", Method: "DELETE", Path: "/servers/{uuid}/invites/{id}", Summary: "Withdraw an invitation to a server", DefaultStatus: 204},
		func(ctx context.Context, in *serverInviteIDIn) (*struct{}, error) {
			u, s, err := a.authorizeOwner(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			if err := a.Store.DeleteInvite(ctx, s.UUID, in.ID); err != nil {
				return nil, huma.Error404NotFound("invitation not found")
			}
			a.audit(ctx, u, "invite.delete", s.UUID, strconv.FormatInt(in.ID, 10))
			return nil, nil
		})
}

// deliverInvite emails the link when it can and returns it either way.
func (a *App) deliverInvite(by store.User, inv store.Invite, tok, serverName string) (*invitationOut, error) {
	out := &invitationOut{Body: invitationResult{Invite: &inv, Link: a.link("/register", tok)}}
	if a.mailOn() {
		what := "an account on GamePanel"
		if serverName != "" {
			what = "access to the game server " + serverName
		}
		a.sendMail(mail.Message{To: inv.Email, Subject: "You are invited to GamePanel", Text: fmt.Sprintf(
			"%s invited you to %s.\n\nCreate your account here within 7 days:\n%s\n\nIf you did not expect this, ignore this email.\n", by.Email, what, out.Body.Link)})
		out.Body.Emailed = true
	}
	return out, nil
}

func (a *App) signup() string {
	if a.Signup == SignupOpen {
		return SignupOpen
	}
	return SignupInvite
}
