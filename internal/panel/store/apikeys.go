package store

import (
	"context"
	"time"
)

const KeyPrefix = "gp_"

type APIKey struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix" doc:"first characters of the key, to recognise it"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt int64  `json:"last_used_at" doc:"0 = never"`
	ExpiresAt  int64  `json:"expires_at" doc:"0 = never"`
}

const maxKeysPerUser = 25

// CreateAPIKey returns the metadata and the secret, which is shown only once.
func (s *Store) CreateAPIKey(ctx context.Context, userID int64, name string, ttl time.Duration) (APIKey, string, error) {
	n, err := count(ctx, s.DB, `SELECT COUNT(*) FROM api_keys WHERE user_id=?`, userID)
	if err != nil {
		return APIKey{}, "", err
	}
	if n >= maxKeysPerUser {
		return APIKey{}, "", userErr("too many API keys; delete unused ones first")
	}
	raw, _ := NewToken()
	tok := KeyPrefix + raw
	k := APIKey{Name: name, Prefix: tok[:len(KeyPrefix)+6], CreatedAt: now()}
	if ttl > 0 {
		k.ExpiresAt = time.Now().Add(ttl).Unix()
	}
	// Hash the full token (prefix included) so lookups use exactly what clients send.
	res, err := s.exec(ctx, `INSERT INTO api_keys(user_id,name,token_hash,prefix,created_at,expires_at) VALUES(?,?,?,?,?,?)`,
		userID, name, HashToken(tok), k.Prefix, k.CreatedAt, k.ExpiresAt)
	if err != nil {
		return APIKey{}, "", err
	}
	k.ID, _ = res.LastInsertId()
	return k, tok, nil
}

func (s *Store) ListAPIKeys(ctx context.Context, userID int64) ([]APIKey, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,prefix,created_at,last_used_at,expires_at FROM api_keys WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.CreatedAt, &k.LastUsedAt, &k.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAPIKey(ctx context.Context, userID, id int64) error {
	res, err := s.exec(ctx, `DELETE FROM api_keys WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UserByAPIKey resolves a bearer token. last_used_at is refreshed at most once
// a minute so busy integrations do not turn every request into a write.
func (s *Store) UserByAPIKey(ctx context.Context, token string) (User, error) {
	var u User
	var keyID, last int64
	h := HashToken(token)
	err := s.DB.QueryRowContext(ctx, `SELECT u.id,u.email,u.is_admin,u.totp_enabled,EXISTS(SELECT 1 FROM passkeys p WHERE p.user_id=u.id),k.id,k.last_used_at
		FROM api_keys k JOIN users u ON u.id=k.user_id
		WHERE k.token_hash=? AND (k.expires_at=0 OR k.expires_at>?)`, h, now()).
		Scan(&u.ID, &u.Email, &u.IsAdmin, &u.TOTPEnabled, &u.HasPasskey, &keyID, &last)
	if err != nil {
		return User{}, nf(err)
	}
	if now()-last > 60 {
		s.execBestEffort(ctx, "api key last used", `UPDATE api_keys SET last_used_at=? WHERE id=?`, now(), keyID)
	}
	return u, nil
}
