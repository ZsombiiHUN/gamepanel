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
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

const MaxEditSize = 10 << 20 // refuse to load >10 MiB files into the editor

var ErrTooLarge = errors.New("file too large to edit")

type Dir struct{ root *os.Root }

// Open creates the directory if needed and confines all access to it.
func Open(hostPath string) (*Dir, error) {
	if err := os.MkdirAll(hostPath, 0o755); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(hostPath)
	if err != nil {
		return nil, err
	}
	return &Dir{root: r}, nil
}

func (d *Dir) Close() error { return d.root.Close() }

// clean maps an API path ("/", "/a/../b") to a root-relative name.
// os.Root enforces the actual confinement; this only normalises.
func clean(p string) string {
	p = path.Clean("/" + strings.TrimSpace(p))
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
	defer f.Close()
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
		out = append(out, proto.FileInfo{Name: e.Name(), Dir: e.IsDir(), Size: fi.Size(), ModUnix: fi.ModTime().Unix()})
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
	if dir := path.Dir(name); dir != "." {
		if err := d.root.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return d.root.WriteFile(name, data, 0o644)
}

func (d *Dir) Mkdir(p string) error { return d.root.MkdirAll(clean(p), 0o755) }

func (d *Dir) Delete(p string) error {
	name := clean(p)
	if name == "." {
		return fs.ErrInvalid // never delete the server root itself
	}
	return d.root.RemoveAll(name)
}
