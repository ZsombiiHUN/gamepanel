package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// Taking over a node from Pterodactyl's Wings: its server folders
// (/var/lib/pterodactyl/volumes/<uuid>) move into the daemon's data directory
// unchanged, and a marker, state/<uuid>.adopted, tells the daemon that the
// server is installed. When the panel then creates the server on this node
// (it was imported with the same UUID), the daemon builds its container
// without running the egg's install script, which would download the game
// again or overwrite its files.
//
// Files keep their owner. Wings ran each server as its own user (988 by
// default) and so does the daemon: a container runs as whoever owns the
// server's folder.
//
// A server may keep running under Wings while its folder moves: on the same
// file system a move is a rename, and the running container's bind mount
// follows the folder. A second marker, state/<uuid>.wings, then makes the
// daemon stop Wings' container the way Wings would (the egg's stop command,
// which the panel sends with the server) before it starts the server itself.

// WingsContainers is what adopting needs from Docker: Wings' own containers
// are named by the server's bare UUID.
type WingsContainers interface {
	WingsRunning(ctx context.Context, uuid string) (bool, error)
	// StopWings sends stop (a console command, or "^C"/"" for SIGINT) and
	// kills the server if it still runs after timeout seconds.
	StopWings(ctx context.Context, uuid, stop string, timeout int) error
}

// AdoptOptions says where Wings kept its servers and what to do with ones
// that still run.
type AdoptOptions struct {
	From    string // Wings' volumes directory
	DataDir string // the daemon's GAMEPANEL_DATA_DIR
	// States is Wings' states.json; servers it lists as running are started
	// again once the panel has created them here. "" = not read.
	States string
	// Backups is Wings' backup directory (<backup uuid>.tar.gz). Its archives
	// move to backups/wings/ in the data directory, and each server claims
	// its own when the panel creates it here. "" or a missing directory =
	// none.
	Backups string
	// StopRunning stops servers Wings still runs when their folders must be
	// copied (another file system): SIGINT, then SIGKILL after StopTimeout
	// seconds. Game images often ignore the signal, so stopping them in
	// Pterodactyl first is kinder to their worlds.
	StopRunning bool
	StopTimeout int
	Docker      WingsContainers
	Out         io.Writer
}

// AdoptResult counts what happened, by server UUID.
type AdoptResult struct {
	Moved, Copied []string
	Backups       int // backup archives taken over
}

func (d *Daemon) adoptedPath(uuid string) string {
	return filepath.Join(d.cfg.DataDir, "state", uuid+".adopted")
}

// handoverPath marks an adopted server whose Wings container may still run.
func (d *Daemon) handoverPath(uuid string) string {
	return filepath.Join(d.cfg.DataDir, "state", uuid+".wings")
}

// sameFS reports whether two paths are on one file system, so a rename
// between them moves a folder at once.
func sameFS(a, b string) bool {
	sa, errA := os.Stat(a)
	sb, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	x, okA := sa.Sys().(*syscall.Stat_t)
	y, okB := sb.Sys().(*syscall.Stat_t)
	return okA && okB && x.Dev == y.Dev
}

