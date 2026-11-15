package api

import (
	"context"

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
	PermSchedulesManage = "schedules.manage"
	PermReinstall       = "settings.reinstall"
)

// authorizeP is authorize plus a permission check.
func (a *App) authorizeP(ctx context.Context, uuid, perm string) (store.User, store.Server, error) {
	return a.authorize(ctx, uuid)
}
