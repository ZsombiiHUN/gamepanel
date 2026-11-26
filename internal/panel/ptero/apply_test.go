package ptero

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.SetSecretKey(make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	return st
}

// The fixture's plan applies as a whole, and everything points at the right
// new records.
func TestApplyFixture(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	k, _ := ParseAppKey(testAppKey)
	src := fixture()
	src.Users[0].UseTOTP = false // its secret is not a real payload
	src.DBHosts, src.Databases = nil, nil
	src.APIKeys = []APIKey{{UserID: 2, Type: keyAccount, Identifier: ns("ptlc_alicebot001"), Token: laravelEncrypt(t, k, apiKeySecret), Memo: ns("bot")}}
	p := Build(src, Options{AppKey: k})
	st := openStore(t)
	res, err := st.ImportPanel(ctx, p.Import())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NodeTokens) != 2 {
		t.Fatalf("tokens: %+v", res.NodeTokens)
	}
	alice, err := st.UserByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := st.UserByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := st.GetServer(ctx, "11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if srv.OwnerID != alice.ID || srv.NodeID != res.NodeIDs[1] || srv.Port != 25565 || srv.Env["MC_VERSION"] != "1.21.4" || !srv.CrashRestart {
		t.Errorf("Survival: %+v", srv)
	}
	ports, _ := st.ListPorts(ctx, srv.UUID)
	if len(ports) != 2 || ports[1].Port != 25566 {
		t.Errorf("ports: %+v", ports)
	}
	if perms, ok := st.SubuserPermissions(ctx, srv.UUID, admin.ID); !ok || len(perms) == 0 {
		t.Errorf("admin as subuser of Survival: %v %v", perms, ok)
	}
	unl, err := st.GetServer(ctx, "22222222-2222-2222-2222-222222222222")
	if err != nil || !unl.Suspended || unl.MemoryMB != DefaultUnlimitedMemoryMB || unl.Startup != "java -jar custom.jar" {
		t.Errorf("Unlimited: %+v %v", unl, err)
	}
	eggs, _ := st.ListEggs(ctx)
	if len(eggs) != len(p.Eggs) {
		t.Errorf("%d eggs, want %d", len(eggs), len(p.Eggs))
	}
	// The Pterodactyl key signs in as its owner, and the backups are there.
	if u, err := st.UserByAPIKey(ctx, "ptlc_alicebot001"+apiKeySecret); err != nil || u.ID != alice.ID {
		t.Errorf("imported key: %+v %v", u, err)
	}
	if keys, _ := st.ListAPIKeys(ctx, alice.ID); len(keys) != 1 || keys[0].Prefix != "ptlc_alicebot001" || keys[0].Name != "bot" {
		t.Errorf("keys: %+v", keys)
	}
	backups, _ := st.ListBackups(ctx, srv.UUID)
	if len(backups) != 2 || backups[0].Status != store.BackupOK {
		t.Errorf("backups: %+v", backups)
	}
	if ids, _ := st.BackupsOnDisk(ctx, srv.UUID, "local"); len(ids) != 1 || ids[0] != "aaaaaaaa-0000-4000-8000-000000000001" {
		t.Errorf("local backups: %v", ids)
	}
	scheds, _ := st.ListSchedules(ctx, srv.UUID)
	if len(scheds) != 1 || len(scheds[0].Tasks) != 2 || scheds[0].Tasks[1].OffsetSeconds != 900 {
		t.Errorf("schedules: %+v", scheds)
	}
}
