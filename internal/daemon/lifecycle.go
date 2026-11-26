package daemon

// Creating, installing, reinstalling and removing servers.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

func (d *Daemon) create(ctx context.Context, s proto.ServerSpec) error {
	if !uuidRe.MatchString(s.UUID) {
		return proto.Invalid("invalid server id")
	}
	dir := d.serverDir(s.UUID)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // G301: the game's user owns it (below) and must traverse it
		return err
	}
	// Game images typically run as a non-root user; hand over the data dir.
	// Files taken over from Wings keep the user Wings ran the server as.
	if _, err := os.Stat(d.adoptedPath(s.UUID)); err != nil {
		uid, gid := d.containerIDs()
		_ = os.Chown(dir, uid, gid)
	}
	// The container is built in the background, since pulling its image can
	// take longer than the panel waits for a reply. Until it exists the server
	// is stale, so a start, also after a daemon restart, builds it. The marker
	// goes first: a state file without it would mean a container exists.
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
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
	// create checked the id already; checked again here, where it becomes
	// marker paths, so that holds even if another caller appears.
	if !uuidRe.MatchString(uuid) {
		return
	}
	spec, err := d.spec(uuid)
	if err != nil {
		return
	}
	_, adoptErr := os.Stat(d.adoptedPath(uuid))
	adopted := adoptErr == nil
	if !adopted { // an adopted server is installed already: no install events
		d.emitState(ctx, uuid, StateInstalling)
	}
	d.sendConsole(uuid, "[gamepanel] preparing the server's image "+spec.Image)
	abs, _ := filepath.Abs(d.serverDir(uuid))
	d.migrateOwner(uuid)
	prepared := false
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
		prepared = true
	}
	switch {
	case adopted:
		// Its files came from Wings (adopt-wings): it is installed already.
		if err := os.WriteFile(d.installedPath(uuid), nil, 0o600); err != nil {
			d.log.Warn("could not record an adopted server as installed", "server", uuid, "err", err)
		}
		if err := os.Remove(d.adoptedPath(uuid)); err != nil {
			d.log.Warn("could not clear the adopted marker", "server", uuid, "err", err)
		}
		d.setInstall(uuid, "")
		d.sendConsole(uuid, "\x1b[32m[gamepanel] kept the server's files from Pterodactyl; nothing was reinstalled\x1b[0m")
		if n := d.claimBackups(uuid, spec.Backups); n > 0 {
			d.sendConsole(uuid, fmt.Sprintf("[gamepanel] kept %d backup(s) from Pterodactyl", n))
		}
		d.handOver(ctx, spec)
		d.emitState(ctx, uuid, runtime.StateOffline)
	case spec.Install == nil:
		d.setInstall(uuid, "")
		if prepared {
			// Without an install script nothing else says the server is ready.
			d.sendConsole(uuid, "\x1b[32m[gamepanel] install complete\x1b[0m")
		}
		d.emitState(ctx, uuid, runtime.StateOffline)
	default:
		d.runInstall(ctx, uuid)
	}
	// An adopted server that Wings had running starts again, as one does
	// after a reboot.
	resume := adopted && !d.cfg.NoResume && d.shouldRun(uuid)
	if !spec.StartOnCompletion && !resume {
		return
	}
	if st, _ := d.installState(uuid); st != "" {
		return // the install failed and said so
	}
	cur, err := d.spec(uuid)
	if err != nil {
		return // deleted meanwhile
	}
	if err := d.start(ctx, cur); err != nil {
		d.log.Warn("could not start a new server", "server", uuid, "err", err)
		d.sendConsole(uuid, "\x1b[31m[gamepanel] could not start the server: "+forPeople(err).Error()+"\x1b[0m")
	}
}

// handOver stops an adopted server that still runs under Wings, the way
// Wings stops it: the egg's stop command, so the game saves its world.
func (d *Daemon) handOver(ctx context.Context, spec proto.ServerSpec) {
	if !uuidRe.MatchString(spec.UUID) {
		return
	}
	defer func() {
		if err := os.Remove(d.handoverPath(spec.UUID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			d.log.Warn("could not clear the Wings handover marker", "server", spec.UUID, "err", err)
		}
	}()
	wc, ok := d.rt.(WingsContainers)
	if _, err := os.Stat(d.handoverPath(spec.UUID)); err != nil || !ok {
		return
	}
	if on, err := wc.WingsRunning(ctx, spec.UUID); err != nil || !on {
		return
	}
	d.sendConsole(spec.UUID, "[gamepanel] stopping the server under Wings ("+spec.Stop+"), to start it here")
	// Longer than a normal stop: this happens once, and a large world can
	// take a while to save.
	if err := wc.StopWings(ctx, spec.UUID, spec.Stop, 120); err != nil {
		d.log.Warn("could not stop the server under Wings", "server", spec.UUID, "err", err)
		d.sendConsole(spec.UUID, "\x1b[31m[gamepanel] could not stop the server under Wings: "+err.Error()+"\x1b[0m")
	}
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
	defer d.removeAll(tmp)
	// World-readable: the installer container runs as its own user.
	if err := os.WriteFile(filepath.Join(tmp, "install.sh"), []byte(spec.Install.Script), 0o644); err != nil { //nolint:gosec // G306: see above
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
	_ = pw.Close() // a PipeWriter's Close always returns nil
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
	_ = os.Remove(d.runningPath(uuid))
	_ = os.RemoveAll(filepath.Join(d.cfg.DataDir, "backups", uuid))
	return os.RemoveAll(d.serverDir(uuid))
}
