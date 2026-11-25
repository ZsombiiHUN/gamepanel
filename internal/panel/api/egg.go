package api

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// ptdlEgg is the subset of the Pterodactyl PTDL_v1/v2 egg format we import,
// so the existing community egg library works unchanged.
type ptdlEgg struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	DockerImages json.RawMessage `json:"docker_images"` // v2; object, order matters
	Images       []string        `json:"images"`        // v1
	DockerImage  string          `json:"docker_image"`  // v1 (older)
	Startup      string          `json:"startup"`
	Features     []string        `json:"features"`
	Config       struct {
		Stop string `json:"stop"`
		// Startup is {"done": "..."} or {"done": ["...", ...]}, usually
		// stored as a JSON string inside the JSON.
		Startup json.RawMessage `json:"startup"`
		// Files is {"file": {"parser": "...", "find": {...}}}, often stored
		// as a JSON string, like Startup.
		Files json.RawMessage `json:"files"`
	} `json:"config"`
	Scripts struct {
		Installation struct {
			Script     string `json:"script"`
			Container  string `json:"container"`
			Entrypoint string `json:"entrypoint"`
		} `json:"installation"`
	} `json:"scripts"`
	Variables []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Env         string `json:"env_variable"`
		Default     any    `json:"default_value"`
		// Pterodactyl stores these as booleans or as 0/1.
		// A missing field means true, so hand-written eggs stay usable.
		UserViewable *flexBool `json:"user_viewable"`
		UserEditable *flexBool `json:"user_editable"`
		Rules        string    `json:"rules"`
	} `json:"variables"`
}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ImportPTDL turns a PTDL egg into GamePanel's form, with the same checks as
// the admin egg import. The Pterodactyl panel import uses it for eggs it
// rebuilds from Pterodactyl's database.
func ImportPTDL(raw []byte) (store.Egg, error) { return importPTDL(raw) }

func importPTDL(raw []byte) (store.Egg, error) {
	var p ptdlEgg
	if err := json.Unmarshal(raw, &p); err != nil {
		return store.Egg{}, &store.UserError{Msg: "not valid egg JSON"}
	}
	e := store.Egg{Name: strings.TrimSpace(p.Name), Description: p.Description, Startup: p.Startup, Stop: p.Config.Stop}
	if e.Name == "" || strings.TrimSpace(e.Startup) == "" {
		return e, &store.UserError{Msg: "egg needs a name and a startup command"}
	}
	switch {
	case len(orderedImages(p.DockerImages)) > 0:
		e.DockerImages = orderedImages(p.DockerImages)
	case len(p.Images) > 0:
		for _, img := range p.Images {
			e.DockerImages = append(e.DockerImages, store.EggImage{Label: img, Image: img})
		}
	case p.DockerImage != "":
		e.DockerImages = []store.EggImage{{Label: p.DockerImage, Image: p.DockerImage}}
	}
	if len(e.DockerImages) == 0 {
		return e, &store.UserError{Msg: "egg has no docker image"}
	}
	for _, img := range e.DockerImages {
		if !validImage(img.Image) {
			return e, &store.UserError{Msg: "egg has an invalid docker image name: " + truncate(img.Image, 80)}
		}
	}
	e.DockerImage = e.DockerImages[0].Image
	e.Features = eggFeatures(p.Features)
	e.Done = doneLines(p.Config.Startup)
	files, err := configFiles(p.Config.Files)
	if err != nil {
		return e, err
	}
	e.ConfigFiles = files
	if e.Stop == "" {
		e.Stop = "^C"
	}
	if in := p.Scripts.Installation; strings.TrimSpace(in.Script) != "" {
		e.InstallScript = strings.ReplaceAll(in.Script, "\r\n", "\n")
		e.InstallImage = strings.TrimSpace(in.Container)
		e.InstallEntrypoint = strings.TrimSpace(in.Entrypoint)
		if e.InstallEntrypoint == "" {
			e.InstallEntrypoint = "bash"
		}
		if e.InstallImage == "" {
			return e, &store.UserError{Msg: "egg has an install script but no installer image"}
		}
		// The daemon places it into a shell command line: a plain name only.
		if !proto.ValidEntrypoint(e.InstallEntrypoint) {
			return e, &store.UserError{Msg: "install entrypoint must be a plain program name"}
		}
	} else {
		e.InstallEntrypoint = "bash"
	}
	e.Variables = []store.EggVar{}
	for _, v := range p.Variables {
		if !envNameRe.MatchString(v.Env) {
			continue // ignore malformed names rather than trusting them
		}
		def := ""
		switch d := v.Default.(type) {
		case string:
			def = d
		case float64:
			def = strconv.FormatFloat(d, 'f', -1, 64)
		case bool:
			if d {
				def = "true"
			} else {
				def = "false"
			}
		}
		e.Variables = append(e.Variables, store.EggVar{
			Env: v.Env, Name: v.Name, Default: def, Description: v.Description,
			UserViewable: v.UserViewable.or(true), UserEditable: v.UserEditable.or(true), Rules: strings.TrimSpace(v.Rules),
		})
	}
	return e, nil
}

