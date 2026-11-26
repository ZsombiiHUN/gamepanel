package e2e

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
)

// The shop flow end to end: a customer pays, the shop makes their account
// and their server (and may retry both), the server starts once installed,
// the shop hears about it through a signed webhook, and suspends and
// unsuspends it as invoices go unpaid and get paid.

type shopServer struct {
	UUID          string
	Name          string
	NodeID        int64  `json:"node_id"`
	OwnerID       int64  `json:"owner_id"`
	ExternalID    string `json:"external_id"`
	Suspended     bool
	InstallStatus string `json:"install_status"`
	State         string
	Permissions   []string
}

// hookSink receives webhook deliveries and checks their signatures.
type hookSink struct {
	t      *testing.T
	secret string
	mu     sync.Mutex
	got    []map[string]any
	fail   int // answer 500 this many times first
}

func (h *hookSink) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.fail > 0 {
		h.fail--
		http.Error(w, "down for maintenance", http.StatusInternalServerError)
		return
	}
	sig := r.Header.Get("X-GamePanel-Signature")
	ts, v1, _ := strings.Cut(strings.TrimPrefix(sig, "t="), ",v1=")
	m := hmac.New(sha256.New, []byte(h.secret))
	m.Write([]byte(ts + "." + string(body)))
	if !hmac.Equal([]byte(hex.EncodeToString(m.Sum(nil))), []byte(v1)) {
		h.t.Errorf("bad signature %q on %s", sig, body)
	}
	var ev map[string]any
	json.Unmarshal(body, &ev)
	if ev["event"] != r.Header.Get("X-GamePanel-Event") {
		h.t.Errorf("event header %q, body %v", r.Header.Get("X-GamePanel-Event"), ev["event"])
	}
	h.got = append(h.got, ev)
}

// event returns the data of the first delivered event of that name.
func (h *hookSink) event(name, uuid string) map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.got {
		d, _ := e["data"].(map[string]any)
		if e["event"] == name && (uuid == "" || d["uuid"] == uuid) {
			return d
		}
	}
	return nil
}

func (h *hookSink) count(name string) (n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.got {
		if e["event"] == name {
			n++
		}
	}
	return n
}

