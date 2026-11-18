// Package daemon is the node agent: it dials the panel, executes requests
// against a Runtime and the safe filesystem layer, and streams console output.
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/fsx"
	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

const Version = "0.1.0"

// Install states, reported through the normal state events.
const (
	StateInstalling    = "installing"
	StateInstallFailed = "install_failed"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Config struct {
	PanelURL string // ws(s)://host/daemon/connect
	Token    string
	DataDir  string
	// Optional tuning: how often stats/disk usage are reported (default 5 s)
	// and how long a measured disk size is trusted (default 30 s).
	StatsInterval time.Duration
	DiskCacheTTL  time.Duration
	// SFTPRecheck is how often an open SFTP session's access is re-checked
	// with the panel (default 30 s).
	SFTPRecheck time.Duration
	// SFTPKeepAlive is how often SFTP clients are pinged; a session that
	// misses three in a row is closed (default 30 s).
	SFTPKeepAlive time.Duration
	// PullAllowPrivate lets "pull from URL" fetch from private and loopback
	// addresses (off by default: see pull).
	PullAllowPrivate bool

	// Test-only: the SSH handshake deadline (30 s) and how long a login
	// waits for the panel (10 s).
	sftpHandshakeTimeout time.Duration
	sftpPanelWait        time.Duration
}

type managed struct {
	spec     proto.ServerSpec
	stdin    io.Writer
	attached bool   // a console reader is active for the current run
	install  string // "", StateInstalling or StateInstallFailed; "" = ready

	// op is the exclusive operation running on this server ("" = none), see
	// begin. backups counts archives being written, which may overlap with
	// most things but not with a restore or a delete.
	op      string
	backups int

	disk         diskCache
	diskStopping bool // a disk-limit stop is already in progress

	// stale: spec changed while the server was running; the container is
	// rebuilt from it at the next start (persisted as <uuid>.stale).
	stale bool

	// running mirrors the runtime's state as last reported (see noteState),
	// so the stats tick needs no container inspect. usage is the latest
	// CPU and memory sample, kept by a stats watcher while it runs.
	running   bool
	usage     proto.Stats
	stopWatch context.CancelFunc

	// stopping: a stop was asked for this run, so its exit is no crash.
	// crashed: it crashed and was left stopped; it reports "crashed" until
	// it starts again. lastCrash guards against restart loops.
	stopping  bool
	crashed   bool
	lastCrash time.Time
}

// Operations that claim a server (begin).
const (
	opStart   = "start"
	opUpdate  = "update"
	opInstall = "install"
	opRestore = "restore"
	opDelete  = "delete"
	opBackup  = "backup"
)

var errUnknownServer = proto.NotFound("this server is not on the node")

// opBusy describes a running operation for the person who has to wait for it.
func opBusy(op string) error {
	what := map[string]string{
		opStart: "the server is starting", opUpdate: "its limits are being changed",
		opInstall: "an install is running", opRestore: "a backup is being restored",
		opDelete: "the server is being deleted", opBackup: "a backup is being made",
	}[op]
	return proto.Conflict("try again in a moment: " + what)
}

// begin claims a server for op and returns the function that releases it.
//
// Requests run concurrently, and most operations first check the server's
// state and then act on it ("offline? then recreate the container"). Without a
// claim, a start could slip in between the check and the act. Exclusive
// operations therefore never overlap; a backup may run alongside anything but
// a restore or a delete. No operation starts while an install is running.
func (d *Daemon) begin(uuid, op string) (func(), error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.servers[uuid]
	if m == nil {
		return nil, errUnknownServer
	}
	if m.install == StateInstalling {
		return nil, opBusy(opInstall)
	}
	if op == opBackup {
		if m.op == opRestore || m.op == opDelete {
			return nil, opBusy(m.op)
		}
		m.backups++
		return func() { d.mu.Lock(); m.backups--; d.mu.Unlock() }, nil
	}
	if m.op != "" {
		return nil, opBusy(m.op)
	}
	if m.backups > 0 && (op == opRestore || op == opDelete) {
		return nil, opBusy(opBackup)
	}
	m.op = op
	return func() { d.mu.Lock(); m.op = ""; d.mu.Unlock() }, nil
}

// installState is the server's install state; ok is false for an unknown server.
func (d *Daemon) installState(uuid string) (st string, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if m := d.servers[uuid]; m != nil {
		return m.install, true
	}
	return "", false
}

type Daemon struct {
	cfg Config
	rt  runtime.Runtime
	log *slog.Logger

	mu      sync.Mutex
	servers map[string]*managed

	wmu  sync.Mutex
	conn *websocket.Conn

	relayClient *http.Client // transfers to and from the panel's relay

	sftp *sftpServer // set by StartSFTP; nil when SFTP is off

	walkHook func(uuid string) (int64, error) // tests: replaces the disk walk
	sendHook func(proto.Envelope)             // tests: receives what would be sent
}

func New(cfg Config, rt runtime.Runtime, log *slog.Logger) (*Daemon, error) {
	for _, d := range []string{"servers", "state"} {
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, d), 0o755); err != nil {
			return nil, err
		}
	}
	d := &Daemon{cfg: cfg, rt: rt, log: log, servers: map[string]*managed{},
		// No overall timeout: a large download runs as long as bytes flow.
		relayClient: &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 2 * time.Minute}}}
	d.loadState()
	return d, nil
}

