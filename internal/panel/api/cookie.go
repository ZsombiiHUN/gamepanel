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
// or overwrite it (OWASP ASVS §3.4.4). Plain-HTTP setups (local development)
// keep the bare name. Both are read, so switching to HTTPS needs no sign-out.
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
// another origin, over the bare one.
func readSessionCookie(r *http.Request) (string, bool) {
	for _, name := range []string{secureCookieName, cookieName} {
		if c, err := r.Cookie(name); err == nil && c.Value != "" {
			return c.Value, true
		}
	}
	return "", false
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
