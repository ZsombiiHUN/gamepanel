package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// captureLog points the hidden-error log at a buffer for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := errLog.Swap(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { errLog.Store(prev) })
	return &buf
}

func detailOf(t *testing.T, err error) (int, string) {
	t.Helper()
	var se huma.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("not a status error: %v", err)
	}
	m, ok := se.(*huma.ErrorModel)
	if !ok {
		t.Fatalf("unexpected error type %T", se)
	}
	return m.Status, m.Detail
}

// Internal failures (database, file system, node) are logged with a reference
// and the client sees only a generic message holding that reference.
func TestInternalErrorsAreHiddenBehindAReference(t *testing.T) {
	log := captureLog(t)
	secret := fmt.Errorf("insert subusers: %w", sql.ErrConnDone)

	for name, err := range map[string]error{
		"userErr":  userErr(http.StatusUnprocessableEntity, secret),
		"huma 500": huma.Error500InternalServerError("db error", secret),
		"nodeErr":  nodeErr(errors.New("openat /var/lib/gamepanel/servers/x/world: permission denied")),
	} {
		status, detail := detailOf(t, err)
		if status < 500 {
			t.Errorf("%s: status %d", name, status)
		}
		for _, leak := range []string{"sql", "insert", "/var/lib", "permission denied", "db error"} {
			if strings.Contains(detail, leak) {
				t.Errorf("%s: %q reached the client: %s", name, leak, detail)
			}
		}
		ref, ok := strings.CutPrefix(detail[strings.Index(detail, "Reference: "):], "Reference: ")
		ref = strings.TrimSuffix(ref, ".")
		if !ok || len(ref) != 8 || !strings.Contains(log.String(), "ref="+ref) {
			t.Errorf("%s: reference %q missing from the reply or the log\nreply: %s\nlog: %s", name, ref, detail, log)
		}
	}
	if !strings.Contains(log.String(), "sql: connection is already closed") {
		t.Errorf("the real error was not logged:\n%s", log)
	}

	// Errors huma wraps itself (a handler returning a plain error) lose their text too.
	b, _ := json.Marshal(huma.NewError(http.StatusInternalServerError, "unexpected error occurred", secret))
	if strings.Contains(string(b), "sql") || strings.Contains(string(b), `"errors"`) {
		t.Fatalf("wrapped error reached the client: %s", b)
	}
}

func TestMessagesWrittenForPeoplePassThrough(t *testing.T) {
	captureLog(t)
	cases := []struct {
		err    error
		status int
		want   string
	}{
		{userErr(http.StatusUnprocessableEntity, &store.UserError{Msg: "port must be within the node range 25565-25600"}), 422, "port must be within the node range 25565-25600"},
		{userErr(http.StatusConflict, store.ErrCapacity{Msg: "this node has 512 of 4096 MB memory left"}), 409, "this node has 512 of 4096 MB memory left"},
		{userErr(http.StatusUnprocessableEntity, store.ErrNotFound), 404, "not found"},
		{nodeErr(proto.Conflict("stop the server first")), 409, "stop the server first"},
		{nodeErr(&proto.Error{Code: proto.CodeDiskFull, Msg: "disk limit reached"}), 507, "disk limit reached"},
		{nodeErr(proto.NotFound("no such file or folder")), 404, "no such file or folder"},
		{nodeErr(proto.Invalid("that is a folder, not a file")), 422, "that is a folder, not a file"},
		{nodeErr(hub.ErrNodeOffline), 503, "the node is offline"},
		{nodeErr(hub.ErrTimeout), 504, "the node did not reply in time"},
	}
	for _, c := range cases {
		status, detail := detailOf(t, c.err)
		if status != c.status || detail != c.want {
			t.Errorf("got %d %q, want %d %q", status, detail, c.status, c.want)
		}
	}

	// Input validation details stay: they name the field that is wrong.
	err := huma.NewError(422, "validation failed", &huma.ErrorDetail{Message: "expected string", Location: "body.name"})
	if b, _ := json.Marshal(err); !strings.Contains(string(b), "body.name") {
		t.Fatalf("validation details were dropped: %s", b)
	}
}

// Failures of background jobs are stored and shown later: same rule.
func TestStoredFailuresDoNotLeak(t *testing.T) {
	log := captureLog(t)
	if got := failureText(proto.Conflict("stop the server before restoring a backup")); got != "stop the server before restoring a backup" {
		t.Fatalf("safe message changed: %q", got)
	}
	got := failureText(errors.New("gzip: invalid header at /var/lib/gamepanel/backups/x.tar.gz"))
	if strings.Contains(got, "gzip") || strings.Contains(got, "/var/lib") || !strings.Contains(got, "reference") {
		t.Fatalf("stored failure leaks or has no reference: %q", got)
	}
	if !strings.Contains(log.String(), "gzip: invalid header") {
		t.Fatal("the real error was not logged")
	}
}

func TestWriteProblemIsProblemJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeProblem(rec, http.StatusForbidden, "blocked")
	var m huma.ErrorModel
	if rec.Header().Get("Content-Type") != "application/problem+json" || json.Unmarshal(rec.Body.Bytes(), &m) != nil || m.Detail != "blocked" || m.Status != 403 {
		t.Fatalf("got %q %s", rec.Header().Get("Content-Type"), rec.Body)
	}
}