func (d *Daemon) serverDir(uuid string) string { return filepath.Join(d.cfg.DataDir, "servers", uuid) }
func (d *Daemon) statePath(uuid string) string {
	return filepath.Join(d.cfg.DataDir, "state", uuid+".json")
}

func (d *Daemon) installedPath(uuid string) string {
	return filepath.Join(d.cfg.DataDir, "state", uuid+".installed")
}

func (d *Daemon) stalePath(uuid string) string {
	return filepath.Join(d.cfg.DataDir, "state", uuid+".stale")
}

// loadState restores specs persisted by earlier runs so a daemon restart does
// not forget its servers.
func (d *Daemon) loadState() {
	files, _ := filepath.Glob(filepath.Join(d.cfg.DataDir, "state", "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s proto.ServerSpec
		if json.Unmarshal(b, &s) == nil && uuidRe.MatchString(s.UUID) {
			m := &managed{spec: s}
			if _, err := os.Stat(d.stalePath(s.UUID)); err == nil {
				m.stale = true
			}
			// An install that was interrupted (daemon killed mid-run) never
			// finished, so it counts as failed until reinstalled.
			if s.Install != nil {
				if _, err := os.Stat(d.installedPath(s.UUID)); err != nil {
					m.install = StateInstallFailed
				}
			}
			d.servers[s.UUID] = m
		}
	}
}

// Run keeps a connection to the panel until ctx ends, reconnecting with
// exponential backoff.
func (d *Daemon) Run(ctx context.Context) error {
	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := d.session(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(start) > 30*time.Second {
			backoff = time.Second
		}
		d.log.Warn("panel connection lost", "err", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
	return nil
}

func (d *Daemon) session(ctx context.Context) error {
	c, _, err := websocket.Dial(ctx, d.cfg.PanelURL, &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + d.cfg.Token}},
	})
	if err != nil {
		return err
	}
	c.SetReadLimit(32 << 20)
	d.wmu.Lock()
	d.conn = c
	d.wmu.Unlock()
	defer c.CloseNow()
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
	hello := proto.Hello{Version: Version, Servers: ids, SFTPPort: sftpPort, Containers: d.untracked(sctx, ids)}
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
	b, _ := json.Marshal(env)
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
	_ = d.send(ctx, proto.Envelope{Type: proto.EvtState, Data: proto.Marshal(proto.State{UUID: uuid, State: st})})
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

// attach streams a server's console to the panel and keeps its stdin handle.
// It is idempotent: both the power handler and the runtime event watcher may
// call it for the same run, and only the first starts a reader.
func (d *Daemon) attach(ctx context.Context, uuid string) {
	d.mu.Lock()
	m := d.servers[uuid]
	if m == nil || m.attached {
		d.mu.Unlock()
		return
	}
	m.attached = true
	d.mu.Unlock()

	// The stream must outlive the request or session that started it: it ends
	// when the container stops, not when the panel connection drops.
	r, w, err := d.rt.Attach(context.WithoutCancel(ctx), uuid)
	if err != nil {
		d.log.Warn("attach failed", "server", uuid, "err", err)
		d.mu.Lock()
		m.attached = false
		d.mu.Unlock()
		return
	}
	d.mu.Lock()
	m.stdin = w
	d.mu.Unlock()
	go func() {
		defer r.Close()
		defer func() {
			d.mu.Lock()
			m.attached, m.stdin = false, nil
			d.mu.Unlock()
		}()
		out := d.newConsole(uuid)
		defer out.Close()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			out.Line(sc.Text())
		}
	}()
}

