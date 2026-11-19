package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func TestLimitsNormalize(t *testing.T) {
	t.Parallel()
	ok := Limits{MemoryMB: 512}
	if err := ok.Normalize(); err != nil || ok.PidsLimit != 1024 || ok.BackupLimit != 20 {
		t.Fatalf("defaults not applied: %+v %v", ok, err)
	}
	for name, mut := range map[string]func(*Limits){
		"memory too small": func(l *Limits) { l.MemoryMB = 8 },
		"swap below -1":    func(l *Limits) { l.SwapMB = -2 },
		"negative cpu":     func(l *Limits) { l.CPUPct = -1 },
		"cpuset junk":      func(l *Limits) { l.CPUSet = "0-3; rm -rf" },
		"pids too low":     func(l *Limits) { l.PidsLimit = 3 },
		"io weight low":    func(l *Limits) { l.IOWeight = 5 },
		"negative disk":    func(l *Limits) { l.DiskMB = -1 },
		"backups too many": func(l *Limits) { l.BackupLimit = 500 },
	} {
		l := Limits{MemoryMB: 512}
		mut(&l)
		if l.Normalize() == nil {
			t.Errorf("%s accepted", name)
		}
	}
	for _, good := range []string{"0", "0-3", "0,2", "1-2,4"} {
		l := Limits{MemoryMB: 512, CPUSet: good}
		if err := l.Normalize(); err != nil {
			t.Errorf("cpuset %q rejected: %v", good, err)
		}
	}
	if l := (Limits{MemoryMB: 512, SwapMB: -1}); l.Normalize() != nil {
		t.Error("unlimited swap rejected")
	}
}

func TestNodeCapacityAccounting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := Open(filepath.Join(t.TempDir(), "p.db"))
	defer s.Close()
	u, _ := s.CreateUser(ctx, "a@example.com", "correct-horse-battery", true)
	n, _, _ := s.CreateNode(ctx, "n", 25565, 25600)
	e, _ := s.CreateEgg(ctx, Egg{Name: "e", DockerImage: "i", Startup: "x", Stop: "^C"})
	mk := func(i int, l Limits) (Server, error) {
		return s.CreateServer(ctx, Server{UUID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i), Name: "s", OwnerID: u.ID, NodeID: n.ID, EggID: e.ID, Limits: l})
	}

	// untracked node: anything goes, including unlimited disk
	if _, err := mk(1, Limits{MemoryMB: 4096}); err != nil {
		t.Fatal(err)
	}
	s.SetNodeCapacity(ctx, n.ID, 8192, 10240, 0) // 4096 already allocated

	var ce ErrCapacity
	if _, err := mk(2, Limits{MemoryMB: 4097, DiskMB: 100}); !errors.As(err, &ce) {
		t.Fatalf("over memory accepted: %v", err)
	}
	if _, err := mk(2, Limits{MemoryMB: 1024}); !errors.As(err, &ce) {
		t.Fatalf("server without a disk limit accepted on a disk-tracked node: %v", err)
	}
	b, err := mk(2, Limits{MemoryMB: 4096, DiskMB: 10240})
	if err != nil {
		t.Fatalf("exact fit refused: %v", err)
	}
	if _, err := mk(3, Limits{MemoryMB: 64, DiskMB: 1}); !errors.As(err, &ce) {
		t.Fatalf("node is full but another server was accepted: %v", err)
	}

	// resizing is checked against everyone else, not against itself
	if err := s.UpdateLimits(ctx, b.UUID, Limits{MemoryMB: 4096, DiskMB: 5000}); err != nil {
		t.Fatalf("shrinking refused: %v", err)
	}
	if err := s.UpdateLimits(ctx, b.UUID, Limits{MemoryMB: 4097, DiskMB: 5000}); !errors.As(err, &ce) {
		t.Fatalf("growing past capacity accepted: %v", err)
	}
	// over-allocation lets the sum exceed capacity by a percentage
	s.SetNodeCapacity(ctx, n.ID, 8192, 10240, 50)
	if err := s.UpdateLimits(ctx, b.UUID, Limits{MemoryMB: 8000, DiskMB: 5000}); err != nil {
		t.Fatalf("50%% over-allocation refused: %v", err)
	}

	nodes, _ := s.ListNodes(ctx)
	if nodes[0].AllocatedMemory != 4096+8000 || nodes[0].MemoryMB != 8192 {
		t.Fatalf("node accounting: %+v", nodes[0])
	}
	if err := s.SetNodeCapacity(ctx, n.ID, -1, 0, 0); err == nil {
		t.Fatal("negative capacity accepted")
	}
}

func TestBackupLimitIsPerServer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, srv := testServer(t)
	if err := s.UpdateLimits(ctx, srv, Limits{MemoryMB: 128, BackupLimit: 2}); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
		if _, err := s.BeginBackup(ctx, Backup{UUID: id, ServerUUID: srv, Name: "b"}, false); err != nil {
			t.Fatal(err)
		}
		s.FinishBackup(ctx, id, 1, "x", "")
	}
	if _, err := s.BeginBackup(ctx, Backup{UUID: "00000000-0000-4000-8000-000000000009", ServerUUID: srv, Name: "c"}, false); !errors.Is(err, ErrBackupLimit) {
		t.Fatalf("third backup past a limit of 2: %v", err)
	}
}
