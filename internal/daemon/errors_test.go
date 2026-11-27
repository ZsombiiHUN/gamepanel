package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// File system errors carry paths and syscall names; the daemon replaces the
// common ones with a code and a plain message before they leave the node.
func TestCommonFailuresAreDescribedForPeople(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		err  error
		code string
	}{
		{&fs.PathError{Op: "openat", Path: "world/level.dat", Err: fs.ErrNotExist}, proto.CodeNotFound},
		{&fs.PathError{Op: "mkdirat", Path: "plugins", Err: fs.ErrExist}, proto.CodeConflict},
		{&fs.PathError{Op: "openat", Path: "plugins/out/x.yml", Err: errors.New("path escapes from parent")}, proto.CodeInvalid},
		{errors.New("OCI runtime create failed: openat2 /sys/fs/cgroup/docker/5a77/io.weight: no such file or directory"), proto.CodeInvalid},
		{fmt.Errorf("read: %w", &fs.PathError{Op: "read", Path: "logs", Err: errors.New("is a directory")}), ""},
	} {
		got := forPeople(c.err)
		if proto.CodeOf(got) != c.code {
			t.Errorf("%v: code %q, want %q", c.err, proto.CodeOf(got), c.code)
		}
		if c.code != "" && (strings.Contains(got.Error(), "openat") || strings.Contains(got.Error(), "level.dat")) {
			t.Errorf("%v: internal detail kept: %q", c.err, got)
		}
	}
	if err := proto.Conflict("x"); !errors.Is(forPeople(err), err) {
		t.Fatal("an already coded error was changed")
	}
}

// End to end through dispatch: reading a missing file is a not-found with a
// plain message, not an os error.
func TestMissingFileReplyIsPlain(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	addServer(t, d, id, 0)
	_, err := d.dispatch(context.Background(), req(proto.ReqFilesRead, proto.FileReq{UUID: id, Path: "nope.txt"}))
	err = forPeople(err)
	if proto.CodeOf(err) != proto.CodeNotFound || err.Error() != "no such file or folder" {
		t.Fatalf("got %q (code %q)", err, proto.CodeOf(err))
	}
	if _, err := d.dispatch(context.Background(), req(proto.ReqServerPower, proto.Power{UUID: uuidN(42), Action: "start"})); proto.CodeOf(err) != proto.CodeNotFound {
		t.Fatalf("unknown server: %v", err)
	}
}
