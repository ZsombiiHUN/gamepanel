package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// The relay moves file bytes between a browser and a node without the
// control websocket and without the node accepting connections: the panel
// hands the node a one-time ticket, and the node connects back to
// /daemon/relay/<ticket> with its token. Uploads stream the browser's body
// to the node; downloads stream the node's body to the browser.

// relayAttachTimeout is how long a ticket waits for its node to connect.
const relayAttachTimeout = time.Minute

// transferTimeout bounds one upload or download.
const transferTimeout = 12 * time.Hour

type relayTicket struct {
	node     int64
	upload   io.Reader           // the browser's body (uploads)
	download http.ResponseWriter // the browser's response (downloads)

	once     sync.Once
	attached chan struct{} // closed when the node connected
	done     chan error    // the copy's outcome, once attached
}

func (a *App) newTicket(node int64) (string, *relayTicket) {
	b := make([]byte, 32)
	rand.Read(b)
	id := base64.RawURLEncoding.EncodeToString(b)
	t := &relayTicket{node: node, attached: make(chan struct{}), done: make(chan error, 1)}
	a.relays.Store(id, t)
	time.AfterFunc(relayAttachTimeout, func() { a.relays.Delete(id) })
	return id, t
}

// relayHandler is where nodes connect for a ticket.
func (a *App) relayHandler(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	node, err := a.Store.NodeByToken(r.Context(), tok)
	if tok == "" || err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	v, ok := a.relays.LoadAndDelete(r.PathValue("ticket")) // single use
	t, _ := v.(*relayTicket)
	if !ok || t.node != node.ID {
		// Unknown, used, expired, or another node's ticket: all look the same.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	t.once.Do(func() { close(t.attached) })
	switch {
	case r.Method == http.MethodGet && t.upload != nil:
		w.Header().Set("Content-Type", "application/octet-stream")
		_, err := io.Copy(w, t.upload)
		t.done <- err
	case r.Method == http.MethodPost && t.download != nil:
		bw := t.download
		h := bw.Header()
		h.Set("Content-Type", "application/octet-stream")
		name := r.Header.Get(proto.HeaderRelayName)
		if name == "" {
			name = "download"
		}
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		if n, err := strconv.ParseInt(r.Header.Get(proto.HeaderRelaySize), 10, 64); err == nil && n >= 0 {
			h.Set("Content-Length", strconv.FormatInt(n, 10))
		}
		// Downloads are files from a game server: never let a browser render one.
		h.Set("Content-Security-Policy", "sandbox")
		bw.WriteHeader(http.StatusOK)
		_, err := io.Copy(bw, r.Body)
		t.done <- err
		w.WriteHeader(http.StatusNoContent)
	default:
		t.done <- errors.New("relay used the wrong way")
		http.Error(w, "bad request", http.StatusBadRequest)
	}
}

// transferCtx outlives the hub's default request timeout: a transfer lasts as
// long as the bytes take, but still ends when the browser goes away.
func transferCtx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), transferTimeout)
}

// writeErr answers a plain (non-huma) handler with err as a problem reply.
func writeErr(w http.ResponseWriter, err error) {
	var se huma.StatusError
	if errors.As(err, &se) {
		msg := err.Error()
		if m, ok := err.(*huma.ErrorModel); ok {
			msg = m.Detail
		}
		writeProblem(w, se.GetStatus(), msg)
		return
	}
	writeProblem(w, http.StatusInternalServerError, "Something went wrong on the panel.")
}

// downloadFile streams a server file to the browser.
func (a *App) downloadFile(w http.ResponseWriter, r *http.Request) {
	_, s, err := a.authorizeP(r.Context(), r.PathValue("uuid"), PermFilesRead)
	if err != nil {
		writeErr(w, err)
		return
	}
	p := r.URL.Query().Get("path")
	if p == "" || len(p) > 4096 {
		writeProblem(w, http.StatusUnprocessableEntity, "choose a file to download")
		return
	}
	a.relayDownload(w, r, s.NodeID, proto.ReqFilesDownload, proto.Transfer{UUID: s.UUID, Path: p})
}

// downloadBackup streams a backup archive to the browser.
func (a *App) downloadBackup(w http.ResponseWriter, r *http.Request) {
	u, s, err := a.authorizeP(r.Context(), r.PathValue("uuid"), PermBackupsDownload)
	if err != nil {
		writeErr(w, err)
		return
	}
	b, err := a.Store.GetBackup(r.Context(), s.UUID, r.PathValue("backup"))
	if err != nil {
		writeProblem(w, http.StatusNotFound, "backup not found")
		return
	}
	if b.Status != "ok" {
		writeProblem(w, http.StatusConflict, "only a completed backup can be downloaded")
		return
	}
	a.audit(r.Context(), u, "backup.download", s.UUID, b.Name)
	if b.Disk == diskS3 {
		if a.S3 == nil {
			writeProblem(w, http.StatusServiceUnavailable, "this backup is in S3 storage, which the panel is no longer set up for")
			return
		}
		// Straight from the bucket: a short-lived link for this one file.
		http.Redirect(w, r, a.S3.DownloadURL(a.S3.Key(s.UUID, b.UUID), safeFileName(b.Name)+".tar.gz", 15*time.Minute), http.StatusFound)
		return
	}
	a.relayDownload(w, r, s.NodeID, proto.ReqBackupDownload, proto.Transfer{UUID: s.UUID, BackupID: b.UUID})
}

