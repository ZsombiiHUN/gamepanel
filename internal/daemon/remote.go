package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// Remote backups: the node never holds storage credentials. It asks the panel
// for presigned URLs, uploads the parts, and reports their ETags back.

// postPanel sends JSON to the panel's daemon endpoints with the node token.
func (d *Daemon) postPanel(ctx context.Context, path string, in, out any) error {
	u, err := panelHTTP(d.cfg.PanelURL)
	if err != nil {
		return err
	}
	u.Path = path
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+d.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.relayClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("panel answered %s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// uploadBackup sends a finished local archive to remote storage and removes
// the local copy once the panel confirmed the upload.
func (d *Daemon) uploadBackup(ctx context.Context, b proto.BackupReq) (err error) {
	path := d.backupPath(b.UUID, b.BackupID)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	var up proto.BackupUpload
	if err := d.postPanel(ctx, "/daemon/backups/upload", proto.BackupUploadReq{Server: b.UUID, BackupID: b.BackupID, Size: fi.Size()}, &up); err != nil {
		return fmt.Errorf("could not start the upload: %w", err)
	}
	complete := proto.BackupCompleteReq{Server: b.UUID, BackupID: b.BackupID}
	defer func() {
		if err != nil {
			complete.Failed, complete.Parts = true, nil
			c, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
			_ = d.postPanel(c, "/daemon/backups/complete", complete, nil)
			cancel()
		}
	}()
	if up.PartSize <= 0 || len(up.URLs) == 0 {
		return errors.New("the panel sent no upload URLs")
	}
	for i, target := range up.URLs {
		off := int64(i) * up.PartSize
		n := min(up.PartSize, fi.Size()-off)
		if n < 0 {
			n = 0
		}
		etag, err := d.putPart(ctx, target, io.NewSectionReader(f, off, n), n)
		if err != nil {
			return fmt.Errorf("uploading part %d of %d: %w", i+1, len(up.URLs), err)
		}
		complete.Parts = append(complete.Parts, proto.UploadPart{Number: i + 1, ETag: etag})
	}
	if err := d.postPanel(ctx, "/daemon/backups/complete", complete, nil); err != nil {
		return fmt.Errorf("could not finish the upload: %w", err)
	}
	return os.Remove(path)
}

// putPart uploads one part, retrying a few times: one dropped connection
// should not fail a two-hour backup.
func (d *Daemon) putPart(ctx context.Context, target string, r *io.SectionReader, n int64) (string, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 && !sleep(ctx, time.Duration(attempt)*2*time.Second) {
			return "", ctx.Err()
		}
		r.Seek(0, io.SeekStart)
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, r)
		if err != nil {
			return "", err
		}
		req.ContentLength = n
		resp, err := d.relayClient.Do(req)
		if err != nil {
			last = err
			continue
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		if resp.StatusCode/100 == 2 && resp.Header.Get("ETag") != "" {
			return resp.Header.Get("ETag"), nil
		}
		last = fmt.Errorf("storage answered %s", resp.Status)
		if resp.StatusCode/100 == 4 && resp.StatusCode != http.StatusRequestTimeout {
			break // a signature or permission problem does not fix itself
		}
	}
	return "", last
}

// fetchBackup downloads a remote archive into a temporary file next to the
// local backups, for a restore. The caller removes it.
func (d *Daemon) fetchBackup(ctx context.Context, b proto.BackupReq) (string, error) {
	dir := filepath.Join(d.cfg.DataDir, "backups", b.UUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp := filepath.Join(dir, b.BackupID+".download")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.URL, nil)
	if err == nil {
		var resp *http.Response
		resp, err = d.relayClient.Do(req)
		if err == nil {
			if resp.StatusCode/100 != 2 {
				err = fmt.Errorf("storage answered %s", resp.Status)
			} else {
				_, err = io.Copy(f, resp.Body)
			}
			resp.Body.Close()
		}
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("could not download the backup: %w", err)
	}
	return tmp, nil
}
