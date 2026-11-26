package api

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

func TestImportPTDLv2(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
	  "name":"Paper","description":"mc",
	  "docker_images":{"Java 21":"ghcr.io/pterodactyl/yolks:java_21","Java 17":"ghcr.io/pterodactyl/yolks:java_17"},
	  "startup":"java -Xmx{{SERVER_MEMORY}}M -jar {{SERVER_JARFILE}}",
	  "config":{"stop":"stop"},
	  "variables":[{"name":"Jar","env_variable":"SERVER_JARFILE","default_value":"server.jar"},
	               {"name":"bad","env_variable":"1BAD NAME","default_value":"x"},
	               {"name":"n","env_variable":"MAXP","default_value":20}]
	}`)
	e, err := importPTDL(raw)
	if err != nil {
		t.Fatal(err)
	}
	if e.DockerImage != "ghcr.io/pterodactyl/yolks:java_21" { // first listed image is the default, as in Pterodactyl
		t.Fatalf("image %q", e.DockerImage)
	}
	if e.Stop != "stop" || len(e.Variables) != 2 || e.Variables[1].Default != "20" {
		t.Fatalf("%+v", e)
	}
}

func TestShellStartupHasNoInjection(t *testing.T) {
	t.Parallel()
	got := shellStartup("java -jar {{ SERVER_JARFILE }} -m {{SERVER_MEMORY}}")
	want := "java -jar ${SERVER_JARFILE} -m ${SERVER_MEMORY}"
	if got != want {
		t.Fatalf("%q", got)
	}
}

func TestShellStartupPanelPlaceholders(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ in, want string }{
		{"java -Xmx{{server.build.memory}}M -jar server.jar", "java -Xmx${SERVER_MEMORY}M -jar server.jar"},
		{"java -Xmx{{ server.build.memory }}M", "java -Xmx${SERVER_MEMORY}M"},
		{"./srv --port {{server.build.default.port}}", "./srv --port ${SERVER_PORT}"},
		{"./srv --port {{  server.build.default.port}}", "./srv --port ${SERVER_PORT}"},
		{"./srv +ip {{server.build.default.ip}}", "./srv +ip ${SERVER_IP}"},
		{"./srv --id {{server.uuid}}", "./srv --id ${P_SERVER_UUID}"},
		// No equivalent: left as written, never filled with panel data.
		{"./srv {{config.docker.interface}} {{server.build.env.X}}", "./srv {{config.docker.interface}} {{server.build.env.X}}"},
		{"./srv {{SERVER_PORT}} {{server.build.default.port}}", "./srv ${SERVER_PORT} ${SERVER_PORT}"},
	} {
		if got := shellStartup(c.in); got != c.want {
			t.Errorf("shellStartup(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuildEnvIgnoresUnknownOverrides(t *testing.T) {
	t.Parallel()
	egg := store.Egg{Variables: []store.EggVar{{Env: "SERVER_JARFILE", Default: "server.jar"}}}
	env := buildEnv(egg, map[string]string{
		"SERVER_JARFILE": "paper.jar",
		"LD_PRELOAD":     "/evil.so", // not an egg variable: must be dropped
		"SERVER_PORT":    "1",        // built-ins cannot be overridden
		"SERVER_IP":      "10.0.0.1",
	}, 2048, 25570, "u1")
	if env["SERVER_JARFILE"] != "paper.jar" {
		t.Fatal("legit override lost")
	}
	if _, ok := env["LD_PRELOAD"]; ok {
		t.Fatal("unknown variable leaked into the container env")
	}
	if env["SERVER_PORT"] != "25570" || env["SERVER_MEMORY"] != "2048" || env["SERVER_IP"] != "0.0.0.0" || env["P_SERVER_UUID"] != "u1" {
		t.Fatalf("built-ins wrong: %v", env)
	}
}

func TestImportKeepsEveryImageAndVariableSettings(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
	  "name":"Paper",
	  "docker_images":{"Java 21":"ghcr.io/pterodactyl/yolks:java_21","Java 8":"ghcr.io/pterodactyl/yolks:java_8","Java 17":"ghcr.io/pterodactyl/yolks:java_17"},
	  "startup":"java -jar {{SERVER_JARFILE}}",
	  "variables":[
	    {"name":"Jar","env_variable":"SERVER_JARFILE","default_value":"server.jar","user_viewable":true,"user_editable":true,"rules":"required|string|max:20"},
	    {"name":"Key","env_variable":"LICENSE","default_value":"","user_viewable":0,"user_editable":"0","rules":"nullable|string"},
	    {"name":"Old","env_variable":"OLD","default_value":"x"}
	  ]
	}`)
	e, err := importPTDL(raw)
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, i := range e.DockerImages {
		labels = append(labels, i.Label)
	}
	if strings.Join(labels, ",") != "Java 21,Java 8,Java 17" { // file order, not sorted
		t.Fatalf("images %v", labels)
	}
	v := e.Variables
	if !v[0].UserViewable || !v[0].UserEditable || v[0].Rules != "required|string|max:20" {
		t.Fatalf("jar %+v", v[0])
	}
	if v[1].UserViewable || v[1].UserEditable {
		t.Fatalf("hidden variable came out visible: %+v", v[1])
	}
	if !v[2].UserViewable || !v[2].UserEditable { // no settings: visible and editable
		t.Fatalf("old-style variable %+v", v[2])
	}
}

func TestImportRejectsBadImageNames(t *testing.T) {
	t.Parallel()
	for _, img := range []string{"--privileged", "ubuntu latest", "a\nb", ""} {
		raw, _ := json.Marshal(map[string]any{"name": "x", "startup": "y", "docker_images": map[string]string{"x": img}})
		if _, err := importPTDL(raw); err == nil {
			t.Errorf("image %q was accepted", img)
		}
	}
}

