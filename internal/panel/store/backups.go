package store

import (
	"context"
	"errors"
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

var ErrBackupLimit = errors.New("backup limit reached; delete one or unlock one for rotation")

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
	CreatedAt   int64  `json:"created_at"`
	CompletedAt int64  `json:"completed_at"`
}

const backupCols = `uuid,server_uuid,name,status,error,size,sha256,ignore,locked,created_at,completed_at`

func scanBackup(sc interface{ Scan(...any) error }) (Backup, error) {
	var b Backup
	err := sc.Scan(&b.UUID, &b.ServerUUID, &b.Name, &b.Status, &b.Error, &b.Size, &b.SHA256, &b.Ignore, &b.Locked, &b.CreatedAt, &b.CompletedAt)
	return b, nf(err)
}

// BeginBackup records a running backup. It enforces one running backup per
// server and the per-server cap; with rotate it deletes the oldest unlocked
// backups to make room and returns their ids so the caller can remove the files.
func (s *Store) BeginBackup(ctx context.Context, b Backup, rotate bool) (evicted []string, err error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var running, total int
	tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM backups WHERE server_uuid=? AND status=?`, b.ServerUUID, BackupRunning).Scan(&running)
	if running > 0 {
		return nil, errors.New("a backup is already running for this server")
	}
	tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM backups WHERE server_uuid=?`, b.ServerUUID).Scan(&total)
	limit := DefaultBackupLimit
	tx.QueryRowContext(ctx, `SELECT backup_limit FROM servers WHERE uuid=?`, b.ServerUUID).Scan(&limit)
	for total >= limit {
		if !rotate {
			return nil, ErrBackupLimit
		}
		var id string
		if tx.QueryRowContext(ctx, `SELECT uuid FROM backups WHERE server_uuid=? AND locked=0 ORDER BY created_at, uuid LIMIT 1`, b.ServerUUID).Scan(&id) != nil {
			return nil, ErrBackupLimit
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM backups WHERE uuid=?`, id); err != nil {
			return nil, err
		}
		evicted = append(evicted, id)
		total--
	}
	b.Status, b.CreatedAt = BackupRunning, now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO backups(`+backupCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		b.UUID, b.ServerUUID, b.Name, b.Status, "", 0, "", b.Ignore, false, b.CreatedAt, 0); err != nil {
		return nil, err
	}
	return evicted, tx.Commit()
}

func (s *Store) FinishBackup(ctx context.Context, uuid string, size int64, sum string, failure string) error {
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

// FailStaleBackups marks backups left "running" by a previous panel process.
func (s *Store) FailStaleBackups(ctx context.Context) error {
	_, err := s.exec(ctx, `UPDATE backups SET status=?,error='interrupted by a panel restart',completed_at=? WHERE status=?`, BackupFailed, now(), BackupRunning)
	return err
}

func (s *Store) GetBackup(ctx context.Context, server, uuid string) (Backup, error) {
	return scanBackup(s.DB.QueryRowContext(ctx, `SELECT `+backupCols+` FROM backups WHERE uuid=? AND server_uuid=?`, uuid, server))
}

func (s *Store) ListBackups(ctx context.Context, server string) ([]Backup, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+backupCols+` FROM backups WHERE server_uuid=? ORDER BY created_at DESC, uuid`, server)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Backup{}
	for rows.Next() {
		b, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
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
