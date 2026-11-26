package daemon

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"syscall"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
)

// markOutdated marks servers whose containers an earlier daemon built (see
// runtime.Outdateder) as stale, so each is rebuilt, and gets the current
// isolation, the next time it starts. Running servers keep their container
// until then.
func (d *Daemon) markOutdated(ctx context.Context) {
	o, ok := d.rt.(runtime.Outdateder)
	if !ok {
		return
	}
	ids, err := o.Outdated(ctx)
	if err != nil {
		d.log.Warn("could not check for containers built by an older daemon", "err", err)
		return
	}
	n := 0
	for _, id := range ids {
		if !uuidRe.MatchString(id) {
			continue
		}
		d.mu.Lock()
		m := d.servers[id]
		if m != nil && !m.stale {
			if err := os.WriteFile(d.stalePath(id), nil, 0o600); err == nil {
				m.stale = true
				n++
			}
		}
		d.mu.Unlock()
	}
	if n > 0 {
		d.log.Info("servers built by an older daemon are rebuilt at their next start", "count", n)
	}
}

// migrateOwner hands a server's files from LegacyContainerID to the
// configured container user, once that is set to someone else. Only entries
// the legacy user owns change: files an image or a person gave to another
// user keep it. The server is stopped (its container is about to be built),
// and Lchown never follows a symlink, so the game cannot redirect it.
func (d *Daemon) migrateOwner(uuid string) {
	uid, gid := d.containerIDs()
	if uid == LegacyContainerID || !uuidRe.MatchString(uuid) {
		return
	}
	// Every lookup goes through the server's root, so a symlink or a folder
	// swapped for one during the walk (by an SFTP session, say) cannot lead
	// it outside: os.Root refuses paths that leave the directory.
	root, err := os.OpenRoot(d.serverDir(uuid))
	if err != nil {
		return
	}
	defer closeQuiet(root)
	owner := func(name string) (uid, gid uint32, ok bool) {
		fi, err := root.Lstat(name)
		if err != nil {
			return 0, 0, false
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return 0, 0, false
		}
		return st.Uid, st.Gid, true
	}
	if u, _, ok := owner("."); !ok || u != LegacyContainerID {
		return
	}
	n := 0
	err = fs.WalkDir(root.FS(), ".", func(name string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil // last, below: it is what says whether to migrate
		}
		u, g, ok := owner(name)
		if !ok || u != LegacyContainerID {
			return nil
		}
		ngid := int(g)
		if g == LegacyContainerID {
			ngid = gid
		}
		n++
		return root.Lchown(name, uid, ngid)
	})
	if err == nil {
		err = root.Lchown(".", uid, gid)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		d.log.Warn("could not hand all of a server's files to the container user", "server", oneLine(uuid), "err", err)
		return
	}
	if n > 0 {
		d.log.Info("handed a server's files to the container user", "server", oneLine(uuid), "uid", uid, "files", n)
	}
}