// The Done text and features are read from every shape eggs use for them.
func TestImportDoneAndFeatures(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		startup string
		want    []string
	}{
		"json string":   {`"{\"done\":\")! For help, type \"}"`, []string{`)! For help, type `}},
		"object":        {`{"done":"Listening on "}`, []string{"Listening on "}},
		"list":          {`{"done":["Done (","Ready"]}`, []string{"Done (", "Ready"}},
		"regex skipped": {`{"done":["regex:^Done","ok"]}`, []string{"ok"}},
		"none":          {`"{}"`, []string{}},
		"missing":       {`null`, []string{}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			raw := `{"name":"x","startup":"run","docker_images":{"a":"img"},"features":["EULA","eula","java_version","bad feature"],"config":{"startup":` + tc.startup + `}}`
			e, err := importPTDL([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(e.Done, "|") != strings.Join(tc.want, "|") || e.Done == nil {
				t.Errorf("done %q, want %q", e.Done, tc.want)
			}
			if strings.Join(e.Features, ",") != "eula,java_version" {
				t.Errorf("features %q", e.Features)
			}
		})
	}
}

// The stock BungeeCord egg's config.files, as Pterodactyl exports it (a JSON
// string inside the JSON).
const bungeeFiles = `"{\n    \"config.yml\": {\n        \"parser\": \"yaml\",\n        \"find\": {\n            \"listeners[0].query_port\": \"{{server.build.default.port}}\",\n            \"listeners[0].host\": \"0.0.0.0:{{server.build.default.port}}\",\n            \"servers.*.address\": {\n                \"regex:^(127\\\\.0\\\\.0\\\\.1|localhost)(:\\\\d{1,5})?$\": \"{{config.docker.interface}}$2\"\n            }\n        }\n    }\n}"`

func TestImportConfigFiles(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"name":"Bungee","docker_images":{"J":"ghcr.io/pterodactyl/yolks:java_21"},"startup":"java -jar b.jar",
	  "config":{"files":` + bungeeFiles + `}}`)
	e, err := importPTDL(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []store.EggConfigFile{{File: "config.yml", Parser: "yaml", Find: []store.EggFind{
		{Key: "listeners[0].query_port", Value: "{{server.build.default.port}}"},
		{Key: "listeners[0].host", Value: "0.0.0.0:{{server.build.default.port}}"},
		{Key: "servers.*.address", IfValue: ptr(`regex:^(127\.0\.0\.1|localhost)(:\d{1,5})?$`), Value: "{{config.docker.interface}}$2"},
	}}}
	got, _ := json.Marshal(e.ConfigFiles)
	exp, _ := json.Marshal(want)
	if !bytes.Equal(got, exp) {
		t.Fatalf("got  %s\nwant %s", got, exp)
	}

	// Objects, numbers and booleans; unusable entries are skipped.
	raw = []byte(`{"name":"X","docker_image":"img","startup":"x","config":{"files":{
	  "a.json":{"parser":"json","find":{"n":5,"b":true,"s":"v","skip":null}},
	  "bad.txt":{"parser":"nope","find":{"a":"b"}},
	  "worse":"not an object"}}}`)
	if e, err = importPTDL(raw); err != nil {
		t.Fatal(err)
	}
	if len(e.ConfigFiles) != 1 || len(e.ConfigFiles[0].Find) != 3 || e.ConfigFiles[0].Find[0].Value != "5" || e.ConfigFiles[0].Find[1].Value != "true" {
		t.Fatalf("%+v", e.ConfigFiles)
	}
	for _, empty := range []string{`"{}"`, `{}`, `""`, `null`, `[]`} {
		e, err := importPTDL([]byte(`{"name":"X","docker_image":"img","startup":"x","config":{"files":` + empty + `}}`))
		if err != nil || len(e.ConfigFiles) != 0 {
			t.Fatalf("%s: %v %+v", empty, err, e.ConfigFiles)
		}
	}
}

func TestSpecConfigFilesUseOnlyTheServersValues(t *testing.T) {
	t.Parallel()
	files := []store.EggConfigFile{{File: "murmur.ini", Parser: "ini", Find: []store.EggFind{
		{Key: "port", Value: "{{server.build.default.port}}"},
		{Key: "users", Value: "{{server.build.env.MAX_USERS}}"},
		{Key: "users2", Value: "{{env.MAX_USERS}}"},
		{Key: "mem", Value: "{{server.build.memory}}M on {{server.build.default.ip}}"},
		{Key: "iface", Value: "{{config.docker.interface}}"},
		{Key: "unknown", Value: "[{{server.secret}}][{{SERVER_PORT}}][{{ server.build.memory }}]"},
	}}}
	s := store.Server{UUID: "u1", Port: 30000, Limits: store.Limits{MemoryMB: 512}}
	got := specConfigFiles(files, s, "img", map[string]string{"MAX_USERS": "20"})
	want := []string{"30000", "20", "20", "512M on 0.0.0.0", "{{config.docker.interface}}", "[][{{SERVER_PORT}}][{{ server.build.memory }}]"}
	for i, w := range want {
		if got[0].Set[i].Value != w {
			t.Errorf("%s = %q, want %q", got[0].Set[i].Key, got[0].Set[i].Value, w)
		}
	}
	if specConfigFiles(nil, s, "img", nil) != nil {
		t.Fatal("no config files should send none")
	}
}

func ptr(s string) *string { return &s }
