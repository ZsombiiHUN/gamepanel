package daemon

// Starting, stopping, restarting, killing servers and their console input.

import (
	"context"
	"io"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/cfgfile"
	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

func (d *Daemon) power(ctx context.Context, p proto.Power) error {
	spec, err := d.spec(p.UUID)
	if err != nil {
		return err
	}
	switch p.Action {
	case "start":
		return d.start(ctx, spec)
	case "stop":
		return d.stop(ctx, spec.UUID)
	case "restart":
		return d.restart(ctx, spec.UUID)
	case "kill":
		return d.kill(ctx, spec.UUID)
	}
	return proto.Invalid("unknown power action")
}

// start boots an installed server that is within its disk limit. It claims
// the server, so it cannot interleave with a stop, a restore, an update or a
// delete.
func (d *Daemon) start(ctx context.Context, spec proto.ServerSpec) error {
	end, err := d.beginAfterStop(ctx, spec.UUID, opStart)
	if err != nil {
		return err
	}
	defer end()
	return d.startClaimed(ctx, spec.UUID)
}

// beginAfterStop is begin that waits a moment for a stop or a kill to let go
// of the server. Their claim outlives the exit by a poll interval, so a start
// sent just after the server went offline would otherwise be turned away.
func (d *Daemon) beginAfterStop(ctx context.Context, uuid, op string) (func(), error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		end, err := d.begin(uuid, op)
		if err == nil || time.Now().After(deadline) {
			return end, err
		}
		d.mu.Lock()
		m := d.servers[uuid]
		stopping := m != nil && (m.op == opStop || m.op == opKill)
		d.mu.Unlock()
		if !stopping {
			return end, err
		}
		if !sleep(ctx, 25*time.Millisecond) {
			return nil, ctx.Err()
		}
	}
}

// startClaimed is start for a caller that already holds the server's claim.
func (d *Daemon) startClaimed(ctx context.Context, uuid string) error {
	if inst, _ := d.installState(uuid); inst == StateInstallFailed {
		return proto.Conflict("the install failed; reinstall the server before starting it")
	}
	// Read the spec again under the claim: a suspension that landed after the
	// caller read it still counts.
	spec, err := d.spec(uuid)
	if err != nil {
		return err
	}
	if spec.Suspended {
		return proto.Conflict("this server is suspended")
	}
	if spec.DiskMB > 0 {
		if used, err := d.diskUsage(uuid); err == nil && used > spec.DiskMB<<20 {
			return DiskFullError{used, spec.DiskMB << 20}
		}
	}
	d.mu.Lock()
	if m := d.servers[uuid]; m != nil {
		m.crashed, m.booted = false, false
	}
	d.mu.Unlock()
	d.emitState(ctx, uuid, runtime.StateStarting)
	d.applyConfigFiles(ctx, spec)
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
	d.markRunning(uuid, true)
	d.emitState(ctx, uuid, runtime.StateRunning)
	return nil
}

// applyConfigFiles edits the egg's config files before a start. A file that
// cannot be edited does not stop the start (Wings does the same); the owner
// is told on the console.
func (d *Daemon) applyConfigFiles(ctx context.Context, spec proto.ServerSpec) {
	if len(spec.ConfigFiles) == 0 {
		return
	}
	dir, err := d.dir(spec.UUID)
	if err != nil {
		d.log.Warn("could not open the server's files to update its config", "server", oneLine(spec.UUID), "err", err)
		return
	}
	defer closeQuiet(dir)
	iface := runtime.DefaultGateway
	if g, ok := d.rt.(runtime.Gatewayer); ok {
		if ip, err := g.Gateway(ctx); err == nil {
			iface = ip
		}
	}
	for _, f := range spec.ConfigFiles {
		if err := cfgfile.Apply(dir, f, iface); err != nil {
			d.log.Warn("could not update a config file", "server", oneLine(spec.UUID), "file", oneLine(f.Path), "err", oneLine(err.Error()))
			d.sendConsole(spec.UUID, "\x1b[33m[gamepanel] could not update "+f.Path+": "+forPeople(err).Error()+"\x1b[0m")
		}
	}
	d.cacheFor(spec.UUID).invalidate()
}

// stop stops the server gracefully. It claims the server, so a stop cannot
// land in the middle of a start and be undone by it.
func (d *Daemon) stop(ctx context.Context, uuid string) error {
	end, err := d.begin(uuid, opStop)
	if err != nil {
		return err
	}
	defer end()
	return d.stopClaimed(ctx, uuid)
}