func TestShopSellsAServer(t *testing.T) {
	t.Parallel()
	s := newStackWith(t, func(d *api.Deps) { d.WebhookTick = 50 * time.Millisecond; d.WebhookAllowPrivate = true }, nil)
	sink := &hookSink{t: t}
	rx := httptest.NewServer(sink)
	t.Cleanup(rx.Close)
	var hook struct {
		ID     int64
		Secret string
	}
	// Deliveries can start before the answer carrying the secret is read
	// (the stack's own server finishing its install, say): the sink waits
	// on its lock until the secret is set.
	// (A failed request ends the test inside; the deferred unlock still runs.)
	func() {
		sink.mu.Lock()
		defer sink.mu.Unlock()
		json.Unmarshal(s.admin.do("POST", "/api/webhooks", map[string]any{"url": rx.URL + "/gamepanel", "events": []string{"*"}}, 201), &hook)
		sink.secret = hook.Secret
	}()
	if !strings.HasPrefix(hook.Secret, "whsec_") {
		t.Fatalf("secret %q", hook.Secret)
	}

	// 1. The customer's account, with the password they chose at checkout.
	// A retry with the same external id gets the same account.
	acct := map[string]any{"email": "Customer@Example.com", "password": "a-long-customer-password", "external_id": "cust-42"}
	var c1, c2 struct {
		ID         int64
		Email      string
		ExternalID string `json:"external_id"`
	}
	json.Unmarshal(s.admin.do("POST", "/api/users", acct, 201), &c1)
	json.Unmarshal(s.admin.do("POST", "/api/users", acct, 200), &c2)
	if c1.ID == 0 || c1.ID != c2.ID || c1.Email != "customer@example.com" || c1.ExternalID != "cust-42" {
		t.Fatalf("account %+v, retry %+v", c1, c2)
	}
	var found []struct{ ID int64 }
	json.Unmarshal(s.admin.do("GET", "/api/users?external_id=cust-42", nil, 200), &found)
	if len(found) != 1 || found[0].ID != c1.ID {
		t.Fatalf("lookup by external id: %+v", found)
	}

	// 2. The server: no node named, so the panel picks one; started once
	// installed. A retried create returns the first server, unchanged.
	order := map[string]any{
		"name": "Survival", "egg_id": 1, "memory_mb": 512, "owner_id": c1.ID,
		"external_id": "order-1001", "start_on_completion": true,
	}
	var srv, again shopServer
	json.Unmarshal(s.admin.do("POST", "/api/servers", order, 201), &srv)
	json.Unmarshal(s.admin.do("POST", "/api/servers", order, 200), &again)
	if srv.NodeID != s.node || srv.ExternalID != "order-1001" || srv.InstallStatus != "installing" || again.UUID != srv.UUID {
		t.Fatalf("server %+v, retry %+v", srv, again)
	}
	var n int
	s.st.DB.QueryRow(`SELECT COUNT(*) FROM servers WHERE external_id='order-1001'`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d servers for one order", n)
	}
	get := func(c *client) (v shopServer) {
		json.Unmarshal(c.do("GET", "/api/servers/"+srv.UUID, nil, 200), &v)
		return v
	}
	waitFor(t, "installed and running", func() bool {
		v := get(s.admin)
		return v.InstallStatus == "installed" && v.State == "running"
	})
	if st, _ := s.fake.State(s.ctx, srv.UUID); st != runtime.StateRunning {
		t.Fatalf("container %s", st)
	}
	waitFor(t, "server.installed webhook", func() bool { return sink.event("server.installed", srv.UUID) != nil })
	if d := sink.event("server.created", srv.UUID); d == nil || d["external_id"] != "order-1001" {
		t.Fatalf("server.created: %v", d)
	}
	if d := sink.event("user.created", ""); d == nil || d["external_id"] != "cust-42" {
		t.Fatalf("user.created: %v", d)
	}
	var byExt []shopServer
	json.Unmarshal(s.admin.do("GET", "/api/servers?external_id=order-1001", nil, 200), &byExt)
	if len(byExt) != 1 || byExt[0].UUID != srv.UUID {
		t.Fatalf("lookup by external id: %+v", byExt)
	}

	// The customer signs in and has their server; they never see the shop's id.
	jar, _ := cookiejar.New(nil)
	cust := &client{t, s.admin.base, &http.Client{Jar: jar}}
	cust.do("POST", "/api/auth/login", map[string]string{"email": "customer@example.com", "password": "a-long-customer-password"}, 200)
	if v := get(cust); v.ExternalID != "" || len(v.Permissions) == 0 {
		t.Fatalf("customer view %+v", v)
	}
	cust.do("GET", "/api/servers?external_id=order-1001", nil, 403)

	// 3. The invoice is unpaid: suspended. The server stops, the customer
	// still sees it but can do nothing, and even an admin cannot start it.
	var sus shopServer
	json.Unmarshal(s.admin.do("POST", "/api/servers/"+srv.UUID+"/suspend", nil, 200), &sus)
	if !sus.Suspended {
		t.Fatal("not suspended")
	}
	s.admin.do("POST", "/api/servers/"+srv.UUID+"/suspend", nil, 200) // again: no change, no event
	waitFor(t, "stopped", func() bool { st, _ := s.fake.State(s.ctx, srv.UUID); return st == runtime.StateOffline })
	if v := get(cust); !v.Suspended || len(v.Permissions) != 0 {
		t.Fatalf("customer view while suspended %+v", v)
	}
	for _, r := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/power", map[string]string{"action": "start"}},
		{"GET", "/files?path=/", nil},
		{"GET", "/backups", nil},
		{"GET", "/startup", nil},
		{"POST", "/command", map[string]string{"command": "say hi"}},
	} {
		if got := string(cust.do(r.method, "/api/servers/"+srv.UUID+r.path, r.body, 403)); !strings.Contains(got, "suspended") {
			t.Errorf("%s %s: %s", r.method, r.path, got)
		}
	}
	s.admin.do("POST", "/api/servers/"+srv.UUID+"/power", map[string]string{"action": "start"}, 409)
	// The Pterodactyl-compatible client API says so and refuses the same.
	s.admin.do("POST", "/api/client/servers/"+srv.UUID+"/power", map[string]string{"signal": "start"}, 409)
	if got := string(s.admin.do("GET", "/api/client/servers/"+srv.UUID+"/resources", nil, 200)); !strings.Contains(got, `"is_suspended":true`) {
		t.Fatalf("client API resources while suspended: %s", got)
	}
	waitFor(t, "server.suspended webhook", func() bool { return sink.event("server.suspended", srv.UUID) != nil })
	if n := sink.count("server.suspended"); n != 1 {
		t.Fatalf("%d suspended events for one suspension", n)
	}

	// 4. Paid: unsuspended. The customer can start it again.
	s.admin.do("POST", "/api/servers/"+srv.UUID+"/unsuspend", nil, 200)
	cust.do("POST", "/api/servers/"+srv.UUID+"/power", map[string]string{"action": "start"}, 202)
	waitFor(t, "running again", func() bool { return get(cust).State == "running" })
	waitFor(t, "server.unsuspended webhook", func() bool { return sink.event("server.unsuspended", srv.UUID) != nil })

	// 5. Cancelled: the server goes, then the account.
	s.admin.do("DELETE", "/api/users/"+itoa(c1.ID), nil, 409) // still owns the server
	s.admin.do("DELETE", "/api/servers/"+srv.UUID, nil, 204)
	s.admin.do("DELETE", "/api/users/"+itoa(c1.ID), nil, 204)
	waitFor(t, "deleted webhooks", func() bool {
		return sink.event("server.deleted", srv.UUID) != nil && sink.event("user.deleted", "") != nil
	})

	// Every delivery is in the log as delivered. The sink has the last events
	// before the panel hears back from it and records them, so wait for that.
	var log []struct{ Event, Status string }
	waitFor(t, "deliveries recorded", func() bool {
		log = nil
		json.Unmarshal(s.admin.do("GET", "/api/webhooks/"+itoa(hook.ID)+"/deliveries", nil, 200), &log)
		for _, d := range log {
			if d.Status == "pending" {
				return false
			}
		}
		return true
	})
	if len(log) < 7 {
		t.Fatalf("delivery log %+v", log)
	}
	for _, d := range log {
		if d.Status != "delivered" {
			t.Errorf("%s: %s", d.Event, d.Status)
		}
	}
}

