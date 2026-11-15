package api

import (
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// ptdlEgg is the subset of the Pterodactyl PTDL_v1/v2 egg format we import,
// so the existing community egg library works unchanged.
type ptdlEgg struct {
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	DockerImages map[string]string `json:"docker_images"` // v2
	Images       []string          `json:"images"`        // v1
	DockerImage  string            `json:"docker_image"`  // v1 (older)
	Startup      string            `json:"startup"`
	Config       struct {
		Stop string `json:"stop"`
	} `json:"config"`
	Variables []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Env         string `json:"env_variable"`
		Default     any    `json:"default_value"`
	} `json:"variables"`
}

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func importPTDL(raw []byte) (store.Egg, error) {
	var p ptdlEgg
	if err := json.Unmarshal(raw, &p); err != nil {
		return store.Egg{}, errors.New("not valid egg JSON")
	}
	e := store.Egg{Name: strings.TrimSpace(p.Name), Description: p.Description, Startup: p.Startup, Stop: p.Config.Stop}
	if e.Name == "" || strings.TrimSpace(e.Startup) == "" {
		return e, errors.New("egg needs a name and a startup command")
	}
	switch {
	case len(p.DockerImages) > 0:
		keys := make([]string, 0, len(p.DockerImages))
		for k := range p.DockerImages {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		e.DockerImage = p.DockerImages[keys[0]]
	case len(p.Images) > 0:
		e.DockerImage = p.Images[0]
	default:
		e.DockerImage = p.DockerImage
	}
	if e.DockerImage == "" {
		return e, errors.New("egg has no docker image")
	}
	if e.Stop == "" {
		e.Stop = "^C"
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
		e.Variables = append(e.Variables, store.EggVar{Env: v.Env, Name: v.Name, Default: def, Description: v.Description})
	}
	return e, nil
}

var placeholderRe = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// shellStartup rewrites Pterodactyl "{{VAR}}" placeholders to "${VAR}".
// Values reach the process only as environment variables, so the shell
// expands them without re-parsing their contents. A variable value therefore
// can never inject commands, and no panel/daemon secret is ever substituted
// (the placeholder set is closed over the server's own env). Pterodactyl's
// string substitution has been the source of injection and token-leak bugs.
func shellStartup(s string) string {
	return placeholderRe.ReplaceAllString(s, "${$1}")
}

// buildEnv merges egg defaults, user overrides and the built-in variables.
func buildEnv(egg store.Egg, overrides map[string]string, memMB int64, port int) map[string]string {
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
	env["SERVER_MEMORY"] = itoa(memMB)
	env["SERVER_PORT"] = itoa(int64(port))
	return env
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