func (a *App) relayDownload(w http.ResponseWriter, r *http.Request, node int64, typ string, t proto.Transfer) {
	ctx, cancel := transferCtx(r)
	defer cancel()
	id, tk := a.newTicket(node)
	tk.download = w
	t.Ticket = id
	errc := make(chan error, 1)
	go func() {
		_, err := a.Hub.Request(ctx, node, typ, t)
		errc <- err
	}()
	select {
	case err := <-errc:
		// The node answered without (or after) streaming. If it streamed, the
		// copy has finished: the node replies only once its upload ended.
		select {
		case <-tk.attached:
			<-tk.done
		default:
			a.relays.Delete(id)
			if err == nil {
				err = errors.New("the node did not send the file")
			}
			writeErr(w, nodeErr(err))
		}
	case <-tk.attached:
		<-tk.done // the response belongs to the relay until the copy ends
	}
}

// uploadFile streams the request body into a file on the server.
func (a *App) uploadFile(w http.ResponseWriter, r *http.Request) {
	u, s, err := a.authorizeP(r.Context(), r.PathValue("uuid"), PermFilesWrite)
	if err != nil {
		writeErr(w, err)
		return
	}
	p := r.URL.Query().Get("path")
	if p == "" || strings.HasSuffix(p, "/") || len(p) > 4096 {
		writeProblem(w, http.StatusUnprocessableEntity, "give the file a name")
		return
	}
	if r.ContentLength < 0 {
		writeProblem(w, http.StatusLengthRequired, "the upload needs a Content-Length")
		return
	}
	ctx, cancel := transferCtx(r)
	defer cancel()
	id, tk := a.newTicket(s.NodeID)
	tk.upload = r.Body
	raw, err := a.Hub.Request(ctx, s.NodeID, proto.ReqFilesUpload, proto.Transfer{UUID: s.UUID, Path: p, Ticket: id, Size: r.ContentLength})
	select {
	case <-tk.attached:
		<-tk.done // never return while the relay still reads the body
	default:
		a.relays.Delete(id)
	}
	if err != nil {
		writeErr(w, nodeErr(err))
		return
	}
	a.audit(r.Context(), u, "file.upload", s.UUID, p+" ("+strconv.FormatInt(r.ContentLength, 10)+" bytes)")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write(raw)
}

// maxEditBytes is the largest file the editor opens or saves, as on the node.
const maxEditBytes = 10 << 20

var errEditTooLarge = errors.New("file too large to edit")

// capture is a ResponseWriter that keeps a relayed file in memory, for the
// editor. It refuses to grow past max.
type capture struct {
	h   http.Header
	buf bytes.Buffer
	max int
}

func (c *capture) Header() http.Header { return c.h }
func (c *capture) WriteHeader(int)     {}
func (c *capture) Write(p []byte) (int, error) {
	if c.buf.Len()+len(p) > c.max {
		return 0, errEditTooLarge
	}
	return c.buf.Write(p)
}

// readSmall fetches a file for the editor through the relay, so its bytes
// never travel on the node's control connection, where they would hold up
// every other server's console and stats.
func (a *App) readSmall(ctx context.Context, node int64, uuid, p string) ([]byte, error) {
	id, tk := a.newTicket(node)
	c := &capture{h: http.Header{}, max: maxEditBytes}
	tk.download = c
	_, err := a.Hub.Request(ctx, node, proto.ReqFilesDownload, proto.Transfer{UUID: uuid, Path: p, Ticket: id, Max: maxEditBytes})
	select {
	case <-tk.attached:
		// The node replies only after its upload ended, so the copy is done
		// or about to be.
		select {
		case cerr := <-tk.done:
			if errors.Is(cerr, errEditTooLarge) {
				return nil, huma.Error422UnprocessableEntity(errEditTooLarge.Error())
			}
			if err == nil {
				err = cerr
			}
		case <-ctx.Done():
			return nil, nodeErr(hub.ErrTimeout)
		}
	default:
		a.relays.Delete(id)
		if err == nil {
			err = errors.New("the node did not send the file")
		}
	}
	if err != nil {
		return nil, nodeErr(err)
	}
	return c.buf.Bytes(), nil
}

// writeSmall saves an editor file through the relay (see readSmall).
func (a *App) writeSmall(ctx context.Context, node int64, uuid, p string, content []byte) error {
	id, tk := a.newTicket(node)
	tk.upload = bytes.NewReader(content)
	_, err := a.Hub.Request(ctx, node, proto.ReqFilesUpload, proto.Transfer{UUID: uuid, Path: p, Ticket: id, Size: int64(len(content))})
	select {
	case <-tk.attached:
		select {
		case <-tk.done:
		case <-ctx.Done():
		}
	default:
		a.relays.Delete(id)
	}
	return nodeErr(err)
}
