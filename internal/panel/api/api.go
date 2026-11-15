// Package api exposes the panel's HTTP API (OpenAPI 3.1 via huma), the
// browser console websocket, and the daemon connection endpoint.
package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

const cookieName = "gp_session"

type Deps struct {
	Store        *store.Store
	Hub          *hub.Hub
	Log          *slog.Logger
	CookieSecure bool
	// TrustedProxies are the reverse proxies whose X-Forwarded-For header is
	// believed. Empty means the socket address is always used.
	TrustedProxies []*net.IPNet
	Web            http.Handler // embedded SPA (optional)
}

type App struct {
	Deps
	limiter *limiter // login attempts per client IP
	fails   *limiter // failed logins per account
}

type ctxKey struct{}

func userFrom(ctx context.Context) (store.User, bool) {
	u, ok := ctx.Value(ctxKey{}).(store.User)
	return u, ok
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
	mux := http.NewServeMux()
	cfg := huma.DefaultConfig("GamePanel API", "0.1.0")
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"session": {Type: "apiKey", In: "cookie", Name: cookieName},
	}
	api := humago.NewWithPrefix(mux, "/api", cfg)
	d.Hub.OnHello = a.onHello
	a.registerAuth(api)
	a.registerAccount(api)
	a.registerAdmin(api)
	a.registerServers(api)

	mux.HandleFunc("GET /api/servers/{uuid}/ws", a.consoleWS)
	mux.HandleFunc("GET /daemon/connect", a.daemonConnect)
	if d.Web != nil {
		mux.Handle("/", d.Web)
	}
	return a.middleware(mux)
}

// middleware resolves the session cookie into the request context, applies
// security headers, and rate-limits login attempts.
func (a *App) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		ip := a.clientIP(r)
		r = r.WithContext(context.WithValue(r.Context(), ipKey{}, ip))
		if !sameOrigin(r) {
			http.Error(w, `{"title":"Forbidden","status":403,"detail":"cross-origin request blocked"}`, http.StatusForbidden)
			return
		}
		if r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost {
			if !a.limiter.allow(ip) {
				http.Error(w, `{"title":"Too Many Requests","status":429,"detail":"too many login attempts, try again later"}`, http.StatusTooManyRequests)
				return
			}
		}
		if c, err := r.Cookie(cookieName); err == nil {
			if u, err := a.Store.UserBySession(r.Context(), c.Value); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, u))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---- tiny fixed-window rate limiter (per key) ----

type limiter struct {
	mu   sync.Mutex
	max  int
	win  time.Duration
	hits map[string][]time.Time
}

func newLimiter(max int, win time.Duration) *limiter {
	return &limiter{max: max, win: win, hits: map[string][]time.Time{}}
}

// over reports whether key already has max recorded hits in the window,
// without recording a new one.
func (l *limiter) over(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(key)) >= l.max
}

// add records one hit for key.
func (l *limiter) add(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[key] = append(l.recent(key), time.Now())
}

func (l *limiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}

// recent returns key's hits inside the window (caller holds the lock) and
// sweeps stale keys so the map cannot grow without bound.
func (l *limiter) recent(key string) []time.Time {
	cut := time.Now().Add(-l.win)
	if len(l.hits) > 4096 {
		for k, v := range l.hits {
			if len(v) == 0 || !v[len(v)-1].After(cut) {
				delete(l.hits, k)
			}
		}
	}
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	l.hits[key] = kept
	return kept
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := time.Now().Add(-l.win)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, time.Now())
	return true
}

// ---- auth ----

type loginIn struct {
	Body struct {
		Email    string `json:"email" format:"email"`
		Password string `json:"password" minLength:"1"`
	}
}
type userOut struct{ Body store.User }
type loginOut struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      store.User
}
type logoutIn struct {
	Session string `cookie:"gp_session"`
}
type logoutOut struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}

