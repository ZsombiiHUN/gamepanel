package store

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxVarLen bounds any variable value: values become environment variables,
// and the kernel limits how large an environment can be.
const MaxVarLen = 4096

// ValidateVar checks a value against the egg variable's Pterodactyl rules
// ("required|string|max:20", "nullable|integer|between:1,64",
// "required|in:true,false", "regex:/^[a-z]+$/" ...). Rules this panel does
// not know are ignored, as are regexes Go cannot compile, so an egg written
// for Pterodactyl never makes a value impossible to save.
func ValidateVar(v EggVar, val string) error {
	name := v.Name
	if name == "" {
		name = v.Env
	}
	bad := func(format string, a ...any) error { return userErrf("%s: "+format, append([]any{name}, a...)...) }
	if len(val) > MaxVarLen {
		return bad("must be at most %d characters", MaxVarLen)
	}
	if strings.ContainsRune(val, 0) || !utf8.ValidString(val) {
		return bad("contains characters that are not allowed")
	}
	rules := splitRules(v.Rules)
	has := func(r string) bool {
		return slices.ContainsFunc(rules, func(x rule) bool { return x.name == r })
	}
	if val == "" {
		if has("required") {
			return bad("is required")
		}
		return nil // empty and optional: nothing else applies (Laravel "nullable")
	}
	vv := varValue{val: val, numeric: has("integer") || has("int") || has("numeric")}
	if vv.numeric {
		whole := has("integer") || has("int")
		f, err := strconv.ParseFloat(val, 64)
		switch {
		case err != nil && whole, whole && strings.ContainsAny(val, ".eE"):
			return bad("must be a whole number")
		case err != nil:
			return bad("must be a number")
		}
		vv.num = f
	}
	for _, r := range rules {
		if why := vv.check(r); why != "" {
			return bad("%s", why)
		}
	}
	return nil
}

// varValue is a non-empty value being checked against a variable's rules.
type varValue struct {
	val     string
	numeric bool    // an integer or numeric rule applies
	num     float64 // the value, when numeric
}

// size is what min, max, between and size compare: the number for numbers,
// the length for text.
func (v varValue) size() float64 {
	if v.numeric {
		return v.num
	}
	return float64(utf8.RuneCountInString(v.val))
}

func (v varValue) unit() string {
	if v.numeric {
		return ""
	}
	return " characters"
}

// check returns why the value breaks rule r, or "" if it does not (or the
// rule is unknown).
func (v varValue) check(r rule) string {
	switch r.name {
	case "max", "min", "between", "size":
		return v.checkSize(r)
	case "boolean", "bool":
		switch strings.ToLower(v.val) {
		case "1", "0", "true", "false":
			return ""
		}
		return "must be true or false (or 1 or 0)"
	case "in":
		if !inList(v.val, r.arg) {
			return "must be one of: " + strings.ReplaceAll(r.arg, ",", ", ")
		}
	case "not_in":
		if inList(v.val, r.arg) {
			return "cannot be " + v.val
		}
	case "regex":
		if re := phpRegex(r.arg); re != nil && !re.MatchString(v.val) {
			return "is not in the expected format"
		}
	case "url":
		if u, err := url.Parse(v.val); err != nil || u.Scheme == "" || u.Host == "" {
			return "must be a full URL"
		}
	default:
		return v.checkCharset(r)
	}
	return ""
}

// checkSize applies max, min, between and size.
func (v varValue) checkSize(r rule) string {
	switch r.name {
	case "max":
		if n, err := strconv.ParseFloat(r.arg, 64); err == nil && v.size() > n {
			return "must be at most " + r.arg + v.unit()
		}
	case "min":
		if n, err := strconv.ParseFloat(r.arg, 64); err == nil && v.size() < n {
			return "must be at least " + r.arg + v.unit()
		}
	case "between":
		lo, hi, ok := strings.Cut(r.arg, ",")
		a, e1 := strconv.ParseFloat(lo, 64)
		b, e2 := strconv.ParseFloat(hi, 64)
		if ok && e1 == nil && e2 == nil && (v.size() < a || v.size() > b) {
			return "must be between " + lo + " and " + hi + v.unit()
		}
	case "size":
		if n, err := strconv.ParseFloat(r.arg, 64); err == nil && v.size() != n {
			return "must be exactly " + r.arg + v.unit()
		}
	}
	return ""
}

