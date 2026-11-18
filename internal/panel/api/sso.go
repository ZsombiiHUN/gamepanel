package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// Single sign-on with OAuth 2.0 / OpenID Connect providers (GitHub, Discord,
// and any OIDC issuer such as Google, Microsoft, Authentik or Keycloak).
//
// The authorization code flow with PKCE; the state is bound to the browser
// by a cookie, so a login cannot be forced on someone (login CSRF). Who the
// person is comes from the provider's user endpoint, read with the access
// token over TLS straight from the provider. An identity is only ever used
// once it is linked to an account: matching email addresses never link one
// automatically, because a provider may let anyone claim any address.

// SSOProvider is one way to sign in. Use GitHubSSO, DiscordSSO or OIDCSSO.
type SSOProvider struct {
	ID, Name               string
	ClientID, ClientSecret string
	// Endpoints; for OIDC they are discovered from Issuer on first use.
	AuthURL, TokenURL, UserURL string
	Issuer                     string
	Scopes                     []string
	kind                       string

	mu         sync.Mutex
	discovered bool
}

func GitHubSSO(clientID, secret string) *SSOProvider {
	return &SSOProvider{ID: "github", Name: "GitHub", kind: "github", ClientID: clientID, ClientSecret: secret,
		AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token",
		UserURL: "https://api.github.com/user", Scopes: []string{"read:user", "user:email"}}
}

func DiscordSSO(clientID, secret string) *SSOProvider {
	return &SSOProvider{ID: "discord", Name: "Discord", kind: "discord", ClientID: clientID, ClientSecret: secret,
		AuthURL: "https://discord.com/oauth2/authorize", TokenURL: "https://discord.com/api/oauth2/token",
		UserURL: "https://discord.com/api/users/@me", Scopes: []string{"identify", "email"}}
}

// OIDCSSO is any OpenID Connect provider; name is what the button says.
func OIDCSSO(name, issuer, clientID, secret string) *SSOProvider {
	if name == "" {
		name = "single sign-on"
	}
	return &SSOProvider{ID: "oidc", Name: name, kind: "oidc", Issuer: strings.TrimRight(issuer, "/"), ClientID: clientID, ClientSecret: secret,
		Scopes: []string{"openid", "email", "profile"}}
}

var ssoHTTP = &http.Client{Timeout: 15 * time.Second}

