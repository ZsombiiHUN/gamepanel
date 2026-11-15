package store

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// Permission describes one grantable per-server permission.
type Permission struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// AllPermissions is the closed set a subuser can be granted. Owners and
// admins implicitly hold every one of them. Managing subusers and deleting a
// server are never grantable.
var AllPermissions = []Permission{
	{"console.read", "See the console output and server status"},
	{"console.send", "Send console commands"},
	{"power", "Start, stop, restart and kill the server"},
	{"files.read", "Browse and read files (also SFTP read)"},
	{"files.write", "Create, edit and delete files (also SFTP write)"},
	{"backups.read", "See backups"},
	{"backups.create", "Create backups"},
	{"backups.restore", "Restore backups"},
	{"backups.delete", "Delete and lock backups"},
	{"schedules.manage", "Create, edit, run and delete schedules"},
	{"settings.reinstall", "Reinstall the server"},
}

// ValidatePermissions checks, de-duplicates and sorts a permission list.
func ValidatePermissions(in []string) ([]string, error) {
	known := map[string]bool{}
	for _, p := range AllPermissions {
		known[p.ID] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range in {
		if !known[p] {
			return nil, errors.New("unknown permission " + p)
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("grant at least one permission")
	}
	sort.Strings(out)
	return out, nil
}

type Subuser struct {
	UserID      int64    `json:"user_id"`
	Email       string   `json:"email"`
	Permissions []string `json:"permissions" nullable:"false"`
	CreatedAt   int64    `json:"created_at"`
}

func splitPerms(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

// AddSubuser gives an existing account access to a server.
func (s *Store) AddSubuser(ctx context.Context, server, email string, perms []string) (Subuser, error) {
	perms, err := ValidatePermissions(perms)
	if err != nil {
		return Subuser{}, err
	}
	var uid, owner int64
	var mail string
	if err := s.DB.QueryRowContext(ctx, `SELECT id,email FROM users WHERE email=?`, strings.ToLower(strings.TrimSpace(email))).Scan(&uid, &mail); err != nil {
		return Subuser{}, errors.New("no account with that email")
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT owner_id FROM servers WHERE uuid=?`, server).Scan(&owner); err != nil {
		return Subuser{}, nf(err)
	}
	if uid == owner {
		return Subuser{}, errors.New("that account already owns this server")
	}
	t := now()
	if _, err := s.exec(ctx, `INSERT INTO server_users(server_uuid,user_id,permissions,created_at) VALUES(?,?,?,?)`, server, uid, strings.Join(perms, ","), t); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "PRIMARY") {
			return Subuser{}, errors.New("that account already has access")
		}
		return Subuser{}, err
	}
	return Subuser{UserID: uid, Email: mail, Permissions: perms, CreatedAt: t}, nil
}

func (s *Store) SetSubuserPermissions(ctx context.Context, server string, userID int64, perms []string) error {
	perms, err := ValidatePermissions(perms)
	if err != nil {
		return err
	}
	res, err := s.exec(ctx, `UPDATE server_users SET permissions=? WHERE server_uuid=? AND user_id=?`, strings.Join(perms, ","), server, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RemoveSubuser(ctx context.Context, server string, userID int64) error {
	res, err := s.exec(ctx, `DELETE FROM server_users WHERE server_uuid=? AND user_id=?`, server, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListSubusers(ctx context.Context, server string) ([]Subuser, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id,u.email,su.permissions,su.created_at FROM server_users su JOIN users u ON u.id=su.user_id WHERE su.server_uuid=? ORDER BY u.email`, server)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subuser{}
	for rows.Next() {
		var v Subuser
		var p string
		if err := rows.Scan(&v.UserID, &v.Email, &p, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.Permissions = splitPerms(p)
		out = append(out, v)
	}
	return out, rows.Err()
}

// SubuserPermissions returns the permissions a user holds on a server as a
// subuser, and whether they are one at all.
func (s *Store) SubuserPermissions(ctx context.Context, server string, userID int64) ([]string, bool) {
	var p string
	if err := s.DB.QueryRowContext(ctx, `SELECT permissions FROM server_users WHERE server_uuid=? AND user_id=?`, server, userID).Scan(&p); err != nil {
		return nil, false
	}
	return splitPerms(p), true
}
