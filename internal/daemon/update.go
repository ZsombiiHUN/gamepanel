package daemon

// Applying new settings to a server, now or at its next start.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// update applies a new spec (limits, image, startup, variables, ports). A
// stopped server gets a new container now; a running one keeps its container
// until it next starts, so nothing needs to be stopped to make a change. Files,
// install state and backups stay either way.
func (d *Daemon) update(ctx context.Context, s proto.ServerSpec) (proto.UpdateResult, error) {
	old, err := d.spec(s.UUID)
	if err != nil {
		return proto.UpdateResult{}, err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return proto.UpdateResult{}, err
	}
	if sameContainer(old, s) {
		// Nothing the container is built from changed (the crash restart
		// setting, a suspension), so it applies at once and needs no rebuild.
		// It must also apply while an install or another operation runs: a
		// suspension cannot wait for those to end.
		d.mu.Lock()
		m := d.servers[s.UUID]
		if m == nil || m.op == opDelete {
			d.mu.Unlock()
			return proto.UpdateResult{}, errUnknownServer
		}
		if err := os.WriteFile(d.statePath(s.UUID), b, 0o600); err != nil {
			d.mu.Unlock()
			return proto.UpdateResult{}, err
		}
		m.spec = s
		d.mu.Unlock()
		if s.Suspended && !old.Suspended {
			go d.stopSuspended(context.WithoutCancel(ctx), s)
		}
		return proto.UpdateResult{}, nil
	}
	end, err := d.begin(s.UUID, opUpdate)
	if err != nil {
		return proto.UpdateResult{}, err
	}
	defer end()
	if err := os.WriteFile(d.statePath(s.UUID), b, 0o600); err != nil {
		return proto.UpdateResult{}, err
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
		// A suspension stops the server now, whatever else changed with it;
		// the new container is built when it is next started.
		if s.Suspended && !old.Suspended {
			go d.stopSuspended(context.WithoutCancel(ctx), s)
		}
		return proto.UpdateResult{Pending: true}, nil
	}
	abs, _ := filepath.Abs(d.serverDir(s.UUID))
	d.migrateOwner(s.UUID)
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

// stopSuspended stops a server that was just suspended, if it runs.
func (d *Daemon) stopSuspended(ctx context.Context, spec proto.ServerSpec) {
	st, err := d.rt.State(ctx, spec.UUID)
	if err != nil || st == runtime.StateOffline {
		return
	}
	d.sendConsole(spec.UUID, "\x1b[33m[gamepanel] this server was suspended; stopping it\x1b[0m")
	if err := d.stopWhenFree(ctx, spec.UUID); err != nil {
		d.log.Warn("could not stop a suspended server", "server", spec.UUID, "err", err)
	}
}

// sameContainer reports whether two specs build the same container.
func sameContainer(a, b proto.ServerSpec) bool {
	a.NoCrashRestart, b.NoCrashRestart = false, false
	a.Suspended, b.Suspended = false, false
	a.StartOnCompletion, b.StartOnCompletion = false, false
	a.Backups, b.Backups = nil, nil         // read once, when the server is created
	a.ConfigFiles, b.ConfigFiles = nil, nil // edited before each start, not part of the container
	ja, erra := json.Marshal(a)
	jb, errb := json.Marshal(b)
	// Specs that cannot be compared are treated as different: a rebuild is safe.
	return erra == nil && errb == nil && bytes.Equal(ja, jb)
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
	d.migrateOwner(uuid)
	if err := d.rt.Create(ctx, runtime.Spec{ServerSpec: spec, DataDir: abs}); err != nil {
		return fmt.Errorf("could not apply the new settings: %w", err)
	}
	_ = os.Remove(d.stalePath(uuid))
	d.mu.Lock()
	m.stale = false
	d.mu.Unlock()
	return nil
}
