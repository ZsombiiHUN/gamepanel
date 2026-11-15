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
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
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
}

type managed struct {
	spec     proto.ServerSpec
	stdin    io.Writer
	attached bool   // a console reader is active for the current run
	install  string // "", StateInstalling or StateInstallFailed; "" = ready
}

type Daemon struct {
	cfg Config
	rt  runtime.Runtime
	log *slog.Logger

	mu      sync.Mutex
	servers map[string]*managed

	wmu  sync.Mutex
	conn *websocket.Conn
}

func New(cfg Config, rt runtime.Runtime, log *slog.Logger) (*Daemon, error) {
	for _, d := range []string{"servers", "state"} {
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, d), 0o755); err != nil {
			return nil, err
		}
	}
	d := &Daemon{cfg: cfg, rt: rt, log: log, servers: map[string]*managed{}}
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
	d.mu.Unlock()
	if err := d.send(sctx, proto.Envelope{Type: proto.EvtHello, Data: proto.Marshal(proto.Hello{Version: Version, Servers: ids})}); err != nil {
		return err
	}

	go d.watchEvents(sctx)
	go d.statsLoop(sctx)
	for _, id := range ids { // surface servers whose install did not finish
		d.mu.Lock()
		m := d.servers[id]
		st := ""
		if m != nil {
			st = m.install
		}
		d.mu.Unlock()
		if st != "" {
			d.emitState(sctx, id, st)
		}
	}
	for _, id := range ids { // re-attach consoles of already-running servers
		if st, err := d.rt.State(sctx, id); err == nil && st == runtime.StateRunning {
			d.attach(sctx, id)
			d.emitState(sctx, id, st)
		}
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

func (d *Daemon) send(ctx context.Context, env proto.Envelope) error {
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
	_ = d.send(ctx, proto.Envelope{Type: proto.EvtState, Data: proto.Marshal(proto.State{UUID: uuid, State: st})})
}

func (d *Daemon) watchEvents(ctx context.Context) {
	for ctx.Err() == nil {
		ch, err := d.rt.Events(ctx)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		for ev := range ch {
			d.emitState(ctx, ev.UUID, ev.State)
			if ev.State == runtime.StateRunning {
				d.attach(ctx, ev.UUID)
			}
		}
		if ctx.Err() == nil {
			time.Sleep(time.Second) // runtime event stream dropped; resubscribe
		}
	}
}

func (d *Daemon) statsLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		d.mu.Lock()
		var ids []string
		for id := range d.servers {
			ids = append(ids, id)
		}
		d.mu.Unlock()
		for _, id := range ids {
			if st, _ := d.rt.State(ctx, id); st != runtime.StateRunning {
				continue
			}
			if s, err := d.rt.Stats(ctx, id); err == nil {
				_ = d.send(ctx, proto.Envelope{Type: proto.EvtStats, Data: proto.Marshal(s)})
			}
		}
	}
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

	r, w, err := d.rt.Attach(ctx, uuid)
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
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			// Send on the session context so a line is never lost to a per-run cancel.
			_ = d.send(ctx, proto.Envelope{Type: proto.EvtConsole, Data: proto.Marshal(proto.Console{UUID: uuid, Line: sc.Text()})})
		}
	}()
}

func (d *Daemon) handle(ctx context.Context, env proto.Envelope) {
	data, err := d.dispatch(ctx, env)
	reply := proto.Envelope{Type: proto.EvtReply, ID: env.ID, Data: data}
	if err != nil {
		reply.Err = err.Error()
		d.log.Warn("request failed", "type", env.Type, "err", err)
	}
	_ = d.send(ctx, reply)
}

func (d *Daemon) dir(uuid string) (*fsx.Dir, error) {
	if !uuidRe.MatchString(uuid) {
		return nil, errors.New("invalid server id")
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
	case proto.ReqServerReinstall:
		var u proto.UUIDOnly
		if err := json.Unmarshal(env.Data, &u); err != nil {
			return nil, err
		}
		return nil, d.reinstall(ctx, u.UUID)
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
		return nil, fs.Write(f.Path, f.Content)
	case proto.ReqFilesMkdir:
		return nil, fs.Mkdir(f.Path)
	default:
		return nil, fs.Delete(f.Path)
	}
}

