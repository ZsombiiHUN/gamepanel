package api

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

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
