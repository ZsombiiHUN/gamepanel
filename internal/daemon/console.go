package daemon

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// Console output shares the node's one connection to the panel with every
// other server's state, stats and replies, so it is sent in batches and a
// server that prints too fast is throttled.
const (
	consoleFlushEvery = 100 * time.Millisecond
	consoleBatchLines = 250       // a batch is sent early once it has this many lines
	consoleBatchBytes = 256 << 10 // or this many bytes
	consoleMaxLine    = 8 << 10   // longer lines are cut
	// consoleRate is how many lines per second a server may print before
	// the rest of that second is dropped.
	consoleRate = 2000
)

const throttledNotice = "\x1b[33m[gamepanel] this server is printing faster than the console can show; some output is skipped\x1b[0m"

// consoleOut batches and throttles one stream of console lines for a server.
type consoleOut struct {
	d    *Daemon
	uuid string

	sendMu sync.Mutex // keeps batches in order

	mu      sync.Mutex
	lines   []string
	bytes   int
	timer   *time.Timer
	window  time.Time // start of the current one-second rate window
	count   int       // lines seen in the window
	dropped int       // lines dropped since the last report
}

func (d *Daemon) newConsole(uuid string) *consoleOut {
	return &consoleOut{d: d, uuid: uuid}
}

// Line queues one line.
func (c *consoleOut) Line(l string) {
	if len(l) > consoleMaxLine {
		l = l[:consoleMaxLine] + "…"
	}
	c.mu.Lock()
	now := time.Now()
	if now.Sub(c.window) >= time.Second {
		c.reportDropped()
		c.window, c.count = now, 0
	}
	c.count++
	if c.count > consoleRate {
		if c.dropped == 0 {
			c.add(throttledNotice)
		}
		c.dropped++
		c.mu.Unlock()
		return
	}
	c.add(l)
	full := len(c.lines) >= consoleBatchLines || c.bytes >= consoleBatchBytes
	if !full && c.timer == nil {
		c.timer = time.AfterFunc(consoleFlushEvery, c.Flush)
	}
	c.mu.Unlock()
	if full {
		c.Flush()
	}
}

func (c *consoleOut) add(l string) {
	c.lines = append(c.lines, l)
	c.bytes += len(l)
}

// reportDropped says how much output was skipped. Caller holds mu.
func (c *consoleOut) reportDropped() {
	if c.dropped > 0 {
		c.add("\x1b[33m[gamepanel] " + strconv.Itoa(c.dropped) + " lines of console output were skipped\x1b[0m")
		c.dropped = 0
	}
}

// Flush sends whatever is queued.
func (c *consoleOut) Flush() {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.mu.Lock()
	lines := c.lines
	c.lines, c.bytes = nil, 0
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()
	if len(lines) > 0 {
		c.d.sendConsole(c.uuid, lines...)
	}
}

// Close reports skipped output and sends the rest; the stream has ended.
func (c *consoleOut) Close() {
	c.mu.Lock()
	c.reportDropped()
	c.mu.Unlock()
	c.Flush()
}

// sendConsole sends lines to the panel as one message. It never uses a
// session context: console readers outlive a reconnect, and writing with a
// cancelled context makes the websocket close the current connection.
func (d *Daemon) sendConsole(uuid string, lines ...string) {
	c := proto.Console{UUID: uuid}
	if len(lines) == 1 {
		c.Line = lines[0]
	} else {
		c.Lines = lines
	}
	_ = d.send(context.Background(), proto.Envelope{Type: proto.EvtConsole, Data: proto.Marshal(c)})
}
