package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// sftpAuth lets a node daemon check an SFTP login. The "password" is an API
// key: it works for accounts with two-factor on, is revocable, and never
// exposes the account password to a protocol clients tend to save in plaintext.
//
// The node token authenticates the daemon, and the server must live on that
// node, so one node can never authorise access to another node's servers.
//
// The daemon also calls this periodically for open sessions (Recheck), so a
// revoked key or lost permission ends the session. Rechecks are not sign-ins:
// they are not audited and a refusal is not counted as a guess.
func (a *App) sftpAuth(w http.ResponseWriter, r *http.Request) {
	deny := func() {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proto.SFTPAuthResp{})
	}
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	node, err := a.Store.NodeByToken(r.Context(), tok)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req proto.SFTPAuthReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// username = "<email>.<8 hex>"; emails contain dots, so split at the last one.
	i := strings.LastIndexByte(req.Username, '.')
	if i < 1 || len(req.Username)-i-1 != 8 {
		deny()
		return
	}
	email, prefix := strings.ToLower(req.Username[:i]), req.Username[i+1:]

	// Guess limits: per account and per client address (reported by the daemon).
	acct, byIP := "sftp:"+email, "sftpip:"+req.IP
	// A recheck holds a key that already worked, so someone guessing at this
	// account must not be able to cut the owner's open session.
	if !req.Recheck && (a.fails.over(acct) || a.fails.over(byIP)) {
		deny()
		return
	}
	fail := func() {
		if !req.Recheck {
			a.fails.add(acct)
			a.fails.add(byIP)
			a.Store.Audit(r.Context(), storeAudit(email, "sftp.fail", prefix, req.IP))
		}
		deny()
	}
	if !strings.HasPrefix(req.Password, "gp_") {
		fail()
		return
	}
	u, err := a.Store.UserByAPIKey(r.Context(), req.Password)
	if err != nil || strings.ToLower(u.Email) != email {
		fail()
		return
	}
	s, err := a.Store.ServerByPrefix(r.Context(), node.ID, prefix)
	if err != nil {
		fail()
		return
	}
	perms, _, ok := a.access(r.Context(), u, s)
	read, write := ok && has(perms, PermFilesRead), ok && has(perms, PermFilesWrite)
	if !read && !write {
		fail()
		return
	}
	if !req.Recheck {
		a.fails.reset(acct)
		a.Store.Audit(r.Context(), storeAudit(email, "sftp.login", s.UUID, req.IP))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proto.SFTPAuthResp{OK: true, ServerUUID: s.UUID, Read: read, Write: write})
}

func storeAudit(email, action, target, ip string) store.AuditEntry {
	return store.AuditEntry{ActorEmail: email, Action: action, Target: target, IP: ip}
}