func (d *Daemon) handle(ctx context.Context, env proto.Envelope) {
	data, err := d.safeDispatch(ctx, env)
	reply := proto.Envelope{Type: proto.EvtReply, ID: env.ID, Data: data}
	if err != nil {
		d.log.Warn("request failed", "type", env.Type, "err", err)
		err = forPeople(err)
		reply.Err, reply.Code = err.Error(), proto.CodeOf(err)
	}
	_ = d.send(ctx, reply)
}

// safeDispatch is dispatch with a recover: requests run in their own
// goroutines, so a panic in one would otherwise take down the whole daemon and
// every server's console with it.
func (d *Daemon) safeDispatch(ctx context.Context, env proto.Envelope) (data json.RawMessage, err error) {
	defer func() {
		if r := recover(); r != nil {
			d.log.Error("request panicked", "type", env.Type, "panic", r, "stack", string(debug.Stack()))
			data, err = nil, fmt.Errorf("internal error handling %s", env.Type)
		}
	}()
	return d.dispatch(ctx, env)
}

func (d *Daemon) dir(uuid string) (*fsx.Dir, error) {
	if !uuidRe.MatchString(uuid) {
		return nil, proto.Invalid("invalid server id")
	}
	return fsx.Open(d.serverDir(uuid))
}

func (d *Daemon) dispatch(ctx context.Context, env proto.Envelope) (json.RawMessage, error) {
	switch env.Type {
	case proto.ReqServerCreate:
		var s proto.ServerSpec
		if err := json.Unmarshal(env.Data, &s); err != nil {
			return nil, err
		}
		return nil, d.create(ctx, s)
	case proto.ReqServerPower:
		var p proto.Power
		if err := json.Unmarshal(env.Data, &p); err != nil {
			return nil, err
		}
		return nil, d.power(ctx, p)
	case proto.ReqServerUpdate:
		var sp proto.ServerSpec
		if err := json.Unmarshal(env.Data, &sp); err != nil {
			return nil, err
		}
		res, err := d.update(ctx, sp)
		return proto.Marshal(res), err
	case proto.ReqServerReinstall:
		var u proto.UUIDOnly
		if err := json.Unmarshal(env.Data, &u); err != nil {
			return nil, err
		}
		return nil, d.reinstall(ctx, u.UUID)
	case proto.ReqBackupCreate, proto.ReqBackupRestore, proto.ReqBackupDelete:
		var b proto.BackupReq
		if err := json.Unmarshal(env.Data, &b); err != nil {
			return nil, err
		}
		return d.backup(ctx, env.Type, b)
	case proto.ReqServerInput:
		var in proto.Input
		if err := json.Unmarshal(env.Data, &in); err != nil {
			return nil, err
		}
		return nil, d.input(in.UUID, in.Line)
	case proto.ReqServerDelete:
		var u proto.UUIDOnly
		if err := json.Unmarshal(env.Data, &u); err != nil {
			return nil, err
		}
		return nil, d.remove(ctx, u.UUID)
	case proto.ReqFilesList, proto.ReqFilesRead, proto.ReqFilesWrite, proto.ReqFilesDelete, proto.ReqFilesMkdir:
		var f proto.FileReq
		if err := json.Unmarshal(env.Data, &f); err != nil {
			return nil, err
		}
		return d.files(env.Type, f)
	case proto.ReqFilesOp:
		var op proto.FileOp
		if err := json.Unmarshal(env.Data, &op); err != nil {
			return nil, err
		}
		res, err := d.fileOp(op)
		return proto.Marshal(res), err
	case proto.ReqFilesUpload, proto.ReqFilesDownload, proto.ReqFilesPull, proto.ReqBackupDownload:
		return d.transfer(ctx, env.Type, env.Data)
	}
	return nil, fmt.Errorf("unknown request %q", env.Type)
}

