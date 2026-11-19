// Package fsx is the only way the daemon touches a server's data directory.
//
// Every operation goes through an *os.Root, which the kernel-assisted
// resolution in the Go runtime confines to the directory even in the presence
// of hostile symlinks. This addresses the symlink/path-traversal CVE class
// that has repeatedly hit Wings (CVE-2023-25168, CVE-2023-25152,
// CVE-2024-27102).
package fsx

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"syscall"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

const MaxEditSize = 10 << 20 // refuse to load >10 MiB files into the editor

var ErrTooLarge = errors.New("file too large to edit")

type Dir struct {
	root *os.Root
	// The game container runs as whoever owns the directory, with every
	// capability dropped. When the daemon runs as root, anything it creates
	// would belong to root and be read-only to the game, so new files and
	// directories are handed to the directory's owner (adopt).
	chown    bool
	uid, gid int
	adopted  func(name string) // test hook: called for every adopted path
}

// Open creates the directory if needed and confines all access to it.
func Open(hostPath string) (*Dir, error) {
	if err := os.MkdirAll(hostPath, 0o755); err != nil { //nolint:gosec // G301: server data dirs belong to the game's user, who must traverse them
		return nil, err
	}
	return OpenExisting(hostPath)
}

// OpenExisting is Open for a directory that must already exist.
func OpenExisting(hostPath string) (*Dir, error) {
	r, err := os.OpenRoot(hostPath)
	if err != nil {
		return nil, err
	}
	d := &Dir{root: r}
	if fi, err := r.Stat("."); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			d.uid, d.gid = int(st.Uid), int(st.Gid)
			d.chown = os.Geteuid() == 0 && (d.uid != 0 || d.gid != 0)
		}
	}
	return d, nil
}

// adopt gives a path the daemon just created to the directory's owner. Lchown
// never follows a symlink and the root confines it, so a swapped path cannot
// redirect it.
func (d *Dir) adopt(name string) error {
	if d.adopted != nil {
		d.adopted(name)
	}
	if !d.chown {
		return nil
	}
	return d.root.Lchown(name, d.uid, d.gid)
}

// adoptFile is adopt for a file the daemon holds open (no path lookup at all).
func (d *Dir) adoptFile(f *os.File, name string) error {
	if d.adopted != nil {
		d.adopted(name)
	}
	if !d.chown {
		return nil
	}
	return f.Chown(d.uid, d.gid)
}

// mkdirAll is os.Root.MkdirAll, adopting each directory it creates.
func (d *Dir) mkdirAll(name string) error {
	if name == "." {
		return nil
	}
	cur := ""
	for _, part := range strings.Split(name, "/") {
		if cur == "" {
			cur = part
		} else {
			cur += "/" + part
		}
		err := d.root.Mkdir(cur, 0o755)
		switch {
		case err == nil:
			if err := d.adopt(cur); err != nil {
				return err
			}
		case errors.Is(err, fs.ErrExist):
			fi, serr := d.root.Stat(cur)
			if serr != nil {
				return serr
			}
			if !fi.IsDir() {
				return &fs.PathError{Op: "mkdir", Path: cur, Err: syscall.ENOTDIR}
			}
		default:
			return err
		}
	}
	return nil
}

// create opens name for writing (creating or truncating it) and makes sure a
// file the daemon created belongs to the directory's owner.
func (d *Dir) create(name string, flag int, perm fs.FileMode) (*os.File, error) {
	_, statErr := d.root.Lstat(name)
	f, err := d.root.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if errors.Is(statErr, fs.ErrNotExist) {
		if err := d.adoptFile(f, name); err != nil {
			return nil, errors.Join(err, f.Close())
		}
	}
	return f, nil
}

func (d *Dir) Close() error { return d.root.Close() }

// clean maps an API path ("/", "/a/../b") to a root-relative name.
// os.Root enforces the actual confinement; this only normalises. Spaces are
// kept: "a " and "a" are different files.
func clean(p string) string {
	p = path.Clean("/" + p)
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "."
	}
	return p
}

func (d *Dir) List(p string) ([]proto.FileInfo, error) {
	f, err := d.root.Open(clean(p))
	if err != nil {
		return nil, err
	}
	defer closeQuiet(f)
	ents, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := make([]proto.FileInfo, 0, len(ents))
	for _, e := range ents {
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, proto.FileInfo{
			Name: e.Name(), Dir: e.IsDir(), Size: fi.Size(), ModUnix: fi.ModTime().Unix(),
			Mode: uint32(fi.Mode().Perm()), Symlink: fi.Mode()&fs.ModeSymlink != 0,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (d *Dir) Read(p string) ([]byte, error) {
	fi, err := d.root.Stat(clean(p))
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, fs.ErrInvalid
	}
	if fi.Size() > MaxEditSize {
		return nil, ErrTooLarge
	}
	return d.root.ReadFile(clean(p))
}

func (d *Dir) Write(p string, data []byte) error {
	name := clean(p)
	if name == "." {
		return fs.ErrInvalid
	}
	if err := d.mkdirAll(path.Dir(name)); err != nil {
		return err
	}
	f, err := d.create(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func (d *Dir) Mkdir(p string) error { return d.mkdirAll(clean(p)) }

func (d *Dir) Delete(p string) error {
	name := clean(p)
	if name == "." {
		return fs.ErrInvalid // never delete the server root itself
	}
	return d.root.RemoveAll(name)
}

// closeQuiet closes a handle no data was written through: one opened for
// reading, used only for metadata calls (stat, truncate), or abandoned before
// any write. Such a close cannot lose data, so its error is not worth reporting.
func closeQuiet(c io.Closer) { _ = c.Close() }
