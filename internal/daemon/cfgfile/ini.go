package cfgfile

import (
	"strings"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// iniKey splits "section.key" at its last dot; a key without one is in the
// part before the first [section].
func iniKey(k string) (section, key string) {
	if i := strings.LastIndex(k, "."); i >= 0 {
		return k[:i], k[i+1:]
	}
	return "", k
}

// iniEntry reads "key = value" (or key: value); ok is false for anything else.
func iniEntry(l string) (key, before, value string, ok bool) {
	t := strings.TrimSpace(l)
	if t == "" || t[0] == ';' || t[0] == '#' || t[0] == '[' {
		return "", "", "", false
	}
	i := strings.IndexAny(l, "=:")
	if i < 0 {
		return "", "", "", false
	}
	key = strings.TrimSpace(l[:i])
	rest := l[i+1:]
	v := strings.TrimLeft(rest, " \t")
	before = l[:i+1] + rest[:len(rest)-len(v)]
	value = strings.TrimSpace(v)
	if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
		value = value[1 : len(value)-1]
	}
	return key, before, value, key != ""
}

// iniSection returns a [section] line's name.
func iniSection(l string) (string, bool) {
	t := strings.TrimSpace(l)
	if len(t) >= 2 && t[0] == '[' && t[len(t)-1] == ']' {
		return strings.TrimSpace(t[1 : len(t)-1]), true
	}
	return "", false
}

// editINI sets keys in place, keeping comments, order and spacing. A missing
// key is added after the last entry of its section, and a missing section is
// added at the end.
func editINI(data []byte, sets []proto.ConfigSet) []byte {
	ls, eol := lines(data)
	for _, s := range sets {
		section, key := iniKey(s.Key)
		cur, found := "", false
		lastInSection, sectionStart := -1, -1
		if section == "" {
			sectionStart = 0
		}
		for i, l := range ls {
			if name, ok := iniSection(l); ok {
				cur = name
				if cur == section && sectionStart < 0 {
					sectionStart = i + 1
				}
				continue
			}
			if cur != section {
				continue
			}
			k, before, v, ok := iniEntry(l)
			if !ok {
				continue
			}
			lastInSection = i
			if k != key {
				continue
			}
			found = true
			if nv, ok := change(s, v, true); ok {
				ls[i] = before + nv
			}
		}
		if found {
			continue
		}
		nv, ok := change(s, "", false)
		if !ok {
			continue
		}
		line := key + "=" + nv
		switch {
		case lastInSection >= 0:
			ls = append(ls[:lastInSection+1], append([]string{line}, ls[lastInSection+1:]...)...)
		case sectionStart >= 0:
			ls = append(ls[:sectionStart], append([]string{line}, ls[sectionStart:]...)...)
		default:
			ls = append(ls, "["+section+"]", line)
		}
	}
	return join(ls, eol)
}
