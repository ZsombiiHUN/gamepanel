package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// Webhooks: the panel tells a shop (or anything else) what happened by
// POSTing signed JSON to the addresses admins register. Each event is stored
// before it is sent and retried with growing gaps until the receiver answers
// 2xx, so a shop that is down for a while still hears about everything.

// Events a webhook can take.
const (
	EvServerCreated       = "server.created"
	EvServerInstalled     = "server.installed"
	EvServerInstallFailed = "server.install_failed"
	EvServerSuspended     = "server.suspended"
	EvServerUnsuspended   = "server.unsuspended"
	EvServerDeleted       = "server.deleted"
	EvUserCreated         = "user.created"
	EvUserDeleted         = "user.deleted"
	EvPing                = "ping" // sent only by the test button
)

var webhookEvents = []string{EvServerCreated, EvServerInstalled, EvServerInstallFailed, EvServerSuspended,
	EvServerUnsuspended, EvServerDeleted, EvUserCreated, EvUserDeleted}

const (
	webhookTimeout     = 10 * time.Second
	webhookMaxAttempts = 12 // about a day of retries
	webhookKeep        = 30 * 24 * time.Hour
)

// webhookDelay is how long to wait before try n+1 after n failed tries:
// 30 s, 1 min, 2 min, ... at most 6 h.
func webhookDelay(n int) time.Duration {
	d := 30 * time.Second << min(n-1, 20)
	return min(d, 6*time.Hour)
}

// webhookEvent is the body of every delivery.
type webhookEvent struct {
	// ID is the same on every retry, so a receiver can drop duplicates.
	ID        string `json:"id"`
	Event     string `json:"event"`
	CreatedAt int64  `json:"created_at"`
	Data      any    `json:"data"`
}

// serverEventData is what server events carry.
type serverEventData struct {
	UUID          string `json:"uuid"`
	Name          string `json:"name"`
	ExternalID    string `json:"external_id"`
	OwnerID       int64  `json:"owner_id"`
	NodeID        int64  `json:"node_id"`
	Suspended     bool   `json:"suspended"`
	InstallStatus string `json:"install_status"`
}

func serverData(s store.Server) serverEventData {
	return serverEventData{UUID: s.UUID, Name: s.Name, ExternalID: s.ExternalID, OwnerID: s.OwnerID, NodeID: s.NodeID, Suspended: s.Suspended, InstallStatus: s.InstallStatus}
}

type userEventData struct {
	ID         int64  `json:"id"`
	Email      string `json:"email"`
	ExternalID string `json:"external_id"`
}

// emit queues an event for every webhook that takes it. Failing to queue is
// logged, never returned: what happened has happened either way.
func (a *App) emit(ctx context.Context, event string, data any) {
	a.emitTo(ctx, event, data, 0)
}

func (a *App) emitTo(ctx context.Context, event string, data any, hookID int64) int {
	body, _ := json.Marshal(webhookEvent{ID: newUUID(), Event: event, CreatedAt: time.Now().Unix(), Data: data})
	n, err := a.Store.QueueEvent(context.WithoutCancel(ctx), event, string(body), hookID)
	if err != nil {
		a.Log.Error("could not queue a webhook event", "event", event, "err", err)
	}
	if n > 0 {
		select {
		case a.hookWake <- struct{}{}:
		default:
		}
	}
	return n
}

// runWebhooks delivers queued events until ctx ends.
func (a *App) runWebhooks(ctx context.Context, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	var pruned time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-a.hookWake:
		}
		if time.Since(pruned) > time.Hour {
			if err := a.Store.PruneDeliveries(ctx, webhookKeep); err != nil {
				a.Log.Warn("could not prune webhook deliveries", "err", err)
			}
			pruned = time.Now()
		}
		for ctx.Err() == nil {
			due, err := a.Store.DueDeliveries(ctx, 50)
			if err != nil {
				a.Log.Warn("could not read webhook deliveries", "err", err)
				break
			}
			if len(due) == 0 {
				break
			}
			for _, d := range due {
				a.deliver(ctx, d)
			}
		}
	}
}

// deliver makes one try at one delivery and records how it went.
func (a *App) deliver(ctx context.Context, d store.Delivery) {
	h, err := a.Store.GetWebhook(ctx, d.WebhookID)
	if err != nil {
		return // deleted: its deliveries went with it
	}
	code, msg := a.post(ctx, h, d)
	if code >= 200 && code < 300 {
		if err := a.Store.DeliveryDone(ctx, d.ID, code); err != nil {
			a.Log.Warn("could not record a webhook delivery", "err", err)
		}
		return
	}
	var next time.Time
	if d.Attempts+1 < webhookMaxAttempts {
		next = time.Now().Add(webhookDelay(d.Attempts + 1))
	} else {
		a.Log.Warn("gave up on a webhook delivery", "webhook", h.ID, "event", d.Event, "err", msg)
	}
	if err := a.Store.DeliveryFailed(ctx, d.ID, code, msg, next); err != nil {
		a.Log.Warn("could not record a webhook delivery", "err", err)
	}
}

