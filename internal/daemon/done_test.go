package daemon

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// stateLog records the states a daemon reports for one server.
type stateLog struct {
	mu     sync.Mutex
	states []string
}

func (l *stateLog) hook(e proto.Envelope) {
	if e.Type != proto.EvtState {
		return
	}
	var s proto.State
	if json.Unmarshal(e.Data, &s) == nil {
		l.mu.Lock()
		l.states = append(l.states, s.State)
		l.mu.Unlock()
	}
}

func (l *stateLog) last() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.states) == 0 {
		return ""
	}
	return l.states[len(l.states)-1]
}

func startWithDone(t *testing.T, done []string) (*Daemon, *stateLog, string) {
	t.Helper()
	d := newTestDaemon(t, runtime.NewFake())
	log := &stateLog{}
	d.sendHook = log.hook
	id := uuidN(1)
	if err := d.create(context.Background(), proto.ServerSpec{UUID: id, Image: "img", Startup: "run", Stop: "stop", MemoryMB: 128, Done: done}); err != nil {
		t.Fatal(err)
	}
	if !eventually(5*time.Second, func() bool { st, _ := d.installState(id); return st == "" }) {
		t.Fatal("server never finished provisioning")
	}
	if err := power(d, id, "start"); err != nil {
		t.Fatal(err)
	}
	return d, log, id
}

// With Done lines, a started server reports "starting" until the console
// prints one, then "running"; colour codes in the line do not matter.
func TestDoneLineMarksTheServerRunning(t *testing.T) {
	t.Parallel()
	d, log, id := startWithDone(t, []string{"For help, type"})
	if !eventually(2*time.Second, func() bool { return log.last() == runtime.StateStarting }) {
		t.Fatalf("want starting before the Done line, got %v", log.states)
	}
	time.Sleep(100 * time.Millisecond) // the fake's boot lines are not Done lines
	if got := log.last(); got != runtime.StateStarting {
		t.Fatalf("running before the Done line: %v", log.states)
	}
	if _, err := d.dispatch(context.Background(), req(proto.ReqServerInput, proto.Input{UUID: id, Line: "\x1b[32mDone (1.2s)! For\x1b[0m help, type \"help\""})); err != nil {
		t.Fatal(err)
	}
	if !eventually(2*time.Second, func() bool { return log.last() == runtime.StateRunning }) {
		t.Fatalf("Done line did not mark it running: %v", log.states)
	}
	// A new run waits for its own Done line.
	if err := power(d, id, "restart"); err != nil {
		t.Fatal(err)
	}
	if !eventually(2*time.Second, func() bool { return log.last() == runtime.StateStarting }) {
		t.Fatalf("restart did not wait for the Done line again: %v", log.states)
	}
}

// Without Done lines a server is running as soon as its container is.
func TestNoDoneLineMeansRunningAtOnce(t *testing.T) {
	t.Parallel()
	_, log, _ := startWithDone(t, nil)
	if !eventually(2*time.Second, func() bool { return log.last() == runtime.StateRunning }) {
		t.Fatalf("want running, got %v", log.states)
	}
}
