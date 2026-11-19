package fsx

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"syscall"
	"time"
)

// File manager operations for the panel (rename, copy, compress, extract,
// streamed upload). Like everything in this package they go through the
// *os.Root, so no path, symlink or archive entry can reach outside the
// server's directory.

// ErrUnsupportedArchive means the file is not an archive this daemon can unpack.
var ErrUnsupportedArchive = errors.New("unsupported archive type: use .zip, .tar, .tar.gz, .tgz, .tar.bz2 or .gz")

// Move renames from to to, creating to's parent folders. It refuses to
// overwrite an existing file or to move a folder into itself.
func (d *Dir) Move(from, to string) error {
	f, t := clean(from), clean(to)
	if f == "." || t == "." {
		return fs.ErrInvalid
	}
	if t == f || strings.HasPrefix(t, f+"/") {
		return &fs.PathError{Op: "move", Path: to, Err: syscall.EINVAL}
	}
	if _, err := d.root.Lstat(t); err == nil {
		return &fs.PathError{Op: "move", Path: to, Err: fs.ErrExist}
	}
	if err := d.mkdirAll(path.Dir(t)); err != nil {
		return err
	}
	return d.root.Rename(f, t)
}

// CopyName is a free name for a copy of p, as Pterodactyl names them:
// "a.txt" -> "a copy.txt", then "a copy 2.txt"; "w.tar.gz" -> "w copy.tar.gz".
func (d *Dir) CopyName(p string) string {
	name := clean(p)
	dir, base := path.Dir(name), path.Base(name)
	ext := ""
	switch lower := strings.ToLower(base); {
	case strings.HasPrefix(base, "."): // dotfiles: the whole name is the name
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tar.bz2"):
		ext = base[strings.LastIndex(lower, ".tar."):]
	default:
		ext = path.Ext(base)
	}
	stem := strings.TrimSuffix(base, ext)
	for n := 1; ; n++ {
		suffix := " copy"
		if n > 1 {
			suffix = fmt.Sprintf(" copy %d", n)
		}
		cand := path.Join(dir, stem+suffix+ext)
		if _, err := d.root.Lstat(cand); errors.Is(err, fs.ErrNotExist) {
			return "/" + cand
		}
	}
}

// Copy duplicates a file or folder to to (which must not exist). Symlinks
// are skipped. reserve is called with each file's size before it is written,
// so the copy cannot pass the disk limit.
func (d *Dir) Copy(from, to string, reserve func(int64) error) error {
	f, t := clean(from), clean(to)
	if f == "." || t == "." {
		return fs.ErrInvalid
	}
	if t == f || strings.HasPrefix(t, f+"/") {
		return &fs.PathError{Op: "copy", Path: to, Err: syscall.EINVAL}
	}
	if _, err := d.root.Lstat(t); err == nil {
		return &fs.PathError{Op: "copy", Path: to, Err: fs.ErrExist}
	}
	fi, err := d.root.Lstat(f)
	if err != nil {
		return err
	}
	if err := d.mkdirAll(path.Dir(t)); err != nil {
		return err
	}
	return d.copyAny(f, t, fi, reserve)
}

func (d *Dir) copyAny(f, t string, fi fs.FileInfo, reserve func(int64) error) error {
	switch {
	case fi.IsDir():
		if err := d.root.Mkdir(t, fi.Mode().Perm()|0o700); err != nil {
			return err
		}
		if err := d.adopt(t); err != nil {
			return err
		}
		ents, err := d.ReadDir(f)
		if err != nil {
			return err
		}
		for _, e := range ents {
			if err := d.copyAny(f+"/"+e.Name(), t+"/"+e.Name(), e, reserve); err != nil {
				return err
			}
		}
		return nil
	case fi.Mode().IsRegular():
		if err := reserve(fi.Size()); err != nil {
			return err
		}
		src, err := d.root.Open(f)
		if err != nil {
			return err
		}
		defer closeQuiet(src)
		dst, err := d.create(t, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm()|0o600)
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, io.LimitReader(src, fi.Size()))
		if cerr := dst.Close(); err == nil {
			err = cerr
		}
		return err
	}
	return nil // symlinks, devices: not copied
}

// WriteStream writes r to p through a temporary file in the same folder,
// replacing p only once everything arrived, so a broken upload never leaves
// half a file. limit caps the bytes written (0 = no cap).
func (d *Dir) WriteStream(p string, r io.Reader, limit int64) (int64, error) {
	return d.WriteStreamCheck(p, r, limit, nil)
}

