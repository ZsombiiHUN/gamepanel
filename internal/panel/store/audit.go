package store

import (
	"context"
	"strings"
)

// AuditEntry is one security-relevant event. ActorID is 0 when there is no
// signed-in user (e.g. a failed login); ActorEmail then holds what was typed.
type AuditEntry struct {
	ID         int64  `json:"id"`
	At         int64  `json:"at"`
	ActorID    int64  `json:"actor_id,omitempty"`
	ActorEmail string `json:"actor_email"`
	Action     string `json:"action"`
	Target     string `json:"target"`
	IP         string `json:"ip"`
	Detail     string `json:"detail"`
}

func (s *Store) Audit(ctx context.Context, e AuditEntry) {
	var actor any
	if e.ActorID != 0 {
		actor = e.ActorID
	}
	s.exec(ctx, `INSERT INTO audit_log(at,actor_id,actor_email,action,target,ip,detail) VALUES(?,?,?,?,?,?,?)`,
		now(), actor, e.ActorEmail, e.Action, e.Target, e.IP, e.Detail)
}

// ListServerActivity returns a server's audit entries, newest first,
// filtered like ListAudit.
func (s *Store) ListServerActivity(ctx context.Context, server, q string, limit, offset int) ([]AuditEntry, error) {
	return s.listAudit(ctx, []string{"target = ?"}, []any{server}, q, limit, offset)
}

// ListAudit returns newest-first entries. A non-empty q keeps only entries
// where every whitespace-separated word appears (case-insensitively) in the
// action, user, target, detail or IP.
func (s *Store) ListAudit(ctx context.Context, q string, limit, offset int) ([]AuditEntry, error) {
	return s.listAudit(ctx, nil, nil, q, limit, offset)
}

func (s *Store) listAudit(ctx context.Context, conds []string, args []any, q string, limit, offset int) ([]AuditEntry, error) {
	for _, w := range strings.Fields(q) {
		like := "%" + likeEscaper.Replace(strings.ToLower(w)) + "%"
		conds = append(conds, `(LOWER(action) LIKE ? ESCAPE '\' OR LOWER(actor_email) LIKE ? ESCAPE '\' OR LOWER(target) LIKE ? ESCAPE '\' OR LOWER(detail) LIKE ? ESCAPE '\' OR LOWER(ip) LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like, like, like)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, limit, offset)
	rows, err := s.DB.QueryContext(ctx, `SELECT id,at,COALESCE(actor_id,0),actor_email,action,target,ip,detail
		FROM audit_log`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.At, &e.ActorID, &e.ActorEmail, &e.Action, &e.Target, &e.IP, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
