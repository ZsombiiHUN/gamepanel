package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// Error responses never carry internal detail (database, file system, node or
// library messages). Messages written for people pass through; anything else
// is logged with a short reference, and the client gets a generic message
// holding that reference so an admin can find the log line.

// errLog is where hidden errors are logged; set by New.
var errLog atomic.Pointer[slog.Logger]

func init() { huma.NewError = publicError }

// publicError replaces huma's error constructor. Every 500 is hidden behind a
// reference; other statuses keep their message (written by us, or huma's own
// input validation) but drop wrapped errors that are not validation details.
func publicError(status int, msg string, errs ...error) huma.StatusError {
	if status == http.StatusInternalServerError {
		return hidden(status, "Something went wrong on the panel.", msg, errs...)
	}
	var details []*huma.ErrorDetail
	for _, e := range errs {
		if d, ok := e.(huma.ErrorDetailer); ok {
			details = append(details, d.ErrorDetail())
		}
	}
	return &huma.ErrorModel{Status: status, Title: http.StatusText(status), Detail: msg, Errors: details}
}

// hidden logs msg and errs under a fresh reference and returns public plus
// that reference.
func hidden(status int, public, msg string, errs ...error) *huma.ErrorModel {
	ref := logRef(msg, errs...)
	return &huma.ErrorModel{Status: status, Title: http.StatusText(status), Detail: public + " Reference: " + ref + "."}
}

// logRef logs msg and errs under a fresh reference and returns it.
func logRef(msg string, errs ...error) string {
	ref := newRef()
	args := []any{"ref", ref}
	for _, e := range errs {
		if e != nil {
			args = append(args, "err", e.Error())
		}
	}
	if msg == "" {
		msg = "request failed"
	}
	log := errLog.Load()
	if log == nil {
		log = slog.Default()
	}
	log.Error(msg, args...)
	return ref
}

func newRef() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// userErr answers with status when err's message is written for people
// (see safeMessage), 404 for store.ErrNotFound, and a hidden 500 otherwise.
func userErr(status int, err error) error {
	if msg, ok := safeMessage(err); ok {
		return huma.NewError(status, msg)
	}
	if errors.Is(err, store.ErrNotFound) {
		return huma.Error404NotFound("not found")
	}
	return huma.Error500InternalServerError("", err)
}

// writeProblem writes a problem+json reply outside huma (middleware, raw handlers).
func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	writeBody(w, huma.ErrorModel{Status: status, Title: http.StatusText(status), Detail: detail})
}

// writeBody encodes v as JSON after the status line has been sent. A failure
// here means the client went away mid-response; there is nobody left to tell,
// so the error is dropped on purpose.
func writeBody(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v) //nolint:errchkjson // see above: the client is gone
}

// safeMessage returns err's message when it was written for people: a
// store.UserError or capacity error, a coded daemon error, or a node that is
// offline or slow.
func safeMessage(err error) (string, bool) {
	var ue *store.UserError
	var ce store.CapacityError
	switch {
	case err == nil:
		return "", false
	case errors.As(err, &ue):
		return ue.Msg, true
	case errors.As(err, &ce):
		return ce.Msg, true
	case errors.Is(err, hub.ErrNodeOffline):
		return "the node is offline", true
	case errors.Is(err, hub.ErrTimeout):
		return "the node did not reply in time", true
	case proto.CodeOf(err) != "":
		return err.Error(), true
	}
	return "", false
}

// failureText is the text stored for a failed background job (backup,
// restore, scheduled task), which users read later: err's own message when it
// is safe, otherwise a reference to the logged error.
func failureText(err error) string {
	if msg, ok := safeMessage(err); ok {
		return msg
	}
	return "unexpected error, reference " + logRef("background job failed", err)
}