// AdoptWings takes over every server folder in o.From. It does not need the
// daemon to be running, and must run before the daemon first connects to
// the panel after the import: a server the panel creates before its files
// are here is installed from scratch.
func AdoptWings(ctx context.Context, o AdoptOptions) (AdoptResult, error) {
	var res AdoptResult
	out := o.Out
	if out == nil {
		out = io.Discard
	}
	ents, err := os.ReadDir(o.From)
	if err != nil {
		return res, err
	}
	for _, sub := range []string{"servers", "state"} {
		if err := os.MkdirAll(filepath.Join(o.DataDir, sub), 0o755); err != nil { //nolint:gosec // G301: as daemon.New makes them
			return res, err
		}
	}
	d := &Daemon{cfg: Config{DataDir: o.DataDir}}
	running := wingsRunning(o.States)

	var ids []string
	for _, e := range ents {
		if e.IsDir() && uuidRe.MatchString(e.Name()) {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		// A second run (install.sh run again after a later step failed)
		// finds the folders already moved: that is done, not an error.
		if have, _ := os.ReadDir(filepath.Join(o.DataDir, "servers")); len(have) > 0 {
			_, _ = fmt.Fprintf(out, "nothing left to take over in %s; %d server folder(s) are already in %s\n", o.From, len(have), o.DataDir) // progress only
			res.Backups, err = d.adoptBackups(o.Backups, out)
			return res, err
		}
		return res, fmt.Errorf("no server folders in %s", o.From)
	}

	// Check everything first, so a refusal leaves every folder where it was.
	move := sameFS(o.From, filepath.Join(o.DataDir, "servers"))
	live := map[string]bool{}
	var busy []string
	for _, id := range ids {
		if o.Docker == nil {
			continue
		}
		on, err := o.Docker.WingsRunning(ctx, id)
		if err != nil {
			return res, fmt.Errorf("ask Docker about %s: %w", id, err)
		}
		if on {
			live[id] = true
			busy = append(busy, id)
		}
	}
	if len(busy) > 0 && !move {
		// A copy of a folder a game is writing to is not a copy.
		if !o.StopRunning {
			return res, fmt.Errorf("%d server(s) still run under Wings: %v, and %s is on another file system than %s, so their files must be copied while they are stopped. Stop them in Pterodactyl first, or pass --stop-running", len(busy), busy, o.From, o.DataDir)
		}
		for _, id := range busy {
			_, _ = fmt.Fprintf(out, "stopping %s\n", id) // progress only
			if err := o.Docker.StopWings(ctx, id, "", o.StopTimeout); err != nil {
				return res, fmt.Errorf("stop %s: %w", id, err)
			}
			live[id] = false
		}
	}

	for _, id := range ids {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		how, err := d.adoptOne(filepath.Join(o.From, id), id)
		switch {
		case err != nil:
			return res, fmt.Errorf("%s: %w", id, err)
		case how == "copied":
			res.Copied = append(res.Copied, id)
		default:
			res.Moved = append(res.Moved, id)
		}
		if live[id] {
			// Still running under Wings: the daemon stops it there with the
			// egg's stop command, then starts it itself.
			if err := os.WriteFile(d.handoverPath(id), nil, 0o600); err != nil {
				return res, err
			}
		}
		if running[id] || live[id] {
			// Started once the panel has created it here, as the daemon
			// starts servers again after a reboot.
			if err := os.WriteFile(d.runningPath(id), nil, 0o600); err != nil {
				return res, err
			}
		}
		_, _ = fmt.Fprintf(out, "%s %s\n", how, id) // progress only
	}
	res.Backups, err = d.adoptBackups(o.Backups, out)
	return res, err
}

// wingsBackupPath is where a backup archive taken over from Wings waits
// until its server claims it.
func (d *Daemon) wingsBackupPath(id string) string {
	return filepath.Join(d.cfg.DataDir, "backups", "wings", id+".tar.gz")
}

// adoptBackups moves (or copies) Wings' backup archives to backups/wings/.
// Wings names them by the backup's id alone, so which server one belongs to
// is only known once the panel creates the server here (claimBackups).
func (d *Daemon) adoptBackups(from string, out io.Writer) (int, error) {
	if from == "" {
		return 0, nil
	}
	ents, err := os.ReadDir(from)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".tar.gz")
		if !ok || !e.Type().IsRegular() || !uuidRe.MatchString(id) {
			continue
		}
		dst := d.wingsBackupPath(id)
		if n == 0 {
			if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
				return n, err
			}
		}
		if err := moveFile(filepath.Join(from, e.Name()), dst); err != nil {
			return n, fmt.Errorf("backup %s: %w", id, err)
		}
		n++
	}
	if n > 0 {
		_, _ = fmt.Fprintf(out, "took over %d backup archive(s) from %s\n", n, from) // progress only
	}
	return n, nil
}

// moveFile renames a file, or across file systems copies it and removes the
// original once the copy is complete.
func moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer closeQuiet(in)
	tmp := dst + ".part"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close() // the copy error is what matters
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

