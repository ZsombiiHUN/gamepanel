package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// Two-factor policy (ASVS v5.0.0-6.3.3), set with GAMEPANEL_REQUIRE_MFA.
const (
	MFAOptional = "off"    // each account chooses (default)
	MFAAdmins   = "admins" // administrators must use two factors
	MFAAll      = "all"    // everyone must
)

// ValidMFAPolicy reports whether p is a policy name ("" means off).
func ValidMFAPolicy(p string) bool {
	switch p {
	case "", MFAOptional, MFAAdmins, MFAAll:
		return true
	}
	return false
}

// mfaRequired reports whether the panel requires two factors of u.
func (a *App) mfaRequired(u store.User) bool {
	switch a.RequireMFA {
	case MFAAll:
		return true
	case MFAAdmins:
		return u.IsAdmin
	}
	return false
}

// enrolmentPath reports whether a session of an account that has no second
// factor yet, on a panel that requires one, may use path: the web app itself, and the account's own security
// settings (sign out, password, authenticator, passkeys, sessions), but not
// API keys, which would outlive the restriction.
func enrolmentPath(path string) bool {
	if !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/daemon/") {
		return true
	}
	if strings.HasPrefix(path, "/api/auth/keys") {
		return false
	}
	return strings.HasPrefix(path, "/api/auth/")
}

const mfaSetupMessage = "this panel requires two-factor authentication: set up an authenticator app or a passkey on your account page"

// gateMFA applies the two-factor policy to a resolved credential. A session
// that signed in with one factor is held to enrolmentPath until it proves a
// second; an API key works only while its owner has a second factor set up.
// It returns the user (with MFARequired set) and whether the request may go on.
func (a *App) gateMFA(w http.ResponseWriter, r *http.Request, u store.User, viaKey bool) (store.User, bool) {
	if !a.mfaRequired(u) {
		return u, true
	}
	if viaKey {
		if u.TOTPEnabled || u.HasPasskey {
			return u, true
		}
		writeProblem(w, http.StatusForbidden, mfaSetupMessage+"; API keys stop working until then")
		return u, false
	}
	if u.SessionMFA {
		return u, true
	}
	u.MFARequired = true
	if u.TOTPEnabled || u.HasPasskey {
		// The account has a second factor but this session did not use it
		// (a password alone with only a passkey set up, or single sign-on).
		// Letting it set up another factor would let a stolen password
		// skip the one the owner has, so it can only sign out.
		u.MFASignInAgain = true
		if signOutPath(r) {
			return u, true
		}
		writeProblem(w, http.StatusForbidden, "this panel requires two-factor authentication: sign out and sign in again with your passkey or authenticator code")
		return u, false
	}
	if enrolmentPath(r.URL.Path) {
		return u, true
	}
	writeProblem(w, http.StatusForbidden, mfaSetupMessage)
	return u, false
}

// signOutPath: what a session that must sign in again may still do.
func signOutPath(r *http.Request) bool {
	p := r.URL.Path
	if !strings.HasPrefix(p, "/api/") && !strings.HasPrefix(p, "/daemon/") {
		return true
	}
	return (p == "/api/auth/me" && r.Method == http.MethodGet) || (p == "/api/auth/logout" && r.Method == http.MethodPost)
}

// loginUser is the user as a sign-in answer reports it: MFARequired tells the
// client to send the person to their account page first.
func (a *App) loginUser(u store.User, method string) store.User {
	u.MFARequired = a.mfaRequired(u) && !store.IsMFAMethod(method)
	u.MFASignInAgain = u.MFARequired && (u.TOTPEnabled || u.HasPasskey)
	return u
}

// secondFactorProved upgrades the caller's session after it set up a second
// factor, lifting the enrolment-only restriction.
func (a *App) secondFactorProved(ctx context.Context) {
	if tok := sessionToken(ctx); tok != "" {
		if err := a.Store.MarkSessionMFA(ctx, tok); err != nil {
			a.Log.Warn("could not mark session as two-factor", "err", err)
		}
	}
}

// keepsSecondFactor refuses removing an account's last second factor while
// the panel requires one.
func (a *App) keepsSecondFactor(u store.User, totpAfter, passkeysAfter bool) error {
	if a.mfaRequired(u) && !totpAfter && !passkeysAfter {
		return huma.Error409Conflict("this panel requires two-factor authentication, so you cannot remove your last second factor; add another one first")
	}
	return nil
}
