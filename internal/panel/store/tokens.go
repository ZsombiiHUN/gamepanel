package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	ResetTTL  = time.Hour
	InviteTTL = 7 * 24 * time.Hour
)

// ErrBadLink: the reset or invite link is unknown, used or expired. One
// message for all three, so a link cannot be probed.
var ErrBadLink = userErr("this link is invalid or has expired; ask for a new one")

// NewResetToken makes a password reset link token for an account, replacing
// any earlier one.
func (s *Store) NewResetToken(ctx context.Context, userID int64) (string, error) {
	return s.newResetToken(ctx, userID, ResetTTL)
}

func (s *Store) newResetToken(ctx context.Context, userID int64, ttl time.Duration) (string, error) {
	tok, h := NewToken()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_tokens WHERE kind='reset' AND (user_id=? OR expires_at<=?)`, userID, now()); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO user_tokens(token_hash,kind,user_id,expires_at,created_at) VALUES(?,?,?,?,?)`,
		h, "reset", userID, time.Now().Add(ttl).Unix(), now()); err != nil {
		return "", err
	}
	return tok, tx.Commit()
}

// ResetPassword uses a reset token: it sets the password, signs the account
// out everywhere and spends every reset token the account had.
func (s *Store) ResetPassword(ctx context.Context, token, password string) (User, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	var u User
	err = tx.QueryRowContext(ctx, `SELECT u.id,u.email,u.is_admin,u.totp_enabled FROM user_tokens t JOIN users u ON u.id=t.user_id
		WHERE t.token_hash=? AND t.kind='reset' AND t.expires_at>?`, HashToken(token), now()).Scan(&u.ID, &u.Email, &u.IsAdmin, &u.TOTPEnabled)
	if err != nil {
		return u, ErrBadLink
	}
	if err := ValidatePassword(password, u.Email); err != nil {
		return u, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=?, has_password=1 WHERE id=?`, hashPassword(password), u.ID); err != nil {
		return u, err
	}
	// Whoever had the old password (or a session made with it) is out.
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, u.ID); err != nil {
		return u, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_tokens WHERE kind='reset' AND user_id=?`, u.ID); err != nil {
		return u, err
	}
	return u, tx.Commit()
}

// Invite is a pending invitation.
type Invite struct {
	ID          int64    `json:"id"`
	Email       string   `json:"email"`
	ServerUUID  string   `json:"server_uuid,omitempty"`
	ServerName  string   `json:"server_name,omitempty"`
	Permissions []string `json:"permissions" nullable:"false"`
	IsAdmin     bool     `json:"is_admin"`
	ExpiresAt   int64    `json:"expires_at"`
	CreatedAt   int64    `json:"created_at"`
}

// NewInvite records an invitation for email, optionally onto a server with
// permissions, and returns the link token. An earlier invite of the same
// email to the same place is replaced.
func (s *Store) NewInvite(ctx context.Context, email string, by int64, server string, perms []string, admin bool) (string, Invite, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if server != "" {
		var err error
		if perms, err = ValidatePermissions(perms); err != nil {
			return "", Invite{}, err
		}
	}
	var exists int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email=?`, email).Scan(&exists)
	if exists > 0 {
		return "", Invite{}, ErrAccountExists
	}
	tok, h := NewToken()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", Invite{}, err
	}
	defer tx.Rollback()
	var srv any
	if server != "" {
		srv = server
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_tokens WHERE kind='invite' AND email=? AND COALESCE(server_uuid,'')=?`, email, server); err != nil {
		return "", Invite{}, err
	}
	inv := Invite{Email: email, ServerUUID: server, Permissions: perms, IsAdmin: admin, ExpiresAt: time.Now().Add(InviteTTL).Unix(), CreatedAt: now()}
	if inv.Permissions == nil {
		inv.Permissions = []string{}
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO user_tokens(token_hash,kind,email,server_uuid,permissions,is_admin,created_by,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		h, "invite", email, srv, strings.Join(perms, ","), admin, by, inv.ExpiresAt, inv.CreatedAt)
	if err != nil {
		return "", Invite{}, err
	}
	inv.ID, _ = res.LastInsertId()
	return tok, inv, tx.Commit()
}

