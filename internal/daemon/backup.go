package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/fsx"
	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// maxRestoreBytes caps how much a restore may write (zip-bomb guard).
const maxRestoreBytes = 200 << 30

func (d *Daemon) backupPath(server, id string) string {
	return filepath.Join(d.cfg.DataDir, "backups", server, id+".tar.gz")
}

func (d *Daemon) backup(ctx context.Context, typ string, b proto.BackupReq) (json.RawMessage, error) {
	if !uuidRe.MatchString(b.UUID) || !uuidRe.MatchString(b.BackupID) {
		return nil, proto.Invalid("invalid backup id")
	}
	if _, err := d.spec(b.UUID); err != nil {
		return nil, err
	}
	switch typ {
	case proto.ReqBackupCreate:
		end, err := d.begin(b.UUID, opBackup)
		if err != nil {
			return nil, err
		}
		defer end()
		res, err := d.backupCreate(b)
		if err == nil && b.Remote {
			if err = d.uploadBackup(ctx, b); err != nil {
				os.Remove(d.backupPath(b.UUID, b.BackupID))
			}
		}
		return proto.Marshal(res), err
	case proto.ReqBackupRestore:
		return nil, d.backupRestore(ctx, b)
	default:
		err := os.Remove(d.backupPath(b.UUID, b.BackupID))
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		return nil, err
	}
}

// backupCreate streams an archive to a temp file (hashing as it goes) and
// moves it into place only once complete, so a failed backup leaves nothing.
func (d *Daemon) backupCreate(b proto.BackupReq) (res proto.BackupResult, err error) {
	dir, err := d.dir(b.UUID)
	if err != nil {
		return res, err
	}
	defer dir.Close()
	final := d.backupPath(b.UUID, b.BackupID)
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return res, err
	}
	tmp := final + ".part"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return res, err
	}
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(tmp)
		}
	}()
	h := sha256.New()
	cw := &countWriter{w: io.MultiWriter(f, h)}
	if err = dir.Archive(cw, b.Ignore); err != nil {
		return res, err
	}
	if err = f.Sync(); err != nil {
		return res, err
	}
	if err = os.Rename(tmp, final); err != nil {
		return res, err
	}
	return proto.BackupResult{Size: cw.n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// backupRestore unpacks an archive over the server's files. The server must be
// stopped: restoring under a running game corrupts it.
func (d *Daemon) backupRestore(ctx context.Context, b proto.BackupReq) error {
	end, err := d.begin(b.UUID, opRestore)
	if err != nil {
		return err
	}
	defer end()
	if st, _ := d.rt.State(ctx, b.UUID); st != runtime.StateOffline {
		return proto.Conflict("stop the server before restoring a backup")
	}
	path := d.backupPath(b.UUID, b.BackupID)
	if b.URL != "" {
		if path, err = d.fetchBackup(ctx, b); err != nil {
			return err
		}
		defer os.Remove(path)
	}
	f, err := os.Open(path)
	if err != nil {
		return proto.NotFound("backup file not found on the node")
	}
	defer f.Close()
	dir, err := d.dir(b.UUID)
	if err != nil {
		return err
	}
	defer dir.Close()
	limit := int64(maxRestoreBytes)
	if room := d.diskHeadroom(b.UUID); room >= 0 {
		if b.Truncate {
			room = d.diskLimitBytes(b.UUID) // everything is deleted first
		}
		limit = min(limit, room)
		if limit == 0 {
			limit = 1 // 0 would mean unlimited to Extract; 1 byte stops it at the first file
		}
	}
	// Extract checks the whole archive (integrity, paths, size) before it
	// changes anything, so a failure here leaves the server as it was.
	err = dir.Extract(f, b.Truncate, limit)
	if errors.Is(err, fsx.ErrArchiveTooLarge) && d.diskLimitBytes(b.UUID) > 0 {
		err = &proto.Error{Code: proto.CodeDiskFull, Msg: "restoring this backup would exceed the server's disk limit"}
	}
	d.cacheFor(b.UUID).invalidate()
	return err
}
