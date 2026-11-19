package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

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
		writeBody(w, proto.SFTPAuthResp{})
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
			a.auditEntry(r.Context(), storeAudit(email, "sftp.fail", prefix, req.IP))
		}
		deny()
	}
	if !strings.HasPrefix(req.Password, "gp_") {
		fail()
		return
	}
	u, err := a.Store.UserByAPIKey(r.Context(), req.Password)
	if err != nil || !strings.EqualFold(u.Email, email) {
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
		a.auditEntry(r.Context(), storeAudit(email, "sftp.login", s.UUID, req.IP))
	}
	w.Header().Set("Content-Type", "application/json")
	writeBody(w, proto.SFTPAuthResp{OK: true, ServerUUID: s.UUID, Read: read, Write: write})
}

func storeAudit(email, action, target, ip string) store.AuditEntry {
	return store.AuditEntry{ActorEmail: email, Action: action, Target: target, IP: ip}
}

type sftpInfoOut struct {
	Body struct {
		// Host is empty when the node has no SFTP address set: the panel's own
		// host name is then the best guess, which only the browser knows.
		Host     string `json:"host"`
		Port     int    `json:"port" doc:"0 while the node is offline and no port is set"`
		Username string `json:"username"`
		// Status: ready, off (the daemon runs without SFTP) or offline.
		Status string `json:"status" enum:"ready,off,offline"`
		Read   bool   `json:"read"`
		Write  bool   `json:"write"`
	}
}

type nodeSFTPIn struct {
	ID   int64 `path:"id"`
	Body struct {
		Host string `json:"host" maxLength:"253" doc:"host name or IP address SFTP clients connect to; empty = the panel's host"`
		Port int    `json:"port" minimum:"0" maximum:"65535" doc:"0 = the port the daemon listens on"`
	}
}

func (a *App) registerSFTP(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getServerSFTP", Method: "GET", Path: "/servers/{uuid}/sftp",
		Summary: "How to connect to this server with an SFTP client",
	},
		func(ctx context.Context, in *uuidIn) (*sftpInfoOut, error) {
			u, s, err := a.authorize(ctx, in.UUID)
			if err != nil {
				return nil, err
			}
			perms, _, _ := a.access(ctx, u, s)
			read, write := has(perms, PermFilesRead), has(perms, PermFilesWrite)
			if !read && !write {
				return nil, huma.Error403Forbidden("you do not have permission to do that (" + PermFilesRead + ")")
			}
			n, err := a.Store.GetNode(ctx, s.NodeID)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			out := &sftpInfoOut{}
			b := &out.Body
			b.Host, b.Read, b.Write = n.SFTPHost, read, write
			b.Username = u.Email + "." + s.UUID[:8]
			reported, online := a.Hub.SFTPPort(n.ID)
			switch {
			case !online:
				b.Status = "offline"
			case reported == 0:
				b.Status = "off"
			default:
				b.Status = "ready"
			}
			if b.Port = n.SFTPPort; b.Port == 0 {
				b.Port = reported
			}
			return out, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "setNodeSFTP", Method: "PUT", Path: "/nodes/{id}/sftp",
		Summary: "Set the address SFTP clients use to reach a node", DefaultStatus: 204,
	},
		func(ctx context.Context, in *nodeSFTPIn) (*struct{}, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			if err := a.Store.SetNodeSFTP(ctx, in.ID, in.Body.Host, in.Body.Port); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return nil, huma.Error404NotFound("node not found")
				}
				return nil, userErr(http.StatusUnprocessableEntity, err)
			}
			a.audit(ctx, u, "node.sftp", strconv.FormatInt(in.ID, 10), fmt.Sprintf("host=%q port=%d", strings.TrimSpace(in.Body.Host), in.Body.Port))
			return nil, nil
		})
}