// discover loads an OIDC provider's endpoints once.
func (p *SSOProvider) discover(ctx context.Context) error {
	if p.kind != "oidc" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.discovered {
		return nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.Issuer+"/.well-known/openid-configuration", nil)
	resp, err := ssoHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var d struct {
		Issuer   string `json:"issuer"`
		Auth     string `json:"authorization_endpoint"`
		Token    string `json:"token_endpoint"`
		UserInfo string `json:"userinfo_endpoint"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&d) != nil {
		return fmt.Errorf("oidc discovery at %s failed (%s)", p.Issuer, resp.Status)
	}
	if strings.TrimRight(d.Issuer, "/") != p.Issuer || d.Auth == "" || d.Token == "" || d.UserInfo == "" {
		return errors.New("oidc discovery: issuer mismatch or missing endpoints")
	}
	p.AuthURL, p.TokenURL, p.UserURL, p.discovered = d.Auth, d.Token, d.UserInfo, true
	return nil
}

// ssoUser is who the provider says signed in.
type ssoUser struct {
	Subject       string
	Email         string
	EmailVerified bool
}

// exchange trades the code for an access token and asks who it belongs to.
func (p *SSOProvider) exchange(ctx context.Context, code, verifier, redirect string) (ssoUser, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect},
		"client_id": {p.ClientID}, "client_secret": {p.ClientSecret}, "code_verifier": {verifier}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := ssoHTTP.Do(req)
	if err != nil {
		return ssoUser{}, err
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tok) != nil || tok.AccessToken == "" {
		return ssoUser{}, fmt.Errorf("token exchange failed: %s %s", resp.Status, tok.Error)
	}
	get := func(u string, v any) error {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "GamePanel") // GitHub refuses requests without one
		resp, err := ssoHTTP.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s answered %s", u, resp.Status)
		}
		return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
	}
	var u ssoUser
	switch p.kind {
	case "github":
		var me struct {
			ID int64 `json:"id"`
		}
		if err := get(p.UserURL, &me); err != nil || me.ID == 0 {
			return u, fmt.Errorf("github user: %v", err)
		}
		u.Subject = strconv.FormatInt(me.ID, 10)
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if err := get(p.UserURL+"/emails", &emails); err == nil {
			for _, e := range emails {
				if e.Primary {
					u.Email, u.EmailVerified = e.Email, e.Verified
				}
			}
		}
	case "discord":
		var me struct {
			ID       string `json:"id"`
			Email    string `json:"email"`
			Verified bool   `json:"verified"`
		}
		if err := get(p.UserURL, &me); err != nil || me.ID == "" {
			return u, fmt.Errorf("discord user: %v", err)
		}
		u.Subject, u.Email, u.EmailVerified = me.ID, me.Email, me.Verified
	default:
		var me struct {
			Sub      string   `json:"sub"`
			Email    string   `json:"email"`
			Verified flexBool `json:"email_verified"`
		}
		if err := get(p.UserURL, &me); err != nil || me.Sub == "" {
			return u, fmt.Errorf("oidc userinfo: %v", err)
		}
		u.Subject, u.Email, u.EmailVerified = me.Sub, me.Email, bool(me.Verified)
	}
	u.Email = strings.ToLower(strings.TrimSpace(u.Email))
	return u, nil
}

// ssoName is a provider's display name ("GitHub"), or its id if it has gone.
func (a *App) ssoName(id string) string {
	if p := a.ssoProvider(id); p != nil && p.Name != "" {
		return p.Name
	}
	return id
}

func (a *App) ssoProvider(id string) *SSOProvider {
	for _, p := range a.SSO {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// ssoProvider is a single sign-on option shown on the sign-in page.
type ssoProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ssoList is what the sign-in page offers. SSO needs the panel's public
// address: the callback the provider sends people back to is built from it.
func (a *App) ssoList() []ssoProvider {
	out := []ssoProvider{}
	if a.PublicURL == "" {
		return out
	}
	for _, p := range a.SSO {
		out = append(out, ssoProvider{ID: p.ID, Name: p.Name})
	}
	return out
}

const (
	ssoStateTTL  = 10 * time.Minute
	ssoMFATTL    = 5 * time.Minute
	ssoCookie    = "gp_sso"
	ssoMFACookie = "gp_sso_mfa"
)

type ssoState struct {
	provider string
	verifier string
	mode     string // login | link
	userID   int64  // link: who is linking
	invite   string // login: an invitation to accept with a new account
	expires  time.Time
}

func randomToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (a *App) ssoRedirect(id string) string {
	return strings.TrimRight(a.PublicURL, "/") + "/api/auth/sso/" + id + "/callback"
}

// shortCookie is a short-lived helper cookie for the SSO round trip. It must
// be Lax, not Strict: the provider sends the browser back with a top-level
// cross-site navigation.
func shortCookie(r *http.Request, name, value, path string, ttl time.Duration) *http.Cookie {
	c := &http.Cookie{Name: name, Value: value, Path: path, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(ttl.Seconds())}
	if v, _ := r.Context().Value(secureKey{}).(bool); v {
		c.Secure = true
	}
	if value == "" {
		c.MaxAge = -1
	}
	return c
}

// ssoStart sends the browser to the provider.
func (a *App) ssoStart(w http.ResponseWriter, r *http.Request) {
	p := a.ssoProvider(r.PathValue("provider"))
	if p == nil || a.PublicURL == "" {
		writeProblem(w, http.StatusNotFound, "this sign-in method is not set up")
		return
	}
	st := ssoState{provider: p.ID, verifier: randomToken(), mode: "login", expires: time.Now().Add(ssoStateTTL)}
	if r.URL.Query().Get("mode") == "link" {
		u, err := requireSession(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		st.mode, st.userID = "link", u.ID
	} else if inv := r.URL.Query().Get("invite"); inv != "" && len(inv) <= 100 {
		st.invite = inv
	}
	if err := p.discover(r.Context()); err != nil {
		a.Log.Warn("sso: provider unavailable", "provider", p.ID, "err", err)
		http.Redirect(w, r, "/login?sso_error=unavailable", http.StatusFound)
		return
	}
	state := randomToken()
	a.ssoStates.Store(state, st)
	time.AfterFunc(ssoStateTTL, func() { a.ssoStates.Delete(state) })
	http.SetCookie(w, shortCookie(r, ssoCookie, state, "/api/auth/sso/", ssoStateTTL))
	sum := sha256.Sum256([]byte(st.verifier))
	q := url.Values{"response_type": {"code"}, "client_id": {p.ClientID}, "redirect_uri": {a.ssoRedirect(p.ID)},
		"scope": {strings.Join(p.Scopes, " ")}, "state": {state},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
	target := p.AuthURL
	if strings.Contains(target, "?") {
		target += "&" + q.Encode()
	} else {
		target += "?" + q.Encode()
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// ssoCallback finishes the round trip: link, sign in, or make an account.
func (a *App) ssoCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(page, code string) {
		http.SetCookie(w, shortCookie(r, ssoCookie, "", "/api/auth/sso/", 0))
		http.Redirect(w, r, page+"?sso_error="+code, http.StatusFound)
	}
	q := r.URL.Query()
	state := q.Get("state")
	c, err := r.Cookie(ssoCookie)
	v, ok := a.ssoStates.LoadAndDelete(state)
	if state == "" || err != nil || c.Value != state || !ok {
		// Started in another browser, already used, or forged.
		fail("/login", "expired")
		return
	}
	st := v.(ssoState)
	back := "/login"
	if st.mode == "link" {
		back = "/account"
	}
	p := a.ssoProvider(r.PathValue("provider"))
	if p == nil || p.ID != st.provider || time.Now().After(st.expires) {
		fail(back, "expired")
		return
	}
	if q.Get("error") != "" || q.Get("code") == "" {
		fail(back, "cancelled")
		return
	}
	ctx := r.Context()
	who, err := p.exchange(ctx, q.Get("code"), st.verifier, a.ssoRedirect(p.ID))
	if err != nil {
		a.Log.Warn("sso: exchange failed", "provider", p.ID, "err", err)
		fail(back, "failed")
		return
	}
	http.SetCookie(w, shortCookie(r, ssoCookie, "", "/api/auth/sso/", 0))

	if st.mode == "link" {
		cur, err := requireSession(ctx)
		if err != nil || cur.ID != st.userID {
			fail(back, "expired")
			return
		}
		if err := a.Store.LinkIdentity(ctx, cur.ID, p.ID, who.Subject, who.Email); err != nil {
			fail(back, "taken")
			return
		}
		a.audit(ctx, cur, "sso.link", cur.Email, p.ID+" "+who.Email)
		http.Redirect(w, r, "/account?sso_linked="+p.ID, http.StatusFound)
		return
	}

	u, err := a.Store.UserByIdentity(ctx, p.ID, who.Subject)
	if errors.Is(err, store.ErrNotFound) {
		// No account has this identity. Make one only for an invitation or
		// when sign-up is open, and only for a verified address that has no
		// account yet: an existing account links its identity itself.
		if st.invite == "" && a.signup() != SignupOpen {
			fail(back, "not_linked")
			return
		}
		if who.Email == "" || !who.EmailVerified {
			fail(back, "unverified")
			return
		}
		if _, err := a.Store.UserByEmail(ctx, who.Email); err == nil {
			fail(back, "email_taken")
			return
		}
		u, err = a.Store.CreateSSOUser(ctx, who.Email, p.ID, who.Subject, st.invite)
		if err != nil {
			a.Log.Info("sso: could not create an account", "provider", p.ID, "err", err)
			fail(back, "invite")
			return
		}
		a.audit(ctx, u, "account.register", u.Email, "single sign-on with "+p.ID)
	} else if err != nil {
		fail(back, "failed")
		return
	}
	if u.TOTPEnabled {
		// The provider proved who they are; the account still wants its own
		// second factor. Hold the sign-in until the code is given.
		tok := randomToken()
		a.ssoMFA.Store(tok, ssoPending{userID: u.ID, provider: p.ID, expires: time.Now().Add(ssoMFATTL)})
		time.AfterFunc(ssoMFATTL, func() { a.ssoMFA.Delete(tok) })
		http.SetCookie(w, shortCookie(r, ssoMFACookie, tok, "/api/auth/", ssoMFATTL))
		http.Redirect(w, r, "/login?sso=totp", http.StatusFound)
		return
	}
	a.ssoSignIn(w, r, u, p.ID)
	http.Redirect(w, r, "/", http.StatusFound)
}

type ssoPending struct {
	userID   int64
	provider string
	expires  time.Time
}

func (a *App) ssoSignIn(w http.ResponseWriter, r *http.Request, u store.User, provider string) {
	tok, err := a.newSession(r.Context(), u.ID, store.MethodSSO+":"+a.ssoName(provider))
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "could not create a session")
		return
	}
	for _, c := range sessionCookies(r.Context(), tok, a.Store.SessionMax) {
		http.SetCookie(w, &c)
	}
	a.audit(r.Context(), u, "login.ok", u.Email, "single sign-on with "+provider)
}

type ssoVerifyIn struct {
	Pending string `cookie:"gp_sso_mfa"`
	Body    struct {
		Code string `json:"code" minLength:"6" maxLength:"64"`
	}
}
type identitiesOut struct {
	Body struct {
		Identities  []store.Identity `json:"identities" nullable:"false"`
		Providers   []ssoProvider    `json:"providers" nullable:"false"`
		HasPassword bool             `json:"has_password"`
	}
}
type providerIn struct {
	Provider string `path:"provider" maxLength:"32"`
}
type firstPasswordIn struct {
	Body struct {
		Password string `json:"password" minLength:"1" maxLength:"256"`
	}
}
type ssoVerifyOut struct {
	SetCookie []http.Cookie `header:"Set-Cookie"`
	Body      store.User
}

func (a *App) registerSSO(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "ssoVerify", Method: "POST", Path: "/auth/sso/verify",
		Summary: "Finish a single sign-on with the account's two-factor code"},
		func(ctx context.Context, in *ssoVerifyIn) (*ssoVerifyOut, error) {
			v, ok := a.ssoMFA.Load(in.Pending)
			p, _ := v.(ssoPending)
			if in.Pending == "" || !ok || time.Now().After(p.expires) {
				return nil, huma.Error401Unauthorized("the sign-in took too long; start again")
			}
			u, err := a.Store.GetUser(ctx, p.userID)
			if err != nil {
				return nil, huma.Error401Unauthorized("the sign-in took too long; start again")
			}
			if err := a.confirmCode(ctx, u, in.Body.Code); err != nil {
				return nil, err
			}
			a.ssoMFA.Delete(in.Pending)
			tok, err := a.newSession(ctx, u.ID, store.MethodSSOTOTP+":"+a.ssoName(p.provider))
			if err != nil {
				return nil, huma.Error500InternalServerError("could not create session")
			}
			a.audit(ctx, u, "login.ok", u.Email, "single sign-on with "+p.provider+" and a second factor")
			clear := http.Cookie{Name: ssoMFACookie, Value: "", Path: "/api/auth/", MaxAge: -1, HttpOnly: true}
			return &ssoVerifyOut{Body: a.loginUser(u, store.MethodSSOTOTP), SetCookie: append(sessionCookies(ctx, tok, a.Store.SessionMax), clear)}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listIdentities", Method: "GET", Path: "/auth/identities", Summary: "Your single sign-on links"},
		func(ctx context.Context, _ *struct{}) (*identitiesOut, error) {
			u, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			ids, err := a.Store.ListIdentities(ctx, u.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			out := &identitiesOut{}
			out.Body.Identities, out.Body.Providers, out.Body.HasPassword = ids, a.ssoList(), a.Store.HasPassword(ctx, u.ID)
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "unlinkIdentity", Method: "DELETE", Path: "/auth/identities/{provider}", Summary: "Unlink a single sign-on provider", DefaultStatus: 204},
		func(ctx context.Context, in *providerIn) (*struct{}, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			if err := a.Store.UnlinkIdentity(ctx, u.ID, in.Provider); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return nil, huma.Error404NotFound("that provider is not linked")
				}
				return nil, userErr(http.StatusConflict, err)
			}
			a.audit(ctx, u, "sso.unlink", u.Email, in.Provider)
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "setFirstPassword", Method: "POST", Path: "/auth/password/first",
		Summary: "Give a single sign-on account a password", DefaultStatus: 204},
		func(ctx context.Context, in *firstPasswordIn) (*struct{}, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			if err := a.Store.SetFirstPassword(ctx, u.ID, in.Body.Password); err != nil {
				return nil, userErr(http.StatusUnprocessableEntity, err)
			}
			a.audit(ctx, u, "password.set", u.Email, "first password for a single sign-on account")
			return nil, nil
		})
}
