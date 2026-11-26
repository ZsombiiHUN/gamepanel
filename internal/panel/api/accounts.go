package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/mail"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// Accounts as admins and shops manage them: a shop makes the customer's
// account when they buy, so the server can be given to it straight away.

type accountCreateIn struct {
	Body struct {
		Email string `json:"email" format:"email" maxLength:"254"`
		// Without a password the account gets a link to choose one (7 days),
		// emailed when the panel can send email and returned either way.
		Password   string `json:"password,omitempty" maxLength:"256" doc:"leave out to send the person a link to choose one"`
		Admin      bool   `json:"admin,omitempty"`
		ExternalID string `json:"external_id,omitempty" maxLength:"191" doc:"your own id for the customer, unique; a retried create with the same one returns the existing account with 200"`
		SendEmail  *bool  `json:"send_email,omitempty" doc:"email the password link (default true; needs email set up)"`
	}
}
type accountCreated struct {
	store.Account
	// SetupLink lets the person choose a password (shown once). Emailed: the
	// panel also sent it.
	SetupLink string `json:"setup_link,omitempty"`
	Emailed   bool   `json:"emailed"`
}
type accountCreateOut struct {
	Status int
	Body   accountCreated
}
type (
	accountOut    struct{ Body store.Account }
	accountListIn struct {
		Email      string `query:"email" maxLength:"254" doc:"only the account with this email"`
		ExternalID string `query:"external_id" maxLength:"191" doc:"only the account with this external id"`
		Limit      int    `query:"limit" minimum:"1" maximum:"500" default:"100"`
		Offset     int    `query:"offset" minimum:"0"`
	}
)

type (
	accountListOut  struct{ Body []store.Account }
	accountUpdateIn struct {
		ID   int64 `path:"id"`
		Body struct {
			Email      *string `json:"email,omitempty" format:"email" maxLength:"254"`
			ExternalID *string `json:"external_id,omitempty" maxLength:"191" doc:"\"\" clears it"`
			Password   *string `json:"password,omitempty" maxLength:"256" doc:"signs the account out everywhere"`
		}
	}
)

func (a *App) registerAccounts(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "createAccount", Method: "POST", Path: "/users", Summary: "Create an account for someone (admin)", DefaultStatus: 201},
		func(ctx context.Context, in *accountCreateIn) (*accountCreateOut, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			b := in.Body
			b.ExternalID = strings.TrimSpace(b.ExternalID)
			if b.ExternalID != "" {
				if l, err := a.Store.ListAccounts(ctx, store.AccountFilter{ExternalID: b.ExternalID}); err == nil && len(l) == 1 {
					if !strings.EqualFold(l[0].Email, strings.TrimSpace(b.Email)) {
						return nil, huma.Error409Conflict(fmt.Sprintf("that external_id belongs to account %d, which has another email", l[0].ID))
					}
					return &accountCreateOut{Status: http.StatusOK, Body: accountCreated{Account: l[0]}}, nil
				}
			}
			out, err := a.createAccount(ctx, u, store.NewAccount{Email: b.Email, Password: b.Password, Admin: b.Admin, ExternalID: b.ExternalID}, b.SendEmail == nil || *b.SendEmail)
			switch {
			case errors.Is(err, store.ErrEmailTaken):
				if l, lerr := a.Store.ListAccounts(ctx, store.AccountFilter{Email: b.Email}); lerr == nil && len(l) == 1 {
					return nil, huma.Error409Conflict(fmt.Sprintf("account %d already has that email", l[0].ID))
				}
				return nil, userErr(http.StatusConflict, err)
			case errors.Is(err, store.ErrExternalIDTaken):
				return nil, userErr(http.StatusConflict, err)
			case err != nil:
				return nil, err
			}
			return &accountCreateOut{Status: http.StatusCreated, Body: out}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listAccounts", Method: "GET", Path: "/users", Summary: "List or find accounts (admin)"},
		func(ctx context.Context, in *accountListIn) (*accountListOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			l, err := a.Store.ListAccounts(ctx, store.AccountFilter{Email: in.Email, ExternalID: in.ExternalID, Limit: in.Limit, Offset: in.Offset})
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			return &accountListOut{Body: l}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "getAccount", Method: "GET", Path: "/users/{id}", Summary: "Get an account (admin)"},
		func(ctx context.Context, in *idIn) (*accountOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			acct, err := a.Store.GetAccount(ctx, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound("account not found")
			}
			return &accountOut{Body: acct}, nil
		})

	a.registerAccountChanges(api)
}

