package store

import (
	"context"
	"strings"
)

const (
	BackupRunning = "running"
	BackupOK      = "ok"
	BackupFailed  = "failed"

	// DefaultBackupLimit applies to servers that do not set their own
	// (servers.backup_limit). At the limit manual backups are refused and
	// scheduled ones rotate out the oldest unlocked backup.
	DefaultBackupLimit = 20
)

var ErrBackupLimit = userErr("backup limit reached; delete one or unlock one for rotation")

type Backup struct {
	UUID        string `json:"uuid"`
	ServerUUID  string `json:"server_uuid"`
	Name        string `json:"name"`
	Status      string `json:"status" doc:"running|ok|failed"`
	Error       string `json:"error,omitempty"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Ignore      string `json:"ignore" doc:"newline separated ignore patterns"`
	Locked      bool   `json:"locked"`
	Disk        string `json:"disk" doc:"local (on the node) or s3"`
	CreatedAt   int64  `json:"created_at"`
	CompletedAt int64  `json:"completed_at"`
	// The last restore of this backup, which runs in the background.
	RestoreStatus string `json:"restore_status" doc:"empty (never restored)|running|ok|failed"`
	RestoreError  string `json:"restore_error,omitempty"`
	RestoredAt    int64  `json:"restored_at" doc:"when the last restore finished, 0 = never or still running"`
}

const backupCols = `uuid,server_uuid,name,status,error,size,sha256,ignore,locked,created_at,completed_at,restore_status,restore_error,restored_at,disk`

func scanBackup(sc rowScanner) (Backup, error) {
	var b Backup
	err := sc.Scan(&b.UUID, &b.ServerUUID, &b.Name, &b.Status, &b.Error, &b.Size, &b.SHA256, &b.Ignore, &b.Locked, &b.CreatedAt, &b.CompletedAt,
		&b.RestoreStatus, &b.RestoreError, &b.RestoredAt, &b.Disk)
	return b, nf(err)
}

// ErrRestoreRunning means a server can only restore one backup at a time.
var ErrRestoreRunning = userErr("a restore is already running for this server")

// BeginRestore marks a backup as being restored, refusing while any backup of
// the same server is already being restored.
func (s *Store) BeginRestore(ctx context.Context, server, uuid string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var running int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM backups WHERE server_uuid=? AND restore_status=?`, server, BackupRunning).Scan(&running); err != nil {
		return err
	}
	if running > 0 {
		return ErrRestoreRunning
	}
	res, err := tx.ExecContext(ctx, `UPDATE backups SET restore_status=?,restore_error='',restored_at=0 WHERE uuid=? AND server_uuid=?`, BackupRunning, uuid, server)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// FinishRestore records how a restore ended ("" failure = success).
func (s *Store) FinishRestore(ctx context.Context, uuid, failure string) error {
	st := BackupOK
	if failure != "" {
		st = BackupFailed
	}
	if len(failure) > 500 {
		failure = failure[:500]
	}
	_, err := s.exec(ctx, `UPDATE backups SET restore_status=?,restore_error=?,restored_at=? WHERE uuid=?`, st, failure, now(), uuid)
	return err
}

// BeginBackup records a running backup. It enforces one running backup per
// server and the per-server cap; with rotate it deletes the oldest unlocked
// backups to make room and returns their ids so the caller can remove the files.
func (s *Store) BeginBackup(ctx context.Context, b Backup, rotate bool) (evicted []Backup, err error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	running, err := count(ctx, tx, `SELECT COUNT(*) FROM backups WHERE server_uuid=? AND status=?`, b.ServerUUID, BackupRunning)
	if err != nil {
		return nil, err
	}
	if running > 0 {
		return nil, userErr("a backup is already running for this server")
	}
	total, err := count(ctx, tx, `SELECT COUNT(*) FROM backups WHERE server_uuid=?`, b.ServerUUID)
	if err != nil {
		return nil, err
	}
	limit, err := count(ctx, tx, `SELECT backup_limit FROM servers WHERE uuid=?`, b.ServerUUID)
	if err != nil {
		return nil, nf(err)
	}
	for total >= limit {
		if !rotate {
			return nil, ErrBackupLimit
		}
		var old Backup
		if tx.QueryRowContext(ctx, `SELECT uuid,disk FROM backups WHERE server_uuid=? AND locked=0 ORDER BY created_at, uuid LIMIT 1`, b.ServerUUID).Scan(&old.UUID, &old.Disk) != nil {
			return nil, ErrBackupLimit
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM backups WHERE uuid=?`, old.UUID); err != nil {
			return nil, err
		}
		evicted = append(evicted, old)
		total--
	}
	b.Status, b.CreatedAt = BackupRunning, now()
	if b.Disk == "" {
		b.Disk = "local"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO backups(uuid,server_uuid,name,status,ignore,disk,created_at) VALUES(?,?,?,?,?,?,?)`,
		b.UUID, b.ServerUUID, b.Name, b.Status, b.Ignore, b.Disk, b.CreatedAt); err != nil {
		return nil, err
	}
	return evicted, tx.Commit()
}

func (s *Store) FinishBackup(ctx context.Context, uuid string, size int64, sum, failure string) error {
	st := BackupOK
	if failure != "" {
		st = BackupFailed
	}
	if len(failure) > 500 {
		failure = failure[:500]
	}
	_, err := s.exec(ctx, `UPDATE backups SET status=?,error=?,size=?,sha256=?,completed_at=? WHERE uuid=?`, st, failure, size, sum, now(), uuid)
	return err
}

// FailStaleBackups marks backups and restores left "running" by a previous
// panel process.
func (s *Store) FailStaleBackups(ctx context.Context) error {
	if _, err := s.exec(ctx, `UPDATE backups SET status=?,error='interrupted by a panel restart',completed_at=? WHERE status=?`, BackupFailed, now(), BackupRunning); err != nil {
		return err
	}
	_, err := s.exec(ctx, `UPDATE backups SET restore_status=?,restore_error='interrupted by a panel restart',restored_at=? WHERE restore_status=?`, BackupFailed, now(), BackupRunning)
	return err
}

func (s *Store) GetBackup(ctx context.Context, server, uuid string) (Backup, error) {
	return scanBackup(s.RO.QueryRowContext(ctx, `SELECT `+backupCols+` FROM backups WHERE uuid=? AND server_uuid=?`, uuid, server))
}

func (s *Store) ListBackups(ctx context.Context, server string) ([]Backup, error) {
	return queryAll(ctx, s.RO, scanBackup, `SELECT `+backupCols+` FROM backups WHERE server_uuid=? ORDER BY created_at DESC, uuid`, server)
}

func (s *Store) SetBackupLocked(ctx context.Context, server, uuid string, locked bool) error {
	res, err := s.exec(ctx, `UPDATE backups SET locked=? WHERE uuid=? AND server_uuid=?`, locked, uuid, server)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteBackup(ctx context.Context, server, uuid string) error {
	_, err := s.exec(ctx, `DELETE FROM backups WHERE uuid=? AND server_uuid=?`, uuid, server)
	return err
}

// BackupsOnDisk lists a server's backups kept on disk ("s3"), for cleanup.
func (s *Store) BackupsOnDisk(ctx context.Context, server, disk string) ([]string, error) {
	return queryAll(ctx, s.RO, func(sc rowScanner) (string, error) {
		var id string
		err := sc.Scan(&id)
		return id, err
	}, `SELECT uuid FROM backups WHERE server_uuid=? AND disk=?`, server, disk)
}

// IgnoreList splits the stored newline-separated patterns.
func IgnoreList(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
