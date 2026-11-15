package store

import "context"

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

func (s *Store) ListAudit(ctx context.Context, limit, offset int) ([]AuditEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,at,COALESCE(actor_id,0),actor_email,action,target,ip,detail
		FROM audit_log ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
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
