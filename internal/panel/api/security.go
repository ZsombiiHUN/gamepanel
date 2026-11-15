package api

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

type ipKey struct{}

func ipFrom(ctx context.Context) string {
	s, _ := ctx.Value(ipKey{}).(string)
	return s
}

func mustUser(ctx context.Context) store.User {
	u, _ := userFrom(ctx)
	return u
}

// audit records a security event with the caller's IP. It never fails the request.
func (a *App) audit(ctx context.Context, actor store.User, action, target, detail string) {
	a.Store.Audit(ctx, store.AuditEntry{
		ActorID: actor.ID, ActorEmail: actor.Email,
		Action: action, Target: target, IP: ipFrom(ctx), Detail: detail,
	})
}

// ParseTrustedProxies parses a comma-separated list of IPs and CIDRs.
func ParseTrustedProxies(list string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			ip, err := netip.ParseAddr(part)
			if err != nil {
				return nil, err
			}
			part = netip.PrefixFrom(ip, ip.BitLen()).String()
		}
		_, n, err := net.ParseCIDR(part)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func (a *App) trusted(ip net.IP) bool {
	for _, n := range a.TrustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP returns the real client address. X-Forwarded-For is honoured only
// when the direct peer is a configured trusted proxy, and is read right to
// left so entries a client forged at the front are ignored.
func (a *App) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || !a.trusted(peer) {
		return host
	}
	parts := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(parts[i]))
		if ip == nil {
			break
		}
		if !a.trusted(ip) {
			return ip.String()
		}
	}
	return host
}

// sameOrigin is a second CSRF layer on top of SameSite=Lax: a state-changing
// request that a browser marks cross-site, or whose Origin host differs from
// ours, is refused. Non-browser clients send no Origin and are unaffected.
func sameOrigin(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

type passwordIn struct {
	Session string `cookie:"gp_session"`
	Body    struct {
		Current string `json:"current_password" minLength:"1" maxLength:"256"`
		New     string `json:"new_password" minLength:"12" maxLength:"256"`
	}
}
type revokeIn struct {
	Session string `cookie:"gp_session"`
}
type auditIn struct {
	Limit  int    `query:"limit" default:"100" minimum:"1" maximum:"500"`
	Offset int    `query:"offset" default:"0" minimum:"0"`
	Q      string `query:"q" maxLength:"200" doc:"Only entries containing every word, in the action, user, target, detail or IP."`
}
type auditOut struct{ Body []store.AuditEntry }

func (a *App) registerAccount(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "changePassword", Method: "POST", Path: "/auth/password",
		Summary: "Change your password (signs out every other session)", DefaultStatus: 204},
		func(ctx context.Context, in *passwordIn) (*struct{}, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			// Same brute-force guard as login: the current password is a guess target too.
			if a.fails.over(u.Email) {
				return nil, huma.Error429TooManyRequests("too many failed attempts, try again later")
			}
			if err := a.Store.ChangePassword(ctx, u.ID, in.Body.Current, in.Body.New, in.Session); err != nil {
				if err.Error() == "invalid current password" {
					a.fails.add(u.Email)
					a.audit(ctx, u, "password.change.fail", u.Email, "")
					return nil, huma.Error403Forbidden("current password is incorrect")
				}
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			a.audit(ctx, u, "password.change", u.Email, "other sessions revoked")
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "revokeOtherSessions", Method: "POST", Path: "/auth/sessions/revoke-others",
		Summary: "Sign out everywhere except this browser", DefaultStatus: 204},
		func(ctx context.Context, in *revokeIn) (*struct{}, error) {
			u, err := requireSession(ctx)
			if err != nil {
				return nil, err
			}
			if err := a.Store.DeleteOtherSessions(ctx, u.ID, in.Session); err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			a.audit(ctx, u, "sessions.revoke_others", u.Email, "")
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listAudit", Method: "GET", Path: "/audit", Summary: "Audit log (admin)"},
		func(ctx context.Context, in *auditIn) (*auditOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			es, err := a.Store.ListAudit(ctx, in.Q, in.Limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			return &auditOut{Body: es}, nil
		})
}