// post sends a delivery. It returns the HTTP status (0 = no answer) and, on
// failure, why.
func (a *App) post(ctx context.Context, h store.Webhook, d store.Delivery) (int, string) {
	ctx, cancel := context.WithTimeout(ctx, webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, strings.NewReader(d.Payload))
	if err != nil {
		return 0, err.Error()
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GamePanel-Webhook/1")
	req.Header.Set("X-GamePanel-Event", d.Event)
	req.Header.Set("X-GamePanel-Delivery", strconv.FormatInt(d.ID, 10))
	req.Header.Set("X-GamePanel-Signature", "t="+ts+",v1="+signWebhook(h.Secret, ts, []byte(d.Payload)))
	resp, err := a.hookClient().Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the URL is on the webhook already
		}
		return 0, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return resp.StatusCode, ""
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	msg := resp.Status
	if s := strings.TrimSpace(string(b)); s != "" {
		msg += ": " + s
	}
	return resp.StatusCode, msg
}

// signWebhook is the v1 signature: hex HMAC-SHA256, keyed with the webhook's
// secret, of "<timestamp>.<body>". The timestamp is signed too, so a
// receiver that refuses old ones cannot be fed a recorded delivery again.
func signWebhook(secret, ts string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts))
	m.Write([]byte("."))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func (a *App) hookClient() *http.Client {
	if a.WebhookClient != nil {
		return a.WebhookClient
	}
	return defaultHookClient
}

// Redirects are not followed: a delivery goes where the admin pointed it.
var defaultHookClient = &http.Client{
	Timeout:       webhookTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// ---- install progress, from node states ----

// onState hears every state a node reports. The install status is kept in
// the database in the order states arrive, by one goroutine.
func (a *App) onState(uuid, state string) {
	if state != "installing" && state != "install_failed" && state != "offline" && state != "starting" && state != "running" {
		return
	}
	a.states <- [2]string{uuid, state}
}

func (a *App) runStates(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case st := <-a.states:
			to, err := a.Store.NoteInstallState(ctx, st[0], st[1])
			if err != nil {
				a.Log.Warn("could not record the install status", "server", st[0], "err", err)
				continue
			}
			ev := map[string]string{store.InstallInstalled: EvServerInstalled, store.InstallFailed: EvServerInstallFailed}[to]
			if ev == "" {
				continue
			}
			if s, err := a.Store.GetServer(ctx, st[0]); err == nil {
				a.emit(ctx, ev, serverData(s))
			}
		}
	}
}

// ---- admin API ----

type webhookBody struct {
	URL         string   `json:"url" minLength:"1" maxLength:"2048" doc:"http(s) address the panel POSTs events to"`
	Description string   `json:"description,omitempty" maxLength:"200"`
	Events      []string `json:"events" minItems:"1" maxItems:"20" doc:"server.created, server.installed, server.install_failed, server.suspended, server.unsuspended, server.deleted, user.created, user.deleted, or * for all"`
	Enabled     *bool    `json:"enabled,omitempty" doc:"default true"`
}
type webhookCreateIn struct{ Body webhookBody }
type webhookUpdateIn struct {
	ID   int64 `path:"id"`
	Body struct {
		webhookBody
		RotateSecret bool `json:"rotate_secret,omitempty" doc:"make a new signing secret (the old one stops working at once)"`
	}
}
type webhookWithSecret struct {
	store.Webhook
	// Secret signs deliveries. It is shown when the webhook is made and when
	// it is rotated, and never again.
	Secret string `json:"secret,omitempty"`
}
type webhookOut struct{ Body webhookWithSecret }
type webhookListOut struct{ Body []store.Webhook }
type deliveryListOut struct{ Body []store.Delivery }
type deliveryIn struct {
	ID         int64 `path:"id"`
	DeliveryID int64 `path:"delivery"`
}
type pingOut struct {
	Body struct {
		Queued bool `json:"queued"`
	}
}