var featureRe = regexp.MustCompile(`^[a-z0-9_.-]{1,40}$`)

// eggFeatures keeps the well-formed, distinct feature names (at most 20).
func eggFeatures(in []string) []string {
	out := []string{}
	for _, f := range in {
		f = strings.ToLower(strings.TrimSpace(f))
		if featureRe.MatchString(f) && !slices.Contains(out, f) && len(out) < 20 {
			out = append(out, f)
		}
	}
	return out
}

// doneLines reads config.startup's "done" text: a string or a list, inside
// an object that Pterodactyl stores either as is or as a JSON string.
// Pterodactyl's "regex:" form is not supported and is skipped.
func doneLines(raw json.RawMessage) []string {
	out := []string{}
	var inner string
	if json.Unmarshal(raw, &inner) == nil {
		raw = json.RawMessage(inner)
	}
	var obj struct {
		Done json.RawMessage `json:"done"`
	}
	if json.Unmarshal(raw, &obj) != nil || len(obj.Done) == 0 {
		return out
	}
	var list []string
	if json.Unmarshal(obj.Done, &list) != nil {
		var one string
		if json.Unmarshal(obj.Done, &one) != nil {
			return out
		}
		list = []string{one}
	}
	for _, l := range list {
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "regex:") || len(l) > 200 || len(out) >= 10 {
			continue
		}
		out = append(out, l)
	}
	return out
}

// member is one key of a JSON object, in file order.
type member struct {
	Key string
	Val json.RawMessage
}

// members reads a JSON object (or a JSON string holding one, as Pterodactyl
// stores egg config) in file order; ok is false if it is not an object.
func members(raw json.RawMessage) (out []member, ok bool) {
	var inner string
	if json.Unmarshal(raw, &inner) == nil {
		raw = json.RawMessage(inner)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, false
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, _ := k.(string)
		var v json.RawMessage
		if dec.Decode(&v) != nil {
			return nil, false
		}
		out = append(out, member{key, v})
	}
	return out, true
}

