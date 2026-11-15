package api

import (
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