func (d *Daemon) files(typ string, f proto.FileReq) (json.RawMessage, error) {
	fs, err := d.dir(f.UUID)
	if err != nil {
		return nil, err
	}
	defer fs.Close()
	switch typ {
	case proto.ReqFilesList:
		l, err := fs.List(f.Path)
		return proto.Marshal(l), err
	case proto.ReqFilesRead:
		b, err := fs.Read(f.Path)
		return proto.Marshal(proto.FileReq{UUID: f.UUID, Path: f.Path, Content: b}), err
	case proto.ReqFilesWrite:
		if err := d.reserveDisk(f.UUID, int64(len(f.Content))); err != nil {
			return nil, err
		}
		return nil, fs.Write(f.Path, f.Content)
	case proto.ReqFilesMkdir:
		return nil, fs.Mkdir(f.Path)
	default:
		return nil, fs.Delete(f.Path)
	}
}

func (d *Daemon) create(ctx context.Context, s proto.ServerSpec) error {
	if !uuidRe.MatchString(s.UUID) {
		return proto.Invalid("invalid server id")
	}
	dir := d.serverDir(s.UUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Game images typically run as a non-root user; hand over the data dir.
	_ = os.Chown(dir, 1000, 1000)
	// The container is built in the background, since pulling its image can
	// take longer than the panel waits for a reply. Until it exists the server
	// is stale, so a start, also after a daemon restart, builds it. The marker
	// goes first: a state file without it would mean a container exists.
	b, _ := json.Marshal(s)
	if err := os.WriteFile(d.stalePath(s.UUID), nil, 0o600); err != nil {
		d.discard(s.UUID)
		return err
	}
	if err := os.WriteFile(d.statePath(s.UUID), b, 0o600); err != nil {
		d.discard(s.UUID)
		return err
	}
	_ = os.Remove(d.installedPath(s.UUID))
	d.mu.Lock()
	d.servers[s.UUID] = &managed{spec: s, stale: true, install: StateInstalling}
	d.mu.Unlock()
	// Reply now. Progress arrives as console and state events, and the work
	// must outlive this request's context.
	go d.provision(context.WithoutCancel(ctx), s.UUID)
	return nil
}

// discard removes what a create that failed half way left on disk, so the
// node does not keep a server the panel has rolled back.
func (d *Daemon) discard(uuid string) {
	for _, p := range []string{d.statePath(uuid), d.stalePath(uuid), d.installedPath(uuid)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			d.log.Warn("could not clean up after a failed create", "server", uuid, "path", p, "err", err)
		}
	}
	if err := os.RemoveAll(d.serverDir(uuid)); err != nil {
		d.log.Warn("could not clean up after a failed create", "server", uuid, "err", err)
	}
}

// provision builds a new server's container, then runs the egg's install
// script if it has one. The server counts as installing throughout, which
// keeps every other operation out. A container that cannot be built (the
// registry is down, say) is not fatal: the server stays stale and its next
// start tries again, saying why on the console if it still fails.
func (d *Daemon) provision(ctx context.Context, uuid string) {
	spec, err := d.spec(uuid)
	if err != nil {
		return
	}
	d.emitState(ctx, uuid, StateInstalling)
	d.sendConsole(uuid, "[gamepanel] preparing the server's image "+spec.Image)
	abs, _ := filepath.Abs(d.serverDir(uuid))
	if err := d.rt.Create(ctx, runtime.Spec{ServerSpec: spec, DataDir: abs}); err != nil {
		d.log.Warn("could not build a new server's container", "server", uuid, "err", err)
		d.sendConsole(uuid, "\x1b[33m[gamepanel] could not prepare the server's image: "+err.Error()+". It is tried again when the server starts.\x1b[0m")
	} else {
		if err := os.Remove(d.stalePath(uuid)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			d.log.Warn("could not clear the stale marker", "server", uuid, "err", err)
		}
		d.mu.Lock()
		if m := d.servers[uuid]; m != nil {
			m.stale = false
		}
		d.mu.Unlock()
	}
	if spec.Install == nil {
		d.setInstall(uuid, "")
		d.emitState(ctx, uuid, runtime.StateOffline)
		return
	}
	d.runInstall(ctx, uuid)
}

