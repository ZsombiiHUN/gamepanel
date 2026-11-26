package store

import (
	"context"
	"strings"
	"time"
)

// What a shop (billing system) uses: looking servers and accounts up by its
// own ids, suspension, install progress, and managing customer accounts.

// ServerByExternalID finds the server a shop created with that external id.
func (s *Store) ServerByExternalID(ctx context.Context, id string) (Server, error) {
	return scanServer(s.RO.QueryRowContext(ctx, `SELECT `+srvCols+` FROM servers WHERE external_id=?`, id))
}

// SetSuspended suspends or unsuspends a server. changed is false when it
// already was in that state.
func (s *Store) SetSuspended(ctx context.Context, uuid string, suspended bool) (changed bool, err error) {
	res, err := s.exec(ctx, `UPDATE servers SET suspended=? WHERE uuid=? AND suspended<>?`, suspended, uuid, suspended)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SetServerExternalID sets or clears ("") a server's external id.
func (s *Store) SetServerExternalID(ctx context.Context, uuid, id string) error {
	res, err := s.exec(ctx, `UPDATE servers SET external_id=? WHERE uuid=?`, nullable(id), uuid)
	if err != nil {
		if strings.Contains(err.Error(), "servers.external_id") {
			return ErrExternalIDTaken
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// NoteInstallState records what a state the node reported means for the
// server's install: "installing" starts one, "install_failed" ends it badly,
// and any other state ends one that was running. It returns the new status
// when it changed, so each change is announced once, also when the panel
// hears about it only after a restart.
func (s *Store) NoteInstallState(ctx context.Context, uuid, state string) (string, error) {
	var to, cond string
	switch state {
	case "installing":
		to, cond = InstallInstalling, `install_status<>'installing'`
	case "install_failed":
		to, cond = InstallFailed, `install_status<>'failed'`
	case "unknown", "":
		return "", nil // says nothing about the install
	default:
		to, cond = InstallInstalled, `install_status='installing'`
	}
	res, err := s.exec(ctx, `UPDATE servers SET install_status=? WHERE uuid=? AND `+cond, to, uuid)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", nil
	}
	return to, nil
}

// ---- accounts ----

// Account is a user as admins (and shops) manage it.
type Account struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	IsAdmin     bool   `json:"is_admin"`
	ExternalID  string `json:"external_id"`
	HasPassword bool   `json:"has_password" doc:"false until the person sets one (or signs in with single sign-on only)"`
	TOTPEnabled bool   `json:"totp_enabled"`
	Servers     int    `json:"servers" doc:"servers this account owns"`
	CreatedAt   int64  `json:"created_at"`
}

const accountCols = `id,email,is_admin,COALESCE(external_id,''),has_password,totp_enabled,
	(SELECT COUNT(*) FROM servers WHERE owner_id=users.id),created_at`

func scanAccount(sc rowScanner) (Account, error) {
	var a Account
	err := sc.Scan(&a.ID, &a.Email, &a.IsAdmin, &a.ExternalID, &a.HasPassword, &a.TOTPEnabled, &a.Servers, &a.CreatedAt)
	return a, nf(err)
}

// ErrEmailTaken means another account has that email.
var ErrEmailTaken = userErr("an account with that email already exists")

// GetAccount loads an account by id.
func (s *Store) GetAccount(ctx context.Context, id int64) (Account, error) {
	return scanAccount(s.RO.QueryRowContext(ctx, `SELECT `+accountCols+` FROM users WHERE id=?`, id))
}

// AccountFilter narrows ListAccounts; empty fields match everything.
type AccountFilter struct {
	Email      string
	ExternalID string
	Limit      int
	Offset     int
}

// where returns the filter's SQL condition and arguments.
func (f AccountFilter) where() (string, []any) {
	q, args := `1=1`, []any{}
	if f.Email != "" {
		q += ` AND email=?`
		args = append(args, strings.ToLower(strings.TrimSpace(f.Email)))
	}
	if f.ExternalID != "" {
		q += ` AND external_id=?`
		args = append(args, f.ExternalID)
	}
	return q, args
}

// ListAccounts lists accounts, oldest first.
func (s *Store) ListAccounts(ctx context.Context, f AccountFilter) ([]Account, error) {
	w, args := f.where()
	if f.Limit <= 0 {
		f.Limit = 100
	}
	args = append(args, f.Limit, f.Offset)
	return queryAll(ctx, s.RO, scanAccount, `SELECT `+accountCols+` FROM users WHERE `+w+` ORDER BY id LIMIT ? OFFSET ?`, args...)
}

// CountAccounts counts the accounts ListAccounts would page through.
func (s *Store) CountAccounts(ctx context.Context, f AccountFilter) (int, error) {
	w, args := f.where()
	return count(ctx, s.RO, `SELECT COUNT(*) FROM users WHERE `+w, args...)
}

// NewAccount is what CreateAccount needs. An empty Password makes an account
// that cannot sign in with a password until its owner sets one (through the
// link CreateAccount's caller hands out).
type NewAccount struct {
	Email      string
	Password   string
	Admin      bool
	ExternalID string
}

// CreateAccount makes an account for someone else (an admin or a shop).
func (s *Store) CreateAccount(ctx context.Context, n NewAccount) (Account, error) {
	email := strings.ToLower(strings.TrimSpace(n.Email))
	hash, has := hashPassword(randomSecret()), false
	if n.Password != "" {
		if err := ValidatePassword(n.Password, email); err != nil {
			return Account{}, err
		}
		hash, has = hashPassword(n.Password), true
	}
	res, err := s.exec(ctx, `INSERT INTO users(email,password_hash,is_admin,external_id,has_password,created_at) VALUES(?,?,?,?,?,?)`,
		email, hash, n.Admin, nullable(n.ExternalID), has, now())
	if err != nil {
		return Account{}, accountUniqueErr(err)
	}
	id, _ := res.LastInsertId()
	return s.GetAccount(ctx, id)
}

func accountUniqueErr(err error) error {
	switch {
	case err == nil:
		return nil
	case strings.Contains(err.Error(), "users.external_id"):
		return ErrExternalIDTaken
	case strings.Contains(err.Error(), "users.email"):
		return ErrEmailTaken
	}
	return err
}

func randomSecret() string { t, _ := NewToken(); return t }

// AccountChange lists what UpdateAccount changes; nil fields stay as they are.
type AccountChange struct {
	Email      *string
	ExternalID *string
	Password   *string
}

// UpdateAccount changes an account's email, external id or password. A new
// password signs the account out everywhere.
func (s *Store) UpdateAccount(ctx context.Context, id int64, c AccountChange) (Account, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	var email string
	if err := tx.QueryRowContext(ctx, `SELECT email FROM users WHERE id=?`, id).Scan(&email); err != nil {
		return Account{}, nf(err)
	}
	if c.Email != nil {
		email = strings.ToLower(strings.TrimSpace(*c.Email))
		if _, err := tx.ExecContext(ctx, `UPDATE users SET email=? WHERE id=?`, email, id); err != nil {
			return Account{}, accountUniqueErr(err)
		}
	}
	if c.ExternalID != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET external_id=? WHERE id=?`, nullable(*c.ExternalID), id); err != nil {
			return Account{}, accountUniqueErr(err)
		}
	}
	if c.Password != nil {
		if err := ValidatePassword(*c.Password, email); err != nil {
			return Account{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=?, has_password=1 WHERE id=?`, hashPassword(*c.Password), id); err != nil {
			return Account{}, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, id); err != nil {
			return Account{}, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_tokens WHERE kind='reset' AND user_id=?`, id); err != nil {
			return Account{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Account{}, err
	}
	return s.GetAccount(ctx, id)
}

// ErrOwnsServers means an account that owns servers cannot be deleted.
var ErrOwnsServers = userErr("this account still owns servers; delete them or hand them to another account first")

// DeleteAccount removes an account that owns no servers. Its sessions, keys,
// subuser access and sign-in methods go with it. Schedules it saved on other
// people's servers are turned off: they ran with its permissions, and nobody
// is left to vouch for them.
func (s *Store) DeleteAccount(ctx context.Context, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owns int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM servers WHERE owner_id=?`, id).Scan(&owns); err != nil {
		return err
	}
	if owns > 0 {
		return ErrOwnsServers
	}
	if _, err := tx.ExecContext(ctx, `UPDATE schedules SET enabled=0, created_by=NULL WHERE created_by=?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// SetupTTL is how long a "choose your password" link for a new account lasts.
const SetupTTL = InviteTTL

// NewSetupToken makes a link token with which the owner of a new account
// chooses a password. It is a reset token that lasts as long as an
// invitation, so it works with the usual reset page.
func (s *Store) NewSetupToken(ctx context.Context, userID int64) (string, error) {
	return s.newResetToken(ctx, userID, SetupTTL)
}

// ---- webhooks ----

// Webhook is an address the panel sends events to.
type Webhook struct {
	ID          int64    `json:"id"`
	URL         string   `json:"url"`
	Description string   `json:"description"`
	Events      []string `json:"events" nullable:"false" doc:"event names, or [\"*\"] for all"`
	Enabled     bool     `json:"enabled"`
	CreatedAt   int64    `json:"created_at"`
	Secret      string   `json:"-"`
}

const hookCols = `id,url,description,secret,events,enabled,created_at`

func scanHook(sc rowScanner) (Webhook, error) {
	var h Webhook
	var ev string
	err := sc.Scan(&h.ID, &h.URL, &h.Description, &h.Secret, &ev, &h.Enabled, &h.CreatedAt)
	h.Events = strings.Split(ev, ",")
	return h, nf(err)
}

// Wants reports whether the webhook takes an event.
func (h Webhook) Wants(event string) bool {
	for _, e := range h.Events {
		if e == "*" || e == event {
			return true
		}
	}
	return false
}

// CreateWebhook stores a webhook with a new signing secret.
func (s *Store) CreateWebhook(ctx context.Context, h Webhook) (Webhook, error) {
	h.Secret = "whsec_" + randomSecret()
	h.CreatedAt = now()
	res, err := s.exec(ctx, `INSERT INTO webhooks(url,description,secret,events,enabled,created_at) VALUES(?,?,?,?,?,?)`,
		h.URL, h.Description, h.Secret, strings.Join(h.Events, ","), h.Enabled, h.CreatedAt)
	if err != nil {
		return h, err
	}
	h.ID, _ = res.LastInsertId()
	return h, nil
}

func (s *Store) GetWebhook(ctx context.Context, id int64) (Webhook, error) {
	return scanHook(s.RO.QueryRowContext(ctx, `SELECT `+hookCols+` FROM webhooks WHERE id=?`, id))
}

func (s *Store) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	return queryAll(ctx, s.RO, scanHook, `SELECT `+hookCols+` FROM webhooks ORDER BY id`)
}

// UpdateWebhook saves a webhook's address, description, events and switch;
// with rotate it also gets a new secret.
func (s *Store) UpdateWebhook(ctx context.Context, h Webhook, rotate bool) (Webhook, error) {
	if rotate {
		h.Secret = "whsec_" + randomSecret()
	}
	res, err := s.exec(ctx, `UPDATE webhooks SET url=?,description=?,events=?,enabled=?,secret=? WHERE id=?`,
		h.URL, h.Description, strings.Join(h.Events, ","), h.Enabled, h.Secret, h.ID)
	if err != nil {
		return h, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return h, ErrNotFound
	}
	return h, nil
}

func (s *Store) DeleteWebhook(ctx context.Context, id int64) error {
	res, err := s.exec(ctx, `DELETE FROM webhooks WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delivery is one event on its way to one webhook.
type Delivery struct {
	ID          int64  `json:"id"`
	WebhookID   int64  `json:"webhook_id"`
	Event       string `json:"event"`
	Payload     string `json:"payload"`
	Status      string `json:"status" enum:"pending,delivered,failed"`
	Attempts    int    `json:"attempts"`
	NextAt      int64  `json:"next_at" doc:"when a pending delivery is tried next"`
	LastCode    int    `json:"last_code" doc:"HTTP status of the last try, 0 = no answer"`
	LastError   string `json:"last_error"`
	CreatedAt   int64  `json:"created_at"`
	DeliveredAt int64  `json:"delivered_at"`
}

const deliveryCols = `id,webhook_id,event,payload,status,attempts,next_at,last_code,last_error,created_at,delivered_at`

func scanDelivery(sc rowScanner) (Delivery, error) {
	var d Delivery
	err := sc.Scan(&d.ID, &d.WebhookID, &d.Event, &d.Payload, &d.Status, &d.Attempts, &d.NextAt, &d.LastCode, &d.LastError, &d.CreatedAt, &d.DeliveredAt)
	return d, nf(err)
}

// QueueEvent adds a delivery of payload to every enabled webhook that takes
// the event, and returns how many it queued. hookID limits it to one webhook
// (0 = all).
func (s *Store) QueueEvent(ctx context.Context, event, payload string, hookID int64) (int, error) {
	hooks, err := s.ListWebhooks(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, h := range hooks {
		if (hookID != 0 && h.ID != hookID) || (hookID == 0 && (!h.Enabled || !h.Wants(event))) {
			continue
		}
		if _, err := s.exec(ctx, `INSERT INTO webhook_deliveries(webhook_id,event,payload,next_at,created_at) VALUES(?,?,?,?,?)`,
			h.ID, event, payload, now(), now()); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// DueDeliveries returns pending deliveries whose time has come, oldest first.
func (s *Store) DueDeliveries(ctx context.Context, limit int) ([]Delivery, error) {
	return queryAll(ctx, s.RO, scanDelivery, `SELECT `+deliveryCols+` FROM webhook_deliveries WHERE status='pending' AND next_at<=? ORDER BY id LIMIT ?`, now(), limit)
}

// DeliveryDone records a successful delivery.
func (s *Store) DeliveryDone(ctx context.Context, id int64, code int) error {
	_, err := s.exec(ctx, `UPDATE webhook_deliveries SET status='delivered', attempts=attempts+1, last_code=?, last_error='', delivered_at=? WHERE id=?`, code, now(), id)
	return err
}

// DeliveryFailed records a failed try: it is tried again at next, or given
// up on when next is zero.
func (s *Store) DeliveryFailed(ctx context.Context, id int64, code int, msg string, next time.Time) error {
	status, at := "pending", next.Unix()
	if next.IsZero() {
		status, at = "failed", 0
	}
	_, err := s.exec(ctx, `UPDATE webhook_deliveries SET status=?, attempts=attempts+1, last_code=?, last_error=?, next_at=? WHERE id=?`,
		status, code, clip(msg, 500), at, id)
	return err
}

// ListDeliveries returns a webhook's latest deliveries, newest first.
func (s *Store) ListDeliveries(ctx context.Context, hookID int64, limit int) ([]Delivery, error) {
	return queryAll(ctx, s.RO, scanDelivery, `SELECT `+deliveryCols+` FROM webhook_deliveries WHERE webhook_id=? ORDER BY id DESC LIMIT ?`, hookID, limit)
}

// RetryDelivery queues a finished delivery again, now.
func (s *Store) RetryDelivery(ctx context.Context, hookID, id int64) error {
	res, err := s.exec(ctx, `UPDATE webhook_deliveries SET status='pending', next_at=? WHERE id=? AND webhook_id=? AND status<>'pending'`, now(), id, hookID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// PruneDeliveries drops finished deliveries older than keep.
func (s *Store) PruneDeliveries(ctx context.Context, keep time.Duration) error {
	_, err := s.exec(ctx, `DELETE FROM webhook_deliveries WHERE status<>'pending' AND created_at<?`, time.Now().Add(-keep).Unix())
	return err
}
