package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func sampleImport() Import {
	return Import{
		Users: []ImportUser{
			{Key: 7, Email: "admin@example.com", Admin: true, PasswordHash: laravelHash, TOTPSecret: "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"},
			{Key: 9, Email: "friend@example.com", PasswordHash: "", ExternalID: "cust-9"},
		},
		Locations: []ImportLocation{{Key: 4, Short: "eu", Long: "Frankfurt"}},
		Nodes:     []ImportNode{{Key: 3, LocationKey: 4, Name: "Node 1", PortStart: 25565, PortEnd: 25700, MemoryMB: 8192, SFTPHost: "node1.example.com", SFTPPort: 2022}},
		Eggs: []ImportEgg{{Key: 5, Egg: Egg{
			Name: "Paper", DockerImage: "ghcr.io/x/java:21", Startup: "java -jar {{SERVER_JARFILE}}", Stop: "stop",
			Variables:   []EggVar{{Env: "SERVER_JARFILE", Default: "server.jar"}},
			ConfigFiles: []EggConfigFile{{File: "server.properties", Parser: "properties", Find: []EggFind{{Key: "server-port", Value: "{{server.build.default.port}}"}}}},
		}}},
		Servers: []ImportServer{{
			Key: 12,
			Server: Server{
				UUID: "11111111-1111-1111-1111-111111111111", Name: "Survival", OwnerID: 7, NodeID: 3, EggID: 5, Port: 25565,
				Env: map[string]string{"SERVER_JARFILE": "paper.jar"}, InstallStatus: InstallInstalled, CrashRestart: true, ExternalID: "order-1",
				Limits: Limits{MemoryMB: 4096, DiskMB: 10000, BackupLimit: 3, PortLimit: 2},
			},
			Extra: []ServerPort{{Port: 25566, Notes: "Dynmap"}},
		}},
		Subusers:  []ImportSubuser{{ServerUUID: "11111111-1111-1111-1111-111111111111", UserKey: 9, Permissions: []string{"console.read", "power"}}},
		Schedules: []Schedule{{ServerUUID: "11111111-1111-1111-1111-111111111111", Name: "Restart", Cron: "0 4 * * *", Enabled: true, Tasks: []Task{{Seq: 2, Action: "power", Payload: "restart"}, {Seq: 1, Action: "command", Payload: "say restarting"}}}},
		DBHosts:   []ImportDBHost{{Key: 1, Host: DatabaseHost{Name: "db1", Host: "127.0.0.1", Port: 3306, Username: "root", NodeID: 3}, Password: "hostpw"}},
		Databases: []ImportDatabase{{ServerUUID: "11111111-1111-1111-1111-111111111111", HostKey: 1, Name: "s1_main", Username: "u1_abc", Remote: "%", Password: "dbpw"}},
	}
}

