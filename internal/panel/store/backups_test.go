package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func testServer(t *testing.T) (*Store, string) {
	t.Helper()
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	u, _ := s.CreateUser(ctx, "a@example.com", "correct-horse-battery", true)
	n, _, _ := s.CreateNode(ctx, "n", 25565, 25600)
	e, _ := s.CreateEgg(ctx, Egg{Name: "e", DockerImage: "i", Startup: "x", Stop: "^C"})
	sv, err := s.CreateServer(ctx, Server{UUID: "11111111-1111-4111-8111-111111111111", Name: "s", OwnerID: u.ID, NodeID: n.ID, EggID: e.ID, Limits: Limits{MemoryMB: 128}})
	if err != nil {
		t.Fatal(err)
	}
	return s, sv.UUID
}

func TestBackupCapRotationAndLocks(t *testing.T) {
	ctx := context.Background()
	s, srv := testServer(t)
	mk := func(i int, rotate bool) ([]string, error) {
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
		ev, err := s.BeginBackup(ctx, Backup{UUID: id, ServerUUID: srv, Name: id}, rotate)
		if err == nil {
			s.FinishBackup(ctx, id, 1, "x", "")
			s.exec(ctx, `UPDATE backups SET created_at=? WHERE uuid=?`, 1000+i, id) // deterministic age
		}
		var ids []string
		for _, b := range ev {
			if b.Disk != "local" {
				t.Fatalf("evicted backup disk %q", b.Disk)
			}
			ids = append(ids, b.UUID)
		}
		return ids, err
	}
	for i := 0; i < DefaultBackupLimit; i++ {
		if _, err := mk(i, false); err != nil {
			t.Fatalf("backup %d: %v", i, err)
		}
	}
	if _, err := mk(99, false); err != ErrBackupLimit {
		t.Fatalf("manual backup past the cap must be refused, got %v", err)
	}
	// lock the oldest; rotation must evict the oldest *unlocked* one
	s.SetBackupLocked(ctx, srv, "00000000-0000-4000-8000-000000000000", true)
	ev, err := mk(100, true)
	if err != nil || len(ev) != 1 || ev[0] != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("rotation evicted %v (%v)", ev, err)
	}
	// everything locked -> even rotation is refused
	l, _ := s.ListBackups(ctx, srv)
	for _, b := range l {
		s.SetBackupLocked(ctx, srv, b.UUID, true)
	}
	if _, err := mk(101, true); err != ErrBackupLimit {
		t.Fatalf("all locked: got %v", err)
	}
}

func TestOnlyOneRunningBackup(t *testing.T) {
	ctx := context.Background()
	s, srv := testServer(t)
	if _, err := s.BeginBackup(ctx, Backup{UUID: "00000000-0000-4000-8000-000000000001", ServerUUID: srv, Name: "a"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBackup(ctx, Backup{UUID: "00000000-0000-4000-8000-000000000002", ServerUUID: srv, Name: "b"}, false); err == nil {
		t.Fatal("second concurrent backup allowed")
	}
	s.FailStaleBackups(ctx) // a panel restart must not leave a server stuck "running"
	if _, err := s.BeginBackup(ctx, Backup{UUID: "00000000-0000-4000-8000-000000000003", ServerUUID: srv, Name: "c"}, false); err != nil {
		t.Fatalf("stuck after restart: %v", err)
	}
}

func TestNextRunAndClaimDue(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 15, 10, 7, 30, 0, time.UTC)
	n, err := NextRun("*/15 * * * *", at)
	if err != nil || !n.Equal(time.Date(2026, 11, 15, 10, 15, 0, 0, time.UTC)) {
		t.Fatalf("next = %v (%v)", n, err)
	}
	for _, bad := range []string{"", "* * * *", "@daily", "61 * * * *", "* * * * * *", "a b c d e"} {
		if _, err := NextRun(bad, at); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}

	s, srv := testServer(t)
	v, err := s.SaveSchedule(ctx, Schedule{ServerUUID: srv, Name: "x", Cron: "*/5 * * * *", Enabled: true,
		Tasks: []Task{{Action: ActionCommand, Payload: "save-all"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ClaimDue(ctx, time.Now()); len(got) != 0 {
		t.Fatal("claimed a schedule that is not due")
	}
	s.exec(ctx, `UPDATE schedules SET next_run=? WHERE id=?`, time.Now().Add(-time.Hour).Unix(), v.ID)
	// two racing claimers: exactly one wins, and the missed slots collapse into one run
	a, _ := s.ClaimDue(ctx, time.Now())
	b, _ := s.ClaimDue(ctx, time.Now())
	if len(a)+len(b) != 1 {
		t.Fatalf("claimed %d+%d times, want exactly 1", len(a), len(b))
	}
	if len(a) == 1 && (len(a[0].Tasks) != 1 || a[0].Tasks[0].Payload != "save-all") {
		t.Fatalf("tasks not loaded: %+v", a[0].Tasks)
	}
	got, _ := s.GetSchedule(ctx, srv, v.ID)
	if got.NextRun <= time.Now().Unix() {
		t.Fatal("next_run not advanced")
	}
	// disabled schedules never fire
	s.SaveSchedule(ctx, Schedule{ID: v.ID, ServerUUID: srv, Name: "x", Cron: "*/5 * * * *", Enabled: false})
	s.exec(ctx, `UPDATE schedules SET next_run=1 WHERE id=?`, v.ID)
	if got, _ := s.ClaimDue(ctx, time.Now()); len(got) != 0 {
		t.Fatal("disabled schedule fired")
	}
}

func TestDeletingServerCascades(t *testing.T) {
	ctx := context.Background()
	s, srv := testServer(t)
	s.BeginBackup(ctx, Backup{UUID: "00000000-0000-4000-8000-000000000001", ServerUUID: srv, Name: "a"}, false)
	s.SaveSchedule(ctx, Schedule{ServerUUID: srv, Name: "x", Cron: "* * * * *", Enabled: true, Tasks: []Task{{Action: ActionPower, Payload: "restart"}}})
	s.DeleteServer(ctx, srv)
	var n int
	s.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM backups)+(SELECT COUNT(*) FROM schedules)+(SELECT COUNT(*) FROM schedule_tasks)`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d orphan rows after deleting the server", n)
	}
}