// An account made without a password gets a link to choose one, which signs
// in like a reset. Admin accounts cannot be taken over through this API.
func TestShopAccounts(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	var a struct {
		ID          int64
		HasPassword bool   `json:"has_password"`
		SetupLink   string `json:"setup_link"`
	}
	json.Unmarshal(s.admin.do("POST", "/api/users", map[string]any{"email": "new@example.com"}, 201), &a)
	if a.HasPassword || !strings.Contains(a.SetupLink, "/reset#token=") {
		t.Fatalf("account %+v", a)
	}
	_, tok, _ := strings.Cut(a.SetupLink, "#token=")
	s.admin.do("POST", "/api/auth/login", map[string]string{"email": "new@example.com", "password": "x"}, 401)
	jar, _ := cookiejar.New(nil)
	c := &client{t, s.admin.base, &http.Client{Jar: jar}}
	c.do("POST", "/api/auth/reset", map[string]string{"token": tok, "password": "my-own-long-password"}, 204)
	c.do("POST", "/api/auth/login", map[string]string{"email": "new@example.com", "password": "my-own-long-password"}, 200)

	// Same email: refused, and told which account has it.
	if got := string(s.admin.do("POST", "/api/users", map[string]any{"email": "NEW@example.com"}, 409)); !strings.Contains(got, "account "+itoa(a.ID)) {
		t.Fatalf("duplicate email: %s", got)
	}
	// An external id that belongs to someone else's email is not reused.
	s.admin.do("PATCH", "/api/users/"+itoa(a.ID), map[string]any{"external_id": "c-1"}, 200)
	s.admin.do("POST", "/api/users", map[string]any{"email": "other@example.com", "external_id": "c-1"}, 409)

	// A new password signs the account out everywhere.
	s.admin.do("PATCH", "/api/users/"+itoa(a.ID), map[string]any{"password": "set-by-the-shop-123"}, 200)
	c.do("GET", "/api/auth/me", nil, 401)

	// Admins are not changed or deleted through here.
	var me struct{ ID int64 }
	json.Unmarshal(s.admin.do("GET", "/api/auth/me", nil, 200), &me)
	s.admin.do("PATCH", "/api/users/"+itoa(me.ID), map[string]any{"email": "x@example.com"}, 403)
	s.admin.do("PATCH", "/api/users/"+itoa(me.ID), map[string]any{"password": "another-long-password-1"}, 403)
	s.admin.do("DELETE", "/api/users/"+itoa(me.ID), nil, 409)

	// Customers cannot use any of it.
	c.do("POST", "/api/auth/login", map[string]string{"email": "new@example.com", "password": "set-by-the-shop-123"}, 200)
	c.do("GET", "/api/users", nil, 403)
	c.do("POST", "/api/users", map[string]any{"email": "z@example.com"}, 403)
	c.do("POST", "/api/servers/"+s.server+"/suspend", nil, 403)
	c.do("GET", "/api/webhooks", nil, 403)
}

