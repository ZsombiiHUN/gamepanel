package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// hookRuntime is the fake runtime with optional failures and panics.
type hookRuntime struct {
	*runtime.Fake
	removeErr error
	stopPanic bool
}

func (h *hookRuntime) Remove(ctx context.Context, uuid string) error {
	if h.removeErr != nil {
		return h.removeErr
	}
	return h.Fake.Remove(ctx, uuid)
}

func (h *hookRuntime) Stop(ctx context.Context, uuid string, timeout int) error {
	if h.stopPanic {
		panic("boom")
	}
	return h.Fake.Stop(ctx, uuid, timeout)
}

func newTestDaemon(t testing.TB, rt runtime.Runtime) *Daemon {
	t.Helper()
	d, err := New(Config{DataDir: t.TempDir(), DiskCacheTTL: time.Minute}, rt, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func uuidN(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }

func addServer(t testing.TB, d *Daemon, uuid string, diskMB int64) {
	t.Helper()
	if err := d.create(context.Background(), proto.ServerSpec{UUID: uuid, Image: "img", Startup: "run", Stop: "^C", MemoryMB: 128, DiskMB: diskMB}); err != nil {
		t.Fatal(err)
	}
	// The container is built in the background; wait until it is ready.
	if !eventually(5*time.Second, func() bool { st, _ := d.installState(uuid); return st == "" }) {
		t.Fatal("server never finished provisioning")
	}
}

func req(typ string, v any) proto.Envelope { return proto.Envelope{Type: typ, Data: proto.Marshal(v)} }

func TestOperationsThatConflictAreRefused(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 0)

	// While one exclusive operation runs, every other one is refused with a
	// conflict (409 at the API); a backup may run alongside it.
	for _, held := range []string{opStart, opUpdate, opRestore, opDelete} {
		end, err := d.begin(id, held)
		if err != nil {
			t.Fatal(err)
		}
		for _, op := range []string{opStart, opUpdate, opInstall, opRestore, opDelete} {
			if _, err := d.begin(id, op); proto.CodeOf(err) != proto.CodeConflict {
				t.Errorf("%s while %s: got %v, want a conflict", op, held, err)
			}
		}
		endB, err := d.begin(id, opBackup)
		switch {
		case held == opRestore || held == opDelete:
			if err == nil {
				t.Errorf("a backup was allowed during %s", held)
				endB()
			}
		case err != nil:
			t.Errorf("a backup was refused during %s: %v", held, err)
		default:
			endB()
		}
		end()
	}

	// A running backup keeps restores and deletes out, nothing else.
	endB, _ := d.begin(id, opBackup)
	for _, op := range []string{opRestore, opDelete} {
		if _, err := d.begin(id, op); err == nil {
			t.Errorf("%s was allowed during a backup", op)
		}
	}
	endS, err := d.begin(id, opStart)
	if err != nil {
		t.Errorf("start was refused during a backup: %v", err)
	} else {
		endS()
	}
	endB()

	// Released claims free the server again.
	end, err := d.begin(id, opRestore)
	if err != nil {
		t.Fatalf("server still busy after every claim was released: %v", err)
	}
	end()
	if _, err := d.begin(uuidN(99), opStart); !errors.Is(err, errUnknownServer) {
		t.Fatalf("unknown server: %v", err)
	}
}

// A restore must not run while the server is being started (and vice versa):
// the "is it offline?" check and the action are one claim.
func TestRestoreIsRefusedWhileStarting(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 0)
	end, _ := d.begin(id, opStart)
	defer end()
	_, err := d.dispatch(context.Background(), req(proto.ReqBackupRestore, proto.BackupReq{UUID: id, BackupID: uuidN(2)}))
	if proto.CodeOf(err) != proto.CodeConflict {
		t.Fatalf("restore during a start: %v", err)
	}
}

func TestAPanickingRequestDoesNotKillTheDaemon(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, &hookRuntime{Fake: runtime.NewFake(), stopPanic: true})
	id := uuidN(1)
	addServer(t, d, id, 0)
	if _, err := d.dispatch(context.Background(), req(proto.ReqServerPower, proto.Power{UUID: id, Action: "start"})); err != nil {
		t.Fatal(err)
	}
	_, err := d.safeDispatch(context.Background(), req(proto.ReqServerPower, proto.Power{UUID: id, Action: "stop"}))
	if err == nil || !strings.Contains(err.Error(), "internal error") {
		t.Fatalf("expected an internal error reply, got %v", err)
	}
}

