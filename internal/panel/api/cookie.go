package api

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"
)

// Session cookie names. Over HTTPS the cookie is "__Host-gp_session": browsers
// only accept that name from a secure origin, for the exact host, with Path=/
// and no Domain, so neither a sibling subdomain nor a plain-HTTP page can plant
// or overwrite it (ASVS v5.0.0-3.3.1, v5.0.0-3.3.3). Plain-HTTP setups (local
// development) use the bare name.
//
// Over HTTPS the bare name is only honoured for sessions that existed before
// the __Host- name was introduced, once each: the session is then re-issued
// under the __Host- name and the bare cookie is removed (see
// store.ClaimLegacySession). Sessions are capped at 12 hours, so the bare name
// stops working over HTTPS within 12 hours of the upgrade.
const (
	cookieName       = "gp_session"
	secureCookieName = "__Host-" + cookieName
)

type secureKey struct{}
type sessionKey struct{}

// isSecure reports whether the browser reached us over HTTPS: directly, via a
// trusted proxy that says so in X-Forwarded-Proto, or because the operator
// declared it (GAMEPANEL_COOKIE_SECURE=1).
func (a *App) isSecure(r *http.Request) bool {
	if a.CookieSecure || r.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if peer := net.ParseIP(host); peer != nil && a.trusted(peer) {
		// The proxy nearest to us appends last; trust only its view.
		protos := strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")
		return strings.EqualFold(strings.TrimSpace(protos[len(protos)-1]), "https")
	}
	return false
}

func secureFrom(ctx context.Context) bool {
	v, _ := ctx.Value(secureKey{}).(bool)
	return v
}

// sessionToken is the session cookie the request carried ("" if none).
func sessionToken(ctx context.Context) string {
	s, _ := ctx.Value(sessionKey{}).(string)
	return s
}

// readSessionCookie prefers the __Host- cookie, which cannot be planted by
// another origin, over the bare one. legacy reports that only the bare one
// was sent.
func readSessionCookie(r *http.Request) (tok string, legacy, ok bool) {
	if c, err := r.Cookie(secureCookieName); err == nil && c.Value != "" {
		return c.Value, false, true
	}
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		return c.Value, true, true
	}
	return "", false, false
}

// clearLegacyCookie removes the bare-named cookie from the browser.
func clearLegacyCookie() http.Cookie {
	return http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1}
}

// sessionCookies is sessionCookie plus, over HTTPS, the removal of any
// bare-named cookie left from before the upgrade.
func sessionCookies(ctx context.Context, tok string, maxAge time.Duration) []http.Cookie {
	cs := []http.Cookie{sessionCookie(ctx, tok, maxAge)}
	if secureFrom(ctx) {
		cs = append(cs, clearLegacyCookie())
	}
	return cs
}

// sessionCookie is the Set-Cookie for a new session (or, with tok "", the one
// that removes it).
func sessionCookie(ctx context.Context, tok string, maxAge time.Duration) http.Cookie {
	c := http.Cookie{
		Name: cookieName, Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int(maxAge.Seconds()),
	}
	if secureFrom(ctx) {
		c.Name, c.Secure = secureCookieName, true
	}
	if tok == "" {
		c.MaxAge = -1
	}
	return c
}
