package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// A start edits the egg's config files first: an existing file in place,
// and one the game has not written yet is created (a new Minecraft server
// needs its port before its first start). A file that cannot be edited does
// not stop the start.
func TestStartAppliesConfigFiles(t *testing.T) {
	t.Parallel()
	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	spec := proto.ServerSpec{UUID: id, Image: "img", Startup: "run", Stop: "^C", MemoryMB: 128, ConfigFiles: []proto.ConfigFile{
		{Path: "server.properties", Parser: "properties", Set: []proto.ConfigSet{{Key: "server-port", Value: "25570"}}},
		{Path: "plugins/x/config.yml", Parser: "yaml", Set: []proto.ConfigSet{{Key: "host", Value: "{{config.docker.interface}}:25570"}}},
		{Path: "broken.json", Parser: "json", Set: []proto.ConfigSet{{Key: "a", Value: "b"}}},
		{Path: "../outside.properties", Parser: "properties", Set: []proto.ConfigSet{{Key: "a", Value: "b"}}},
	}}
	if err := d.create(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if !eventually(5*time.Second, func() bool { st, _ := d.installState(id); return st == "" }) {
		t.Fatal("server never finished provisioning")
	}
	dir := d.serverDir(id)
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte("#comment\nserver-port=25565\nmotd=hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := power(d, id, "start"); err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]string{
		"server.properties":    "#comment\nserver-port=25570\nmotd=hi\n",
		"plugins/x/config.yml": "host: 172.17.0.1:25570\n",
		"broken.json":          "{",
	} {
		got, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q (%v), want %q", file, got, err, want)
		}
	}
	// "../" stays inside the server's directory.
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "outside.properties")); err == nil {
		t.Error("a config file was written outside the server's directory")
	}
	if st, _ := d.rt.State(context.Background(), id); st != runtime.StateRunning {
		t.Fatalf("state %s", st)
	}
}
