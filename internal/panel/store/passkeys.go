package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"
)

// Passkey is a passkey as its owner sees it.
type Passkey struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt int64  `json:"last_used_at" doc:"0 = never"`
}

// WebAuthnHandle returns the account's WebAuthn user handle, making one the
// first time.
func (s *Store) WebAuthnHandle(ctx context.Context, userID int64) ([]byte, error) {
	var h []byte
	if err := s.DB.QueryRowContext(ctx, `SELECT webauthn_id FROM users WHERE id=?`, userID).Scan(&h); err != nil {
		return nil, nf(err)
	}
	if len(h) > 0 {
		return h, nil
	}
	h = make([]byte, 32)
	rand.Read(h)
	if _, err := s.exec(ctx, `UPDATE users SET webauthn_id=? WHERE id=? AND webauthn_id IS NULL`, h, userID); err != nil {
		return nil, err
	}
	// Read back: a concurrent request may have set it first.
	err := s.DB.QueryRowContext(ctx, `SELECT webauthn_id FROM users WHERE id=?`, userID).Scan(&h)
	return h, err
}

// UserByWebAuthnHandle finds the account a passkey says it belongs to.
func (s *Store) UserByWebAuthnHandle(ctx context.Context, handle []byte) (User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx, `SELECT id,email,is_admin,totp_enabled FROM users WHERE webauthn_id=?`, handle).Scan(&u.ID, &u.Email, &u.IsAdmin, &u.TOTPEnabled)
	return u, nf(err)
}

// PasskeyData returns the stored credential records of an account.
func (s *Store) PasskeyData(ctx context.Context, userID int64) ([][]byte, error) {
	return queryAll(ctx, s.DB, func(sc rowScanner) ([]byte, error) {
		var d []byte
		err := sc.Scan(&d)
		return d, err
	}, `SELECT data FROM passkeys WHERE user_id=? ORDER BY created_at`, userID)
}

func passkeyID(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

// AddPasskey stores a new passkey.
func (s *Store) AddPasskey(ctx context.Context, userID int64, credID []byte, name string, data []byte) (Passkey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Passkey"
	}
	p := Passkey{ID: passkeyID(credID), Name: name, CreatedAt: now()}
	_, err := s.exec(ctx, `INSERT INTO passkeys(id,user_id,name,data,created_at) VALUES(?,?,?,?,?)`, p.ID, userID, p.Name, data, p.CreatedAt)
	return p, uniqueErr(err, "that passkey is already registered")
}

// UsePasskey records a sign-in: the updated record (sign counter, flags)
// and when. It only touches a passkey of that account.
func (s *Store) UsePasskey(ctx context.Context, userID int64, credID, data []byte) error {
	res, err := s.exec(ctx, `UPDATE passkeys SET data=?, last_used_at=? WHERE id=? AND user_id=?`, data, now(), passkeyID(credID), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListPasskeys(ctx context.Context, userID int64) ([]Passkey, error) {
	return queryAll(ctx, s.DB, func(sc rowScanner) (Passkey, error) {
		var p Passkey
		err := sc.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.LastUsedAt)
		return p, err
	}, `SELECT id,name,created_at,last_used_at FROM passkeys WHERE user_id=? ORDER BY created_at`, userID)
}

func (s *Store) RenamePasskey(ctx context.Context, userID int64, id, name string) error {
	res, err := s.exec(ctx, `UPDATE passkeys SET name=? WHERE id=? AND user_id=?`, strings.TrimSpace(name), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePasskey removes a passkey unless it is the account's last way in.
func (s *Store) DeletePasskey(ctx context.Context, userID int64, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hasPw bool
	var others, ids int
	if err := tx.QueryRowContext(ctx, `SELECT has_password,
		(SELECT COUNT(*) FROM passkeys WHERE user_id=users.id AND id<>?),
		(SELECT COUNT(*) FROM user_identities WHERE user_id=users.id) FROM users WHERE id=?`, id, userID).Scan(&hasPw, &others, &ids); err != nil {
		return nf(err)
	}
	if !hasPw && others == 0 && ids == 0 {
		return ErrLastSignIn
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM passkeys WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}
