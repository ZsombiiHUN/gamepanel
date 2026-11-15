package fsx

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

// matchIgnore reports whether rel (slash-separated, root-relative) is excluded
// by any pattern. A pattern matches the full path or just the base name, and a
// directory match excludes everything below it (the walk skips it).
func matchIgnore(patterns []string, rel string) bool {
	base := path.Base(rel)
	for _, p := range patterns {
		p = strings.TrimSuffix(strings.TrimSpace(p), "/")
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		p = strings.TrimPrefix(p, "/")
		if ok, _ := path.Match(p, rel); ok {
			return true
		}
		if !strings.Contains(p, "/") {
			if ok, _ := path.Match(p, base); ok {
				return true
			}
		}
	}
	return false
}

// Archive writes the directory as a gzip-compressed tar stream. Symlinks and
// special files are skipped, never followed, so a hostile server cannot make a
// backup read files outside its directory.
func (d *Dir) Archive(w io.Writer, ignore []string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	if err := d.addDir(tw, ".", ignore); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func (d *Dir) addDir(tw *tar.Writer, dir string, ignore []string) error {
	f, err := d.root.Open(dir)
	if err != nil {
		return err
	}
	ents, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return err
	}
	for _, e := range ents {
		rel := e.Name()
		if dir != "." {
			rel = dir + "/" + e.Name()
		}
		if matchIgnore(ignore, rel) {
			continue
		}
		switch {
		case e.IsDir():
			fi, err := e.Info()
			if err != nil {
				continue // vanished while walking
			}
			if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: rel + "/", Mode: int64(fi.Mode().Perm()), ModTime: fi.ModTime()}); err != nil {
				return err
			}
			if err := d.addDir(tw, rel, ignore); err != nil {
				return err
			}
		case e.Type().IsRegular():
			if err := d.addFile(tw, rel); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *Dir) addFile(tw *tar.Writer, rel string) error {
	f, err := d.root.Open(rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // removed while walking
		}
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() { // swapped for a symlink/device after the walk
		return nil
	}
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: rel, Size: fi.Size(), Mode: int64(fi.Mode().Perm()), ModTime: fi.ModTime()}); err != nil {
		return err
	}
	// A file may grow while being read; copy exactly the announced size.
	n, err := io.Copy(tw, io.LimitReader(f, fi.Size()))
	if err != nil {
		return err
	}
	if n < fi.Size() { // shrank: pad so the tar stays valid
		_, err = io.CopyN(tw, zeroReader{}, fi.Size()-n)
	}
	return err
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

// ErrArchiveTooLarge is returned when extraction would exceed the byte limit.
var ErrArchiveTooLarge = errors.New("archive expands beyond the allowed size")

// Extract unpacks a tar.gz stream into the directory. If truncate is set the
// directory is emptied first. Entries that would leave the directory, and
// anything other than plain files and directories, are rejected or ignored.
// maxBytes bounds the total extracted size (0 = unlimited) against zip bombs.
func (d *Dir) Extract(r io.Reader, truncate bool, maxBytes int64) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("not a gzip archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	if truncate {
		if err := d.clear(); err != nil {
			return err
		}
	}
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(h.Name)
		if name == "." {
			continue
		}
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("unsafe path in archive: %q", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := d.root.MkdirAll(name, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if maxBytes > 0 && total+h.Size > maxBytes {
				return ErrArchiveTooLarge
			}
			total += h.Size
			if dir := path.Dir(name); dir != "." {
				if err := d.root.MkdirAll(dir, 0o755); err != nil {
					return err
				}
			}
			// Permission bits only: never restore setuid/setgid/sticky.
			mode := fs.FileMode(h.Mode)&0o777 | 0o600
			f, err := d.root.OpenFile(name, 0x1|0x40|0x200, mode) // O_WRONLY|O_CREATE|O_TRUNC
			if err != nil {
				return err
			}
			_, err = io.Copy(f, io.LimitReader(tr, h.Size))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		default: // symlinks, hardlinks, devices: not restored
		}
	}
}

func (d *Dir) clear() error {
	f, err := d.root.Open(".")
	if err != nil {
		return err
	}
	ents, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return err
	}
	for _, e := range ents {
		if err := d.root.RemoveAll(e.Name()); err != nil {
			return err
		}
	}
	return nil
}
