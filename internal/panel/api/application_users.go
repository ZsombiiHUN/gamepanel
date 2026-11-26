package api

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// Accounts in the application API. GamePanel accounts have no user name or
// real name: the user name is the part of the email before the @, and names
// sent are accepted and not kept.

func appUser(acct store.Account) map[string]any {
	return map[string]any{
		"id": acct.ID, "external_id": nullable(acct.ExternalID), "uuid": nil,
		"username": strings.Split(acct.Email, "@")[0], "email": acct.Email,
		"first_name": "", "last_name": "", "language": "en",
		"root_admin": acct.IsAdmin, "2fa": acct.TOTPEnabled,
		"created_at": pteroTime(acct.CreatedAt), "updated_at": pteroTime(acct.CreatedAt),
	}
}

type appUserIn struct {
	Email      *string   `json:"email"`
	ExternalID *string   `json:"external_id"`
	Password   *string   `json:"password"`
	RootAdmin  looseBool `json:"root_admin"`
	// Pterodactyl's username, first_name, last_name and language are
	// accepted and not kept.
}

// accountErr maps account refusals to Pterodactyl's validation errors.
func accountErr(err error) error {
	switch {
	case errors.Is(err, store.ErrEmailTaken):
		return invalid("email", "unique", "The email has already been taken.")
	case errors.Is(err, store.ErrExternalIDTaken):
		return invalid("external_id", "unique", "The external id has already been taken.")
	}
	return err
}

func checkEmail(email string) error {
	if strings.TrimSpace(email) == "" {
		return invalid("email", "required", "The email field is required.")
	}
	if _, err := mail.ParseAddress(email); err != nil || len(email) > 254 {
		return invalid("email", "email", "The email must be a valid email address.")
	}
	return nil
}

func (a *App) userByExternalID(ctx context.Context, ext string) (store.Account, error) {
	l, err := a.Store.ListAccounts(ctx, store.AccountFilter{ExternalID: ext, Limit: 1})
	if err != nil {
		return store.Account{}, err
	}
	if len(l) == 0 {
		return store.Account{}, notFound()
	}
	return l[0], nil
}

func (a *App) registerAppUsers(mux *http.ServeMux) {
	h := a.appRoute(mux)

	h("GET /api/application/users", func(w http.ResponseWriter, r *http.Request, _ store.User) error {
		q := r.URL.Query()
		f := store.AccountFilter{Email: q.Get("filter[email]"), ExternalID: q.Get("filter[external_id]")}
		if u := q.Get("filter[username]"); u != "" && f.Email == "" {
			// No user names here: only an email can match one exactly.
			f.Email = u
		}
		p := pageOf(r)
		total, err := a.Store.CountAccounts(r.Context(), f)
		if err != nil {
			return err
		}
		f.Limit, f.Offset = p.per, p.offset()
		l, err := a.Store.ListAccounts(r.Context(), f)
		if err != nil {
			return err
		}
		items := make([]any, 0, len(l))
		for _, acct := range l {
			items = append(items, appUser(acct))
		}
		pteroList(w, "user", items, total, p)
		return nil
	})

	h("GET /api/application/users/{id}", func(w http.ResponseWriter, r *http.Request, _ store.User) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		acct, err := a.Store.GetAccount(r.Context(), id)
		if err != nil {
			return notFound()
		}
		pteroJSON(w, http.StatusOK, pteroObj{"user", appUser(acct)})
		return nil
	})

	h("GET /api/application/users/external/{external}", func(w http.ResponseWriter, r *http.Request, _ store.User) error {
		acct, err := a.userByExternalID(r.Context(), r.PathValue("external"))
		if err != nil {
			return err
		}
		pteroJSON(w, http.StatusOK, pteroObj{"user", appUser(acct)})
		return nil
	})

	h("POST /api/application/users", a.appCreateUser)
	h("PATCH /api/application/users/{id}", a.appUpdateUser)

	h("DELETE /api/application/users/{id}", func(w http.ResponseWriter, r *http.Request, u store.User) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		if err := a.deleteAccount(r.Context(), u, id); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
}

// appCreateUser makes an account. Without a password, or with one the
// password policy refuses, the person gets a link to choose one.
func (a *App) appCreateUser(w http.ResponseWriter, r *http.Request, u store.User) error {
	var in appUserIn
	if err := readJSON(r, &in); err != nil {
		return err
	}
	if in.Email == nil {
		return invalid("email", "required", "The email field is required.")
	}
	if err := checkEmail(*in.Email); err != nil {
		return err
	}
	n := store.NewAccount{Email: *in.Email, Admin: in.RootAdmin.V}
	if in.ExternalID != nil {
		n.ExternalID = strings.TrimSpace(*in.ExternalID)
	}
	// A billing system's generated password may be shorter than the panel
	// allows. The account is still made, without it: the person gets a
	// link to choose one, as when none is sent.
	weak := false
	if in.Password != nil && *in.Password != "" {
		if store.ValidatePassword(*in.Password, n.Email) == nil {
			n.Password = *in.Password
		} else {
			weak = true
		}
	}
	out, err := a.createAccount(r.Context(), u, n, true)
	if err != nil {
		return accountErr(err)
	}
	if weak {
		a.Log.Info("application API: the new account's password does not meet the password policy; it gets a setup link instead", "account", out.ID)
	}
	pteroJSON(w, http.StatusCreated, pteroObj{"user", appUser(out.Account)})
	return nil
}

// appUpdateUser changes an account's email, external id or password.
func (a *App) appUpdateUser(w http.ResponseWriter, r *http.Request, u store.User) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	acct, err := a.Store.GetAccount(r.Context(), id)
	if err != nil {
		return notFound()
	}
	var in appUserIn
	if err := readJSON(r, &in); err != nil {
		return err
	}
	var c store.AccountChange
	// Modules send every field back, changed or not: only real changes
	// count, so an unchanged email on an admin account is not refused.
	if in.Email != nil && !strings.EqualFold(strings.TrimSpace(*in.Email), acct.Email) {
		if err := checkEmail(*in.Email); err != nil {
			return err
		}
		c.Email = in.Email
	}
	if in.ExternalID != nil && strings.TrimSpace(*in.ExternalID) != acct.ExternalID {
		c.ExternalID = in.ExternalID
	}
	if in.Password != nil && *in.Password != "" {
		email := acct.Email
		if c.Email != nil {
			email = *c.Email
		}
		if err := store.ValidatePassword(*in.Password, email); err != nil {
			msg, _ := safeMessage(err)
			return invalid("password", "min", "The password does not meet the panel's password policy: "+msg+".")
		}
		c.Password = in.Password
	}
	if in.RootAdmin.Set && in.RootAdmin.V != acct.IsAdmin {
		return invalid("root_admin", "prohibited", "Administrator rights cannot be changed through the API; change them in the panel.")
	}
	if c != (store.AccountChange{}) {
		if acct, err = a.updateAccount(r.Context(), u, acct, c); err != nil {
			return accountErr(err)
		}
	}
	pteroJSON(w, http.StatusOK, pteroObj{"user", appUser(acct)})
	return nil
}