// checkCharset applies alpha, alpha_num and alpha_dash.
func (v varValue) checkCharset(r rule) string {
	switch r.name {
	case "alpha":
		if !alphaRe.MatchString(v.val) {
			return "may only contain letters"
		}
	case "alpha_num":
		if !alphaNumRe.MatchString(v.val) {
			return "may only contain letters and numbers"
		}
	case "alpha_dash":
		if !alphaDashRe.MatchString(v.val) {
			return "may only contain letters, numbers, dashes and underscores"
		}
	}
	return ""
}

var (
	alphaRe     = regexp.MustCompile(`^\pL+$`)
	alphaNumRe  = regexp.MustCompile(`^[\pL\pN]+$`)
	alphaDashRe = regexp.MustCompile(`^[\pL\pN_-]+$`)
)

type rule struct{ name, arg string }

// splitRules parses "a|b:1,2|regex:/x|y/". A regex keeps any "|" it contains:
// everything after "regex:" up to its closing delimiter belongs to it.
func splitRules(s string) []rule {
	var out []rule
	for s != "" {
		var part string
		if strings.HasPrefix(s, "regex:") && len(s) > 7 {
			delim := s[6]
			if end := strings.LastIndexByte(s[7:], delim); end >= 0 {
				stop := 7 + end + 1
				// flags after the closing delimiter, up to the next "|"
				for stop < len(s) && s[stop] != '|' {
					stop++
				}
				part, s = s[:stop], strings.TrimPrefix(s[stop:], "|")
				out = append(out, rule{"regex", part[6:]})
				continue
			}
		}
		part, s, _ = strings.Cut(s, "|")
		name, arg, _ := strings.Cut(strings.TrimSpace(part), ":")
		if name != "" {
			out = append(out, rule{strings.ToLower(name), arg})
		}
	}
	return out
}

func inList(val, list string) bool {
	for _, x := range strings.Split(list, ",") {
		if strings.Trim(strings.TrimSpace(x), `"'`) == val {
			return true
		}
	}
	return false
}

// phpRegex turns "/pattern/flags" into a Go regexp, or nil when Go's syntax
// cannot express it (look-arounds, backreferences).
func phpRegex(s string) *regexp.Regexp {
	if len(s) < 2 {
		return nil
	}
	d := s[0]
	end := strings.LastIndexByte(s, d)
	if end <= 0 {
		return nil
	}
	pat, flags := s[1:end], s[end+1:]
	prefix := ""
	for _, f := range flags {
		if f == 'i' || f == 'm' || f == 's' {
			prefix += string(f)
		}
	}
	if prefix != "" {
		pat = "(?" + prefix + ")" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil
	}
	return re
}

// VarRulesSummary describes rules for people: "required, whole number, 1 to 64".
func VarRulesSummary(rules string) string {
	var parts []string
	for _, r := range splitRules(rules) {
		switch r.name {
		case "required":
			parts = append(parts, "required")
		case "integer", "int":
			parts = append(parts, "whole number")
		case "numeric":
			parts = append(parts, "number")
		case "boolean", "bool":
			parts = append(parts, "true or false")
		case "max":
			parts = append(parts, "max "+r.arg)
		case "min":
			parts = append(parts, "min "+r.arg)
		case "between":
			parts = append(parts, strings.Replace(r.arg, ",", " to ", 1))
		case "in":
			parts = append(parts, "one of "+strings.ReplaceAll(r.arg, ",", ", "))
		}
	}
	return strings.Join(parts, ", ")
}

// StartupChange is a set of changes to a server's startup settings. Nil
// fields stay as they are.
type StartupChange struct {
	Env          map[string]string // merged into the current values
	Image        *string
	Startup      *string
	CrashRestart *bool
}

// UpdateStartup applies c to a server and returns it as saved.
func (s *Store) UpdateStartup(ctx context.Context, uuid string, c StartupChange) (Server, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Server{}, err
	}
	defer tx.Rollback()
	v, err := scanServer(tx.QueryRowContext(ctx, `SELECT `+srvCols+` FROM servers WHERE uuid=?`, uuid))
	if err != nil {
		return v, err
	}
	for k, val := range c.Env {
		v.Env[k] = val
	}
	if c.Image != nil {
		v.Image = *c.Image
	}
	if c.Startup != nil {
		v.Startup = *c.Startup
	}
	if c.CrashRestart != nil {
		v.CrashRestart = *c.CrashRestart
	}
	env, err := json.Marshal(v.Env)
	if err != nil {
		return v, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE servers SET env=?, image=?, startup=?, crash_restart=? WHERE uuid=?`, string(env), v.Image, v.Startup, v.CrashRestart, uuid); err != nil {
		return v, err
	}
	return v, tx.Commit()
}