// runInstall executes the egg install script and records the outcome.
func (d *Daemon) runInstall(ctx context.Context, uuid string) {
	spec, err := d.spec(uuid)
	if err != nil || spec.Install == nil {
		return
	}
	d.emitState(ctx, uuid, StateInstalling)
	_ = os.Remove(d.installedPath(uuid))

	say := func(line string) { d.sendConsole(uuid, line) }
	fail := func(err error) {
		d.log.Warn("install failed", "server", uuid, "err", err)
		say("\x1b[31m[install] failed: " + err.Error() + "\x1b[0m")
		d.setInstall(uuid, StateInstallFailed)
		d.emitState(ctx, uuid, StateInstallFailed)
	}

	tmp, err := os.MkdirTemp(filepath.Join(d.cfg.DataDir, "state"), "install-")
	if err != nil {
		fail(err)
		return
	}
	defer os.RemoveAll(tmp)
	if err := os.WriteFile(filepath.Join(tmp, "install.sh"), []byte(spec.Install.Script), 0o644); err != nil {
		fail(err)
		return
	}
	abs, _ := filepath.Abs(d.serverDir(uuid))
	absTmp, _ := filepath.Abs(tmp)

	say("[install] running installer " + spec.Install.Image)
	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		out := d.newConsole(uuid)
		defer out.Close() // everything is sent before the outcome is said
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			out.Line(sc.Text())
		}
	}()
	err = d.rt.Install(ctx, runtime.InstallRun{
		UUID: uuid, Image: spec.Install.Image, Entrypoint: spec.Install.Entrypoint,
		ScriptDir: absTmp, DataDir: abs, Env: spec.Env, MemoryMB: spec.MemoryMB,
	}, pw)
	pw.Close()
	<-done
	// The installer wrote files the daemon did not account for.
	d.cacheFor(uuid).invalidate()
	if err != nil {
		fail(err)
		return
	}
	if err := os.WriteFile(d.installedPath(uuid), nil, 0o600); err != nil {
		fail(err)
		return
	}
	d.setInstall(uuid, "")
	say("\x1b[32m[install] complete\x1b[0m")
	d.emitState(ctx, uuid, runtime.StateOffline)
}

func (d *Daemon) setInstall(uuid, st string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if m := d.servers[uuid]; m != nil {
		m.install = st
	}
}

// reinstall re-runs the install script; the server must be offline.
func (d *Daemon) reinstall(ctx context.Context, uuid string) error {
	spec, err := d.spec(uuid)
	if err != nil {
		return err
	}
	if spec.Install == nil {
		return proto.Invalid("this server's egg has no install script")
	}
	end, err := d.begin(uuid, opInstall)
	if err != nil {
		return err
	}
	defer end()
	// A server whose first container could not be built has none to ask; it
	// is not running either.
	if st, err := d.rt.State(ctx, uuid); (err != nil && !d.isStale(uuid)) || (err == nil && st != runtime.StateOffline) {
		return proto.Conflict("stop the server before reinstalling")
	}
	// From here the install state itself keeps every other operation out.
	d.setInstall(uuid, StateInstalling)
	go d.runInstall(context.WithoutCancel(ctx), uuid)
	return nil
}

func (d *Daemon) spec(uuid string) (proto.ServerSpec, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.servers[uuid]
	if m == nil {
		return proto.ServerSpec{}, errUnknownServer
	}
	return m.spec, nil
}

func (d *Daemon) power(ctx context.Context, p proto.Power) error {
	spec, err := d.spec(p.UUID)
	if err != nil {
		return err
	}
	switch p.Action {
	case "start":
		return d.start(ctx, spec)
	case "stop":
		return d.stop(ctx, spec)
	case "restart":
		if err := d.stop(ctx, spec); err != nil {
			return err
		}
		return d.start(ctx, spec)
	case "kill":
		d.expectStop(p.UUID)
		return d.rt.Kill(ctx, p.UUID)
	}
	return proto.Invalid("unknown power action")
}

