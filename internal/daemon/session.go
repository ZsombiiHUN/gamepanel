package daemon

// The connection to the panel, and what the daemon reports over it: states,
// console lines, crashes and usage.

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
	"github.com/ZsombiiHUN/gamepanel/internal/version"
)

// Run keeps a connection to the panel until ctx ends, reconnecting with
// exponential backoff. Losing the connection is never fatal, so there is no
// error to return.
func (d *Daemon) Run(ctx context.Context) {
	d.markOutdated(ctx)
	if n := d.resume(ctx); n > 0 {
		d.log.Info("started servers that were running before the restart", "count", n)
	}
	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := d.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) > 30*time.Second {
			backoff = time.Second
		}
		d.log.Warn("panel connection lost", "err", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (d *Daemon) session(ctx context.Context) error {
	c, _, err := websocket.Dial(ctx, d.cfg.PanelURL, &websocket.DialOptions{ //nolint:bodyclose // websocket.Dial owns the response body
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + d.cfg.Token}},
	})
	if err != nil {
		return err
	}
	c.SetReadLimit(32 << 20)
	d.wmu.Lock()
	d.conn = c
	d.wmu.Unlock()
	defer c.CloseNow() //nolint:errcheck // the session is over either way
	d.log.Info("connected to panel", "url", d.cfg.PanelURL)

	sctx, cancel := context.WithCancel(ctx)
	defer cancel()

	d.mu.Lock()
	var ids []string
	for id := range d.servers {
		ids = append(ids, id)
	}
	sftpPort := 0
	if d.sftp != nil {
		if a, ok := d.sftp.ln.Addr().(*net.TCPAddr); ok {
			sftpPort = a.Port
		}
	}
	d.mu.Unlock()
	hello := proto.Hello{Version: version.Version, Servers: ids, SFTPPort: sftpPort, Containers: d.untracked(sctx, ids)}
	if err := d.send(sctx, proto.Envelope{Type: proto.EvtHello, Data: proto.Marshal(hello)}); err != nil {
		return err
	}

	go d.watchEvents(sctx)
	go d.statsLoop(sctx)
	// Report every server's state: the panel keeps state only in memory, so
	// after it restarts a server it hears nothing about shows as unknown.
	for _, id := range ids {
		d.mu.Lock()
		m := d.servers[id]
		install := ""
		if m != nil {
			install = m.install
		}
		d.mu.Unlock()
		if install != "" { // install did not finish
			d.emitState(sctx, id, install)
			continue
		}
		st, err := d.rt.State(sctx, id)
		if err != nil {
			continue
		}
		if st == runtime.StateRunning { // re-attach the console
			d.mu.Lock()
			if m != nil && !m.attached {
				// It was running before this daemon started, so its Done
				// line has long been printed.
				m.booted = true
			}
			d.mu.Unlock()
			d.attach(sctx, id)
		}
		d.emitState(sctx, id, st)
	}

	for {
		_, data, err := c.Read(sctx)
		if err != nil {
			return err
		}
		var env proto.Envelope
		if json.Unmarshal(data, &env) != nil {
			continue
		}
		go d.handle(sctx, env) // requests run concurrently; replies carry the id
	}
}

// untracked lists server containers the runtime has that the daemon does not.
func (d *Daemon) untracked(ctx context.Context, tracked []string) []string {
	all, err := d.rt.List(ctx)
	if err != nil {
		d.log.Warn("could not list containers", "err", err)
		return nil
	}
	known := map[string]bool{}
	for _, id := range tracked {
		known[id] = true
	}
	var out []string
	for _, id := range all {
		if !known[id] && uuidRe.MatchString(id) {
			out = append(out, id)
		}
	}
	return out
}

