package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

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

// How long the panel waits for a node to finish an archive. Both run in the
// background, detached from any HTTP request.
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
	b := store.Backup{UUID: newUUID(), ServerUUID: s.UUID, Name: name, Ignore: ignore, Disk: "local"}
	if a.S3 != nil {
		b.Disk = diskS3
	}
	evicted, err := a.Store.BeginBackup(ctx, b, rotate)
	if err != nil {
		return b, nil, err
	}
	done := make(chan error, 1)
	go func() {
		defer close(done)
		c, cancel := context.WithTimeout(ctx, backupTimeout)
		defer cancel()
		for _, old := range evicted { // rotated out: free the space too
			if err := a.removeBackupFile(c, s, old); err != nil {
				a.Log.Warn("could not delete a rotated-out backup", "server", s.UUID, "backup", old.UUID, "err", err)
			}
		}
		raw, err := a.Hub.Request(c, s.NodeID, proto.ReqBackupCreate, proto.BackupReq{UUID: s.UUID, BackupID: b.UUID, Ignore: store.IgnoreList(ignore), Remote: b.Disk == diskS3})
		var res proto.BackupResult
		if err == nil {
			err = json.Unmarshal(raw, &res)
		}
		if err != nil {
			a.Log.Warn("backup failed", "server", s.UUID, "err", err)
			if ferr := a.Store.FinishBackup(ctx, b.UUID, 0, "", failureText(err)); ferr != nil {
				a.Log.Error("could not record a failed backup", "backup", b.UUID, "err", ferr)
			}
			// The node may still have written (part of) the archive, e.g. when
			// the wait timed out: remove it so no file outlives its record.
			rm, cancel := context.WithTimeout(ctx, time.Minute)
			_, _ = a.Hub.Request(rm, s.NodeID, proto.ReqBackupDelete, proto.BackupReq{UUID: s.UUID, BackupID: b.UUID})
			cancel()
			done <- err
			return
		}
		if err := a.Store.FinishBackup(ctx, b.UUID, res.Size, res.SHA256, ""); err != nil {
			a.Log.Error("could not record a finished backup", "backup", b.UUID, "err", err)
		}
		done <- nil
	}()
	return b, done, nil
}

// startRestore runs a restore on the node in the background and records its
// outcome on the backup. A large world takes far longer than an HTTP request
// (or a reverse proxy) will wait, so the API answers at once and the UI polls.
func (a *App) startRestore(s store.Server, b store.Backup, truncate bool) error {
	ctx := context.Background()
	if err := a.Store.BeginRestore(ctx, s.UUID, b.UUID); err != nil {
		return err
	}
	go func() {
		c, cancel := context.WithTimeout(ctx, restoreTimeout)
		defer cancel()
		failure := ""
		req := proto.BackupReq{UUID: s.UUID, BackupID: b.UUID, Truncate: truncate}
		if b.Disk == diskS3 {
			if a.S3 == nil {
				if ferr := a.Store.FinishRestore(ctx, b.UUID, "this backup is in S3 storage, which the panel is no longer set up for"); ferr != nil {
					a.Log.Error("could not record a restore's outcome", "backup", b.UUID, "err", ferr)
				}
				return
			}
			req.URL = a.S3.Presign(http.MethodGet, a.S3.Key(s.UUID, b.UUID), restoreTimeout, nil)
		}
		if _, err := a.Hub.Request(c, s.NodeID, proto.ReqBackupRestore, req); err != nil {
			failure = failureText(err)
			a.Log.Warn("restore failed", "server", s.UUID, "backup", b.UUID, "err", err)
		}
		if err := a.Store.FinishRestore(ctx, b.UUID, failure); err != nil {
			a.Log.Error("could not record a restore's outcome", "backup", b.UUID, "err", err)
		}
	}()
	return nil
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
				return nil, userErr(http.StatusConflict, err)
			}
			a.audit(ctx, u, "backup.create", s.UUID, b.Name)
			b.Status = store.BackupRunning
			return &backupOut{Body: b}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "lockBackup", Method: "PUT", Path: "/servers/{uuid}/backups/{backup}/lock",
		Summary: "Lock or unlock a backup (locked backups cannot be deleted or rotated out)", DefaultStatus: 204},
		func(ctx context.Context, in *backupLockIn) (*struct{}, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermBackupsDelete)
			if err != nil {
				return nil, err
			}
			if err := a.Store.SetBackupLocked(ctx, s.UUID, in.Backup, in.Body.Locked); err != nil {
				return nil, huma.Error404NotFound("backup not found")
			}
			action := "backup.unlock"
			if in.Body.Locked {
				action = "backup.lock"
			}
			a.audit(ctx, u, action, s.UUID, in.Backup)
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
			// File first: if it cannot be removed keep the row so nothing is orphaned.
			if err := a.removeBackupFile(ctx, s, b); err != nil {
				return nil, err
			}
			if err := a.Store.DeleteBackup(ctx, s.UUID, b.UUID); err != nil {
				return nil, huma.Error500InternalServerError("the node deleted the file but the panel could not", err)
			}
			a.audit(ctx, u, "backup.delete", s.UUID, b.Name)
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "restoreBackup", Method: "POST", Path: "/servers/{uuid}/backups/{backup}/restore",
		Summary:       "Restore a backup over the server's files (server must be stopped)",
		Description:   "Runs in the background. Poll the backup list: restore_status goes from running to ok or failed (with restore_error).",
		DefaultStatus: 202},
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
			if !a.Hub.Online(s.NodeID) {
				return nil, huma.Error503ServiceUnavailable("the node is offline")
			}
			// Fail fast on the common mistake; the node checks again (and decides).
			if st := a.Hub.State(s.UUID); st != "offline" && st != "unknown" && st != "install_failed" {
				return nil, huma.Error409Conflict("stop the server before restoring a backup")
			}
			if err := a.startRestore(s, b, in.Body.Truncate); err != nil {
				if errors.Is(err, store.ErrRestoreRunning) {
					return nil, userErr(http.StatusConflict, err)
				}
				return nil, huma.Error500InternalServerError("db error")
			}
			a.audit(ctx, u, "backup.restore", s.UUID, fmt.Sprintf("%s truncate=%v", b.Name, in.Body.Truncate))
			return nil, nil
		})
}
