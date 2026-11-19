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
		return "", userErr("two-factor authentication is already enabled")
	}
	return b32.EncodeToString(raw), nil
}

func (s *Store) totpSecret(ctx context.Context, userID int64) (secret []byte, enabled bool, last int64, err error) {
	var enc string
	var en int
	if err = s.DB.QueryRowContext(ctx, `SELECT totp_secret,totp_enabled,totp_last_step FROM users WHERE id=?`, userID).Scan(&enc, &en, &last); err != nil {
		return secret, enabled, last, err
	}
	if enc == "" {
		return nil, false, 0, userErr("two-factor setup not started")
	}
	secret, err = s.decodeSecret(enc)
	return secret, en == 1, last, err
}

// decodeSecret turns a stored (possibly encrypted) TOTP secret into its bytes.
func (s *Store) decodeSecret(enc string) ([]byte, error) {
	plain, err := s.open(enc)
	if err != nil {
		return nil, err
	}
	return b32.DecodeString(plain)
}

// EnableTOTP confirms the pending secret with a valid code, turns enforcement
// on and returns one-time recovery codes.
func (s *Store) EnableTOTP(ctx context.Context, userID int64, code string) ([]string, error) {
	secret, enabled, last, err := s.totpSecret(ctx, userID)
	if err != nil {
		return nil, err
	}
	if enabled {
		return nil, userErr("two-factor authentication is already enabled")
	}
	step, ok := verifyTOTP(secret, code, time.Now(), last)
	if !ok {
		return nil, userErr("invalid code")
	}
	if _, err := s.exec(ctx, `UPDATE users SET totp_enabled=1, totp_last_step=? WHERE id=?`, step, userID); err != nil {
		return nil, err
	}
	return s.NewRecoveryCodes(ctx, userID)
}

// Recovery codes: RecoveryCodeCount of them, each recoveryBytes random bytes
// (120 bits), shown as six groups of four base32 characters. From 112 bits a
// fast hash is enough to store a lookup secret (ASVS v5.0.0-6.5.2): finding
// one from its SHA-256 means searching 2^120 codes, far beyond any attacker,
// so a slow salted hash (which would also force checking every stored code on
// each try) adds nothing.
const (
	RecoveryCodeCount = 10
	recoveryBytes     = 15
	recoveryChars     = recoveryBytes * 8 / 5 // 24
	legacyChars       = 16                    // 80-bit codes from before migration 00115
)

// normalizeCode lowercases a typed code and drops the spaces and dashes people
// copy along with it, so "ABCD-efgh ijkl" and "abcdefghijkl" are the same.
func normalizeCode(code string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '-' || r == ' ' || r == '\t':
			return -1
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		}
		return r
	}, strings.TrimSpace(code))
}

// recoveryHash is how a normalized code is stored. Legacy codes were stored
// as typed at the time, with dashes after every four characters.
func recoveryHash(norm string) (hash string, legacy bool) {
	if len(norm) == legacyChars {
		return HashToken(norm[0:4] + "-" + norm[4:8] + "-" + norm[8:12] + "-" + norm[12:16]), true
	}
	return HashToken(norm), false
}

// NewRecoveryCodes replaces the user's recovery codes, old ones included.
func (s *Store) NewRecoveryCodes(ctx context.Context, userID int64) ([]string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=?`, userID); err != nil {
		return nil, err
	}
	codes := make([]string, RecoveryCodeCount)
	for i := range codes {
		raw := make([]byte, recoveryBytes)
		rand.Read(raw) // never fails (crypto/rand panics instead)
		c := strings.ToLower(b32.EncodeToString(raw))
		h, _ := recoveryHash(c)
		var groups []string
		for j := 0; j < len(c); j += 4 {
			groups = append(groups, c[j:j+4])
		}
		codes[i] = strings.Join(groups, "-")
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes(user_id,code_hash,legacy) VALUES(?,?,0)`, userID, h); err != nil {
			return nil, err
		}
	}
	return codes, tx.Commit()
}

// RecoveryStatus is what the account owner may know about their recovery codes.
type RecoveryStatus struct {
	Remaining int `json:"remaining" doc:"unused recovery codes"`
	// Outdated: the codes were made before they had 120 bits. They still
	// work, but should be replaced.
	Outdated bool `json:"outdated"`
}

