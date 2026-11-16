package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// Per-server permissions. Owners and admins hold all of them; subusers hold
// the subset they were granted.
const (
	PermConsoleRead     = "console.read"
	PermConsoleSend     = "console.send"
	PermPower           = "power"
	PermFilesRead       = "files.read"
	PermFilesWrite      = "files.write"
	PermBackupsRead     = "backups.read"
	PermBackupsCreate   = "backups.create"
	PermBackupsRestore  = "backups.restore"
	PermBackupsDelete   = "backups.delete"
	PermBackupsDownload = "backups.download"
	PermSchedulesManage = "schedules.manage"
	PermReinstall       = "settings.reinstall"
	PermStartupRead     = "startup.read"
	PermStartupUpdate   = "startup.update"
	PermStartupImage    = "startup.docker-image"
	PermNetworkRead     = "network.read"
	PermNetworkManage   = "network.manage"
)

// access resolves what the caller may do on a server. Owners and admins hold
// every permission; subusers only what they were granted. ok is false when
// the caller has no relationship with the server at all.
func (a *App) access(ctx context.Context, u store.User, s store.Server) (perms []string, full, ok bool) {
	if u.IsAdmin || s.OwnerID == u.ID {
		return allPermissions(), true, true
	}
	perms, ok = a.Store.SubuserPermissions(ctx, s.UUID, u.ID)
	return perms, false, ok
}

func allPermissions() []string {
	all := make([]string, len(store.AllPermissions))
	for i, p := range store.AllPermissions {
		all[i] = p.ID
	}
	return all
}

func has(perms []string, p string) bool {
	for _, x := range perms {
		if x == p {
			return true
		}
	}
	return false
}

// authorizeP loads the server and checks the caller holds perm ("" = any
// access). Strangers get the same 404 as a missing server so UUIDs cannot be
// probed; members lacking the permission get 403.
func (a *App) authorizeP(ctx context.Context, uuid, perm string) (store.User, store.Server, error) {
	u, err := requireUser(ctx)
	if err != nil {
		return u, store.Server{}, err
	}
	s, err := a.Store.GetServer(ctx, uuid)
	if err != nil {
		return u, s, huma.Error404NotFound("server not found")
	}
	perms, _, ok := a.access(ctx, u, s)
	if !ok {
		return u, s, huma.Error404NotFound("server not found")
	}
	if perm != "" && !has(perms, perm) {
		return u, s, huma.Error403Forbidden("you do not have permission to do that (" + perm + ")")
	}
	return u, s, nil
}

// authorizeOwner is for actions that are never delegated: managing who has
// access. Only the owner and admins pass.
func (a *App) authorizeOwner(ctx context.Context, uuid string) (store.User, store.Server, error) {
	u, s, err := a.authorizeP(ctx, uuid, "")
	if err != nil {
		return u, s, err
	}
	if !u.IsAdmin && s.OwnerID != u.ID {
		return u, s, huma.Error403Forbidden("only the owner can do that")
	}
	return u, s, nil
}

// authorize grants any access to the server (read-only views such as status).
func (a *App) authorize(ctx context.Context, uuid string) (store.User, store.Server, error) {
	return a.authorizeP(ctx, uuid, "")
}

// taskPerm is the permission a scheduled task needs: a schedule must never be
// a way to do what its author could not do directly.
func taskPerm(action string) string {
	switch action {
	case store.ActionCommand:
		return PermConsoleSend
	case store.ActionPower:
		return PermPower
	default:
		return PermBackupsCreate
	}
}
