package cfgfile

import (
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

type memFS struct {
	files      map[string]string
	writes     int
	failWrites bool
}

func (m *memFS) Read(p string) ([]byte, error) {
	s, ok := m.files[p]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(s), nil
}

func (m *memFS) WriteStream(p string, r io.Reader, _ int64) (int64, error) {
	if m.failWrites {
		return 0, errors.New("no space left on device")
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	m.files[p] = string(b)
	m.writes++
	return int64(len(b)), nil
}

func ptr(s string) *string { return &s }

// The sets below are what the panel sends for the stock Pterodactyl eggs
// on a server whose port is 25570.

var paper = proto.ConfigFile{Path: "server.properties", Parser: "properties", Set: []proto.ConfigSet{
	{Key: "server-ip", Value: "0.0.0.0"},
	{Key: "server-port", Value: "25570"},
	{Key: "query.port", Value: "25570"},
}}

func TestPropertiesKeepsCommentsAndOrder(t *testing.T) {
	t.Parallel()
	m := &memFS{files: map[string]string{"server.properties": `#Minecraft server properties
#Mon Nov 24 10:00:00 UTC 2026
enable-jmx-monitoring=false
server-ip=
motd=A Minecraft Server \u00E9t\u00E9
server-port=25565
query.port = 25565
long=one\
  two
`}}
	if err := Apply(m, paper, ""); err != nil {
		t.Fatal(err)
	}
	want := `#Minecraft server properties
#Mon Nov 24 10:00:00 UTC 2026
enable-jmx-monitoring=false
server-ip=0.0.0.0
motd=A Minecraft Server \u00E9t\u00E9
server-port=25570
query.port = 25570
long=one\
  two
`
	if got := m.files["server.properties"]; got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	// A second start changes nothing, so the file is not written again.
	m.writes = 0
	if err := Apply(m, paper, ""); err != nil || m.writes != 0 {
		t.Fatalf("rewrote an unchanged file: %d writes, %v", m.writes, err)
	}
}

func TestMissingFileIsCreated(t *testing.T) {
	t.Parallel()
	m := &memFS{files: map[string]string{}}
	if err := Apply(m, paper, ""); err != nil {
		t.Fatal(err)
	}
	if got, want := m.files["server.properties"], "server-ip=0.0.0.0\nserver-port=25570\nquery.port=25570\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPropertiesEscapes(t *testing.T) {
	t.Parallel()
	m := &memFS{files: map[string]string{"a.properties": "k:old\r\n"}}
	f := proto.ConfigFile{Path: "a.properties", Parser: "properties", Set: []proto.ConfigSet{
		{Key: "k", Value: " lead\\ été 😀"},
		{Key: "new key", Value: "x=y"},
	}}
	if err := Apply(m, f, ""); err != nil {
		t.Fatal(err)
	}
	want := "k:\\ lead\\\\ \\u00E9t\\u00E9 \\uD83D\\uDE00\r\nnew\\ key=x=y\r\n"
	if got := m.files["a.properties"]; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	pls, _ := parseProperties([]byte(want))
	if pls[0].value != " lead\\ été 😀" || pls[1].key != "new key" {
		t.Fatalf("does not read back: %+v", pls)
	}
}

const bungee = `# BungeeCord config
listeners:
- query_port: 25577
  motd: '&1Another Bungee server'
  host: 0.0.0.0:25577
  max_players: 1
servers:
  lobby:
    motd: '&1Just another BungeeCord - Forced Host'
    address: localhost:25565 # the lobby
    restricted: false
  other:
    address: 10.0.0.5:25565
`

func TestYAMLBungeecord(t *testing.T) {
	t.Parallel()
	m := &memFS{files: map[string]string{"config.yml": bungee}}
	f := proto.ConfigFile{Path: "config.yml", Parser: "yaml", Set: []proto.ConfigSet{
		{Key: "listeners[0].query_port", Value: "25570"},
		{Key: "listeners[0].host", Value: "0.0.0.0:25570"},
		{Key: "servers.*.address", IfValue: ptr(`regex:^(127\.0\.0\.1|localhost)(:\d{1,5})?$`), Value: "{{config.docker.interface}}$2"},
	}}
	if err := Apply(m, f, "172.17.0.1"); err != nil {
		t.Fatal(err)
	}
	got := m.files["config.yml"]
	for _, want := range []string{
		"# BungeeCord config",
		"query_port: 25570\n", // a number, as Wings writes it
		"host: 0.0.0.0:25570",
		"address: 172.17.0.1:25565 # the lobby",
		"address: 10.0.0.5:25565", // did not match the regex
		"motd: '&1Another Bungee server'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestJSONKeepsOrderAndTypes(t *testing.T) {
	t.Parallel()
	m := &memFS{files: map[string]string{"data/server-settings.json": `{
  "name": "Name of the game",
  "visibility": {"public": true, "lan": true},
  "max_players": 0,
  "tags": ["game", "tags"],
  "note": "<a & b>"
}
`}}
	f := proto.ConfigFile{Path: "data/server-settings.json", Parser: "json", Set: []proto.ConfigSet{
		{Key: "name", Value: "My server"},
		{Key: "max_players", Value: "10"},
		{Key: "visibility.public", Value: "false"},
		{Key: "new.nested", Value: "x"},
		{Key: "missing", IfValue: ptr("y"), Value: "z"},
	}}
	if err := Apply(m, f, ""); err != nil {
		t.Fatal(err)
	}
	want := `{
  "name": "My server",
  "visibility": {
    "public": false,
    "lan": true
  },
  "max_players": 10,
  "tags": [
    "game",
    "tags"
  ],
  "note": "<a & b>",
  "new": {
    "nested": "x"
  }
}
`
	if got := m.files["data/server-settings.json"]; got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestINIMumble(t *testing.T) {
	t.Parallel()
	m := &memFS{files: map[string]string{"murmur.ini": `; Murmur configuration file
database=
port=64738
; host=
users = 100

[Ice]
Ice.Warn.UnknownProperties=1
`}}
	f := proto.ConfigFile{Path: "murmur.ini", Parser: "ini", Set: []proto.ConfigSet{
		{Key: "database", Value: "/home/container/murmur.sqlite"},
		{Key: "port", Value: "30000"},
		{Key: "host", Value: "0.0.0.0"},
		{Key: "users", Value: "20"},
		{Key: "Other.key", Value: "v"},
	}}
	if err := Apply(m, f, ""); err != nil {
		t.Fatal(err)
	}
	want := `; Murmur configuration file
database=/home/container/murmur.sqlite
port=30000
; host=
users = 20
host=0.0.0.0

[Ice]
Ice.Warn.UnknownProperties=1
[Other]
key=v
`
	if got := m.files["murmur.ini"]; got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTextReplacesWholeLines(t *testing.T) {
	t.Parallel()
	m := &memFS{files: map[string]string{"velocity.toml": "config-version = \"2.6\"\nbind = \"0.0.0.0:25577\"\nmotd = \"bind\"\n"}}
	f := proto.ConfigFile{Path: "velocity.toml", Parser: "file", Set: []proto.ConfigSet{{Key: "bind", Value: `bind = "0.0.0.0:25570"`}}}
	if err := Apply(m, f, ""); err != nil {
		t.Fatal(err)
	}
	if got, want := m.files["velocity.toml"], "config-version = \"2.6\"\nbind = \"0.0.0.0:25570\"\nmotd = \"bind\"\n"; got != want {
		t.Fatalf("got %q", got)
	}
}

func TestNodeSecretsAreNeverFilledIn(t *testing.T) {
	t.Parallel()
	m := &memFS{files: map[string]string{}}
	f := proto.ConfigFile{Path: "a.properties", Parser: "properties", Set: []proto.ConfigSet{
		{Key: "token", Value: "{{config.token}}{{config.remote}}"},
		{Key: "ip", Value: "{{config.docker.interface}}"},
	}}
	if err := Apply(m, f, "172.17.0.1"); err != nil {
		t.Fatal(err)
	}
	if got := m.files["a.properties"]; got != "token=\nip=172.17.0.1\n" {
		t.Fatalf("got %q", got)
	}
}

func TestIfValue(t *testing.T) {
	t.Parallel()
	m := &memFS{files: map[string]string{"s.properties": "a=1\nb=2\n"}}
	f := proto.ConfigFile{Path: "s.properties", Parser: "properties", Set: []proto.ConfigSet{
		{Key: "a", IfValue: ptr("1"), Value: "x"},
		{Key: "b", IfValue: ptr("1"), Value: "x"},
		{Key: "c", IfValue: ptr("1"), Value: "x"},
		{Key: "a", IfValue: ptr("regex:("), Value: "bad regex is skipped"},
	}}
	if err := Apply(m, f, ""); err != nil {
		t.Fatal(err)
	}
	if got := m.files["s.properties"]; got != "a=x\nb=2\n" {
		t.Fatalf("got %q", got)
	}
}

func TestUnknownParserAndBadFiles(t *testing.T) {
	t.Parallel()
	m := &memFS{files: map[string]string{"bad.json": "{nope", "x.xml": "<a/>"}}
	if err := Apply(m, proto.ConfigFile{Path: "x.xml", Parser: "xml"}, ""); err == nil {
		t.Fatal("xml should be refused")
	}
	if err := Apply(m, proto.ConfigFile{Path: "bad.json", Parser: "json", Set: []proto.ConfigSet{{Key: "a", Value: "b"}}}, ""); err == nil {
		t.Fatal("broken JSON should fail, not be overwritten")
	}
	if m.files["bad.json"] != "{nope" {
		t.Fatal("broken file was changed")
	}
}

// CodeRabbit's findings on #102, one case each.

func TestPropertiesKeyWithoutValue(t *testing.T) {
	t.Parallel()
	m := &memFS{files: map[string]string{"server.properties": "server-port\nmotd=hi\n"}}
	if err := Apply(m, paper, ""); err != nil {
		t.Fatal(err)
	}
	if got := m.files["server.properties"]; !strings.Contains(got, "server-port=25570\n") {
		t.Fatalf("got %q", got)
	}
}

func TestEmptyConditionMatchesOnlyAnEmptyValue(t *testing.T) {
	t.Parallel()
	m := &memFS{files: map[string]string{"s.properties": "motd=Custom\nempty=\n"}}
	f := proto.ConfigFile{Path: "s.properties", Parser: "properties", Set: []proto.ConfigSet{
		{Key: "motd", IfValue: ptr(""), Value: "Welcome"},
		{Key: "empty", IfValue: ptr(""), Value: "Welcome"},
		{Key: "absent", IfValue: ptr(""), Value: "Welcome"},
	}}
	if err := Apply(m, f, ""); err != nil {
		t.Fatal(err)
	}
	if got := m.files["s.properties"]; got != "motd=Custom\nempty=Welcome\n" {
		t.Fatalf("got %q", got)
	}
}

func TestTerminalWildcard(t *testing.T) {
	t.Parallel()
	m := &memFS{files: map[string]string{"a.json": `{"ports": [1, 2], "hosts": {"a": "x", "b": "y"}}`}}
	f := proto.ConfigFile{Path: "a.json", Parser: "json", Set: []proto.ConfigSet{
		{Key: "ports.*", Value: "25570"},
		{Key: "hosts.*", IfValue: ptr("x"), Value: "z"},
	}}
	if err := Apply(m, f, ""); err != nil {
		t.Fatal(err)
	}
	got := m.files["a.json"]
	if strings.Count(got, "25570") != 2 || !strings.Contains(got, `"a": "z"`) || !strings.Contains(got, `"b": "y"`) {
		t.Fatalf("got:\n%s", got)
	}
}

// A new BungeeCord config.yml: listeners is a list, as BungeeCord reads it.
func TestIndexedPathsInANewFileMakeLists(t *testing.T) {
	t.Parallel()
	m := &memFS{files: map[string]string{}}
	f := proto.ConfigFile{Path: "config.yml", Parser: "yaml", Set: []proto.ConfigSet{
		{Key: "listeners[0].query_port", Value: "25570"},
		{Key: "listeners[0].host", Value: "0.0.0.0:25570"},
		{Key: "listeners[5].host", Value: "skipped: not right after the last element"},
	}}
	if err := Apply(m, f, ""); err != nil {
		t.Fatal(err)
	}
	want := "listeners:\n  - query_port: 25570\n    host: 0.0.0.0:25570\n"
	if got := m.files["config.yml"]; got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestYAMLWithSeveralDocumentsIsNotCut(t *testing.T) {
	t.Parallel()
	orig := "port: 25565\n---\nkeep: important\n"
	m := &memFS{files: map[string]string{"a.yml": orig}}
	f := proto.ConfigFile{Path: "a.yml", Parser: "yaml", Set: []proto.ConfigSet{{Key: "port", Value: "25570"}}}
	if err := Apply(m, f, ""); err == nil || !strings.Contains(err.Error(), "more than one YAML document") {
		t.Fatalf("got %v", err)
	}
	if m.files["a.yml"] != orig {
		t.Fatal("the file was changed")
	}
}

func TestJSONUnicodeEscapes(t *testing.T) {
	t.Parallel()
	for _, name := range []string{`"\uD83D\uDE00"`, `"😀"`} {
		m := &memFS{files: map[string]string{"a.json": `{"name":` + name + `,"max_players":0}`}}
		f := proto.ConfigFile{Path: "a.json", Parser: "json", Set: []proto.ConfigSet{{Key: "max_players", Value: "10"}}}
		if err := Apply(m, f, ""); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := m.files["a.json"]; !strings.Contains(got, `"name": "😀"`) || !strings.Contains(got, `"max_players": 10`) {
			t.Fatalf("%s: got %s", name, got)
		}
	}
}

// A write that fails leaves the file as it was (the real FS writes a
// temporary file and renames it).
func TestFailedWriteKeepsTheFile(t *testing.T) {
	t.Parallel()
	m := &memFS{files: map[string]string{"server.properties": "server-port=25565\n"}, failWrites: true}
	if err := Apply(m, paper, ""); err == nil {
		t.Fatal("a failed write was not reported")
	}
	if m.files["server.properties"] != "server-port=25565\n" {
		t.Fatal("the file changed")
	}
}