// RecoveryCodeStatus counts the user's unused recovery codes.
func (s *Store) RecoveryCodeStatus(ctx context.Context, userID int64) (RecoveryStatus, error) {
	var st RecoveryStatus
	var legacy int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(legacy),0) FROM recovery_codes WHERE user_id=?`, userID).Scan(&st.Remaining, &legacy)
	st.Outdated = legacy > 0
	return st, err
}

// ErrSecondFactorLocked: too many wrong authenticator codes in a row; the
// account must wait (or use a recovery code) before trying another.
var ErrSecondFactorLocked = userErr("too many wrong codes: wait before trying again, or use a recovery code")

// mfaFreeFailures wrong authenticator codes in a row are allowed before each
// further one adds a delay that doubles, up to mfaMaxLock. A 6-digit code (3
// accepted per try with clock skew) then survives years of guessing, even by
// someone who has the password and many IP addresses, and the count survives
// restarts (ASVS v5.0.0-6.3.1). NIST SP 800-63B §5.2.2 asks for at most 100
// failures in a row.
const (
	mfaFreeFailures = 5
	mfaMaxLock      = 24 * time.Hour
)

// mfaLockFor is the wait imposed after the n-th wrong code in a row.
func mfaLockFor(n int64) time.Duration {
	if n < mfaFreeFailures {
		return 0
	}
	shift := n - mfaFreeFailures
	if shift > 20 {
		return mfaMaxLock
	}
	return min(time.Minute<<shift, mfaMaxLock)
}

// VerifySecondFactor checks a TOTP code or a recovery code (single use).
// kind is "totp" or "recovery". Authenticator codes are refused with
// ErrSecondFactorLocked while the account is serving a guessing delay;
// recovery codes (120 bits, or 80 for codes from before migration 00115; not
// guessable either way) still work then, and a good one clears the delay.
func (s *Store) VerifySecondFactor(ctx context.Context, userID int64, code string) (kind string, err error) {
	code = normalizeCode(code)
	if len(code) == 6 {
		return s.verifyTOTPAttempt(ctx, userID, code)
	}
	bad := userErr("invalid code")
	if len(code) != recoveryChars && len(code) != legacyChars {
		return "", bad
	}
	h, legacy := recoveryHash(code)
	res, err := s.exec(ctx, `DELETE FROM recovery_codes WHERE user_id=? AND code_hash=? AND legacy=?`, userID, h, legacy)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		s.exec(ctx, `UPDATE users SET mfa_fail_count=0, mfa_locked_until=0 WHERE id=?`, userID)
		return "recovery", nil
	}
	return "", bad
}

// verifyTOTPAttempt checks an authenticator code. Checking the delay,
// verifying the code and recording the result happen in one transaction, so
// parallel guesses cannot all pass the delay check before any of them records
// a failure. (The store uses a single connection, so transactions run one at
// a time.) A delay is only ever extended, never shortened, by a failure.
func (s *Store) verifyTOTPAttempt(ctx context.Context, userID int64, code string) (string, error) {
	bad := userErr("invalid code")
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var enc string
	var enabled int
	var last, until int64
	if err := tx.QueryRowContext(ctx, `SELECT totp_secret,totp_enabled,totp_last_step,mfa_locked_until FROM users WHERE id=?`, userID).
		Scan(&enc, &enabled, &last, &until); err != nil {
		return "", bad
	}
	if until > now() {
		return "", ErrSecondFactorLocked
	}
	if enabled != 1 || enc == "" {
		return "", bad
	}
	secret, err := s.decodeSecret(enc)
	if err != nil {
		return "", bad
	}
	step, ok := verifyTOTP(secret, code, time.Now(), last)
	if !ok {
		var n int64
		if err := tx.QueryRowContext(ctx, `UPDATE users SET mfa_fail_count=mfa_fail_count+1 WHERE id=? RETURNING mfa_fail_count`, userID).Scan(&n); err != nil {
			return "", err
		}
		if d := mfaLockFor(n); d > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE users SET mfa_locked_until=MAX(mfa_locked_until,?) WHERE id=?`, time.Now().Add(d).Unix(), userID); err != nil {
				return "", err
			}
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return "", bad
	}
	// Steps only move forward, so one code cannot be used twice.
	if _, err := tx.ExecContext(ctx, `UPDATE users SET totp_last_step=?, mfa_fail_count=0, mfa_locked_until=0 WHERE id=?`, step, userID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return "totp", nil
}

// DisableTOTP removes the second factor and recovery codes.
func (s *Store) DisableTOTP(ctx context.Context, userID int64) error {
	if _, err := s.exec(ctx, `UPDATE users SET totp_secret='', totp_enabled=0, totp_last_step=0, mfa_fail_count=0, mfa_locked_until=0 WHERE id=?`, userID); err != nil {
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
