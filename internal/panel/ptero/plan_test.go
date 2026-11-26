package ptero

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
)

func ns(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
func ni(i int64) sql.NullInt64   { return sql.NullInt64{Int64: i, Valid: true} }

const paperStartup = "java -Xms128M -Xmx{{SERVER_MEMORY}}M -jar {{SERVER_JARFILE}}"

// fixture is a small Pterodactyl panel: two nests with a same-named egg, an
// egg that inherits its script, and servers with the awkward cases.
func fixture() *Source {
	return &Source{
		Users: []User{
			{ID: 1, Email: "Admin@Example.com", Username: "admin", Password: "$2y$10$abcdefghijklmnopqrstuv", Admin: true, UseTOTP: true, TOTPSecret: ns("ENC")},
			{ID: 2, Email: "alice@example.com", Password: "$2y$10$abcdefghijklmnopqrstuv"},
			{ID: 3, Email: "old@example.com", Password: "md5:whatever"},
		},
		Nodes: []Node{
			{ID: 1, Name: "Node 1", Memory: 8192, Disk: 50000, SFTPPort: 2022, Base: "/var/lib/pterodactyl/volumes"},
			{ID: 2, Name: "node 1", Memory: 1, MemoryOverallocate: -1, DiskOverallocate: -1, Base: "/srv/wings"},
		},
		Allocations: []Allocation{
			{ID: 10, NodeID: 1, IP: "0.0.0.0", Port: 25565, ServerID: ni(100)},
			{ID: 11, NodeID: 1, IP: "0.0.0.0", Port: 25566, ServerID: ni(100), Notes: ns("Dynmap")},
			{ID: 12, NodeID: 1, IP: "10.0.0.5", Port: 25565, ServerID: ni(100)}, // same port, other IP
			{ID: 13, NodeID: 1, IP: "0.0.0.0", Port: 25600, ServerID: ni(101)},
			{ID: 14, NodeID: 1, IP: "0.0.0.0", Port: 25700},
		},
		Nests: map[int64]string{1: "Minecraft", 2: "Other"},
		Eggs: []Egg{
			{
				ID: 1, NestID: 1, Name: "Paper", DockerImages: ns(`{"Java 21":"ghcr.io/x/java:21","Java 17":"ghcr.io/x/java:17"}`),
				Startup: ns(paperStartup), ConfigStop: ns("stop"), ConfigFiles: ns(`{"server.properties":{"parser":"properties","find":{"server-port":"{{server.build.default.port}}"}}}`), ConfigStartup: ns(`{"done":")! For help"}`),
				ScriptImage: "ghcr.io/x/installer", ScriptEntry: "bash", ScriptInstall: ns("echo install"),
			},
			{
				ID: 2, NestID: 1, Name: "Paper child", DockerImages: ns(`{"Java 21":"ghcr.io/x/java:21"}`), Startup: ns(paperStartup),
				ConfigFrom: ni(1), CopyScriptFrom: ni(1), ScriptImage: "alpine:3.4", ScriptEntry: "ash", FileDenylist: ns(`["server.jar"]`),
			},
			{ID: 3, NestID: 2, Name: "paper", DockerImages: ns(`["ghcr.io/x/old:1"]`), Startup: ns("./run"), ConfigFiles: ns(`{"serverconfig.xml":{"parser":"xml","find":{"a":"b"}}}`)},
			{ID: 4, NestID: 2, Name: "Broken", DockerImages: ns(`{}`), Startup: ns("./run")},
		},
		EggVars: []EggVar{
			{ID: 1, EggID: 1, Name: "Jar", Env: "SERVER_JARFILE", Default: "server.jar", UserViewable: true, UserEditable: true},
			{ID: 2, EggID: 1, Name: "Version", Env: "MC_VERSION", Default: "latest", UserViewable: true, UserEditable: true},
		},
		Servers: []Server{
			{
				ID: 100, UUID: "11111111-1111-1111-1111-111111111111", NodeID: 1, Name: "Survival", OwnerID: 2, Memory: 4096, Disk: 10000, IO: 500, CPU: 200,
				AllocationID: 10, EggID: 1, Startup: paperStartup, Image: "ghcr.io/x/java:21", AllocationLimit: ni(3), DatabaseLimit: ni(2), BackupLimit: 5,
				OOMDisabled: true, ExternalID: ns("order-1"),
			},
			{
				ID: 101, UUID: "22222222-2222-2222-2222-222222222222", NodeID: 1, Name: "Unlimited", OwnerID: 1, Memory: 0, Swap: -5, Disk: 0, IO: 500,
				Threads: ns("0-1"), AllocationID: 13, EggID: 1, Startup: "java -jar custom.jar", Image: "ghcr.io/x/java:17", BackupLimit: 0, Status: ns("suspended"),
			},
		},
		ServerVars: []ServerVar{{ServerID: 100, VariableID: 2, Value: "1.21.4"}, {ServerID: 100, VariableID: 99, Value: "orphan"}},
		Subusers: []Subuser{
			{UserID: 1, ServerID: 100, Permissions: ns(`["control.console","control.start","control.stop","control.restart","file.read","file.sftp","user.create"]`)},
			{UserID: 2, ServerID: 100, Permissions: ns(`["control.console"]`)}, // the owner
		},
		Schedules: []Schedule{
			{ID: 1, ServerID: 100, Name: "Nightly", Minute: "0", Hour: "4", DayOfMonth: "*", Month: "*", DayOfWeek: "*", Active: true},
			{ID: 2, ServerID: 100, Name: "Bad cron", Minute: "61", Hour: "*", DayOfMonth: "*", Month: "*", DayOfWeek: "*"},
		},
		Tasks: []Task{
			{ScheduleID: 1, Sequence: 1, Action: "command", Payload: "say bye"},
			{ScheduleID: 1, Sequence: 2, Action: "power", Payload: "restart", Offset: 1200},
		},
		DBHosts:   []DBHost{{ID: 1, Name: "db1", Host: "10.0.0.10", Port: 3306, Username: "ptero", Password: "ENC", NodeID: ni(1)}},
		Databases: []Database{{ServerID: 100, HostID: 1, Name: "s1_db", Username: "u1", Remote: "%", Password: "ENC"}},
		Backups:   2,
	}
}

func hasNote(p *Plan, l Level, sub string) bool {
	return slices.ContainsFunc(p.Notes, func(n Note) bool { return n.Level == l && strings.Contains(n.Subject+" "+n.Msg, sub) })
}

func server(t *testing.T, p *Plan, name string) PlanServer {
	t.Helper()
	for _, s := range p.Servers {
		if s.Server.Name == name {
			return s
		}
	}
	t.Fatalf("server %q not planned; notes: %+v", name, p.Notes)
	return PlanServer{}
}

func TestPlanWithoutAppKey(t *testing.T) {
	t.Parallel()
	p := Build(fixture(), Options{})
	if p.Blocked() {
		t.Fatalf("blocked: %+v", p.Notes)
	}
	// Users: emails lower-cased, bcrypt kept, others flagged; 2FA off.
	if p.Users[0].Email != "admin@example.com" || !p.Users[0].Admin || p.Users[0].TOTPSecret != "" {
		t.Errorf("admin: %+v", p.Users[0])
	}
	if p.Users[2].PasswordHash != "" || !hasNote(p, Changed, "old@example.com") {
		t.Error("a non-bcrypt hash must be dropped and reported")
	}
	if !hasNote(p, Changed, "two-factor is turned off for 1 user(s)") {
		t.Error("2FA without a key not reported")
	}
	// Database hosts need the key.
	if len(p.DBHosts) != 0 || len(p.Databases) != 0 || !hasNote(p, Changed, `database host "db1"`) {
		t.Errorf("hosts without a key: %+v", p.DBHosts)
	}
}

func TestPlanNodes(t *testing.T) {
	t.Parallel()
	p := Build(fixture(), Options{})
	n1, n2 := p.Nodes[0], p.Nodes[1]
	if n1.PortStart != 25565 || n1.PortEnd != 25700 || n1.MemoryMB != 8192 || n1.SFTPPort != 2022 {
		t.Errorf("node 1: %+v", n1)
	}
	if n2.Name != "node 1 (2)" || n2.MemoryMB != 0 || n2.DiskMB != 0 || n2.WingsDir != "/srv/wings" {
		t.Errorf("node 2 (same name, -1 overallocation): %+v", n2)
	}
}

func TestPlanEggs(t *testing.T) {
	t.Parallel()
	p := Build(fixture(), Options{})
	byName := map[string]PlanEgg{}
	for _, e := range p.Eggs {
		byName[e.Egg.Name] = e
	}
	paper := byName["Paper (Minecraft)"]
	if paper.Egg.DockerImage != "ghcr.io/x/java:21" || len(paper.Egg.DockerImages) != 2 || paper.Egg.Stop != "stop" || len(paper.Egg.Done) != 1 {
		t.Errorf("paper: %+v", paper.Egg)
	}
	if _, ok := byName["paper (Other)"]; !ok {
		t.Errorf("same-named egg in another nest not renamed: %v", byName)
	}
	child := byName["Paper child"]
	if child.Egg.InstallScript != "echo install" || child.Egg.InstallImage != "ghcr.io/x/installer" || child.Egg.Stop != "stop" {
		t.Errorf("child did not inherit script and config: %+v", child.Egg)
	}
	if !hasNote(p, Changed, "file denylist") || !hasNote(p, Changed, "XML config files") {
		t.Error("denylist / XML config files not reported")
	}
	// The child inherits config.files from its parent (config_from).
	for _, e := range []PlanEgg{paper, child} {
		if len(e.Egg.ConfigFiles) != 1 || e.Egg.ConfigFiles[0].File != "server.properties" || e.Egg.ConfigFiles[0].Find[0].Value != "{{server.build.default.port}}" {
			t.Errorf("%s config files: %+v", e.Egg.Name, e.Egg.ConfigFiles)
		}
	}
	if !hasNote(p, Changed, `egg "Broken" skipped`) {
		t.Error("an unused broken egg should be skipped with a note")
	}
}

func TestPlanServers(t *testing.T) {
	t.Parallel()
	p := Build(fixture(), Options{UnlimitedMemoryMB: 2048})
	s := server(t, p, "Survival")
	v := s.Server
	if v.Port != 25565 || len(s.Extra) != 1 || s.Extra[0] != (PlanPort{25566, "Dynmap"}) {
		t.Errorf("ports: %d %+v", v.Port, s.Extra)
	}
	if !hasNote(p, Changed, "10.0.0.5:25565 is dropped") {
		t.Error("a duplicate port on another IP must be dropped and reported")
	}
	if v.Startup != "" || v.Image != "" {
		t.Errorf("egg defaults should be stored as empty: startup %q image %q", v.Startup, v.Image)
	}
	if v.Env["MC_VERSION"] != "1.21.4" || len(v.Env) != 1 {
		t.Errorf("env: %v", v.Env)
	}
	if v.PortLimit != 2 || v.DatabaseLimit != 2 || v.BackupLimit != 5 || v.IOWeight != 0 || v.ExternalID != "order-1" {
		t.Errorf("limits: %+v", v.Limits)
	}
	if v.InstallStatus != "installed" || v.Suspended {
		t.Errorf("status: %q suspended=%v", v.InstallStatus, v.Suspended)
	}

	u := server(t, p, "Unlimited").Server
	if u.MemoryMB != 2048 || u.SwapMB != -1 || u.BackupLimit != 1 || u.PortLimit != 0 || u.CPUSet != "0-1" {
		t.Errorf("unlimited limits: %+v", u.Limits)
	}
	if u.Startup != "java -jar custom.jar" || u.Image != "ghcr.io/x/java:17" || !u.Suspended {
		t.Errorf("unlimited: %+v", u)
	}
	if !hasNote(p, Changed, "no memory limit; it gets 2048 MB") || !hasNote(p, Changed, "allowed no backups") {
		t.Error("raised limits not reported")
	}
}

func TestPlanBlocksSharedPrimaryPort(t *testing.T) {
	t.Parallel()
	src := fixture()
	src.Allocations[2].ServerID = ni(101)
	src.Servers[1].AllocationID = 12 // 10.0.0.5:25565, the same port as Survival
	p := Build(src, Options{})
	if !p.Blocked() || !hasNote(p, Blocking, "uses port 25565") {
		t.Fatalf("two primaries on one port must block: %+v", p.Notes)
	}
}

func TestPlanBlocksServerWithUnimportableEgg(t *testing.T) {
	t.Parallel()
	src := fixture()
	src.Servers[1].EggID = 4
	p := Build(src, Options{})
	if !hasNote(p, Blocking, `egg "Broken" cannot be imported`) {
		t.Fatalf("notes: %+v", p.Notes)
	}
}

func TestPlanSubusersAndSchedules(t *testing.T) {
	t.Parallel()
	p := Build(fixture(), Options{})
	if len(p.Subusers) != 1 {
		t.Fatalf("subusers: %+v", p.Subusers)
	}
	want := []string{"console.read", "console.send", "files.read", "power"}
	if !slices.Equal(p.Subusers[0].Permissions, want) {
		t.Errorf("permissions %v, want %v", p.Subusers[0].Permissions, want)
	}
	if !hasNote(p, Changed, "loses user.create") || !hasNote(p, Info, "is the server's owner") {
		t.Errorf("notes: %+v", p.Notes)
	}
	if len(p.Schedules) != 1 || p.Schedules[0].Cron != "0 4 * * *" || p.Schedules[0].Tasks[1].OffsetSeconds != 900 {
		t.Errorf("schedules: %+v", p.Schedules)
	}
	if !hasNote(p, Changed, `"Bad cron"`) || !hasNote(p, Changed, "waited 1200 s") {
		t.Errorf("notes: %+v", p.Notes)
	}
}

func TestMapPermissions(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in, out, dropped []string
	}{
		{[]string{"control.start"}, nil, []string{"control.start"}},
		{[]string{"control.start", "control.stop", "control.restart"}, []string{"power"}, nil},
		{[]string{"file.create", "file.update", "file.delete", "file.archive", "file.sftp"}, []string{"files.write"}, nil},
		{[]string{"file.archive"}, nil, []string{"file.archive"}},
		{[]string{"database.view_password", "schedule.read"}, []string{"databases.read"}, []string{"schedule.read"}},
		{[]string{"websocket.connect"}, []string{"console.read"}, nil},
	} {
		out, dropped := mapPermissions(c.in)
		if !slices.Equal(out, c.out) || !slices.Equal(dropped, c.dropped) {
			t.Errorf("%v -> %v / %v, want %v / %v", c.in, out, dropped, c.out, c.dropped)
		}
	}
}

