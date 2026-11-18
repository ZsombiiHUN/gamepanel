package daemon

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// consoleSink records the console messages a daemon sends.
type consoleSink struct {
	mu   sync.Mutex
	msgs [][]string
}

func (s *consoleSink) hook(env proto.Envelope) {
	if env.Type != proto.EvtConsole {
		return
	}
	var c proto.Console
	json.Unmarshal(env.Data, &c)
	s.mu.Lock()
	s.msgs = append(s.msgs, c.All())
	s.mu.Unlock()
}

func (s *consoleSink) lines() (all []string, msgs int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.msgs {
		all = append(all, m...)
	}
	return all, len(s.msgs)
}

func TestConsoleIsBatchedInOrder(t *testing.T) {
	d := newTestDaemon(t, runtime.NewFake())
	var sink consoleSink
	d.sendHook = sink.hook
	out := d.newConsole(uuidN(1))
	for i := range 1000 {
		out.Line(strconv.Itoa(i))
	}
	out.Close()
	all, msgs := sink.lines()
	if len(all) != 1000 {
		t.Fatalf("%d lines arrived", len(all))
	}
	for i, l := range all {
		if l != strconv.Itoa(i) {
			t.Fatalf("line %d is %q", i, l)
		}
	}
	if msgs > 1000/consoleBatchLines+1 {
		t.Fatalf("1000 lines took %d messages", msgs)
	}

	// A lone line is not held back for long.
	out = d.newConsole(uuidN(1))
	out.Line("late")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if all, _ := sink.lines(); all[len(all)-1] == "late" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a single line was never flushed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestConsoleIsThrottled(t *testing.T) {
	d := newTestDaemon(t, runtime.NewFake())
	var sink consoleSink
	d.sendHook = sink.hook
	out := d.newConsole(uuidN(1))
	for i := range consoleRate * 3 {
		out.Line("spam " + strconv.Itoa(i))
	}
	out.Line(strings.Repeat("x", consoleMaxLine*2))
	out.Close()
	all, _ := sink.lines()
	spam, notices, skipped := 0, 0, false
	for _, l := range all {
		switch {
		case strings.HasPrefix(l, "spam"):
			spam++
		case l == throttledNotice:
			notices++
		case strings.Contains(l, "lines of console output were skipped"):
			skipped = true
		}
		if len(l) > consoleMaxLine+8 {
			t.Fatalf("a %d byte line was sent", len(l))
		}
	}
	if spam > consoleRate || notices != 1 || !skipped {
		t.Fatalf("spam=%d notices=%d skipped=%v", spam, notices, skipped)
	}
}