// start boots an installed server that is within its disk limit. It claims
// the server, so it cannot interleave with a restore, an update or a delete.
func (d *Daemon) start(ctx context.Context, spec proto.ServerSpec) error {
	uuid := spec.UUID
	if inst, _ := d.installState(uuid); inst == StateInstallFailed {
		return proto.Conflict("server is not installed (" + inst + ")")
	}
	end, err := d.begin(uuid, opStart)
	if err != nil {
		return err
	}
	defer end()
	if spec.DiskMB > 0 {
		if used, err := d.diskUsage(uuid, 0); err == nil && used > spec.DiskMB<<20 {
			return ErrDiskFull{used, spec.DiskMB << 20}
		}
	}
	d.mu.Lock()
	if m := d.servers[uuid]; m != nil {
		m.crashed = false
	}
	d.mu.Unlock()
	d.emitState(ctx, uuid, runtime.StateStarting)
	if err := d.rebuildIfStale(ctx, uuid); err != nil {
		d.emitState(ctx, uuid, runtime.StateOffline)
		return err
	}
	if err := d.rt.Start(ctx, uuid); err != nil {
		d.emitState(ctx, uuid, runtime.StateOffline)
		return err
	}
	d.attach(ctx, uuid)
	if spec.BandwidthMbit > 0 {
		mode, err := d.rt.LimitBandwidth(ctx, uuid)
		switch {
		case err != nil:
			// Not fatal: the server runs, unlimited, and its owner is told why.
			d.log.Warn("could not apply the bandwidth limit", "server", uuid, "err", err)
			d.sendConsole(uuid, "\x1b[33m[gamepanel] the network limit could not be applied on this node (it needs tc, ip and nsenter); the server runs without it\x1b[0m")
		case mode == runtime.IngressPoliced:
			d.log.Info("incoming traffic is policed, not shaped: this kernel has no ifb device", "server", uuid)
		}
	}
	d.emitState(ctx, uuid, runtime.StateRunning)
	return nil
}

// stop sends the egg's graceful stop command (or SIGTERM for "^C"), then
// escalates to a hard stop if the server has not exited in time.
func (d *Daemon) stop(ctx context.Context, spec proto.ServerSpec) error {
	// Nothing to stop. Saying "stopping" here would leave the server shown as
	// stopping, since no exit follows.
	if st, err := d.rt.State(ctx, spec.UUID); (err == nil && st == runtime.StateOffline) || (err != nil && d.isStale(spec.UUID)) {
		return nil
	}
	d.expectStop(spec.UUID)
	d.emitState(ctx, spec.UUID, runtime.StateStopping)
	if spec.Stop != "" && spec.Stop != "^C" {
		if err := d.input(spec.UUID, spec.Stop); err == nil {
			deadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				if st, _ := d.rt.State(ctx, spec.UUID); st == runtime.StateOffline {
					return nil
				}
				if !sleep(ctx, 250*time.Millisecond) {
					return ctx.Err()
				}
			}
		}
	}
	return d.rt.Stop(ctx, spec.UUID, 10)
}

func (d *Daemon) input(uuid, line string) error {
	d.mu.Lock()
	m := d.servers[uuid]
	var w io.Writer
	if m != nil {
		w = m.stdin
	}
	d.mu.Unlock()
	if w == nil {
		return proto.Conflict("server is not running")
	}
	_, err := io.WriteString(w, line+"\n")
	return err
}

func (d *Daemon) remove(ctx context.Context, uuid string) error {
	if !uuidRe.MatchString(uuid) {
		return proto.Invalid("invalid server id")
	}
	end, err := d.begin(uuid, opDelete)
	switch {
	case errors.Is(err, errUnknownServer):
		// Not tracked (e.g. already half-removed): still clean up what is left.
	case err != nil:
		return err
	default:
		defer end()
	}
	// The container must be gone before the data: deleting files under a
	// running game while the panel forgets the server would leave a container
	// nobody can manage. Runtimes report a missing container as success.
	if err := d.rt.Remove(ctx, uuid); err != nil {
		return fmt.Errorf("could not remove the container, nothing was deleted: %w", err)
	}
	d.mu.Lock()
	if m := d.servers[uuid]; m != nil {
		m.stopStats()
	}
	delete(d.servers, uuid)
	d.mu.Unlock()
	d.closeSFTP(uuid)
	_ = os.Remove(d.statePath(uuid))
	_ = os.Remove(d.installedPath(uuid))
	_ = os.Remove(d.stalePath(uuid))
	_ = os.RemoveAll(filepath.Join(d.cfg.DataDir, "backups", uuid))
	return os.RemoveAll(d.serverDir(uuid))
}