func (a *App) registerAuth(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "login", Method: "POST", Path: "/auth/login", Summary: "Sign in"},
		func(ctx context.Context, in *loginIn) (*loginOut, error) {
			email := strings.ToLower(strings.TrimSpace(in.Body.Email))
			if a.fails.over(email) {
				a.audit(ctx, store.User{Email: email}, "login.blocked", email, "too many failures for this account")
				return nil, huma.Error429TooManyRequests("too many failed attempts for this account, try again later")
			}
			u, err := a.Store.Login(ctx, email, in.Body.Password)
			if err != nil {
				a.fails.add(email)
				a.audit(ctx, store.User{Email: email}, "login.fail", email, "")
				return nil, huma.Error401Unauthorized("invalid email or password")
			}
			a.fails.reset(email)
			a.audit(ctx, u, "login.ok", email, "")
			tok, err := a.Store.NewSession(ctx, u.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("could not create session")
			}
			return &loginOut{Body: u, SetCookie: http.Cookie{
				Name: cookieName, Value: tok, Path: "/", HttpOnly: true, Secure: a.CookieSecure,
				SameSite: http.SameSiteLaxMode, MaxAge: int(store.SessionTTL.Seconds()),
			}}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "logout", Method: "POST", Path: "/auth/logout", Summary: "Sign out"},
		func(ctx context.Context, in *logoutIn) (*logoutOut, error) {
			if in.Session != "" {
				a.Store.DeleteSession(ctx, in.Session) // revoke server-side, not just in the browser
			}
			if u, ok := userFrom(ctx); ok {
				a.audit(ctx, u, "logout", u.Email, "")
			}
			return &logoutOut{SetCookie: http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.CookieSecure, SameSite: http.SameSiteLaxMode}}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "me", Method: "GET", Path: "/auth/me", Summary: "Current user"},
		func(ctx context.Context, _ *struct{}) (*userOut, error) {
			u, err := requireUser(ctx)
			if err != nil {
				return nil, err
			}
			return &userOut{Body: u}, nil
		})
}

// ---- admin: nodes & eggs ----

type nodeCreateIn struct {
	Body struct {
		Name      string `json:"name" minLength:"1" maxLength:"64"`
		PortStart int    `json:"port_start,omitempty" minimum:"1024" maximum:"65535" default:"25565"`
		PortEnd   int    `json:"port_end,omitempty" minimum:"1024" maximum:"65535" default:"25665"`
	}
}
type nodeCreateOut struct {
	Body struct {
		Node  store.Node `json:"node"`
		Token string     `json:"token" doc:"Shown once. Set as GAMEPANEL_NODE_TOKEN on the daemon."`
	}
}
type nodeView struct {
	store.Node
	Online bool `json:"online"`
}
type nodeListOut struct{ Body []nodeView }
type idIn struct {
	ID int64 `path:"id"`
}

type eggImportIn struct {
	RawBody []byte
}
type eggOut struct{ Body store.Egg }
type eggListOut struct{ Body []store.Egg }