func TestImportPanel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	if err := s.SetSecretKey(make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	// An admin made with create-admin before the import stays.
	if _, err := s.CreateUser(ctx, "first@example.com", "a-long-enough-password", true); err != nil {
		t.Fatal(err)
	}
	res, err := s.ImportPanel(ctx, sampleImport())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NodeTokens[3]) < 32 {
		t.Fatalf("node token: %+v", res)
	}
	node, err := s.NodeByToken(ctx, res.NodeTokens[3])
	if err != nil || node.Name != "Node 1" || node.SFTPHost != "node1.example.com" || node.MemoryMB != 8192 {
		t.Fatalf("node %+v %v", node, err)
	}
	// Locations, nodes, eggs and servers keep Pterodactyl's ids, so a billing
	// module's products still point at them.
	if node.ID != 3 || node.LocationID != 4 {
		t.Fatalf("node ids: %+v", node)
	}
	if l, err := s.GetLocation(ctx, 4); err != nil || l.Short != "eu" || l.Long != "Frankfurt" || l.Nodes != 1 {
		t.Fatalf("location %+v %v", l, err)
	}
	if e, err := s.GetEgg(ctx, 5); err != nil || e.Name != "Paper" {
		t.Fatalf("egg 5: %+v %v", e, err)
	}
	if srv, err := s.ServerByNumber(ctx, 12); err != nil || srv.Name != "Survival" {
		t.Fatalf("server 12: %+v %v", srv, err)
	}
	// The next of each continues after the imported ids.
	if n, _, err := s.CreateNode(ctx, "Node 2", 30000, 30010); err != nil || n.ID != 4 {
		t.Fatalf("next node %+v %v", n, err)
	}
	next, err := s.CreateServer(ctx, Server{UUID: "22222222-2222-2222-2222-222222222222", Name: "Next", OwnerID: u0(t, s), NodeID: 3, EggID: 5, Limits: Limits{MemoryMB: 512}})
	if err != nil {
		t.Fatal(err)
	}
	if m, err := s.GetServerMeta(ctx, next.UUID); err != nil || m.ID != 13 {
		t.Fatalf("next server number %+v %v", m, err)
	}

	// The Pterodactyl password works, with its second factor.
	u, err := s.Login(ctx, "admin@example.com", "password")
	if err != nil || !u.IsAdmin || !u.TOTPEnabled {
		t.Fatalf("login %+v %v", u, err)
	}
	secret, _ := b32.DecodeString("JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP")
	if _, err := s.VerifySecondFactor(ctx, u.ID, hotp(secret, uint64(time.Now().Unix()/30))); err != nil {
		t.Fatalf("imported TOTP secret does not verify: %v", err)
	}
	var stored string
	s.DB.QueryRow(`SELECT totp_secret FROM users WHERE id=?`, u.ID).Scan(&stored)
	if !strings.HasPrefix(stored, "gcm:") {
		t.Fatalf("TOTP secret not sealed: %q", stored)
	}
	// No usable hash: no password signs in until a reset link sets one.
	for _, pw := range []string{"", "!", "password"} {
		if _, err := s.Login(ctx, "friend@example.com", pw); err == nil {
			t.Fatalf("signed in without a password with %q", pw)
		}
	}

	srv, err := s.GetServer(ctx, "11111111-1111-1111-1111-111111111111")
	if err != nil || srv.OwnerID != u.ID || srv.NodeID != node.ID || srv.InstallStatus != InstallInstalled || srv.Env["SERVER_JARFILE"] != "paper.jar" || srv.ExternalID != "order-1" || !srv.CrashRestart || srv.PortLimit != 2 {
		t.Fatalf("server %+v %v", srv, err)
	}
	egg, err := s.GetEgg(ctx, srv.EggID)
	if err != nil || len(egg.ConfigFiles) != 1 || egg.ConfigFiles[0].Find[0].Key != "server-port" {
		t.Fatalf("egg %+v %v", egg, err)
	}
	ports, _ := s.ListPorts(ctx, srv.UUID)
	if len(ports) != 2 || ports[1].Port != 25566 || ports[1].Notes != "Dynmap" {
		t.Fatalf("ports %+v", ports)
	}
	friend, _ := s.UserByEmail(ctx, "friend@example.com")
	if perms, ok := s.SubuserPermissions(ctx, srv.UUID, friend.ID); !ok || strings.Join(perms, ",") != "console.read,power" {
		t.Fatalf("subuser %v %v", perms, ok)
	}
	scheds, _ := s.ListSchedules(ctx, srv.UUID)
	if len(scheds) != 1 || scheds[0].NextRun == 0 || len(scheds[0].Tasks) != 2 || scheds[0].Tasks[0].Payload != "say restarting" {
		t.Fatalf("schedules %+v", scheds)
	}
	h, err := s.HostWithSecret(ctx, 1)
	if err != nil || h.Password != "hostpw" || h.NodeID != node.ID {
		t.Fatalf("host %+v %v", h, err)
	}
	dbs, _ := s.ListServerDatabases(ctx, srv.UUID)
	if len(dbs) != 1 {
		t.Fatalf("databases %+v", dbs)
	}
	if pw, err := s.DatabasePassword(ctx, dbs[0].ID); err != nil || pw != "dbpw" {
		t.Fatalf("database password %q %v", pw, err)
	}

	// A second import is refused: the panel is no longer empty.
	var nonEmpty *ImportNotEmptyError
	if _, err := s.ImportPanel(ctx, sampleImport()); !errors.As(err, &nonEmpty) {
		t.Fatalf("second import: %v", err)
	}
}

// u0 is the id of the first account, made before the import.
func u0(t *testing.T, s *Store) int64 {
	t.Helper()
	var id int64
	if err := s.DB.QueryRow(`SELECT id FROM users WHERE email='first@example.com'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestImportRefusesTakenAccounts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	if _, err := s.CreateUser(ctx, "admin@example.com", "a-long-enough-password", true); err != nil {
		t.Fatal(err)
	}
	_, err := s.ImportPanel(ctx, sampleImport())
	if err == nil || !strings.Contains(err.Error(), "an account for admin@example.com") {
		t.Fatalf("got %v", err)
	}
}

// Everything or nothing: a failure part way leaves the panel as it was.
func TestImportRollsBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	in := sampleImport()
	in.Databases[0].HostKey = 99 // not imported
	if _, err := s.ImportPanel(ctx, in); err == nil {
		t.Fatal("import with a dangling reference succeeded")
	}
	for _, table := range []string{"users", "nodes", "eggs", "servers", "server_ports", "schedules", "database_hosts"} {
		n, err := count(ctx, s.DB, `SELECT COUNT(*) FROM `+table)
		if err != nil || n != 0 {
			t.Errorf("%s has %d rows after a failed import (%v)", table, n, err)
		}
	}
	// And it can then be imported for real.
	if _, err := s.ImportPanel(ctx, sampleImport()); err != nil {
		t.Fatal(err)
	}
}