// stopWhenFree is stop for the node's own reasons (a suspension, the disk
// limit): it cannot be turned away, so it waits for whatever holds the server
// to finish, then stops it.
func (d *Daemon) stopWhenFree(ctx context.Context, uuid string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	for {
		end, err := d.begin(uuid, opStop)
		if err == nil {
			return d.stopClaimedThenEnd(ctx, uuid, end)
		}
		if proto.CodeOf(err) != proto.CodeConflict {
			return err
		}
		if !sleep(ctx, 250*time.Millisecond) {
			return ctx.Err()
		}
	}
}

// restart stops the server and starts it again under one claim, so nothing
// else can slip in between. A kill during the stop ends the restart there.
func (d *Daemon) restart(ctx context.Context, uuid string) error {
	end, err := d.begin(uuid, opRestart)
	if err != nil {
		return err
	}
	defer end()
	d.mu.Lock()
	m := d.servers[uuid]
	m.killed = false
	d.mu.Unlock()
	if err := d.stopClaimed(ctx, uuid); err != nil {
		return err
	}
	// From here the restart is a start: a kill is turned away like one,
	// since the start would undo it.
	d.mu.Lock()
	killed := m.killed
	m.op = opStart
	d.mu.Unlock()
	if killed {
		return nil
	}
	return d.startClaimed(ctx, uuid)
}

// kill ends the server's process at once. It goes through while a stop or a
// restart holds the server, since cutting a hung stop short is what it is
// for, and ends a restart instead of letting it start the server again. Any
// other operation turns it away: a kill during a start would be undone by it.
// A server that is not running has nothing to kill.
func (d *Daemon) kill(ctx context.Context, uuid string) error {
	d.mu.Lock()
	m := d.servers[uuid]
	if m == nil {
		d.mu.Unlock()
		return errUnknownServer
	}
	release := func() {}
	switch {
	case m.install == StateInstalling:
		d.mu.Unlock()
		return opBusy(opInstall)
	case m.op == opStop:
	case m.op == opRestart:
		m.killed = true
	case m.op != "":
		op := m.op
		d.mu.Unlock()
		return opBusy(op)
	default:
		m.op = opKill
		release = func() { d.mu.Lock(); m.op = ""; d.mu.Unlock() }
	}
	d.mu.Unlock()
	defer release()
	d.markRunning(uuid, false)
	if st, err := d.rt.State(ctx, uuid); (err == nil && st == runtime.StateOffline) || (err != nil && d.isStale(uuid)) {
		return nil
	}
	d.expectStop(uuid)
	return d.rt.Kill(ctx, uuid)
}

// stopClaimed sends the egg's graceful stop command (or SIGTERM for "^C"),
// then escalates to a hard stop if the server has not exited in time. The
// caller holds the server's claim.
func (d *Daemon) stopClaimed(ctx context.Context, uuid string) error {
	spec, err := d.spec(uuid)
	if err != nil {
		return err
	}
	d.markRunning(uuid, false)
	// Nothing to stop. Saying "stopping" here would leave the server shown as
	// stopping, since no exit follows.
	if st, err := d.rt.State(ctx, uuid); (err == nil && st == runtime.StateOffline) || (err != nil && d.isStale(uuid)) {
		return nil
	}
	d.expectStop(uuid)
	d.emitState(ctx, uuid, runtime.StateStopping)
	if spec.Stop != "" && spec.Stop != "^C" {
		if err := d.input(uuid, spec.Stop); err == nil {
			deadline := time.Now().Add(gracefulStop)
			for time.Now().Before(deadline) {
				if st, _ := d.rt.State(ctx, uuid); st == runtime.StateOffline {
					return nil
				}
				if !sleep(ctx, 250*time.Millisecond) {
					return ctx.Err()
				}
			}
		}
	}
	return d.rt.Stop(ctx, uuid, 10)
}

// gracefulStop is how long a stop waits for the server to exit on its own
// stop command before it is stopped by signal.
var gracefulStop = 30 * time.Second

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

// stopClaimedThenEnd stops a server whose stop was claimed with begin and
// releases the claim, even if stopping panics.
func (d *Daemon) stopClaimedThenEnd(ctx context.Context, uuid string, end func()) error {
	defer end()
	return d.stopClaimed(ctx, uuid)
}