// update applies a new spec (limits, image, startup, variables, ports). A
// stopped server gets a new container now; a running one keeps its container
// until it next starts, so nothing needs to be stopped to make a change. Files,
// install state and backups stay either way.
func (d *Daemon) update(ctx context.Context, s proto.ServerSpec) (proto.UpdateResult, error) {
	old, err := d.spec(s.UUID)
	if err != nil {
		return proto.UpdateResult{}, err
	}
	end, err := d.begin(s.UUID, opUpdate)
	if err != nil {
		return proto.UpdateResult{}, err
	}
	defer end()
	b, _ := json.Marshal(s)
	if err := os.WriteFile(d.statePath(s.UUID), b, 0o600); err != nil {
		return proto.UpdateResult{}, err
	}
	if sameContainer(old, s) {
		// Nothing the container is built from changed (e.g. only the crash
		// restart setting), so it applies at once and needs no rebuild.
		d.mu.Lock()
		if m := d.servers[s.UUID]; m != nil {
			m.spec = s
		}
		d.mu.Unlock()
		return proto.UpdateResult{}, nil
	}
	if st, _ := d.rt.State(ctx, s.UUID); st != runtime.StateOffline {
		if err := os.WriteFile(d.stalePath(s.UUID), nil, 0o600); err != nil {
			return proto.UpdateResult{}, err
		}
		d.mu.Lock()
		if m := d.servers[s.UUID]; m != nil {
			m.spec, m.stale = s, true
		}
		d.mu.Unlock()
		return proto.UpdateResult{Pending: true}, nil
	}
	abs, _ := filepath.Abs(d.serverDir(s.UUID))
	if err := d.rt.Create(ctx, runtime.Spec{ServerSpec: s, DataDir: abs}); err != nil {
		// Put the previous container back so the server is not left without one.
		if ob, merr := json.Marshal(old); merr == nil {
			if werr := os.WriteFile(d.statePath(s.UUID), ob, 0o600); werr != nil {
				d.log.Error("could not restore the previous settings", "server", s.UUID, "err", werr)
			}
		}
		if rerr := d.rt.Create(ctx, runtime.Spec{ServerSpec: old, DataDir: abs}); rerr != nil {
			// Neither container exists now. Marking the server stale makes its
			// next start build one again instead of failing on a missing one.
			d.log.Error("could not restore the previous container", "server", s.UUID, "err", rerr)
			if werr := os.WriteFile(d.stalePath(s.UUID), nil, 0o600); werr == nil {
				d.mu.Lock()
				if m := d.servers[s.UUID]; m != nil {
					m.spec, m.stale = old, true
				}
				d.mu.Unlock()
			}
		}
		return proto.UpdateResult{}, err
	}
	_ = os.Remove(d.stalePath(s.UUID))
	d.mu.Lock()
	if m := d.servers[s.UUID]; m != nil {
		m.spec, m.stale = s, false
	}
	d.mu.Unlock()
	return proto.UpdateResult{}, nil
}

// sameContainer reports whether two specs build the same container.
func sameContainer(a, b proto.ServerSpec) bool {
	a.NoCrashRestart, b.NoCrashRestart = false, false
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

func (d *Daemon) isStale(uuid string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.servers[uuid]
	return m != nil && m.stale
}

// rebuildIfStale recreates the container of a server whose spec changed while
// it ran. The caller holds the server (begin), and the server is stopped.
func (d *Daemon) rebuildIfStale(ctx context.Context, uuid string) error {
	d.mu.Lock()
	m := d.servers[uuid]
	if m == nil || !m.stale {
		d.mu.Unlock()
		return nil
	}
	spec := m.spec
	d.mu.Unlock()
	abs, _ := filepath.Abs(d.serverDir(uuid))
	if err := d.rt.Create(ctx, runtime.Spec{ServerSpec: spec, DataDir: abs}); err != nil {
		return fmt.Errorf("could not apply the new settings: %w", err)
	}
	_ = os.Remove(d.stalePath(uuid))
	d.mu.Lock()
	m.stale = false
	d.mu.Unlock()
	return nil
}
