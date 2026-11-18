package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSigningInClearsExpiredSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := testServer(t)
	u, _ := s.Login(ctx, "a@example.com", "correct-horse-battery")
	s.exec(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES('stale',?,?)`, u.ID, time.Now().Add(-time.Hour).Unix())
	if _, err := s.NewSession(ctx, u.ID, SessionMeta{}); err != nil {
		t.Fatal(err)
	}
	var n int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE token_hash='stale'`).Scan(&n)
	if n != 0 {
		t.Fatal("an expired session was kept")
	}
}

func TestOneRestoreAtATimeAndStaleRestoresFail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, srv := testServer(t)
	for _, id := range []string{"b1", "b2"} {
		if _, err := s.BeginBackup(ctx, Backup{UUID: id, ServerUUID: srv, Name: id}, false); err != nil {
			t.Fatal(err)
		}
		s.FinishBackup(ctx, id, 1, "x", "")
	}
	if err := s.BeginRestore(ctx, srv, "b1"); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginRestore(ctx, srv, "b2"); !errors.Is(err, ErrRestoreRunning) {
		t.Fatalf("second restore on the same server: %v", err)
	}
	if err := s.BeginRestore(ctx, srv, "nope"); !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrRestoreRunning) {
		t.Fatalf("unknown backup: %v", err)
	}
	// A panel restart while restoring leaves no restore "running" forever.
	if err := s.FailStaleBackups(ctx); err != nil {
		t.Fatal(err)
	}
	b, _ := s.GetBackup(ctx, srv, "b1")
	if b.RestoreStatus != BackupFailed || b.RestoreError == "" || b.Status != BackupOK {
		t.Fatalf("after restart: %+v", b)
	}
	if err := s.BeginRestore(ctx, srv, "b2"); err != nil {
		t.Fatal(err)
	}
	s.FinishRestore(ctx, "b2", "")
	if b, _ := s.GetBackup(ctx, srv, "b2"); b.RestoreStatus != BackupOK || b.RestoredAt == 0 {
		t.Fatalf("finished restore: %+v", b)
	}
}

func TestDeleteNodeSaysWhy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := testServer(t)
	nodes, _ := s.ListNodes(ctx)
	if err := s.DeleteNode(ctx, nodes[0].ID); !errors.Is(err, ErrNodeInUse) {
		t.Fatalf("node with servers: %v", err)
	}
	if err := s.DeleteNode(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown node: %v", err)
	}
	empty, _, _ := s.CreateNode(ctx, "empty", 30000, 30001)
	if err := s.DeleteNode(ctx, empty.ID); err != nil {
		t.Fatal(err)
	}
}

func TestChangePasswordReportsAWrongCurrentPassword(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := testServer(t)
	u, _ := s.Login(ctx, "a@example.com", "correct-horse-battery")
	if err := s.ChangePassword(ctx, u.ID, "wrong-password-here", "a-new-long-password", ""); !errors.Is(err, ErrBadPassword) {
		t.Fatalf("got %v", err)
	}
}

func TestSchedulesRememberTheirAuthor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, srv := testServer(t)
	u, _ := s.Login(ctx, "a@example.com", "correct-horse-battery")
	v, err := s.SaveSchedule(ctx, Schedule{ServerUUID: srv, Name: "x", Cron: "0 0 * * *", CreatedBy: u.ID})
	if err != nil || v.CreatedBy != u.ID {
		t.Fatalf("created_by %d (%v)", v.CreatedBy, err)
	}
	// Schedules from before authors were recorded read back as 0 (run as owner).
	s.exec(ctx, `UPDATE schedules SET created_by=NULL WHERE id=?`, v.ID)
	if v, _ = s.GetSchedule(ctx, srv, v.ID); v.CreatedBy != 0 {
		t.Fatalf("legacy schedule author: %d", v.CreatedBy)
	}
}

func TestSubuserServersInOneQuery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, srv := testServer(t)
	b, _ := s.CreateUser(ctx, "b@example.com", "correct-horse-battery", false)
	if _, err := s.AddSubuser(ctx, srv, "b@example.com", []string{"files.read", "power"}); err != nil {
		t.Fatal(err)
	}
	m, err := s.SubuserServers(ctx, b.ID)
	if err != nil || len(m) != 1 || len(m[srv]) != 2 {
		t.Fatalf("got %v, %v", m, err)
	}
}