// checkWebhook validates an address and events and normalises them.
func checkWebhook(b *webhookBody) error {
	u, err := url.Parse(strings.TrimSpace(b.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return huma.Error422UnprocessableEntity("the url must be an http:// or https:// address, without a user name or password in it")
	}
	b.URL = u.String()
	seen := map[string]bool{}
	var evs []string
	for _, e := range b.Events {
		e = strings.TrimSpace(e)
		if e != "*" && !slices.Contains(webhookEvents, e) {
			return huma.Error422UnprocessableEntity("unknown event " + truncate(e, 64) + "; use one of " + strings.Join(webhookEvents, ", ") + " or *")
		}
		if !seen[e] {
			seen[e] = true
			evs = append(evs, e)
		}
	}
	b.Events = evs
	return nil
}

func (a *App) registerWebhooks(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "createWebhook", Method: "POST", Path: "/webhooks", Summary: "Send events to an address (admin)", DefaultStatus: 201,
		Description: "Each event is POSTed as JSON with an `X-GamePanel-Signature: t=<unix time>,v1=<hex>` header, where v1 is HMAC-SHA256 of `<t>.<body>` keyed with the secret returned here (shown once). " +
			"Answer 2xx; anything else, or no answer within 10 s, is retried with growing gaps (30 s up to 6 h) for about a day."},
		func(ctx context.Context, in *webhookCreateIn) (*webhookOut, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			b := in.Body
			if err := checkWebhook(&b); err != nil {
				return nil, err
			}
			h, err := a.Store.CreateWebhook(ctx, store.Webhook{URL: b.URL, Description: strings.TrimSpace(b.Description), Events: b.Events, Enabled: b.Enabled == nil || *b.Enabled})
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			a.audit(ctx, u, "webhook.create", strconv.FormatInt(h.ID, 10), h.URL)
			return &webhookOut{Body: webhookWithSecret{Webhook: h, Secret: h.Secret}}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listWebhooks", Method: "GET", Path: "/webhooks", Summary: "List webhooks (admin)"},
		func(ctx context.Context, _ *struct{}) (*webhookListOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			l, err := a.Store.ListWebhooks(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			return &webhookListOut{Body: l}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "updateWebhook", Method: "PUT", Path: "/webhooks/{id}", Summary: "Change a webhook (admin)"},
		func(ctx context.Context, in *webhookUpdateIn) (*webhookOut, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			h, err := a.Store.GetWebhook(ctx, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound("webhook not found")
			}
			b := in.Body.webhookBody
			if err := checkWebhook(&b); err != nil {
				return nil, err
			}
			h.URL, h.Description, h.Events = b.URL, strings.TrimSpace(b.Description), b.Events
			if b.Enabled != nil {
				h.Enabled = *b.Enabled
			}
			h, err = a.Store.UpdateWebhook(ctx, h, in.Body.RotateSecret)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			detail := h.URL
			out := webhookWithSecret{Webhook: h}
			if in.Body.RotateSecret {
				out.Secret = h.Secret
				detail += " (new secret)"
			}
			a.audit(ctx, u, "webhook.update", strconv.FormatInt(h.ID, 10), detail)
			return &webhookOut{Body: out}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteWebhook", Method: "DELETE", Path: "/webhooks/{id}", Summary: "Delete a webhook and its delivery log (admin)", DefaultStatus: 204},
		func(ctx context.Context, in *idIn) (*struct{}, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			if err := a.Store.DeleteWebhook(ctx, in.ID); err != nil {
				return nil, huma.Error404NotFound("webhook not found")
			}
			a.audit(ctx, u, "webhook.delete", strconv.FormatInt(in.ID, 10), "")
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "pingWebhook", Method: "POST", Path: "/webhooks/{id}/ping", Summary: "Send a test event (admin)", DefaultStatus: 202,
		Description: "Queues a `ping` event to this webhook, also when it is turned off. Its result shows in the delivery log."},
		func(ctx context.Context, in *idIn) (*pingOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			if _, err := a.Store.GetWebhook(ctx, in.ID); err != nil {
				return nil, huma.Error404NotFound("webhook not found")
			}
			out := &pingOut{}
			out.Body.Queued = a.emitTo(ctx, EvPing, map[string]string{"message": "Webhook test from GamePanel"}, in.ID) > 0
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listWebhookDeliveries", Method: "GET", Path: "/webhooks/{id}/deliveries", Summary: "A webhook's latest 100 deliveries, newest first (admin)"},
		func(ctx context.Context, in *idIn) (*deliveryListOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			if _, err := a.Store.GetWebhook(ctx, in.ID); err != nil {
				return nil, huma.Error404NotFound("webhook not found")
			}
			l, err := a.Store.ListDeliveries(ctx, in.ID, 100)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			return &deliveryListOut{Body: l}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "retryWebhookDelivery", Method: "POST", Path: "/webhooks/{id}/deliveries/{delivery}/retry",
		Summary: "Send a delivered or given-up delivery again now (admin)", DefaultStatus: 202},
		func(ctx context.Context, in *deliveryIn) (*struct{}, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			if err := a.Store.RetryDelivery(ctx, in.ID, in.DeliveryID); err != nil {
				return nil, huma.Error404NotFound("no finished delivery with that id on this webhook")
			}
			select {
			case a.hookWake <- struct{}{}:
			default:
			}
			return nil, nil
		})
}