// registerAccountChanges registers updating and deleting accounts.
func (a *App) registerAccountChanges(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "updateAccount", Method: "PATCH", Path: "/users/{id}", Summary: "Change an account's email, external id or password (admin)",
		Description: "Fields left out stay as they are. The email and password of admin accounts can only be changed from their own account settings.",
	},
		func(ctx context.Context, in *accountUpdateIn) (*accountOut, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			acct, err := a.Store.GetAccount(ctx, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound("account not found")
			}
			b := in.Body
			acct, err = a.updateAccount(ctx, u, acct, store.AccountChange{Email: b.Email, ExternalID: b.ExternalID, Password: b.Password})
			switch {
			case errors.Is(err, store.ErrEmailTaken), errors.Is(err, store.ErrExternalIDTaken):
				return nil, userErr(http.StatusConflict, err)
			case err != nil:
				return nil, err
			}
			return &accountOut{Body: acct}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "deleteAccount", Method: "DELETE", Path: "/users/{id}", Summary: "Delete an account that owns no servers (admin)", DefaultStatus: 204,
		Description: "Its access to other people's servers goes with it, and schedules it saved there are turned off.",
	},
		func(ctx context.Context, in *idIn) (*struct{}, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			return nil, a.deleteAccount(ctx, u, in.ID)
		})
}

// createAccount makes an account for someone else. Without a password it
// makes a link to choose one, emailed when sendEmail is set and the panel
// can send email. Taken emails and external ids come back as the store's
// errors; other refusals (a weak password) as 422.
func (a *App) createAccount(ctx context.Context, u store.User, n store.NewAccount, sendEmail bool) (accountCreated, error) {
	acct, err := a.Store.CreateAccount(ctx, n)
	switch {
	case errors.Is(err, store.ErrEmailTaken), errors.Is(err, store.ErrExternalIDTaken):
		return accountCreated{}, err
	case err != nil:
		return accountCreated{}, userErr(http.StatusUnprocessableEntity, err)
	}
	out := accountCreated{Account: acct}
	if n.Password == "" {
		tok, err := a.Store.NewSetupToken(ctx, acct.ID)
		if err != nil {
			return out, huma.Error500InternalServerError("the account was made, but not its password link; send a password reset", err)
		}
		out.SetupLink = a.link("/reset", tok)
		if a.mailOn() && sendEmail {
			a.sendMail(mail.Message{To: acct.Email, Subject: "Your GamePanel account", Text: fmt.Sprintf(
				"An account was made for you on GamePanel (%s).\n\nChoose your password here within 7 days:\n%s\n\nAfter that, use \"forgot password\" on the sign-in page.\n",
				acct.Email, out.SetupLink)})
			out.Emailed = true
		}
	}
	a.audit(ctx, u, "account.create", acct.Email, map[bool]string{true: "admin", false: ""}[acct.IsAdmin])
	a.emit(ctx, EvUserCreated, userEventData{ID: acct.ID, Email: acct.Email, ExternalID: acct.ExternalID})
	return out, nil
}

// updateAccount changes an account's email, external id or password. An
// admin account's email and password cannot be changed this way: a stolen
// shop key must not be a way into the other admins' accounts. Taken emails
// and external ids come back as the store's errors.
func (a *App) updateAccount(ctx context.Context, u store.User, acct store.Account, c store.AccountChange) (store.Account, error) {
	if acct.IsAdmin && (c.Email != nil || c.Password != nil) {
		return acct, huma.Error403Forbidden("change an admin account's email or password from its own account settings")
	}
	if c.ExternalID != nil {
		t := strings.TrimSpace(*c.ExternalID)
		c.ExternalID = &t
	}
	next, err := a.Store.UpdateAccount(ctx, acct.ID, c)
	switch {
	case errors.Is(err, store.ErrEmailTaken), errors.Is(err, store.ErrExternalIDTaken):
		return acct, err
	case err != nil:
		return acct, userErr(http.StatusUnprocessableEntity, err)
	}
	var what []string
	if c.Email != nil {
		what = append(what, "email")
	}
	if c.ExternalID != nil {
		what = append(what, "external id")
	}
	if c.Password != nil {
		what = append(what, "password (signed out everywhere)")
	}
	a.audit(ctx, u, "account.update", next.Email, strings.Join(what, ", "))
	return next, nil
}

// deleteAccount deletes an account that owns no servers, other than the
// caller's own.
func (a *App) deleteAccount(ctx context.Context, u store.User, id int64) error {
	if id == u.ID {
		return huma.Error409Conflict("you cannot delete your own account")
	}
	acct, err := a.Store.GetAccount(ctx, id)
	if err != nil {
		return huma.Error404NotFound("account not found")
	}
	switch err := a.Store.DeleteAccount(ctx, id); {
	case errors.Is(err, store.ErrOwnsServers):
		return userErr(http.StatusConflict, err)
	case errors.Is(err, store.ErrNotFound):
		return huma.Error404NotFound("account not found")
	case err != nil:
		return huma.Error500InternalServerError("db error", err)
	}
	a.fails.reset(acct.Email)
	a.audit(ctx, u, "account.delete", acct.Email, strconv.FormatInt(acct.ID, 10))
	a.emit(ctx, EvUserDeleted, userEventData{ID: acct.ID, Email: acct.Email, ExternalID: acct.ExternalID})
	return nil
}
