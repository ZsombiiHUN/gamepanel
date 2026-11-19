package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

// Sign-in methods recorded on a session.
const (
	MethodPassword = "password"
	MethodTOTP     = "password+totp"
	MethodRecovery = "password+recovery"
	MethodPasskey  = "passkey"
	MethodSSO      = "sso"
	MethodSSOTOTP  = "sso+totp"
	MethodRegister = "register"
)

// IsMFAMethod reports whether a sign-in method used two factors. A passkey
// counts: sign-in requires user verification, so it is something you have
// plus a PIN or biometric. Single sign-on alone does not, since the panel
// cannot see how the provider checked the person.
func IsMFAMethod(m string) bool {
	switch {
	case m == MethodTOTP, m == MethodRecovery, m == MethodPasskey, strings.HasPrefix(m, MethodSSOTOTP):
		return true
	}
	return false
}

// MarkSessionMFA records that a session has now proved a second factor (it
// just set one up).
func (s *Store) MarkSessionMFA(ctx context.Context, token string) error {
	_, err := s.exec(ctx, `UPDATE sessions SET mfa=1 WHERE token_hash=?`, HashToken(token))
	return err
}

// Caps on what a client may make the panel store about its session.
const (
	maxUALen = 512
	maxIPLen = 64
)

// SessionMeta is where a sign-in came from.
type SessionMeta struct {
	IP        string
	UserAgent string
	// Method is how the person signed in (Method* constants); for SSO it may
	// carry the provider after a colon, e.g. "sso:github".
	Method string
}

func (m SessionMeta) clean() SessionMeta {
	m.IP = clip(m.IP, maxIPLen)
	m.UserAgent = clip(m.UserAgent, maxUALen)
	m.Method = clip(m.Method, 64)
	return m
}

// clip trims s to at most n bytes without splitting a UTF-8 sequence, and
// drops control characters so a stored header cannot break a log line or UI.
func clip(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	for len(s) > n {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

func newPublicID() string {
	b := make([]byte, 16)
	rand.Read(b) // never fails (crypto/rand panics instead)
	return hex.EncodeToString(b)
}

// Session is one signed-in browser or device, as the account owner sees it.
type Session struct {
	ID        string `json:"id" doc:"opaque id, for revoking this session"`
	Current   bool   `json:"current" doc:"the session this request was made with"`
	CreatedAt int64  `json:"created_at" doc:"sign-in time (unix seconds); 0 for sessions from before this was recorded"`
	LastSeen  int64  `json:"last_seen_at" doc:"last use, to within a minute"`
	ExpiresAt int64  `json:"expires_at" doc:"absolute end, however busy the session is"`
	IdleEnds  int64  `json:"idle_expires_at" doc:"when the session ends if it goes unused"`
	IP        string `json:"ip" doc:"address it signed in from"`
	LastIP    string `json:"last_ip" doc:"address it was last used from"`
	UserAgent string `json:"user_agent"`
	Method    string `json:"method" doc:"how it signed in: password, password+totp, password+recovery, passkey, sso:<provider name>, sso+totp:<provider name>, register; empty for sessions from before this was recorded"`
}

// ListSessions returns the user's live sessions, most recently used first.
// current is the caller's own session token, which is marked.
func (s *Store) ListSessions(ctx context.Context, userID int64, current string) ([]Session, error) {
	cur := HashToken(current)
	idle := int64(s.SessionIdle.Seconds())
	rows, err := s.DB.QueryContext(ctx, `SELECT public_id,token_hash=?,created_at,last_seen_at,expires_at,ip,last_ip,user_agent,method
		FROM sessions WHERE user_id=? AND expires_at>? AND last_seen_at>? ORDER BY token_hash=? DESC, last_seen_at DESC, created_at DESC`,
		cur, userID, now(), s.idleCutoff(), cur)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var x Session
		if err := rows.Scan(&x.ID, &x.Current, &x.CreatedAt, &x.LastSeen, &x.ExpiresAt, &x.IP, &x.LastIP, &x.UserAgent, &x.Method); err != nil {
			return nil, err
		}
		x.IdleEnds = min(x.LastSeen+idle, x.ExpiresAt)
		out = append(out, x)
	}
	return out, rows.Err()
}

// ErrCurrentSession is returned when revoking your own session by id; sign out instead.
var ErrCurrentSession = userErr("that is the session you are using; sign out instead")

// DeleteSessionByID revokes one of the user's sessions, by its public id. It
// will not revoke current (the caller's own token): signing out does that,
// and also clears the cookie.
func (s *Store) DeleteSessionByID(ctx context.Context, userID int64, id, current string) error {
	var h string
	if err := s.DB.QueryRowContext(ctx, `SELECT token_hash FROM sessions WHERE user_id=? AND public_id=? AND public_id<>''`, userID, id).Scan(&h); err != nil {
		return nf(err)
	}
	if h == HashToken(current) {
		return ErrCurrentSession
	}
	res, err := s.exec(ctx, `DELETE FROM sessions WHERE user_id=? AND token_hash=?`, userID, h)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
