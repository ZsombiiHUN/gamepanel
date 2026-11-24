package daemon

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// lingeringConsole is the fake runtime whose console streams stay open for a
// while after their run ended, as a slow Docker attach stream can: the
// reader only sees EOF once the test closes release.
type lingeringConsole struct {
	*runtime.Fake
	release chan struct{}
	closed  chan struct{} // one value per console stream the daemon closed
}

type lingeringReader struct {
	io.ReadCloser
	release chan struct{}
	closed  chan struct{}
	once    sync.Once
}

// Close is the attach goroutine's last deferred call, made after it has
// cleaned up the attachment, so a signal here means that clean-up is done.
func (r *lingeringReader) Close() error {
	err := r.ReadCloser.Close()
	r.closed <- struct{}{}
	return err
}

func (r *lingeringReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err == io.EOF {
		r.once.Do(func() { <-r.release })
	}
	return n, err
}

func (l *lingeringConsole) Attach(ctx context.Context, uuid string) (io.ReadCloser, io.WriteCloser, error) {
	r, w, err := l.Fake.Attach(ctx, uuid)
	if err != nil {
		return nil, nil, err
	}
	return &lingeringReader{ReadCloser: r, release: l.release, closed: l.closed}, w, nil
}

func stdinOf(d *Daemon, id string) io.Writer {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.servers[id].stdin
}

// A restart's new run gets its own console, even while the old run's stream
// is still draining. Before, the new run was skipped (the old one still
// counted as attached), so a stop typed into the console went to the old,
// dead run and waited out the whole graceful-stop time; and the old stream,
// once it ended, cleared the console the new run should have had.
func TestRestartAttachesTheNewRun(t *testing.T) {
	t.Parallel()
	rt := &lingeringConsole{Fake: runtime.NewFake(), release: make(chan struct{}), closed: make(chan struct{}, 8)}
	d := newTestDaemon(t, rt)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.watchEvents(ctx)
	id := uuidN(1)
	if err := d.create(ctx, proto.ServerSpec{UUID: id, Image: "img", Startup: "run", Stop: "stop", MemoryMB: 128}); err != nil {
		t.Fatal(err)
	}
	if !eventually(5*time.Second, func() bool { st, _ := d.installState(id); return st == "" }) {
		t.Fatal("not provisioned")
	}

	if err := power(d, id, "start"); err != nil {
		t.Fatal(err)
	}
	if !eventually(5*time.Second, func() bool { return stdinOf(d, id) != nil }) {
		t.Fatal("first run never attached")
	}
	if err := power(d, id, "restart"); err != nil {
		t.Fatal(err)
	}
	// The old run's stream has not ended yet (release is still open).
	if !eventually(5*time.Second, func() bool { return stdinOf(d, id) != nil }) {
		t.Fatal("the restarted run has no console")
	}

	// The stop goes to the new run and finishes long before gracefulStop.
	done := make(chan error, 1)
	go func() { done <- power(d, id, "stop") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		close(rt.release)
		t.Fatal("stop typed into the console was lost: it waited for the graceful-stop timeout")
	}

	// Start again, then let the old streams end: they must not take the
	// current run's console with them.
	if err := power(d, id, "start"); err != nil {
		t.Fatal(err)
	}
	if !eventually(5*time.Second, func() bool { return stdinOf(d, id) != nil }) {
		t.Fatal("third run never attached")
	}
	close(rt.release)
	// The first two runs' streams end and clean up; the third's stays open.
	for i := range 2 {
		select {
		case <-rt.closed:
		case <-time.After(5 * time.Second):
			t.Fatalf("old console stream %d never finished", i+1)
		}
	}
	if stdinOf(d, id) == nil {
		t.Fatal("an old run's stream cleared the current run's console")
	}
}
