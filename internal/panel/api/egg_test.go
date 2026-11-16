package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

func TestImportPTDLv2(t *testing.T) {
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
	got := shellStartup("java -jar {{ SERVER_JARFILE }} -m {{SERVER_MEMORY}}")
	want := "java -jar ${SERVER_JARFILE} -m ${SERVER_MEMORY}"
	if got != want {
		t.Fatalf("%q", got)
	}
}

func TestBuildEnvIgnoresUnknownOverrides(t *testing.T) {
	egg := store.Egg{Variables: []store.EggVar{{Env: "SERVER_JARFILE", Default: "server.jar"}}}
	env := buildEnv(egg, map[string]string{
		"SERVER_JARFILE": "paper.jar",
		"LD_PRELOAD":     "/evil.so", // not an egg variable: must be dropped
		"SERVER_PORT":    "1",        // built-ins cannot be overridden
	}, 2048, 25570)
	if env["SERVER_JARFILE"] != "paper.jar" {
		t.Fatal("legit override lost")
	}
	if _, ok := env["LD_PRELOAD"]; ok {
		t.Fatal("unknown variable leaked into the container env")
	}
	if env["SERVER_PORT"] != "25570" || env["SERVER_MEMORY"] != "2048" {
		t.Fatalf("built-ins wrong: %v", env)
	}
}

func TestImportKeepsEveryImageAndVariableSettings(t *testing.T) {
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
	for _, img := range []string{"--privileged", "ubuntu latest", "a\nb", ""} {
		raw, _ := json.Marshal(map[string]any{"name": "x", "startup": "y", "docker_images": map[string]string{"x": img}})
		if _, err := importPTDL(raw); err == nil {
			t.Errorf("image %q was accepted", img)
		}
	}
}