// Requests run concurrently. Deleting a server while other requests for it are
// in flight used to dereference a server that was gone and crash the daemon.
func TestConcurrentRequestsDuringDelete(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	ctx := context.Background()
	for i := range 30 {
		id := uuidN(i)
		addServer(t, d, id, 0)
		spec, _ := d.spec(id)
		reqs := []proto.Envelope{
			req(proto.ReqServerDelete, proto.UUIDOnly{UUID: id}),
			req(proto.ReqServerPower, proto.Power{UUID: id, Action: "start"}),
			req(proto.ReqServerUpdate, spec),
			req(proto.ReqServerReinstall, proto.UUIDOnly{UUID: id}),
			req(proto.ReqBackupCreate, proto.BackupReq{UUID: id, BackupID: uuidN(1000 + i)}),
			req(proto.ReqBackupRestore, proto.BackupReq{UUID: id, BackupID: uuidN(1000 + i)}),
			req(proto.ReqFilesWrite, proto.FileReq{UUID: id, Path: "a.txt", Content: []byte("x")}),
		}
		var wg sync.WaitGroup
		for _, r := range reqs {
			wg.Go(func() {
				if _, err := d.safeDispatch(ctx, r); err != nil && strings.Contains(err.Error(), "internal error") {
					t.Errorf("%s panicked during a concurrent delete", r.Type)
				}
			})
		}
		wg.Wait()
	}
}

// If the container cannot be removed, the server's files must stay: deleting
// the data while the panel forgets the server would orphan a running container.
func TestDeleteKeepsDataWhenTheContainerCannotBeRemoved(t *testing.T) {
	t.Parallel()
	rt := &hookRuntime{Fake: runtime.NewFake(), removeErr: errors.New("engine unavailable")}
	d := newTestDaemon(t, rt)
	id := uuidN(1)
	addServer(t, d, id, 0)
	os.WriteFile(d.serverDir(id)+"/world.dat", []byte("w"), 0o644)
	if _, err := d.dispatch(context.Background(), req(proto.ReqServerDelete, proto.UUIDOnly{UUID: id})); err == nil {
		t.Fatal("delete reported success although the container is still there")
	}
	if _, err := os.Stat(d.serverDir(id) + "/world.dat"); err != nil {
		t.Fatal("the server's files were deleted")
	}
	if _, err := d.spec(id); err != nil {
		t.Fatal("the daemon forgot a server whose container still exists")
	}
	rt.removeErr = nil
	if _, err := d.dispatch(context.Background(), req(proto.ReqServerDelete, proto.UUIDOnly{UUID: id})); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d.serverDir(id)); !os.IsNotExist(err) {
		t.Fatal("data left after a successful delete")
	}
}

// Writers that check the limit at the same time must not both spend the same
// free space: the check and the accounting are one step.
func TestDiskReservationsCannotShareHeadroom(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 1) // 1 MB
	if err := d.reserveDisk(id, 700<<10); err != nil {
		t.Fatal(err)
	}
	err := d.reserveDisk(id, 700<<10)
	var full DiskFullError
	if !errors.As(err, &full) || proto.CodeOf(err) != proto.CodeDiskFull {
		t.Fatalf("second 700 KB reservation should fail with disk_full, got %v", err)
	}

	// The same through two SFTP uploads open at once.
	id2 := uuidN(2)
	addServer(t, d, id2, 1)
	dir, _ := d.dir(id2)
	defer dir.Close()
	h := &sftpFS{dir: dir, read: true, write: true, check: func(n int64) error { return d.reserveDisk(id2, n) }}
	open := func(name string) *limitedWriter {
		f, err := dir.OpenFile(name, os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return &limitedWriter{f: f, shape: &d.cacheFor(id2).shape, check: h.check}
	}
	a, b := open("a.bin"), open("b.bin")
	chunk := make([]byte, 700<<10)
	if _, err := a.WriteAt(chunk, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := b.WriteAt(chunk, 0); err == nil {
		t.Fatal("two parallel uploads went past the disk limit together")
	}
	// Rewriting bytes a file already has costs nothing.
	if _, err := a.WriteAt(chunk[:1000], 0); err != nil {
		t.Fatalf("rewriting existing bytes was refused: %v", err)
	}
}

// Errors carry a code across the websocket so the panel never has to match
// on message text.
func TestRepliesCarryErrorCodes(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 1)
	_, err := d.dispatch(context.Background(), req(proto.ReqFilesWrite, proto.FileReq{UUID: id, Path: "big", Content: make([]byte, 2<<20)}))
	if proto.CodeOf(err) != proto.CodeDiskFull {
		t.Fatalf("disk full: %v (code %q)", err, proto.CodeOf(err))
	}
	_, err = d.dispatch(context.Background(), req(proto.ReqServerInput, proto.Input{UUID: id, Line: "say hi"}))
	if proto.CodeOf(err) != proto.CodeConflict {
		t.Fatalf("input to a stopped server: %v (code %q)", err, proto.CodeOf(err))
	}
	b, _ := json.Marshal(proto.Envelope{Err: "x", Code: proto.CodeConflict})
	if !strings.Contains(string(b), `"code":"conflict"`) {
		t.Fatalf("code not on the wire: %s", b)
	}
}
