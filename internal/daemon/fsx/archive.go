package fsx

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
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

// entries walks a tar.gz stream, rejecting unsafe paths, and calls fn for
// every entry with its cleaned name ("." entries are skipped).
func entries(r io.Reader, fn func(h *tar.Header, name string, body io.Reader) error) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("not a gzip archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("damaged archive: %w", err)
		}
		name := path.Clean(h.Name)
		if name == "." {
			continue
		}
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("unsafe path in archive: %q", h.Name)
		}
		if err := fn(h, name, tr); err != nil {
			return err
		}
	}
}

// Scan reads a whole tar.gz stream without writing anything and returns the
// total size of its regular files. It fails on a damaged archive or an unsafe
// path, which is what Extract checks before it changes anything.
func Scan(r io.Reader) (int64, error) {
	var total int64
	err := entries(r, func(h *tar.Header, _ string, body io.Reader) error {
		if h.Typeflag == tar.TypeReg {
			total += h.Size
			// Read the body so a truncated or corrupt stream is caught here.
			if _, err := io.Copy(io.Discard, body); err != nil {
				return fmt.Errorf("damaged archive: %w", err)
			}
		}
		return nil
	})
	return total, err
}

// Extract unpacks a tar.gz stream into the directory. The whole archive is
// checked first (Scan): a damaged archive, an unsafe path, or one bigger than
// maxBytes (0 = unlimited) fails before any file is touched, so a bad backup
// never leaves a server emptied or half-restored. If truncate is set the
// directory is emptied before unpacking. Only plain files and directories are
// restored; links and devices are ignored.
func (d *Dir) Extract(r io.ReadSeeker, truncate bool, maxBytes int64) error {
	total, err := Scan(r)
	if err != nil {
		return err
	}
	if maxBytes > 0 && total > maxBytes {
		return ErrArchiveTooLarge
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if truncate {
		if err := d.clear(); err != nil {
			return err
		}
	}
	var written int64
	return entries(r, func(h *tar.Header, name string, body io.Reader) error {
		switch h.Typeflag {
		case tar.TypeDir:
			return d.mkdirAll(name)
		case tar.TypeReg:
			// The archive was scanned, but a file could change underneath us.
			if maxBytes > 0 && written+h.Size > maxBytes {
				return ErrArchiveTooLarge
			}
			written += h.Size
			if err := d.mkdirAll(path.Dir(name)); err != nil {
				return err
			}
			// Permission bits only: never restore setuid/setgid/sticky.
			mode := fs.FileMode(h.Mode)&0o777 | 0o600
			f, err := d.create(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, io.LimitReader(body, h.Size))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			return err
		}
		return nil // symlinks, hardlinks, devices: not restored
	})
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