// claimBackups puts the archives of a server's backups that came from Wings
// where its own backups are kept. A backup without one (Wings had already
// lost it) is left for the panel to report when it is restored.
func (d *Daemon) claimBackups(uuid string, ids []string) int {
	n := 0
	for _, id := range ids {
		if !uuidRe.MatchString(id) {
			continue
		}
		src, dst := d.wingsBackupPath(id), d.backupPath(uuid, id)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			d.log.Warn("could not take over a backup from Wings", "server", uuid, "backup", id, "err", err)
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			d.log.Warn("could not take over a backup from Wings", "server", uuid, "backup", id, "err", err)
			continue
		}
		n++
	}
	return n
}

// adoptOne moves (or, across file systems, copies) one server folder and
// says which it did.
func (d *Daemon) adoptOne(src, id string) (string, error) {
	dst := d.serverDir(id)
	if _, err := os.Stat(d.statePath(id)); err == nil {
		// The panel already created it here: it was installed from scratch.
		return "", fmt.Errorf("the daemon already set this server up (%s exists); stop the daemon, delete %s and that file, and run this again", d.statePath(id), dst)
	}
	if ents, err := os.ReadDir(dst); err == nil {
		if len(ents) > 0 {
			return "", fmt.Errorf("%s already has files", dst)
		}
		if err := os.Remove(dst); err != nil {
			return "", err
		}
	}
	how := "moved"
	if err := os.Rename(src, dst); err != nil {
		if !errors.Is(err, syscall.EXDEV) {
			return "", err
		}
		// Another file system: copy, and leave the original for the admin
		// to delete once the server works.
		tmp := dst + ".adopting"
		if err := os.RemoveAll(tmp); err != nil {
			return "", err
		}
		if err := copyTree(src, tmp); err != nil {
			return "", err
		}
		if err := os.Rename(tmp, dst); err != nil {
			return "", err
		}
		how = "copied"
	}
	return how, os.WriteFile(d.adoptedPath(id), nil, 0o600)
}

// wingsRunning reads Wings' states.json ({"<uuid>": "running", ...}) and
// returns the servers it had running or starting.
func wingsRunning(path string) map[string]bool {
	out := map[string]bool{}
	if path == "" {
		return out
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var states map[string]string
	if json.Unmarshal(b, &states) != nil {
		return out
	}
	for id, st := range states {
		if st == "running" || st == "starting" {
			out[id] = true
		}
	}
	return out
}

// copyTree copies a folder with its owners, modes, times and symlinks.
// Both sides are opened as os.Root, so a symlink a game planted in its folder
// is copied as a link and never followed, on either side.
func copyTree(src, dst string) error {
	if err := os.Mkdir(dst, 0o700); err != nil {
		return err
	}
	from, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer closeQuiet(from)
	to, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer closeQuiet(to)
	return fs.WalkDir(from.FS(), ".", func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := from.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case fi.Mode()&fs.ModeSymlink != 0:
			target, err := from.Readlink(p)
			if err != nil {
				return err
			}
			if err := to.Symlink(target, p); err != nil {
				return err
			}
		case fi.IsDir():
			if p != "." {
				if err := to.Mkdir(p, 0o700); err != nil {
					return err
				}
			}
		case fi.Mode().IsRegular():
			if err := copyFile(from, to, p); err != nil {
				return err
			}
		default:
			return nil // sockets, fifos and devices are not server files
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			if err := to.Lchown(p, int(st.Uid), int(st.Gid)); err != nil && !errors.Is(err, fs.ErrPermission) {
				return err
			}
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return nil
		}
		if err := to.Chmod(p, fi.Mode().Perm()); err != nil {
			return err
		}
		return to.Chtimes(p, fi.ModTime(), fi.ModTime())
	})
}

func copyFile(from, to *os.Root, p string) error {
	in, err := from.Open(p)
	if err != nil {
		return err
	}
	defer closeQuiet(in)
	out, err := to.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		return errors.Join(err, out.Close())
	}
	return out.Close()
}
