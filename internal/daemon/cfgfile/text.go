package cfgfile

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// lines splits a file into lines without their endings, and returns the
// ending the file uses ("\n" for a new file).
func lines(data []byte) ([]string, string) {
	eol := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		eol = "\r\n"
	}
	s := strings.TrimSuffix(string(data), eol)
	if s == "" {
		return nil, eol
	}
	return strings.Split(s, eol), eol
}

func join(ls []string, eol string) []byte {
	if len(ls) == 0 {
		return []byte{}
	}
	return []byte(strings.Join(ls, eol) + eol)
}

// editText is Wings' "file" parser: a line that starts with a key is
// replaced, whole, by its value. Nothing is added.
func editText(data []byte, sets []proto.ConfigSet) []byte {
	ls, eol := lines(data)
	for i, l := range ls {
		for _, s := range sets {
			if s.Key == "" || !strings.HasPrefix(l, s.Key) {
				continue
			}
			if v, ok := change(s, l, true); ok {
				ls[i] = v
			}
			break
		}
	}
	return join(ls, eol)
}

// ---- .properties (Java) ----

// propLine is one logical line of a .properties file: it may span several
// physical lines when they end in a backslash.
type propLine struct {
	raw     []string // the physical lines
	key     string   // unescaped; "" for blank and comment lines
	keyText string   // the key as written
	sep     string   // what separates key and value, e.g. "=" or " = "
	value   string   // unescaped
}

func parseProperties(data []byte) ([]propLine, string) {
	ls, eol := lines(data)
	var out []propLine
	for i := 0; i < len(ls); i++ {
		pl := propLine{raw: []string{ls[i]}}
		t := strings.TrimLeft(ls[i], " \t\f")
		if t == "" || t[0] == '#' || t[0] == '!' {
			out = append(out, pl)
			continue
		}
		logical := t
		for continues(logical) && i+1 < len(ls) {
			i++
			pl.raw = append(pl.raw, ls[i])
			logical = logical[:len(logical)-1] + strings.TrimLeft(ls[i], " \t\f")
		}
		k := 0
		for k < len(logical) {
			c := logical[k]
			if c == '\\' {
				k += 2
				continue
			}
			if c == '=' || c == ':' || c == ' ' || c == '\t' || c == '\f' {
				break
			}
			k++
		}
		k = min(k, len(logical))
		pl.keyText = logical[:k]
		rest := logical[k:]
		v := strings.TrimLeft(rest, " \t\f")
		if v != "" && (v[0] == '=' || v[0] == ':') {
			v = strings.TrimLeft(v[1:], " \t\f")
		}
		pl.sep = rest[:len(rest)-len(v)]
		pl.key = unescapeProp(pl.keyText)
		pl.value = unescapeProp(v)
		out = append(out, pl)
	}
	return out, eol
}

// continues reports whether a line ends in an odd number of backslashes.
func continues(l string) bool {
	n := 0
	for i := len(l) - 1; i >= 0 && l[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

func unescapeProp(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 == len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			if r, ok := hex4(s[i+1:]); ok {
				i += 4
				// A pair of \u escapes can encode one character (UTF-16).
				if utf16.IsSurrogate(r) && strings.HasPrefix(s[i+1:], `\u`) {
					if r2, ok := hex4(s[i+3:]); ok {
						if c := utf16.DecodeRune(r, r2); c != utf8.RuneError {
							b.WriteRune(c)
							i += 6
							continue
						}
					}
				}
				b.WriteRune(r)
				continue
			}
			b.WriteByte('u')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// hex4 reads the four hex digits a \u escape starts with.
func hex4(s string) (rune, bool) {
	if len(s) < 4 {
		return 0, false
	}
	var r rune
	for _, c := range s[:4] {
		switch {
		case c >= '0' && c <= '9':
			r = r*16 + c - '0'
		case c >= 'a' && c <= 'f':
			r = r*16 + c - 'a' + 10
		case c >= 'A' && c <= 'F':
			r = r*16 + c - 'A' + 10
		default:
			return 0, false
		}
	}
	return r, true
}

// escapeProp writes a key or value so Java reads it back as s, with
// characters outside ASCII as \uXXXX (read correctly in any encoding, as
// Wings writes them).
func escapeProp(s string, key bool) string {
	var b strings.Builder
	for i, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == ' ' && (key || i == 0):
			b.WriteString(`\ `)
		case key && (r == '=' || r == ':' || r == '#' || r == '!'):
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r > 0x7e:
			if r == utf8.RuneError {
				r = 0xfffd
			}
			for _, u := range utf16.Encode([]rune{r}) {
				fmt.Fprintf(&b, `\u%04X`, u)
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// editProperties sets each key where it is (every occurrence), keeping
// comments, order and the file's own separators; a key that is missing is
// added at the end.
func editProperties(data []byte, sets []proto.ConfigSet) []byte {
	pls, eol := parseProperties(data)
	for _, s := range sets {
		found := false
		for i := range pls {
			pl := &pls[i]
			if pl.keyText == "" || pl.key != s.Key {
				continue
			}
			found = true
			if v, ok := change(s, pl.value, true); ok {
				pl.value = v
				if pl.sep == "" { // a key alone: "key" means key=""
					pl.sep = "="
				}
				pl.raw = []string{pl.keyText + pl.sep + escapeProp(v, false)}
			}
		}
		if !found {
			if v, ok := change(s, "", false); ok {
				pls = append(pls, propLine{raw: []string{escapeProp(s.Key, true) + "=" + escapeProp(v, false)}, key: s.Key, keyText: escapeProp(s.Key, true), sep: "=", value: v})
			}
		}
	}
	var out []string
	for _, pl := range pls {
		out = append(out, pl.raw...)
	}
	return join(out, eol)
}