func (d *Daemon) create(ctx context.Context, s proto.ServerSpec) error {
	if !uuidRe.MatchString(s.UUID) {
		return errors.New("invalid server id")
	}
	dir := d.serverDir(s.UUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Game images typically run as a non-root user; hand over the data dir.
	_ = os.Chown(dir, 1000, 1000)
	b, _ := json.Marshal(s)
	if err := os.WriteFile(d.statePath(s.UUID), b, 0o600); err != nil {
		return err
	}
	abs, _ := filepath.Abs(dir)
	if err := d.rt.Create(ctx, runtime.Spec{ServerSpec: s, DataDir: abs}); err != nil {
		return err
	}
	_ = os.Remove(d.installedPath(s.UUID))
	m := &managed{spec: s}
	if s.Install != nil {
		m.install = StateInstalling
	}
	d.mu.Lock()
	d.servers[s.UUID] = m
	d.mu.Unlock()
	if s.Install == nil {
		d.emitState(ctx, s.UUID, runtime.StateOffline)
		return nil
	}
	// Reply now; the install can take minutes. Progress arrives as console and
	// state events. It must outlive this request's context.
	go d.runInstall(context.WithoutCancel(ctx), s.UUID)
	return nil
}

// runInstall executes the egg install script and records the outcome.
func (d *Daemon) runInstall(ctx context.Context, uuid string) {
	spec, err := d.spec(uuid)
	if err != nil || spec.Install == nil {
		return
	}
	d.setInstall(uuid, StateInstalling)
	d.emitState(ctx, uuid, StateInstalling)
	_ = os.Remove(d.installedPath(uuid))

	say := func(line string) {
		_ = d.send(ctx, proto.Envelope{Type: proto.EvtConsole, Data: proto.Marshal(proto.Console{UUID: uuid, Line: line})})
	}
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
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			say(sc.Text())
		}
	}()
	err = d.rt.Install(ctx, runtime.InstallRun{
		UUID: uuid, Image: spec.Install.Image, Entrypoint: spec.Install.Entrypoint,
		ScriptDir: absTmp, DataDir: abs, Env: spec.Env, MemoryMB: spec.MemoryMB,
	}, pw)
	pw.Close()
	<-done
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
		return errors.New("this server's egg has no install script")
	}
	if st, _ := d.rt.State(ctx, uuid); st != runtime.StateOffline {
		return errors.New("stop the server before reinstalling")
	}
	d.mu.Lock()
	m := d.servers[uuid]
	if m.install == StateInstalling {
		d.mu.Unlock()
		return errors.New("an install is already running")
	}
	m.install = StateInstalling
	d.mu.Unlock()
	go d.runInstall(context.WithoutCancel(ctx), uuid)
	return nil
}

func (d *Daemon) spec(uuid string) (proto.ServerSpec, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.servers[uuid]
	if m == nil {
		return proto.ServerSpec{}, errors.New("unknown server")
	}
	return m.spec, nil
}

func (d *Daemon) power(ctx context.Context, p proto.Power) error {
	spec, err := d.spec(p.UUID)
	if err != nil {
		return err
	}
	d.mu.Lock()
	inst := d.servers[p.UUID].install
	d.mu.Unlock()
	if inst != "" && (p.Action == "start" || p.Action == "restart") {
		return errors.New("server is not installed (" + inst + ")")
	}
	switch p.Action {
	case "start":
		d.emitState(ctx, p.UUID, runtime.StateStarting)
		if err := d.rt.Start(ctx, p.UUID); err != nil {
			d.emitState(ctx, p.UUID, runtime.StateOffline)
			return err
		}
		d.attach(ctx, p.UUID)
		d.emitState(ctx, p.UUID, runtime.StateRunning)
		return nil
	case "stop":
		return d.stop(ctx, spec)
	case "restart":
		if err := d.stop(ctx, spec); err != nil {
			return err
		}
		return d.power(ctx, proto.Power{UUID: p.UUID, Action: "start"})
	case "kill":
		return d.rt.Kill(ctx, p.UUID)
	}
	return fmt.Errorf("unknown power action %q", p.Action)
}

// stop sends the egg's graceful stop command (or SIGTERM for "^C"), then
// escalates to a hard stop if the server has not exited in time.
func (d *Daemon) stop(ctx context.Context, spec proto.ServerSpec) error {
	d.emitState(ctx, spec.UUID, runtime.StateStopping)
	if spec.Stop != "" && spec.Stop != "^C" {
		if err := d.input(spec.UUID, spec.Stop); err == nil {
			deadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				if st, _ := d.rt.State(ctx, spec.UUID); st == runtime.StateOffline {
					return nil
				}
				time.Sleep(250 * time.Millisecond)
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
		return errors.New("server is not running")
	}
	_, err := io.WriteString(w, line+"\n")
	return err
}

func (d *Daemon) remove(ctx context.Context, uuid string) error {
	if !uuidRe.MatchString(uuid) {
		return errors.New("invalid server id")
	}
	_ = d.rt.Remove(ctx, uuid)
	d.mu.Lock()
	delete(d.servers, uuid)
	d.mu.Unlock()
	_ = os.Remove(d.statePath(uuid))
	_ = os.Remove(d.installedPath(uuid))
	return os.RemoveAll(d.serverDir(uuid))
}