// WriteStreamCheck is WriteStream with a last check (a checksum) that runs
// once everything arrived; if it fails, the file is never put in place.
func (d *Dir) WriteStreamCheck(p string, r io.Reader, limit int64, check func() error) (int64, error) {
	name := clean(p)
	if name == "." {
		return 0, fs.ErrInvalid
	}
	var keepMode fs.FileMode
	if fi, err := d.root.Lstat(name); err == nil {
		if fi.IsDir() {
			return 0, &fs.PathError{Op: "write", Path: p, Err: syscall.EISDIR}
		}
		if fi.Mode().IsRegular() {
			keepMode = fi.Mode().Perm() // a replaced script stays executable
		}
	}
	if err := d.mkdirAll(path.Dir(name)); err != nil {
		return 0, err
	}
	tmp := path.Join(path.Dir(name), fmt.Sprintf(".%s.upload-%d", path.Base(name), time.Now().UnixNano()))
	f, err := d.create(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	var w io.Writer = f
	if limit > 0 {
		w = &capWriter{w: f, left: limit}
	}
	n, err := io.Copy(w, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && check != nil {
		err = check()
	}
	if err == nil && keepMode != 0 && keepMode != 0o644 {
		err = d.root.Chmod(tmp, keepMode)
	}
	if err == nil {
		err = d.root.Rename(tmp, name)
	}
	if err != nil {
		_ = d.root.Remove(tmp) // best effort; err is what the caller needs
		return n, err
	}
	return n, nil
}

// ErrTooBig is returned by a capped writer that ran out of room.
var ErrTooBig = errors.New("not enough disk space left for this")

type capWriter struct {
	w    io.Writer
	left int64
}

func (c *capWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > c.left {
		return 0, ErrTooBig
	}
	n, err := c.w.Write(p)
	c.left -= int64(n)
	return n, err
}

// Compress archives paths (all inside dir) into a new file in dir and returns
// its path. Entry names are relative to dir. format is "zip" or "tar.gz".
// limit caps the archive's size (0 = none).
func (d *Dir) Compress(dir string, paths []string, format string, limit int64) (string, error) {
	base := clean(dir)
	if len(paths) == 0 {
		return "", fs.ErrInvalid
	}
	var rels []string
	for _, p := range paths {
		c := clean(p)
		if c == "." || (base != "." && !strings.HasPrefix(c, base+"/")) {
			return "", &fs.PathError{Op: "compress", Path: p, Err: syscall.EINVAL}
		}
		rels = append(rels, c)
	}
	ext := ".tar.gz"
	if format == "zip" {
		ext = ".zip"
	}
	stamp := time.Now().UTC().Format("2006-01-02-150405")
	out := path.Join(base, "archive-"+stamp+ext)
	for n := 2; ; n++ {
		if _, err := d.root.Lstat(out); errors.Is(err, fs.ErrNotExist) {
			break
		}
		out = path.Join(base, fmt.Sprintf("archive-%s-%d%s", stamp, n, ext))
	}
	f, err := d.create(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	var w io.Writer = f
	if limit > 0 {
		w = &capWriter{w: f, left: limit}
	}
	strip := func(rel string) string {
		if base == "." {
			return rel
		}
		return strings.TrimPrefix(rel, base+"/")
	}
	if format == "zip" {
		err = d.writeZip(w, rels, strip)
	} else {
		err = d.writeTarGz(w, rels, strip)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = d.root.Remove(out) // best effort; err is what the caller needs
		return "", err
	}
	return "/" + out, nil
}

// walk calls fn for rel and, if it is a folder, everything below it.
// Symlinks and other special files are skipped.
func (d *Dir) walk(rel string, fn func(rel string, fi fs.FileInfo) error) error {
	fi, err := d.root.Lstat(rel)
	if err != nil {
		return err
	}
	if !fi.IsDir() && !fi.Mode().IsRegular() {
		return nil
	}
	if err := fn(rel, fi); err != nil {
		return err
	}
	if !fi.IsDir() {
		return nil
	}
	ents, err := d.ReadDir(rel)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if err := d.walk(rel+"/"+e.Name(), fn); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (d *Dir) writeTarGz(w io.Writer, rels []string, strip func(string) string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	for _, r := range rels {
		err := d.walk(r, func(rel string, fi fs.FileInfo) error {
			if fi.IsDir() {
				return tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: strip(rel) + "/", Mode: int64(fi.Mode().Perm()), ModTime: fi.ModTime()})
			}
			return d.addFileAs(tw, rel, strip(rel))
		})
		if err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// addFileAs is addFile with a different name inside the archive.
func (d *Dir) addFileAs(tw *tar.Writer, rel, name string) error {
	f, err := d.root.Open(rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	defer closeQuiet(f)
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Size: fi.Size(), Mode: int64(fi.Mode().Perm()), ModTime: fi.ModTime()}); err != nil {
		return err
	}
	n, err := io.Copy(tw, io.LimitReader(f, fi.Size()))
	if err == nil && n < fi.Size() {
		_, err = io.CopyN(tw, zeroReader{}, fi.Size()-n)
	}
	return err
}

func (d *Dir) writeZip(w io.Writer, rels []string, strip func(string) string) error {
	zw := zip.NewWriter(w)
	for _, r := range rels {
		err := d.walk(r, func(rel string, fi fs.FileInfo) error {
			h, err := zip.FileInfoHeader(fi)
			if err != nil {
				return err
			}
			h.Name = strip(rel)
			if fi.IsDir() {
				h.Name += "/"
				_, err = zw.CreateHeader(h)
				return err
			}
			h.Method = zip.Deflate
			dst, err := zw.CreateHeader(h)
			if err != nil {
				return err
			}
			src, err := d.root.Open(rel)
			if err != nil {
				return err
			}
			defer closeQuiet(src)
			_, err = io.Copy(dst, io.LimitReader(src, fi.Size()))
			return err
		})
		if err != nil {
			return err
		}
	}
	return zw.Close()
}

// Decompress unpacks the archive at p into dest, without overwriting checks
// beyond what the archive holds (existing files with the same names are
// replaced, as in Pterodactyl). maxBytes caps the bytes written (0 = none);
// past it the extraction stops with ErrArchiveTooLarge. Symlinks, hard links
// and devices in the archive are skipped; unsafe names are refused.
func (d *Dir) Decompress(p, dest string, maxBytes int64) error {
	name, to := clean(p), clean(dest)
	lower := strings.ToLower(name)
	var written int64
	put := func(entry string, mode fs.FileMode, body io.Reader) error {
		rel, err := safeJoin(to, entry)
		if err != nil {
			return err
		}
		if err := d.mkdirAll(path.Dir(rel)); err != nil {
			return err
		}
		if fi, err := d.root.Lstat(rel); err == nil && !fi.Mode().IsRegular() {
			return &fs.PathError{Op: "extract", Path: entry, Err: fs.ErrExist}
		}
		f, err := d.create(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm()|0o600)
		if err != nil {
			return err
		}
		var w io.Writer = f
		if maxBytes > 0 {
			w = &capWriter{w: f, left: maxBytes - written}
		}
		n, err := io.Copy(w, body)
		written += n
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if errors.Is(err, ErrTooBig) {
			return ErrArchiveTooLarge
		}
		return err
	}
	mkdir := func(entry string) error {
		rel, err := safeJoin(to, entry)
		if err != nil {
			return err
		}
		return d.mkdirAll(rel)
	}

	src, err := d.root.Open(name)
	if err != nil {
		return err
	}
	defer closeQuiet(src)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		fi, err := src.Stat()
		if err != nil {
			return err
		}
		zr, err := zip.NewReader(src, fi.Size())
		if err != nil {
			return fmt.Errorf("damaged zip archive: %w", err)
		}
		for _, zf := range zr.File {
			switch {
			case strings.HasSuffix(zf.Name, "/"):
				if err := mkdir(zf.Name); err != nil {
					return err
				}
			case zf.Mode().IsRegular():
				rc, err := zf.Open()
				if err != nil {
					return fmt.Errorf("damaged zip archive: %w", err)
				}
				err = put(zf.Name, zf.Mode(), rc)
				rc.Close()
				if err != nil {
					return err
				}
			}
		}
		return nil
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"),
		strings.HasSuffix(lower, ".tar.bz2"), strings.HasSuffix(lower, ".tbz2"), strings.HasSuffix(lower, ".tar"):
		var r io.Reader = src
		switch {
		case strings.HasSuffix(lower, "gz"):
			gz, err := gzip.NewReader(src)
			if err != nil {
				return fmt.Errorf("not a gzip archive: %w", err)
			}
			defer closeQuiet(gz)
			r = gz
		case strings.HasSuffix(lower, "bz2"):
			r = bzip2.NewReader(src)
		}
		tr := tar.NewReader(r)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return fmt.Errorf("damaged archive: %w", err)
			}
			switch h.Typeflag {
			case tar.TypeDir:
				if err := mkdir(h.Name); err != nil {
					return err
				}
			case tar.TypeReg:
				// Permission bits only, as put keeps: never setuid/setgid/sticky.
				if err := put(h.Name, fs.FileMode(h.Mode&0o777), io.LimitReader(tr, h.Size)); err != nil {
					return err
				}
			}
		}
	case strings.HasSuffix(lower, ".gz"):
		gz, err := gzip.NewReader(src)
		if err != nil {
			return fmt.Errorf("not a gzip file: %w", err)
		}
		defer closeQuiet(gz)
		return put(strings.TrimSuffix(path.Base(name), path.Ext(name)), 0o644, gz)
	}
	return ErrUnsupportedArchive
}

// safeJoin places an archive entry under dest, refusing absolute names and
// any that climb out ("../").
func safeJoin(dest, entry string) (string, error) {
	e := strings.ReplaceAll(entry, "\\", "/")
	c := path.Clean(e)
	if c == "." || path.IsAbs(e) || c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("unsafe path in archive: %q", entry)
	}
	if dest == "." {
		return c, nil
	}
	return dest + "/" + c, nil
}

// Info describes one path (List's format) without listing a folder.
func (d *Dir) Info(p string) (fs.FileInfo, error) { return d.root.Lstat(clean(p)) }

// Open opens a regular file for reading (downloads).
func (d *Dir) Open(p string) (*os.File, fs.FileInfo, error) {
	name := clean(p)
	f, err := d.root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		closeQuiet(f)
		return nil, nil, err
	}
	if !fi.Mode().IsRegular() {
		closeQuiet(f)
		return nil, nil, &fs.PathError{Op: "open", Path: p, Err: syscall.EISDIR}
	}
	return f, fi, nil
}