// ErrAccountExists: an invitation or sign-up for an email that already has an account.
var ErrAccountExists = userErr("an account with this email already exists")

const inviteCols = `t.id,t.email,COALESCE(t.server_uuid,''),COALESCE(s.name,''),t.permissions,t.is_admin,t.expires_at,t.created_at`

func scanInvite(sc interface{ Scan(...any) error }) (Invite, error) {
	var inv Invite
	var perms string
	err := sc.Scan(&inv.ID, &inv.Email, &inv.ServerUUID, &inv.ServerName, &perms, &inv.IsAdmin, &inv.ExpiresAt, &inv.CreatedAt)
	inv.Permissions = splitPerms(perms)
	return inv, err
}

// GetInvite looks up a valid invitation by its token.
func (s *Store) GetInvite(ctx context.Context, token string) (Invite, error) {
	inv, err := scanInvite(s.DB.QueryRowContext(ctx, `SELECT `+inviteCols+` FROM user_tokens t LEFT JOIN servers s ON s.uuid=t.server_uuid
		WHERE t.token_hash=? AND t.kind='invite' AND t.expires_at>?`, HashToken(token), now()))
	if err != nil {
		return inv, ErrBadLink
	}
	return inv, nil
}

// ListInvites returns a server's pending invitations ("" = account invites).
func (s *Store) ListInvites(ctx context.Context, server string) ([]Invite, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+inviteCols+` FROM user_tokens t LEFT JOIN servers s ON s.uuid=t.server_uuid
		WHERE t.kind='invite' AND COALESCE(t.server_uuid,'')=? AND t.expires_at>? ORDER BY t.created_at DESC`, server, now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// DeleteInvite withdraws an invitation of a server ("" = account invites).
func (s *Store) DeleteInvite(ctx context.Context, server string, id int64) error {
	res, err := s.exec(ctx, `DELETE FROM user_tokens WHERE id=? AND kind='invite' AND COALESCE(server_uuid,'')=?`, id, server)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Register creates an account. With an invite token, the account gets the
// invited email (whatever was typed), and the server access the invite
// carries; every other invite to that email is honoured too.
func (s *Store) Register(ctx context.Context, token, email, password string) (User, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	admin := false
	if token != "" {
		var e string
		err := tx.QueryRowContext(ctx, `SELECT email,is_admin FROM user_tokens WHERE token_hash=? AND kind='invite' AND expires_at>?`, HashToken(token), now()).Scan(&e, &admin)
		if err != nil {
			return User{}, ErrBadLink
		}
		email = e
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if err := ValidatePassword(password, email); err != nil {
		return User{}, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO users(email,password_hash,is_admin,created_at) VALUES(?,?,?,?)`, email, hashPassword(password), admin, now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, ErrAccountExists
		}
		return User{}, err
	}
	id, _ := res.LastInsertId()
	if token != "" {
		// Every pending server invite for this email, not just the one clicked.
		rows, err := tx.QueryContext(ctx, `SELECT server_uuid,permissions FROM user_tokens WHERE kind='invite' AND email=? AND server_uuid IS NOT NULL AND expires_at>?`, email, now())
		if err != nil {
			return User{}, err
		}
		type grant struct{ server, perms string }
		var gs []grant
		for rows.Next() {
			var g grant
			if err := rows.Scan(&g.server, &g.perms); err != nil {
				rows.Close()
				return User{}, err
			}
			gs = append(gs, g)
		}
		rows.Close()
		for _, g := range gs {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO server_users(server_uuid,user_id,permissions,created_at) VALUES(?,?,?,?)`, g.server, id, g.perms, now()); err != nil {
				return User{}, err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_tokens WHERE kind='invite' AND email=?`, email); err != nil {
			return User{}, err
		}
	}
	return User{ID: id, Email: email, IsAdmin: admin}, tx.Commit()
}

// UserByEmail finds an account.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx, `SELECT id,email,is_admin,totp_enabled FROM users WHERE email=?`, strings.ToLower(strings.TrimSpace(email))).Scan(&u.ID, &u.Email, &u.IsAdmin, &u.TOTPEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}
