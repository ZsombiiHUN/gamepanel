package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

type backupIn struct {
	UUID   string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Backup string `path:"backup" pattern:"^[0-9a-f-]{36}$"`
}
type backupCreateIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Name   string `json:"name,omitempty" maxLength:"64"`
		Ignore string `json:"ignore,omitempty" maxLength:"2000" doc:"patterns to leave out, one per line (e.g. logs, *.tmp, cache/)"`
	}
}
type backupOut struct{ Body store.Backup }
type backupListOut struct{ Body []store.Backup }
type backupLockIn struct {
	UUID   string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Backup string `path:"backup" pattern:"^[0-9a-f-]{36}$"`
	Body   struct {
		Locked bool `json:"locked"`
	}
}
type backupRestoreIn struct {
	UUID   string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Backup string `path:"backup" pattern:"^[0-9a-f-]{36}$"`
	Body   struct {
		Truncate bool `json:"truncate,omitempty" doc:"delete all current files first"`
	}
}

const (
	backupTimeout  = 2 * time.Hour
	restoreTimeout = 2 * time.Hour
)

// startBackup records a backup and runs it on the node in the background.
// The returned channel yields the outcome (nil on success) and is closed.
func (a *App) startBackup(s store.Server, name, ignore string, rotate bool) (store.Backup, <-chan error, error) {
	ctx := context.Background()
	if name == "" {
		name = "Backup " + time.Now().UTC().Format("2006-01-02 15:04")
	}
	b := store.Backup{UUID: newUUID(), ServerUUID: s.UUID, Name: name, Ignore: ignore}
	evicted, err := a.Store.BeginBackup(ctx, b, rotate)
	if err != nil {
		return b, nil, err
	}
	done := make(chan error, 1)
	go func() {
		defer close(done)
		c, cancel := context.WithTimeout(ctx, backupTimeout)
		defer cancel()
		for _, id := range evicted { // rotated out: free the disk too
			a.Hub.Request(c, s.NodeID, proto.ReqBackupDelete, proto.BackupReq{UUID: s.UUID, BackupID: id})
		}
		raw, err := a.Hub.Request(c, s.NodeID, proto.ReqBackupCreate, proto.BackupReq{UUID: s.UUID, BackupID: b.UUID, Ignore: store.IgnoreList(ignore)})
		var res proto.BackupResult
		if err == nil {
			err = json.Unmarshal(raw, &res)
		}
		if err != nil {
			a.Store.FinishBackup(ctx, b.UUID, 0, "", err.Error())
			a.Log.Warn("backup failed", "server", s.UUID, "err", err)
			done <- err
			return
		}
		a.Store.FinishBackup(ctx, b.UUID, res.Size, res.SHA256, "")
		done <- nil
	}()
	return b, done, nil
}

func (a *App) registerBackups(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "listBackups", Method: "GET", Path: "/servers/{uuid}/backups", Summary: "List backups"},
		func(ctx context.Context, in *uuidIn) (*backupListOut, error) {
			_, s, err := a.authorizeP(ctx, in.UUID, PermBackupsRead)
			if err != nil {
				return nil, err
			}
			bs, err := a.Store.ListBackups(ctx, s.UUID)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			return &backupListOut{Body: bs}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "createBackup", Method: "POST", Path: "/servers/{uuid}/backups",
		Summary: "Start a backup (runs in the background)", DefaultStatus: 202},
		func(ctx context.Context, in *backupCreateIn) (*backupOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermBackupsCreate)
			if err != nil {
				return nil, err
			}
			if !a.Hub.Online(s.NodeID) {
				return nil, huma.Error503ServiceUnavailable("the node is offline")
			}
			b, _, err := a.startBackup(s, in.Body.Name, in.Body.Ignore, false)
			if err != nil {
				return nil, huma.Error409Conflict(err.Error())
			}
			a.audit(ctx, u, "backup.create", s.UUID, b.Name)
			b.Status = store.BackupRunning
			return &backupOut{Body: b}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "lockBackup", Method: "PUT", Path: "/servers/{uuid}/backups/{backup}/lock",
		Summary: "Lock or unlock a backup (locked backups cannot be deleted or rotated out)", DefaultStatus: 204},
		func(ctx context.Context, in *backupLockIn) (*struct{}, error) {
			if _, _, err := a.authorizeP(ctx, in.UUID, PermBackupsDelete); err != nil {
				return nil, err
			}
			if err := a.Store.SetBackupLocked(ctx, in.UUID, in.Backup, in.Body.Locked); err != nil {
				return nil, huma.Error404NotFound("backup not found")
			}
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteBackup", Method: "DELETE", Path: "/servers/{uuid}/backups/{backup}",
		Summary: "Delete a backup", DefaultStatus: 204},
		func(ctx context.Context, in *backupIn) (*struct{}, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermBackupsDelete)
			if err != nil {
				return nil, err
			}
			b, err := a.Store.GetBackup(ctx, s.UUID, in.Backup)
			if err != nil {
				return nil, huma.Error404NotFound("backup not found")
			}
			if b.Locked {
				return nil, huma.Error409Conflict("backup is locked")
			}
			if b.Status == store.BackupRunning {
				return nil, huma.Error409Conflict("backup is still running")
			}
			// File first: if the node is unreachable keep the row so nothing is orphaned.
			if _, err := a.Hub.Request(ctx, s.NodeID, proto.ReqBackupDelete, proto.BackupReq{UUID: s.UUID, BackupID: b.UUID}); err != nil {
				return nil, a.nodeErr(err)
			}
			a.Store.DeleteBackup(ctx, s.UUID, b.UUID)
			a.audit(ctx, u, "backup.delete", s.UUID, b.Name)
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "restoreBackup", Method: "POST", Path: "/servers/{uuid}/backups/{backup}/restore",
		Summary: "Restore a backup over the server's files (server must be stopped)", DefaultStatus: 204},
		func(ctx context.Context, in *backupRestoreIn) (*struct{}, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermBackupsRestore)
			if err != nil {
				return nil, err
			}
			b, err := a.Store.GetBackup(ctx, s.UUID, in.Backup)
			if err != nil {
				return nil, huma.Error404NotFound("backup not found")
			}
			if b.Status != store.BackupOK {
				return nil, huma.Error409Conflict("only a completed backup can be restored")
			}
			c, cancel := context.WithTimeout(ctx, restoreTimeout)
			defer cancel()
			if _, err := a.Hub.Request(c, s.NodeID, proto.ReqBackupRestore, proto.BackupReq{UUID: s.UUID, BackupID: b.UUID, Truncate: in.Body.Truncate}); err != nil {
				if errors.Is(err, hub.ErrNodeOffline) {
					return nil, huma.Error503ServiceUnavailable("the node is offline")
				}
				// The daemon's message is meant for people ("stop the server first").
				return nil, huma.Error409Conflict(err.Error())
			}
			a.audit(ctx, u, "backup.restore", s.UUID, fmt.Sprintf("%s truncate=%v", b.Name, in.Body.Truncate))
			return nil, nil
		})
}
