package daemon

import (
	"context"
	"os"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
)

// Servers come back after a reboot. Game containers have no Docker restart
// policy (the daemon owns their lifecycle, so crash limits and suspension
// hold), so after the node or Docker restarts they would all stay offline.
// Instead the daemon keeps a marker, state/<uuid>.running, for every server
// that should be running: written when a start succeeds, removed when someone
// stops or kills it, when it exits cleanly on its own (e.g. "stop" typed in
// its console), and when it crashes and is left stopped. At startup the daemon
// starts every marked server that is not running.

func (d *Daemon) runningPath(uuid string) string {
	return d.statePath(uuid)[:len(d.statePath(uuid))-len(".json")] + ".running"
}

// markRunning records whether the server should be running after a restart.
func (d *Daemon) markRunning(uuid string, on bool) {
	if !uuidRe.MatchString(uuid) {
		return
	}
	p := d.runningPath(uuid)
	if !on {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			d.log.Warn("could not clear the running marker", "server", uuid, "err", err)
		}
		return
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		d.log.Warn("could not save that the server runs; it will not start again after a reboot", "server", uuid, "err", err)
	}
}

func (d *Daemon) shouldRun(uuid string) bool {
	_, err := os.Stat(d.runningPath(uuid))
	return err == nil
}

// resume starts every server that was running when the daemon last stopped
// and is offline now. It runs once, before the first panel connection, so a
// node whose panel is unreachable still brings its servers back. It returns
// how many it started.
func (d *Daemon) resume(ctx context.Context) int {
	if d.cfg.NoResume {
		return 0
	}
	d.mu.Lock()
	var ids []string
	for id, m := range d.servers {
		if m.install == "" && !m.spec.Suspended && d.shouldRun(id) {
			ids = append(ids, id)
		}
	}
	d.mu.Unlock()
	started := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		st, err := d.rt.State(ctx, id)
		if err != nil || st != runtime.StateOffline {
			continue // still running (only the daemon restarted), or no container
		}
		spec, err := d.spec(id)
		if err != nil {
			continue
		}
		d.log.Info("starting a server that was running before the restart", "server", id)
		if err := d.start(ctx, spec); err != nil {
			d.log.Warn("could not start the server again", "server", id, "err", err)
			d.markRunning(id, false)
			continue
		}
		d.sendConsole(id, "\x1b[33m[gamepanel] started again: it was running when the node went down\x1b[0m")
		started++
	}
	return started
}