// TestReadRealPterodactyl reads a real Pterodactyl database when
// GAMEPANEL_TEST_PTERO_DSN is set (scripts/ptero-import-smoke.sh seeds one).
func TestReadRealPterodactyl(t *testing.T) {
	dsn := os.Getenv("GAMEPANEL_TEST_PTERO_DSN")
	if dsn == "" {
		t.Skip("GAMEPANEL_TEST_PTERO_DSN not set")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(conn)
	defer db.Close()
	src, err := Read(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Eggs) == 0 || len(src.Nests) == 0 {
		t.Fatalf("no eggs read: %+v", src)
	}
	var key AppKey
	if k := os.Getenv("GAMEPANEL_TEST_PTERO_APP_KEY"); k != "" {
		if key, err = ParseAppKey(k); err != nil {
			t.Fatal(err)
		}
	}
	p := Build(src, Options{AppKey: key})
	for _, e := range p.Eggs {
		if e.Egg.Name == "" || e.Egg.DockerImage == "" {
			t.Errorf("stock egg imported badly: %+v", e.Egg)
		}
	}
	if len(p.Eggs) != len(src.Eggs) {
		t.Errorf("only %d of %d eggs imported: %+v", len(p.Eggs), len(src.Eggs), p.Notes)
	}
	// Every node is in a location (Pterodactyl requires one), and each comes over.
	if len(p.Locations) != len(src.Locations) {
		t.Errorf("%d of %d locations planned", len(p.Locations), len(src.Locations))
	}
	for _, n := range p.Nodes {
		if !slices.ContainsFunc(p.Locations, func(l PlanLocation) bool { return l.PteroID == n.LocationID }) {
			t.Errorf("node %q is in location %d, which is not planned", n.Name, n.LocationID)
		}
	}
}

func TestPlanLocations(t *testing.T) {
	t.Parallel()
	src := &Source{Nests: map[int64]string{},
		Locations: []Location{{ID: 4, Short: "eu", Long: sql.NullString{String: "Frankfurt", Valid: true}}, {ID: 9, Short: "EU "}, {ID: 2, Short: strings.Repeat("x", 70)}},
		Nodes:     []Node{{ID: 1, LocationID: 4, Name: "n1"}},
	}
	p := Build(src, Options{})
	if len(p.Locations) != 3 || p.Locations[0] != (PlanLocation{PteroID: 4, Short: "eu", Long: "Frankfurt"}) {
		t.Fatalf("locations %+v", p.Locations)
	}
	// Codes are unique ignoring case, and at most 60 characters.
	if p.Locations[1].Short != "EU-2" || utf8.RuneCountInString(p.Locations[2].Short) != 60 {
		t.Fatalf("renamed %+v", p.Locations)
	}
	if p.Nodes[0].LocationID != 4 {
		t.Fatalf("node location %d", p.Nodes[0].LocationID)
	}
	in := p.Import()
	if len(in.Locations) != 3 || in.Nodes[0].LocationKey != 4 {
		t.Fatalf("import %+v", in)
	}
}

func TestPlanNodeNamesStayWithinLimit(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("é", 70)
	src := &Source{Nests: map[int64]string{}, Nodes: []Node{{ID: 1, Name: long}, {ID: 2, Name: long}}}
	p := Build(src, Options{})
	a, b := p.Nodes[0].Name, p.Nodes[1].Name
	if a == b || utf8.RuneCountInString(a) > 64 || utf8.RuneCountInString(b) > 64 || !utf8.ValidString(b) || !strings.HasSuffix(b, " (2)") {
		t.Fatalf("names %q / %q", a, b)
	}
}