// With no node named, the panel picks an online node with room, and says so
// when there is none.
func TestShopPicksANode(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	var off struct{ Node struct{ ID int64 } }
	json.Unmarshal(s.admin.do("POST", "/api/nodes", map[string]any{"name": "offline", "port_start": 30000, "port_end": 30010}, 201), &off)
	body := func(extra map[string]any) map[string]any {
		b := map[string]any{"name": "x", "egg_id": 1, "memory_mb": 512}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	var v shopServer
	json.Unmarshal(s.admin.do("POST", "/api/servers", body(nil), 201), &v)
	if v.NodeID != s.node {
		t.Fatalf("placed on node %d, want the online one %d", v.NodeID, s.node)
	}
	s.admin.do("POST", "/api/servers", body(map[string]any{"node_ids": []int64{off.Node.ID}}), 503)
	// The online node is full.
	s.st.DB.Exec(`UPDATE nodes SET memory_mb=1024 WHERE id=?`, s.node)
	if got := string(s.admin.do("POST", "/api/servers", body(nil), 422)); !strings.Contains(got, "no node has room") {
		t.Fatal(got)
	}
}

// A receiver that fails is tried again later; the log says why it failed.
func TestWebhookFailuresAreRetried(t *testing.T) {
	t.Parallel()
	s := newStackWith(t, func(d *api.Deps) { d.WebhookTick = 50 * time.Millisecond; d.WebhookAllowPrivate = true }, nil)
	sink := &hookSink{t: t, fail: 1}
	rx := httptest.NewServer(sink)
	t.Cleanup(rx.Close)
	var hook struct {
		ID     int64
		Secret string
	}
	func() { // see TestShopSellsAServer
		sink.mu.Lock()
		defer sink.mu.Unlock()
		json.Unmarshal(s.admin.do("POST", "/api/webhooks", map[string]any{"url": rx.URL, "events": []string{"server.suspended"}}, 201), &hook)
		sink.secret = hook.Secret
	}()
	s.admin.do("POST", "/api/webhooks", map[string]any{"url": "ftp://x", "events": []string{"*"}}, 422)
	s.admin.do("POST", "/api/webhooks", map[string]any{"url": rx.URL, "events": []string{"server.exploded"}}, 422)

	s.admin.do("POST", "/api/servers/"+s.server+"/suspend", nil, 200)
	type delivery struct {
		ID        int64
		Status    string
		LastError string `json:"last_error"`
		LastCode  int    `json:"last_code"`
		Attempts  int
	}
	var log []delivery
	waitFor(t, "first try", func() bool {
		json.Unmarshal(s.admin.do("GET", "/api/webhooks/"+itoa(hook.ID)+"/deliveries", nil, 200), &log)
		return len(log) == 1 && log[0].Attempts == 1
	})
	if d := log[0]; d.Status != "pending" || d.LastCode != 500 || !strings.Contains(d.LastError, "down for maintenance") {
		t.Fatalf("after a failure: %+v", d)
	}
	// Not sent again before its time (30 s), however often the sender looks.
	time.Sleep(300 * time.Millisecond)
	if sink.count("server.suspended") != 0 {
		t.Fatal("retried at once")
	}
	// Only events the webhook asked for, and the test ping, are sent.
	s.admin.do("POST", "/api/servers/"+s.server+"/unsuspend", nil, 200)
	s.admin.do("POST", "/api/webhooks/"+itoa(hook.ID)+"/ping", nil, 202)
	waitFor(t, "ping", func() bool { return sink.count("ping") == 1 })
	if sink.count("server.unsuspended") != 0 {
		t.Fatal("got an event it did not ask for")
	}
	// A pending delivery cannot be "retried"; a finished one can.
	s.admin.do("POST", "/api/webhooks/"+itoa(hook.ID)+"/deliveries/"+itoa(log[0].ID)+"/retry", nil, 404)
}

// Without GAMEPANEL_WEBHOOK_ALLOW_PRIVATE, webhooks cannot point at private
// or local addresses: plain ones are refused, and names that resolve inward
// fail at delivery, with the reason recorded.
func TestWebhooksStayOffPrivateAddresses(t *testing.T) {
	t.Parallel()
	s := newStackWith(t, func(d *api.Deps) { d.WebhookTick = 50 * time.Millisecond }, nil)
	for _, u := range []string{"http://127.0.0.1:9/x", "http://localhost/x", "https://10.0.0.5/", "http://169.254.169.254/latest/meta-data/", "http://[::1]/", "http://[::ffff:127.0.0.1]/", "http://app.localhost/"} {
		s.admin.do("POST", "/api/webhooks", map[string]any{"url": u, "events": []string{"*"}}, 422)
	}
	// A name that resolves to loopback passes the first check and is
	// stopped when the delivery connects.
	var hook struct{ ID int64 }
	json.Unmarshal(s.admin.do("POST", "/api/webhooks", map[string]any{"url": "http://localtest.me:1/", "events": []string{"*"}}, 201), &hook)
	s.admin.do("POST", fmt.Sprintf("/api/webhooks/%d/ping", hook.ID), nil, 202)
	deadline := time.Now().Add(5 * time.Second)
	for {
		var ds []struct {
			Error string `json:"last_error"`
		}
		json.Unmarshal(s.admin.do("GET", fmt.Sprintf("/api/webhooks/%d/deliveries", hook.ID), nil, 200), &ds)
		if len(ds) > 0 && strings.Contains(ds[0].Error, "non-public") {
			return
		}
		if len(ds) > 0 && ds[0].Error != "" && !strings.Contains(ds[0].Error, "non-public") {
			// localtest.me may not resolve here (no DNS): nothing to check.
			if strings.Contains(ds[0].Error, "no such host") || strings.Contains(ds[0].Error, "server misbehaving") {
				t.Skip("no DNS for localtest.me")
			}
			t.Fatalf("delivery failed for another reason: %s", ds[0].Error)
		}
		if time.Now().After(deadline) {
			t.Fatalf("no blocked delivery recorded: %+v", ds)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
