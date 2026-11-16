// Package api exposes the panel's HTTP API (OpenAPI 3.1 via huma), the
// browser console websocket, and the daemon connection endpoint.
//
// Files: auth.go (sign-in, account), admin.go (nodes, eggs), servers.go,
// backups.go, schedules.go, subusers.go, mfa.go, console.go (browser
// websocket), daemonconn.go (node connections, SFTP auth in sftp.go),
// security.go (client IP, CSRF, audit), limiter.go.
package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

type Deps struct {
	Store        *store.Store
	Hub          *hub.Hub
	Log          *slog.Logger
	CookieSecure bool
	// TrustedProxies are the reverse proxies whose X-Forwarded-For header is
	// believed. Empty means the socket address is always used.
	TrustedProxies []*net.IPNet
	Web            http.Handler // embedded SPA (optional)
	// SchedulerCtx, when set, starts the schedule runner; it stops when the
	// context ends. SchedulerTick overrides the 20 s check interval (tests).
	SchedulerCtx  context.Context
	SchedulerTick time.Duration
	// AuthRecheck is how often an open console websocket re-checks that its
	// session or API key is still valid and still grants console access
	// (default 10 s).
	AuthRecheck time.Duration
}

type App struct {
	Deps
	limiter *limiter // login attempts per client IP
	fails   *limiter // failed logins per account
	running sync.Map // schedule id -> struct{}: one execution at a time
}

type ctxKey struct{}

// credKey holds a func that resolves the request's credential again, for
// long-lived connections that must notice a logout or a revoked key.
type credKey struct{}

func userFrom(ctx context.Context) (store.User, bool) {
	u, ok := ctx.Value(ctxKey{}).(store.User)
	return u, ok
}

// recheckUser resolves the credential the request was made with again.
func recheckUser(ctx context.Context) (store.User, error) {
	f, ok := ctx.Value(credKey{}).(func(context.Context) (store.User, error))
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return f(ctx)
}

func requireUser(ctx context.Context) (store.User, error) {
	if u, ok := userFrom(ctx); ok {
		return u, nil
	}
	return store.User{}, huma.Error401Unauthorized("sign in required")
}

func requireAdmin(ctx context.Context) (store.User, error) {
	u, err := requireUser(ctx)
	if err != nil {
		return u, err
	}
	if !u.IsAdmin {
		return u, huma.Error403Forbidden("admin only")
	}
	return u, nil
}

// New builds the full HTTP handler.
func New(d Deps) http.Handler {
	a := &App{Deps: d, limiter: newLimiter(10, time.Minute), fails: newLimiter(20, 15*time.Minute)}
	errLog.Store(d.Log)
	if a.AuthRecheck <= 0 {
		a.AuthRecheck = 10 * time.Second
	}
	mux := http.NewServeMux()
	api := humago.NewWithPrefix(mux, "/api", apiConfig())
	d.Hub.OnHello = a.onHello
	d.Hub.Locate = func(ctx context.Context, uuid string) (int64, error) {
		s, err := a.Store.GetServer(ctx, uuid)
		return s.NodeID, err
	}
	if err := a.Store.FailStaleBackups(context.Background()); err != nil {
		a.Log.Warn("could not mark interrupted backups as failed", "err", err)
	}
	if d.SchedulerCtx != nil {
		tick := d.SchedulerTick
		if tick <= 0 {
			tick = 20 * time.Second
		}
		go a.RunScheduler(d.SchedulerCtx, tick)
	}
	a.register(api)

	mux.HandleFunc("GET /api/servers/{uuid}/ws", a.consoleWS)
	mux.HandleFunc("GET /daemon/connect", a.daemonConnect)
	mux.HandleFunc("POST /daemon/sftp-auth", a.sftpAuth)
	if d.Web != nil {
		mux.Handle("/", d.Web)
	}
	return a.middleware(mux)
}

func apiConfig() huma.Config {
	cfg := huma.DefaultConfig("GamePanel API", "0.1.0")
	// No "$schema" link in every response body: it points at the schema files,
	// which only signed-in users may read.
	cfg.CreateHooks = nil
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"session": {Type: "apiKey", In: "cookie", Name: secureCookieName, Description: "`__Host-gp_session` over HTTPS, `gp_session` over plain HTTP"},
		"apikey":  {Type: "http", Scheme: "bearer"},
	}
	return cfg
}

// register adds every API operation.
func (a *App) register(api huma.API) {
	a.registerAuth(api)
	a.registerAccount(api)
	a.registerMFA(api)
	a.registerAdmin(api)
	a.registerServers(api)
	a.registerBackups(api)
	a.registerSchedules(api)
	a.registerSubusers(api)
	a.registerStartup(api)
}

// OpenAPI returns the API description as JSON, the same document a running
// panel serves at /api/openapi.json (web/openapi.json is made from it).
func OpenAPI() ([]byte, error) {
	api := humago.NewWithPrefix(http.NewServeMux(), "/api", apiConfig())
	(&App{}).register(api)
	return json.Marshal(api.OpenAPI())
}

// middleware resolves the session cookie into the request context, applies
// security headers, and rate-limits login attempts.
func (a *App) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		secure := a.isSecure(r)
		if secure {
			// Browsers that have seen the panel over HTTPS never try plain HTTP
			// again, so the session cookie cannot be stripped in transit.
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/daemon/") {
			// API answers carry account data and one-time secrets (new API
			// keys, recovery codes, node tokens): never keep them in a cache.
			h.Set("Cache-Control", "no-store")
		}
		ip := a.clientIP(r)
		ctx := context.WithValue(r.Context(), ipKey{}, ip)
		ctx = context.WithValue(ctx, secureKey{}, secure)
		r = r.WithContext(ctx)
		if !sameOrigin(r) {
			writeProblem(w, http.StatusForbidden, "This request came from another site and was blocked. Open the panel directly and try again.")
			return
		}
		if r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost {
			if !a.limiter.allow(ip) {
				writeProblem(w, http.StatusTooManyRequests, "too many sign-in attempts, try again in a minute")
				return
			}
		}
		var resolve func(context.Context) (store.User, error)
		viaKey := false
		if tok, ok := readSessionCookie(r); ok {
			resolve = func(ctx context.Context) (store.User, error) { return a.Store.UserBySession(ctx, tok) }
			r = r.WithContext(context.WithValue(r.Context(), sessionKey{}, tok))
		} else if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && strings.HasPrefix(tok, store.KeyPrefix) {
			// API keys: no cookie, so no CSRF exposure; flagged so sensitive
			// account actions can insist on a browser session.
			resolve = func(ctx context.Context) (store.User, error) { return a.Store.UserByAPIKey(ctx, tok) }
			viaKey = true
		}
		if resolve != nil {
			if u, err := resolve(r.Context()); err == nil {
				ctx := context.WithValue(r.Context(), ctxKey{}, u)
				ctx = context.WithValue(ctx, credKey{}, resolve)
				if viaKey {
					ctx = context.WithValue(ctx, viaKeyCtx{}, true)
				}
				r = r.WithContext(ctx)
			}
		}
		if isAPIDocs(r.URL.Path) && r.Context().Value(ctxKey{}) == nil {
			// The API description lists every endpoint and field: for signed-in
			// users (and API keys) only. Anyone else sees nothing there.
			writeProblem(w, http.StatusNotFound, "not found")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isAPIDocs reports whether path is part of the generated API description.
func isAPIDocs(path string) bool {
	return path == "/api/docs" || strings.HasPrefix(path, "/api/openapi") || strings.HasPrefix(path, "/api/schemas/")
}

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b) // never fails (crypto/rand panics instead)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
