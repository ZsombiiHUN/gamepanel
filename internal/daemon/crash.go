package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// crashWindow: a server that crashes again this soon after being restarted
// for a crash is left stopped, so a broken server does not loop forever.
const crashWindow = 60 * time.Second

// crashRestartDelay is a short pause before restarting a crashed server.
var crashRestartDelay = 2 * time.Second

// expectStop marks the current run as being stopped on purpose (stop, kill,
// restart, a disk-limit stop), so its exit is not taken for a crash.
func (d *Daemon) expectStop(uuid string) {
	d.mu.Lock()
	if m := d.servers[uuid]; m != nil {
		m.stopping = true
	}
	d.mu.Unlock()
}

// exited handles the end of a server's process. It reports whether the exit
// was a crash; the caller then reports no plain offline state for it.
func (d *Daemon) exited(ctx context.Context, uuid string, x runtime.Exit) bool {
	if ctx.Err() != nil {
		// The daemon is shutting down, likely with Docker and the machine:
		// that is no reason to forget the server should run (see resume).
		return false
	}
	d.mu.Lock()
	m := d.servers[uuid]
	if m == nil || m.install != "" || m.op == opDelete || m.op == opRestore {
		d.mu.Unlock()
		return false
	}
	asked := m.stopping
	// Exit code 0 is a clean stop, e.g. "stop" typed in the game console.
	if asked || (x.Code == 0 && !x.OOM) {
		d.mu.Unlock()
		if !asked {
			d.markRunning(uuid, false) // it stopped itself; leave it stopped after a reboot
		}
		return false
	}
	spec := m.spec
	now := time.Now()
	c := proto.Crash{UUID: uuid, ExitCode: x.Code, OOM: x.OOM}
	switch {
	case spec.NoCrashRestart:
		c.Reason = "automatic restart is off for this server"
	case !m.lastCrash.IsZero() && now.Sub(m.lastCrash) < crashWindow:
		c.Reason = "it crashed again less than a minute after the last restart"
	default:
		c.Restarting = true
	}
	m.lastCrash = now
	m.crashed = !c.Restarting
	d.mu.Unlock()

	how := fmt.Sprintf("exit code %d", x.Code)
	if x.OOM {
		how += ", killed for running out of memory"
	}
	if c.Restarting {
		d.sendConsole(uuid, "\x1b[31m[gamepanel] the server crashed ("+how+"); restarting it\x1b[0m")
	} else {
		d.sendConsole(uuid, "\x1b[31m[gamepanel] the server crashed ("+how+"); not restarting it: "+c.Reason+"\x1b[0m")
	}
	_ = d.send(ctx, proto.Envelope{Type: proto.EvtCrash, Data: proto.Marshal(c)})
	if !c.Restarting {
		d.markRunning(uuid, false)
		d.emitState(ctx, uuid, proto.StateCrashed)
		return true
	}
	d.emitState(ctx, uuid, runtime.StateOffline)
	go func() {
		ctx := context.WithoutCancel(ctx)
		if !sleep(ctx, crashRestartDelay) {
			return
		}
		spec, err := d.spec(uuid)
		if err != nil {
			return
		}
		if err := d.start(ctx, spec); err != nil {
			d.sendConsole(uuid, "\x1b[31m[gamepanel] could not restart the server: "+forPeople(err).Error()+"\x1b[0m")
			d.mu.Lock()
			if m := d.servers[uuid]; m != nil {
				m.crashed = true
			}
			d.mu.Unlock()
			d.markRunning(uuid, false)
			d.emitState(ctx, uuid, proto.StateCrashed)
		}
	}()
	return true
}
