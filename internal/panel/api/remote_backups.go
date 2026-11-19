package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/s3"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

const diskS3 = "s3"

// uploadURLTimeout is how long a node may take to upload a backup's parts.
const uploadURLTimeout = 6 * time.Hour

// removeBackupFile deletes a backup's archive wherever it is.
func (a *App) removeBackupFile(ctx context.Context, s store.Server, b store.Backup) error {
	if b.Disk == diskS3 {
		if a.S3 == nil {
			return huma.Error503ServiceUnavailable("this backup is in S3 storage, which the panel is no longer set up for")
		}
		if err := a.S3.Delete(ctx, a.S3.Key(s.UUID, b.UUID)); err != nil {
			return hidden(http.StatusBadGateway, "The backup storage did not answer.", "s3 delete failed", err)
		}
		return nil
	}
	if _, err := a.Hub.Request(ctx, s.NodeID, proto.ReqBackupDelete, proto.BackupReq{UUID: s.UUID, BackupID: b.UUID}); err != nil {
		return nodeErr(err)
	}
	return nil
}

// removeRemoteBackups deletes a server's S3 archives (the server is going away).
func (a *App) removeRemoteBackups(ctx context.Context, server string) {
	if a.S3 == nil {
		return
	}
	ids, err := a.Store.BackupsOnDisk(ctx, server, diskS3)
	if err != nil {
		a.Log.Warn("could not list a deleted server's S3 backups", "server", server, "err", err)
		return
	}
	for _, id := range ids {
		if err := a.S3.Delete(ctx, a.S3.Key(server, id)); err != nil {
			a.Log.Warn("could not delete a deleted server's S3 backup", "server", server, "backup", id, "err", err)
		}
	}
}

// nodeBackup checks that a node's request is about a running S3 backup of a
// server on that node.
func (a *App) nodeBackup(r *http.Request, server, backup string) bool {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	node, err := a.Store.NodeByToken(r.Context(), tok)
	if tok == "" || err != nil {
		return false
	}
	s, err := a.Store.GetServer(r.Context(), server)
	if err != nil || s.NodeID != node.ID {
		return false
	}
	b, err := a.Store.GetBackup(r.Context(), server, backup)
	if err != nil || b.Status != store.BackupRunning || b.Disk != diskS3 {
		return false
	}
	return true
}

// backupUpload starts a multipart upload and hands the node presigned URLs.
func (a *App) backupUpload(w http.ResponseWriter, r *http.Request) {
	var in proto.BackupUploadReq
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil || in.Size < 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !a.nodeBackup(r, in.Server, in.BackupID) || a.S3 == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	key := a.S3.Key(in.Server, in.BackupID)
	id, err := a.S3.CreateUpload(r.Context(), key)
	if err != nil {
		a.Log.Warn("s3: could not start an upload", "backup", in.BackupID, "err", err)
		http.Error(w, "the backup storage did not answer", http.StatusBadGateway)
		return
	}
	a.uploads.Store(in.BackupID, id)
	size, urls := a.S3.PartURLs(key, id, in.Size, uploadURLTimeout)
	w.Header().Set("Content-Type", "application/json")
	writeBody(w, proto.BackupUpload{PartSize: size, URLs: urls})
}

// backupComplete finishes (or aborts) a node's multipart upload.
func (a *App) backupComplete(w http.ResponseWriter, r *http.Request) {
	var in proto.BackupCompleteReq
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&in) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	v, found := a.uploads.LoadAndDelete(in.BackupID)
	if !a.nodeBackup(r, in.Server, in.BackupID) || !found || a.S3 == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	id, ok := v.(string)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	key := a.S3.Key(in.Server, in.BackupID)
	if in.Failed {
		if err := a.S3.AbortUpload(r.Context(), key, id); err != nil {
			a.Log.Warn("s3: could not abort an upload", "backup", in.BackupID, "err", err)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	parts := make([]s3.Part, len(in.Parts))
	for i, p := range in.Parts {
		parts[i] = s3.Part{Number: p.Number, ETag: p.ETag}
	}
	if err := a.S3.CompleteUpload(r.Context(), key, id, parts); err != nil {
		a.Log.Warn("s3: could not complete an upload", "backup", in.BackupID, "err", err)
		if err := a.S3.AbortUpload(r.Context(), key, id); err != nil {
			a.Log.Warn("s3: could not abort an upload", "backup", in.BackupID, "err", err)
		}
		http.Error(w, "the backup storage refused the upload", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// safeFileName keeps a backup's name usable as a download file name.
func safeFileName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`"\/:*?<>|`, r) {
			return '_'
		}
		return r
	}, s)
	if s == "" {
		return "backup"
	}
	return s
}
