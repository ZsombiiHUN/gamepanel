package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// Identity is a single sign-on login linked to an account.
type Identity struct {
	Provider  string `json:"provider"`
	Email     string `json:"email" doc:"the address the provider reported"`
	CreatedAt int64  `json:"created_at"`
}

// UserByIdentity finds the account an identity is linked to.
func (s *Store) UserByIdentity(ctx context.Context, provider, subject string) (User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx, `SELECT u.id,u.email,u.is_admin,u.totp_enabled FROM user_identities i JOIN users u ON u.id=i.user_id
		WHERE i.provider=? AND i.subject=?`, provider, subject).Scan(&u.ID, &u.Email, &u.IsAdmin, &u.TOTPEnabled)
	return u, nf(err)
}

// ErrIdentityTaken means the identity is linked to another account.
var ErrIdentityTaken = userErr("that sign-in is already linked to another account")

// LinkIdentity links an identity to an account (relinking to the same
// account just updates the email).
func (s *Store) LinkIdentity(ctx context.Context, userID int64, provider, subject, email string) error {
	var owner int64
	err := s.DB.QueryRowContext(ctx, `SELECT user_id FROM user_identities WHERE provider=? AND subject=?`, provider, subject).Scan(&owner)
	if err == nil && owner != userID {
		return ErrIdentityTaken
	}
	_, err = s.exec(ctx, `INSERT INTO user_identities(provider,subject,user_id,email,created_at) VALUES(?,?,?,?,?)
		ON CONFLICT(provider,subject) DO UPDATE SET email=excluded.email`, provider, subject, userID, email, now())
	return err
}

func (s *Store) ListIdentities(ctx context.Context, userID int64) ([]Identity, error) {
	return queryAll(ctx, s.DB, func(sc rowScanner) (Identity, error) {
		var i Identity
		err := sc.Scan(&i.Provider, &i.Email, &i.CreatedAt)
		return i, err
	}, `SELECT provider,email,created_at FROM user_identities WHERE user_id=? ORDER BY provider`, userID)
}

// ErrLastSignIn means removing this would leave the account no way to sign in.
var ErrLastSignIn = userErr("this is your only way to sign in; set a password or link another first")

// UnlinkIdentity removes a provider from an account, unless it is the
// account's last way in.
func (s *Store) UnlinkIdentity(ctx context.Context, userID int64, provider string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hasPw bool
	var others int
	if err := tx.QueryRowContext(ctx, `SELECT has_password,(SELECT COUNT(*) FROM user_identities WHERE user_id=users.id AND provider<>?) FROM users WHERE id=?`, provider, userID).Scan(&hasPw, &others); err != nil {
		return nf(err)
	}
	passkeys, err := count(ctx, tx, `SELECT COUNT(*) FROM passkeys WHERE user_id=?`, userID)
	if err != nil {
		return err
	}
	if !hasPw && others == 0 && passkeys == 0 {
		return ErrLastSignIn
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM user_identities WHERE user_id=? AND provider=?`, userID, provider)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// CreateSSOUser makes an account for a verified single sign-on identity,
// without a password, and links the identity. With an invite token the
// invitation's access is granted too (the invited email must match).
func (s *Store) CreateSSOUser(ctx context.Context, email, provider, subject, invite string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if invite != "" {
		inv, err := s.GetInvite(ctx, invite)
		if err != nil {
			return User{}, err
		}
		if inv.Email != email {
			return User{}, userErr("this invitation is for " + inv.Email + ", but that sign-in belongs to " + email)
		}
	}
	// An unusable password: no one can sign in with it until a real one is set.
	b := make([]byte, 32)
	rand.Read(b)
	u, err := s.Register(ctx, invite, email, "sso-only-"+hex.EncodeToString(b))
	if err != nil {
		return u, err
	}
	if _, err := s.exec(ctx, `UPDATE users SET has_password=0 WHERE id=?`, u.ID); err != nil {
		return u, err
	}
	return u, s.LinkIdentity(ctx, u.ID, provider, subject, email)
}

// HasPassword reports whether an account has a password it can sign in with.
func (s *Store) HasPassword(ctx context.Context, userID int64) (bool, error) {
	var has bool
	err := s.DB.QueryRowContext(ctx, `SELECT has_password FROM users WHERE id=?`, userID).Scan(&has)
	return has, nf(err)
}

// SetFirstPassword gives a password to an account that has none (made
// through single sign-on). It fails if the account already has one.
func (s *Store) SetFirstPassword(ctx context.Context, userID int64, password string) error {
	u, err := s.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if err := ValidatePassword(password, u.Email); err != nil {
		return err
	}
	res, err := s.exec(ctx, `UPDATE users SET password_hash=?, has_password=1 WHERE id=? AND has_password=0`, hashPassword(password), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return userErr("this account already has a password; change it instead")
	}
	return nil
}