func (a *App) registerAdmin(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "createNode", Method: "POST", Path: "/nodes", Summary: "Register a node", DefaultStatus: 201},
		func(ctx context.Context, in *nodeCreateIn) (*nodeCreateOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			if in.Body.PortEnd < in.Body.PortStart {
				return nil, huma.Error422UnprocessableEntity("port_end must be >= port_start")
			}
			n, tok, err := a.Store.CreateNode(ctx, in.Body.Name, in.Body.PortStart, in.Body.PortEnd)
			if err != nil {
				return nil, huma.Error409Conflict("could not create node (name taken?)")
			}
			a.audit(ctx, mustUser(ctx), "node.create", n.Name, "")
			out := &nodeCreateOut{}
			out.Body.Node, out.Body.Token = n, tok
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listNodes", Method: "GET", Path: "/nodes", Summary: "List nodes"},
		func(ctx context.Context, _ *struct{}) (*nodeListOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			ns, err := a.Store.ListNodes(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			out := &nodeListOut{Body: []nodeView{}}
			for _, n := range ns {
				out.Body = append(out.Body, nodeView{Node: n, Online: a.Hub.Online(n.ID)})
			}
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteNode", Method: "DELETE", Path: "/nodes/{id}", Summary: "Delete a node", DefaultStatus: 204},
		func(ctx context.Context, in *idIn) (*struct{}, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			if err := a.Store.DeleteNode(ctx, in.ID); err != nil {
				return nil, huma.Error409Conflict("node still has servers")
			}
			a.audit(ctx, mustUser(ctx), "node.delete", fmt.Sprint(in.ID), "")
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "importEgg", Method: "POST", Path: "/eggs/import", Summary: "Import a Pterodactyl (PTDL) egg", DefaultStatus: 201,
		RequestBody:  &huma.RequestBody{Content: map[string]*huma.MediaType{"application/json": {Schema: &huma.Schema{Type: "object"}}}},
		MaxBodyBytes: 1 << 20},
		func(ctx context.Context, in *eggImportIn) (*eggOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			e, err := importPTDL(in.RawBody)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			e, err = a.Store.CreateEgg(ctx, e)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			a.audit(ctx, mustUser(ctx), "egg.import", e.Name, "")
			return &eggOut{Body: e}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listEggs", Method: "GET", Path: "/eggs", Summary: "List eggs"},
		func(ctx context.Context, _ *struct{}) (*eggListOut, error) {
			if _, err := requireUser(ctx); err != nil {
				return nil, err
			}
			es, err := a.Store.ListEggs(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			return &eggListOut{Body: es}, nil
		})
}

// ---- daemon connection ----

func (a *App) daemonConnect(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	node, err := a.Store.NodeByToken(r.Context(), tok)
	if tok == "" || err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Daemons are not browsers: no Origin header is sent, so skip origin checks.
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	a.Hub.Serve(r.Context(), ws, node.ID)
}

// onHello makes the daemon match the database: servers it lacks are created.
func (a *App) onHello(ctx context.Context, nodeID int64, h proto.Hello) {
	have := map[string]bool{}
	for _, id := range h.Servers {
		have[id] = true
	}
	servers, err := a.Store.ServersOnNode(ctx, nodeID)
	if err != nil {
		return
	}
	for _, s := range servers {
		if have[s.UUID] {
			continue
		}
		if err := a.pushServer(ctx, s); err != nil {
			a.Log.Warn("resync create failed", "server", s.UUID, "err", err)
		}
	}
}

func (a *App) specFor(ctx context.Context, s store.Server) (proto.ServerSpec, error) {
	egg, err := a.Store.GetEgg(ctx, s.EggID)
	if err != nil {
		return proto.ServerSpec{}, err
	}
	return proto.ServerSpec{
		UUID: s.UUID, Image: egg.DockerImage, Startup: shellStartup(egg.Startup), Stop: egg.Stop,
		Env: buildEnv(egg, s.Env, s.MemoryMB, s.Port), MemoryMB: s.MemoryMB, CPUPct: s.CPUPct,
		Ports: []proto.PortMap{
			{Host: s.Port, Container: s.Port, Proto: "tcp"},
			{Host: s.Port, Container: s.Port, Proto: "udp"},
		},
	}, nil
}

func (a *App) pushServer(ctx context.Context, s store.Server) error {
	spec, err := a.specFor(ctx, s)
	if err != nil {
		return err
	}
	_, err = a.Hub.Request(ctx, s.NodeID, proto.ReqServerCreate, spec)
	return err
}

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

var errForbidden = errors.New("forbidden")

func (a *App) authorize(ctx context.Context, uuid string) (store.User, store.Server, error) {
	u, err := requireUser(ctx)
	if err != nil {
		return u, store.Server{}, err
	}
	s, err := a.Store.GetServer(ctx, uuid)
	// Same response for "missing" and "not yours" so UUIDs can't be probed.
	if err != nil || (!u.IsAdmin && s.OwnerID != u.ID) {
		return u, s, huma.Error404NotFound("server not found")
	}
	return u, s, nil
}

// ---- browser console websocket ----

func (a *App) consoleWS(w http.ResponseWriter, r *http.Request) {
	u, ok := userFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	srv, err := a.Store.GetServer(r.Context(), r.PathValue("uuid"))
	if err != nil || (!u.IsAdmin && srv.OwnerID != u.ID) {
		http.NotFound(w, r)
		return
	}
	// Accept enforces same-origin by default, blocking cross-site websocket hijacking.
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ctx := ws.CloseRead(r.Context())

	sub, backlog := a.Hub.Subscribe(srv.UUID)
	defer a.Hub.Unsubscribe(sub)
	write := func(ev hub.Event) error {
		b, _ := json.Marshal(ev)
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return ws.Write(wctx, websocket.MessageText, b)
	}
	write(hub.Event{Type: "state", Data: proto.Marshal(proto.State{UUID: srv.UUID, State: a.Hub.State(srv.UUID)})})
	for _, l := range backlog {
		write(hub.Event{Type: "console", Data: proto.Marshal(proto.Console{UUID: srv.UUID, Line: l})})
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-sub.C:
			if write(ev) != nil {
				return
			}
		}
	}
}
