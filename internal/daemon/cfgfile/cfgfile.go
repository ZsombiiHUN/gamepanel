// Package cfgfile edits a server's config files before it starts, as
// Pterodactyl's Wings does for an egg's config.files: Minecraft's
// server.properties gets the server's port, BungeeCord's config.yml its bind
// address, and so on.
//
// The panel sends the keys and values with the server's own settings already
// filled in. The only placeholder the node fills is
// {{config.docker.interface}}, the address containers reach the host on; every
// other {{config.*}} becomes empty. Wings looked such names up in its own
// configuration, which is how its node token could end up in a game's files.
package cfgfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strings"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// FS is the server's directory (an *fsx.Dir): reads and writes are confined
// to it, and files it creates belong to the directory's owner. WriteStream
// writes a temporary file and renames it into place, so a write that fails
// (a full disk) leaves the old file as it was.
type FS interface {
	Read(p string) ([]byte, error)
	WriteStream(p string, r io.Reader, limit int64) (int64, error)
}

// Parsers lists the supported config.files parsers.
var Parsers = []string{"properties", "ini", "yaml", "yml", "json", "file"}

var configRe = regexp.MustCompile(`\{\{(config\.[\w.-]*)\}\}`)

// nodeValue fills the node's placeholders in v.
func nodeValue(v, iface string) string {
	return configRe.ReplaceAllStringFunc(v, func(m string) string {
		switch configRe.FindStringSubmatch(m)[1] {
		case "config.docker.interface", "config.docker.network.interface":
			return iface
		}
		return ""
	})
}

// Apply edits one file. A file that does not exist is created first, as Wings
// does: a new Minecraft server must have its port in server.properties before
// its first start, when the game writes the rest of the file. A file whose
// content would not change is left alone.
func Apply(fsys FS, f proto.ConfigFile, iface string) error {
	old, err := fsys.Read(f.Path)
	missing := errors.Is(err, fs.ErrNotExist)
	if err != nil && !missing {
		return err
	}
	err = nil
	sets := make([]proto.ConfigSet, 0, len(f.Set))
	for _, s := range f.Set {
		s.Value = nodeValue(s.Value, iface)
		sets = append(sets, s)
	}
	var out []byte
	switch f.Parser {
	case "properties":
		out = editProperties(old, sets)
	case "ini":
		out = editINI(old, sets)
	case "file":
		out = editText(old, sets)
	case "yaml", "yml":
		out, err = editYAML(old, sets)
	case "json":
		out, err = editJSON(old, sets)
	default:
		return fmt.Errorf("the %q parser is not supported", f.Parser)
	}
	if err != nil {
		return err
	}
	if !missing && bytes.Equal(out, old) {
		return nil
	}
	_, err = fsys.WriteStream(f.Path, bytes.NewReader(out), 0)
	return err
}

// change decides what a set does to a key whose current value is cur
// (found reports whether the key exists): the new value, and whether to
// write it at all.
func change(s proto.ConfigSet, cur string, found bool) (string, bool) {
	if s.IfValue == nil {
		return s.Value, true
	}
	if !found {
		return "", false
	}
	if expr, ok := strings.CutPrefix(*s.IfValue, "regex:"); ok {
		re, err := regexp.Compile(expr)
		if err != nil || !re.MatchString(cur) {
			return "", false
		}
		return re.ReplaceAllString(cur, s.Value), true
	}
	return s.Value, cur == *s.IfValue
}