// scalarText is a find value as text: strings as they are, numbers and
// booleans as written. ok is false for objects, arrays and null.
func scalarText(raw json.RawMessage) (string, bool) {
	if strings.TrimSpace(string(raw)) == "null" {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	t := strings.TrimSpace(string(raw))
	if t == "true" || t == "false" {
		return t, true
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String(), true
	}
	return "", false
}

// Limits on config.files, so an egg cannot make every start slow.
const (
	maxConfigFiles = 20
	maxConfigFinds = 100
)

var configParsers = []string{"properties", "ini", "yaml", "yml", "json", "file", "xml"}

// configFiles reads an egg's config.files. A find value that is an object
// ({"old": "new"}) changes the key only while its value is "old", as in
// Pterodactyl.
func configFiles(raw json.RawMessage) ([]store.EggConfigFile, error) {
	out := []store.EggConfigFile{}
	if len(raw) == 0 {
		return out, nil
	}
	files, ok := members(raw)
	if !ok {
		return out, nil // "{}", "", null and the like: nothing to edit
	}
	if len(files) > maxConfigFiles {
		return out, &store.UserError{Msg: "egg edits too many config files"}
	}
	for _, f := range files {
		var spec struct {
			Parser string          `json:"parser"`
			Find   json.RawMessage `json:"find"`
		}
		// An entry Wings could not use either is skipped, so the egg still
		// imports as it worked in Pterodactyl.
		if json.Unmarshal(f.Val, &spec) != nil || strings.TrimSpace(f.Key) == "" || !slices.Contains(configParsers, spec.Parser) {
			continue
		}
		cf := store.EggConfigFile{File: f.Key, Parser: spec.Parser, Find: []store.EggFind{}}
		finds, _ := members(spec.Find)
		for _, fd := range finds {
			if v, ok := scalarText(fd.Val); ok {
				cf.Find = append(cf.Find, store.EggFind{Key: fd.Key, Value: v})
				continue
			}
			conds, _ := members(fd.Val)
			for _, c := range conds {
				if v, ok := scalarText(c.Val); ok {
					cf.Find = append(cf.Find, store.EggFind{Key: fd.Key, IfValue: c.Key, Value: v})
				}
			}
		}
		if len(cf.Find) > maxConfigFinds {
			return out, &store.UserError{Msg: "egg config file " + truncate(f.Key, 80) + " sets too many keys"}
		}
		out = append(out, cf)
	}
	return out, nil
}

var placeholderRe = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// panelPlaceholderRe matches the panel values Pterodactyl eggs write with
// dots, such as {{server.build.memory}}.
var panelPlaceholderRe = regexp.MustCompile(`\{\{\s*([a-z_]+(?:\.[a-z_]+)+)\s*\}\}`)

// panelPlaceholders maps the dotted panel values that have a built-in
// variable to it. Any other dotted placeholder is left as written.
var panelPlaceholders = map[string]string{
	"server.build.default.port": "SERVER_PORT",
	"server.build.default.ip":   "SERVER_IP",
	"server.build.memory":       "SERVER_MEMORY",
	"server.uuid":               "P_SERVER_UUID",
}

// shellStartup rewrites Pterodactyl "{{VAR}}" placeholders to "${VAR}".
// Values reach the process only as environment variables, so the shell
// expands them without re-parsing their contents. A variable value therefore
// can never inject commands, and no panel/daemon secret is ever substituted
// (the placeholder set is closed over the server's own env). Pterodactyl's
// string substitution has been the source of injection and token-leak bugs.
//
// Dotted panel values ({{server.build.memory}}) become the built-in
// variable that holds the same value, so they too arrive only through the
// environment.
func shellStartup(s string) string {
	s = panelPlaceholderRe.ReplaceAllStringFunc(s, func(m string) string {
		if env, ok := panelPlaceholders[panelPlaceholderRe.FindStringSubmatch(m)[1]]; ok {
			return "${" + env + "}"
		}
		return m
	})
	return placeholderRe.ReplaceAllString(s, "${$1}")
}

// cfgPlaceholderRe is the placeholder Pterodactyl fills in config.files
// values (no spaces inside the braces).
var cfgPlaceholderRe = regexp.MustCompile(`\{\{([\w.-]*)\}\}`)

// configValue fills a config.files value the way Pterodactyl's panel does:
// {{server.X}} from the server's settings, {{env.X}} from its variables,
// anything unknown as "". {{config.X}} is left for the node, which fills
// only the address containers reach the host on.
func configValue(v string, vals, env map[string]string) string {
	return cfgPlaceholderRe.ReplaceAllStringFunc(v, func(m string) string {
		key := cfgPlaceholderRe.FindStringSubmatch(m)[1]
		switch {
		case strings.HasPrefix(key, "config."):
			return m
		case strings.HasPrefix(key, "env."):
			return env[strings.TrimPrefix(key, "env.")]
		case strings.HasPrefix(key, "server.build.env."):
			return env[strings.TrimPrefix(key, "server.build.env.")]
		case strings.HasPrefix(key, "server."):
			return vals[strings.TrimPrefix(key, "server.")]
		}
		return m
	})
}

// specConfigFiles turns an egg's config.files into what the node applies
// for one server. Values come only from the server's own settings and
// variables.
func specConfigFiles(files []store.EggConfigFile, s store.Server, image string, env map[string]string) []proto.ConfigFile {
	if len(files) == 0 {
		return nil
	}
	i64 := func(n int64) string { return strconv.FormatInt(n, 10) }
	vals := map[string]string{
		"uuid":               s.UUID,
		"build.default.ip":   "0.0.0.0",
		"build.default.port": strconv.Itoa(s.Port),
		"build.memory":       i64(s.MemoryMB),
		"build.swap":         i64(s.SwapMB),
		"build.io":           i64(s.IOWeight),
		"build.cpu":          i64(s.CPUPct),
		"build.disk":         i64(s.DiskMB),
		"build.threads":      s.CPUSet,
		"build.image":        image,
		"container.image":    image,
	}
	out := make([]proto.ConfigFile, 0, len(files))
	for _, f := range files {
		cf := proto.ConfigFile{Path: f.File, Parser: f.Parser, Set: make([]proto.ConfigSet, 0, len(f.Find))}
		for _, fd := range f.Find {
			cf.Set = append(cf.Set, proto.ConfigSet{
				Key: fd.Key, IfValue: fd.IfValue, Value: configValue(fd.Value, vals, env),
			})
		}
		out = append(out, cf)
	}
	return out
}

// buildEnv merges egg defaults, user overrides and the built-in variables.
//
// The built-ins are those Wings sets, so eggs written for Pterodactyl find
// them: SERVER_MEMORY, SERVER_PORT, SERVER_IP (GamePanel publishes ports on
// every address, so 0.0.0.0) and P_SERVER_UUID.
func buildEnv(egg store.Egg, overrides map[string]string, memMB int64, port int, uuid string) map[string]string {
	env := map[string]string{}
	allowed := map[string]bool{}
	for _, v := range egg.Variables {
		env[v.Env] = v.Default
		allowed[v.Env] = true
	}
	for k, v := range overrides {
		if allowed[k] {
			env[k] = v
		}
	}
	env["SERVER_MEMORY"] = strconv.FormatInt(memMB, 10)
	env["SERVER_PORT"] = strconv.Itoa(port)
	env["SERVER_IP"] = "0.0.0.0"
	env["P_SERVER_UUID"] = uuid
	return env
}

// orderedImages reads a {"label": "image"} object in file order. Pterodactyl
// treats the first listed image as the default, and for Java eggs the first
// entry is the newest runtime, so sorting would be wrong.
func orderedImages(raw json.RawMessage) []store.EggImage {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var out []store.EggImage
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return out
		}
		label, _ := k.(string)
		var v string
		if dec.Decode(&v) != nil {
			return out
		}
		if strings.TrimSpace(v) == "" {
			continue
		}
		if strings.TrimSpace(label) == "" {
			label = v
		}
		out = append(out, store.EggImage{Label: label, Image: strings.TrimSpace(v)})
	}
	return out
}

// imageRe accepts Docker image references: registry/name[:tag][@digest].
var imageRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]*$`)

// validImage reports whether s is a plausible image reference. It goes to
// the engine as a single argument, never through a shell; this keeps out
// whitespace, control characters and option-looking values.
func validImage(s string) bool {
	return len(s) <= 255 && imageRe.MatchString(s)
}

func (b *flexBool) or(def bool) bool {
	if b == nil {
		return def
	}
	return bool(*b)
}

// flexBool decodes true/false, 1/0 and "1"/"0", as eggs use all of them.
// Anything else (including a missing field) is false.
type flexBool bool

func (b *flexBool) UnmarshalJSON(raw []byte) error {
	switch strings.Trim(string(raw), `"`) {
	case "true", "1":
		*b = true
	default:
		*b = false
	}
	return nil
}