func (d *Daemon) send(ctx context.Context, env proto.Envelope) error {
	if d.sendHook != nil {
		d.sendHook(env)
		return nil
	}
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	d.wmu.Lock()
	defer d.wmu.Unlock()
	if d.conn == nil {
		return errors.New("not connected")
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return d.conn.Write(wctx, websocket.MessageText, b)
}

func (d *Daemon) emitState(ctx context.Context, uuid, st string) {
	if st == runtime.StateOffline {
		d.mu.Lock()
		if m := d.servers[uuid]; m != nil && m.crashed {
			st = proto.StateCrashed
		}
		d.mu.Unlock()
	}
	d.noteState(uuid, st)
	shown := st
	if st == runtime.StateRunning {
		// The container runs, but the game may still be loading: it says
		// "starting" until the console prints one of the egg's Done lines.
		d.mu.Lock()
		if m := d.servers[uuid]; m != nil && len(m.spec.Done) > 0 && !m.booted {
			shown = runtime.StateStarting
		}
		d.mu.Unlock()
	}
	_ = d.send(ctx, proto.Envelope{Type: proto.EvtState, Data: proto.Marshal(proto.State{UUID: uuid, State: shown})})
}

// ansiRe matches terminal escape sequences (colours), which servers mix into
// their lines.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// sawLine checks a console line for the spec's Done text. The first match in
// a run reports the server as running.
func (d *Daemon) sawLine(ctx context.Context, uuid string, m *managed, line string) {
	d.mu.Lock()
	if m.booted || len(m.spec.Done) == 0 {
		d.mu.Unlock()
		return
	}
	plain := ansiRe.ReplaceAllString(line, "")
	hit := false
	for _, t := range m.spec.Done {
		if strings.Contains(plain, t) {
			hit = true
			break
		}
	}
	if hit {
		m.booted = true
	}
	d.mu.Unlock()
	if hit {
		_ = d.send(ctx, proto.Envelope{Type: proto.EvtState, Data: proto.Marshal(proto.State{UUID: uuid, State: runtime.StateRunning})})
	}
}

// sleep waits for d or until ctx ends; it reports whether the full time passed.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// noteState records a server's state and starts or stops its stats watcher.
func (d *Daemon) noteState(uuid, st string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.servers[uuid]
	if m == nil {
		return
	}
	switch st {
	case runtime.StateRunning:
		m.running = true
		if m.stopWatch == nil {
			ctx, cancel := context.WithCancel(context.Background())
			m.stopWatch = cancel
			go d.watchStats(ctx, uuid, m)
		}
	case runtime.StateOffline, proto.StateCrashed, StateInstalling, StateInstallFailed:
		m.stopStats()
		m.booted = false
	}
}

// stopStats marks a server as not running. Caller holds d.mu.
func (m *managed) stopStats() {
	m.running, m.usage = false, proto.Stats{}
	if m.stopWatch != nil {
		m.stopWatch()
		m.stopWatch = nil
	}
}

// watchStats keeps the latest usage sample of a running server, re-opening
// the stream if it drops while the server still runs.
func (d *Daemon) watchStats(ctx context.Context, uuid string, m *managed) {
	for {
		err := d.rt.WatchStats(ctx, uuid, func(s proto.Stats) {
			d.mu.Lock()
			if ctx.Err() == nil {
				m.usage = s
			}
			d.mu.Unlock()
		})
		if err != nil && ctx.Err() == nil {
			d.log.Debug("stats stream ended", "server", uuid, "err", err)
		}
		if !sleep(ctx, 2*time.Second) {
			return
		}
	}
}

// refreshStates asks the runtime for every server's state, for when runtime
// events may have been missed (the event stream was re-opened).
func (d *Daemon) refreshStates(ctx context.Context) {
	d.mu.Lock()
	var ids []string
	for id, m := range d.servers {
		if m.install == "" {
			ids = append(ids, id)
		}
	}
	d.mu.Unlock()
	for _, id := range ids {
		if st, err := d.rt.State(ctx, id); err == nil {
			d.emitState(ctx, id, st)
		}
	}
}

func (d *Daemon) watchEvents(ctx context.Context) {
	first := true
	for ctx.Err() == nil {
		ch, err := d.rt.Events(ctx)
		if err != nil {
			sleep(ctx, 2*time.Second)
			continue
		}
		if !first {
			d.refreshStates(ctx)
		}
		first = false
		for ev := range ch {
			if ev.Exit != nil || ev.State == runtime.StateOffline {
				d.detach(ev.UUID)
			}
			if ev.Exit != nil && d.exited(ctx, ev.UUID, *ev.Exit) {
				continue
			}
			if ev.State == runtime.StateRunning {
				// A new run began. The engine reports the previous run's exit
				// before this, so a stop asked for that run is used up.
				d.mu.Lock()
				if m := d.servers[ev.UUID]; m != nil {
					m.stopping = false
				}
				d.mu.Unlock()
			}
			d.emitState(ctx, ev.UUID, ev.State)
			if ev.State == runtime.StateRunning {
				d.attach(ctx, ev.UUID)
			}
		}
		sleep(ctx, time.Second) // runtime event stream dropped; resubscribe
	}
}

// statsLoop reports every server's usage once per tick, in one message. CPU
// and memory come from the watchers' latest samples and the state from
// runtime events, so a tick costs no engine calls; disk usage comes from the
// disk cache, which measures on its own schedule.
func (d *Daemon) statsLoop(ctx context.Context) {
	t := time.NewTicker(d.cfg.statsEvery())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		d.sendStats(ctx)
	}
}

func (d *Daemon) sendStats(ctx context.Context) {
	type row struct {
		s       proto.Stats
		running bool
	}
	d.mu.Lock()
	rows := make([]row, 0, len(d.servers))
	for id, m := range d.servers {
		r := row{running: m.running}
		if m.running {
			r.s = m.usage
		}
		r.s.UUID, r.s.Pending = id, m.stale
		rows = append(rows, r)
	}
	d.mu.Unlock()
	if len(rows) == 0 {
		return
	}
	batch := proto.StatsBatch{Stats: make([]proto.Stats, 0, len(rows))}
	for _, r := range rows {
		r.s.DiskBytes, _ = d.enforceDisk(ctx, r.s.UUID, r.running)
		batch.Stats = append(batch.Stats, r.s)
	}
	_ = d.send(ctx, proto.Envelope{Type: proto.EvtStatsBatch, Data: proto.Marshal(batch)})
}
