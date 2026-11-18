package fsx

import (
	"errors"
	"io/fs"
	"os"
	"sort"
	"syscall"
	"time"
)

// Low-level operations for protocols that need streaming and per-call
// semantics (SFTP). They are as confined as everything else in this package:
// every path goes through the *os.Root.

// OpenFile opens a file for streaming. The caller closes it, and checks that
// it is a regular file before using it.
//
// O_NONBLOCK: opening a named pipe (a game can create one) would otherwise
// block until something opens the other end, possibly forever. It changes
// nothing for regular files.
func (d *Dir) OpenFile(p string, flag int, perm fs.FileMode) (*os.File, error) {
	name := clean(p)
	if name == "." {
		return nil, fs.ErrInvalid
	}
	flag |= syscall.O_NONBLOCK
	if flag&os.O_CREATE != 0 {
		return d.create(name, flag, perm&0o777)
	}
	return d.root.OpenFile(name, flag, perm&0o777)
}

func (d *Dir) Stat(p string) (fs.FileInfo, error) { return d.root.Stat(clean(p)) }

// Lstat does not follow a final symlink (it is reported as a symlink).
func (d *Dir) Lstat(p string) (fs.FileInfo, error) { return d.root.Lstat(clean(p)) }

// ReadDir lists a directory sorted by name, without following symlinks.
func (d *Dir) ReadDir(p string) ([]fs.FileInfo, error) {
	f, err := d.root.Open(clean(p))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ents, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := make([]fs.FileInfo, 0, len(ents))
	for _, e := range ents {
		if fi, err := e.Info(); err == nil {
			out = append(out, fi)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

// MkdirOne creates a single directory (parents must exist).
func (d *Dir) MkdirOne(p string) error {
	name := clean(p)
	if name == "." {
		return fs.ErrExist
	}
	if err := d.root.Mkdir(name, 0o755); err != nil {
		return err
	}
	return d.adopt(name)
}

// Remove deletes one file or one empty directory. The root itself is never removable.
func (d *Dir) Remove(p string) error {
	name := clean(p)
	if name == "." {
		return fs.ErrInvalid
	}
	return d.root.Remove(name)
}

func (d *Dir) Rename(oldp, newp string) error {
	from, to := clean(oldp), clean(newp)
	if from == "." || to == "." {
		return fs.ErrInvalid
	}
	return d.root.Rename(from, to)
}

// Link gives oldp's file the additional name newp, failing if newp exists.
func (d *Dir) Link(oldp, newp string) error {
	from, to := clean(oldp), clean(newp)
	if from == "." || to == "." {
		return fs.ErrInvalid
	}
	return d.root.Link(from, to)
}

// Chmod applies permission bits only (never setuid/setgid/sticky). The owner
// (the game) always keeps read and write, and on a directory search too:
// FTP clients apply one mode to a whole tree, and a 0644 directory would
// lock the game and the panel out of everything in it.
func (d *Dir) Chmod(p string, mode fs.FileMode) error {
	name := clean(p)
	keep := fs.FileMode(0o600)
	if fi, err := d.root.Stat(name); err == nil && fi.IsDir() {
		keep = 0o700
	}
	return d.root.Chmod(name, mode.Perm()|keep)
}

func (d *Dir) Chtimes(p string, atime, mtime time.Time) error {
	return d.root.Chtimes(clean(p), atime, mtime)
}

// Usage returns the total size in bytes of the regular files under the
// directory. Symlinks are not followed and not counted, so the figure cannot
// be inflated or deflated by links pointing elsewhere.
func (d *Dir) Usage() (int64, error) { return d.usage(".") }

func (d *Dir) usage(dir string) (int64, error) {
	f, err := d.root.Open(dir)
	if err != nil {
		return 0, err
	}
	ents, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, e := range ents {
		rel := e.Name()
		if dir != "." {
			rel = dir + "/" + e.Name()
		}
		switch {
		case e.IsDir():
			n, err := d.usage(rel)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return 0, err
			}
			total += n
		case e.Type().IsRegular():
			if fi, err := e.Info(); err == nil {
				total += fi.Size()
			}
		}
	}
	return total, nil
}
