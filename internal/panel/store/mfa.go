package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 default; the HMAC key is secret, SHA-1 collisions are irrelevant
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// ---- secret encryption at rest ----

// SetSecretKey enables AES-256-GCM encryption of TOTP secrets. The key should
// live outside the database file (see cmd/panel) so a leaked database alone
// does not reveal second factors.
func (s *Store) SetSecretKey(key []byte) error {
	if len(key) != 32 {
		return errors.New("secret key must be 32 bytes")
	}
	s.key = key
	return nil
}

func (s *Store) seal(plain string) (string, error) {
	if s.key == nil {
		return "plain:" + plain, nil
	}
	blk, _ := aes.NewCipher(s.key)
	g, _ := cipher.NewGCM(blk)
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return "gcm:" + base64.RawStdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (s *Store) open(enc string) (string, error) {
	switch {
	case strings.HasPrefix(enc, "plain:"):
		return strings.TrimPrefix(enc, "plain:"), nil
	case strings.HasPrefix(enc, "gcm:"):
		if s.key == nil {
			return "", errors.New("secret key not configured")
		}
		raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(enc, "gcm:"))
		blk, _ := aes.NewCipher(s.key)
		g, _ := cipher.NewGCM(blk)
		if err != nil || len(raw) < g.NonceSize() {
			return "", errors.New("corrupt secret")
		}
		b, err := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], nil)
		return string(b), err
	}
	return "", errors.New("unknown secret format")
}

// ---- TOTP (RFC 6238, SHA-1, 6 digits, 30 s) ----

func hotp(secret []byte, counter uint64) string {
	var c [8]byte
	binary.BigEndian.PutUint64(c[:], counter)
	m := hmac.New(sha1.New, secret)
	m.Write(c[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1_000_000)
}

// verifyTOTP accepts the current step and one either side (clock skew) and
// returns the matching step. Steps at or before lastStep are rejected so a
// code cannot be replayed.
func verifyTOTP(secret []byte, code string, at time.Time, lastStep int64) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	cur := at.Unix() / 30
	var hit int64
	for d := int64(-1); d <= 1; d++ { // no early exit: constant work per attempt
		step := cur + d
		if hmac.Equal([]byte(hotp(secret, uint64(step))), []byte(code)) && step > lastStep {
			hit = step
		}
	}
	return hit, hit != 0
}

// TOTPURI is the otpauth:// URI authenticator apps import (via QR code).
func TOTPURI(issuer, email, secret string) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		urlEsc(issuer), urlEsc(email), secret, urlEsc(issuer))
}

func urlEsc(s string) string {
	return strings.NewReplacer("%", "%25", " ", "%20", ":", "%3A", "@", "%40", "&", "%26", "?", "%3F", "#", "%23", "+", "%2B").Replace(s)
}

// BeginTOTP generates and stores a pending secret (not yet enforced).
func (s *Store) BeginTOTP(ctx context.Context, userID int64) (string, error) {
	raw := make([]byte, 20)
	rand.Read(raw)
	enc, err := s.seal(b32.EncodeToString(raw))
	if err != nil {
		return "", err
	}
	res, err := s.exec(ctx, `UPDATE users SET totp_secret=?, totp_enabled=0, totp_last_step=0 WHERE id=? AND totp_enabled=0`, enc, userID)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", errors.New("two-factor authentication is already enabled")
	}
	return b32.EncodeToString(raw), nil
}

func (s *Store) totpSecret(ctx context.Context, userID int64) (secret []byte, enabled bool, last int64, err error) {
	var enc string
	var en int
	if err = s.DB.QueryRowContext(ctx, `SELECT totp_secret,totp_enabled,totp_last_step FROM users WHERE id=?`, userID).Scan(&enc, &en, &last); err != nil {
		return
	}
	if enc == "" {
		return nil, false, 0, errors.New("two-factor setup not started")
	}
	plain, err := s.open(enc)
	if err != nil {
		return
	}
	secret, err = b32.DecodeString(plain)
	return secret, en == 1, last, err
}

// EnableTOTP confirms the pending secret with a valid code, turns enforcement
// on and returns one-time recovery codes.
func (s *Store) EnableTOTP(ctx context.Context, userID int64, code string) ([]string, error) {
	secret, enabled, last, err := s.totpSecret(ctx, userID)
	if err != nil {
		return nil, err
	}
	if enabled {
		return nil, errors.New("two-factor authentication is already enabled")
	}
	step, ok := verifyTOTP(secret, code, time.Now(), last)
	if !ok {
		return nil, errors.New("invalid code")
	}
	if _, err := s.exec(ctx, `UPDATE users SET totp_enabled=1, totp_last_step=? WHERE id=?`, step, userID); err != nil {
		return nil, err
	}
	return s.NewRecoveryCodes(ctx, userID)
}

// NewRecoveryCodes replaces the user's recovery codes. 80 bits each, so a
// stolen hash cannot be brute-forced.
func (s *Store) NewRecoveryCodes(ctx context.Context, userID int64) ([]string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=?`, userID); err != nil {
		return nil, err
	}
	codes := make([]string, 10)
	for i := range codes {
		raw := make([]byte, 10)
		rand.Read(raw)
		c := strings.ToLower(b32.EncodeToString(raw)) // 16 chars
		codes[i] = c[0:4] + "-" + c[4:8] + "-" + c[8:12] + "-" + c[12:16]
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes(user_id,code_hash) VALUES(?,?)`, userID, HashToken(codes[i])); err != nil {
			return nil, err
		}
	}
	return codes, tx.Commit()
}

// VerifySecondFactor checks a TOTP code or a recovery code (single use).
// kind is "totp" or "recovery".
func (s *Store) VerifySecondFactor(ctx context.Context, userID int64, code string) (kind string, err error) {
	code = strings.ToLower(strings.TrimSpace(code))
	bad := errors.New("invalid code")
	if len(code) == 6 {
		secret, enabled, last, err := s.totpSecret(ctx, userID)
		if err != nil || !enabled {
			return "", bad
		}
		step, ok := verifyTOTP(secret, code, time.Now(), last)
		if !ok {
			return "", bad
		}
		// Compare-and-set so two concurrent logins cannot both use one code.
		res, err := s.exec(ctx, `UPDATE users SET totp_last_step=? WHERE id=? AND totp_last_step<?`, step, userID, step)
		if err != nil {
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return "", bad
		}
		return "totp", nil
	}
	res, err := s.exec(ctx, `DELETE FROM recovery_codes WHERE user_id=? AND code_hash=?`, userID, HashToken(code))
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return "recovery", nil
	}
	return "", bad
}

// DisableTOTP removes the second factor and recovery codes.
func (s *Store) DisableTOTP(ctx context.Context, userID int64) error {
	if _, err := s.exec(ctx, `UPDATE users SET totp_secret='', totp_enabled=0, totp_last_step=0 WHERE id=?`, userID); err != nil {
		return err
	}
	_, err := s.exec(ctx, `DELETE FROM recovery_codes WHERE user_id=?`, userID)
	return err
}

// CheckPassword verifies a user's current password (for sensitive actions).
func (s *Store) CheckPassword(ctx context.Context, userID int64, pw string) bool {
	var enc string
	if s.DB.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id=?`, userID).Scan(&enc) != nil {
		checkPassword(pw, dummyHash)
		return false
	}
	return checkPassword(pw, enc)
}

// UserIDByEmail is used by the offline reset command.
func (s *Store) UserIDByEmail(ctx context.Context, email string) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM users WHERE email=?`, strings.ToLower(strings.TrimSpace(email))).Scan(&id)
	return id, nf(err)
}
