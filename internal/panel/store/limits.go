package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
)

// Limits are the per-server resource limits. They are enforced by the node:
// memory, swap, CPU, cpuset, pids and IO weight by the container engine, disk
// by the daemon (see internal/daemon/disk.go).
type Limits struct {
	MemoryMB      int64  `json:"memory_mb" minimum:"64" maximum:"1048576" doc:"hard memory limit"`
	SwapMB        int64  `json:"swap_mb" minimum:"-1" maximum:"1048576" doc:"extra swap on top of memory: 0 = none, -1 = unlimited"`
	CPUPct        int64  `json:"cpu_pct" minimum:"0" maximum:"6400" doc:"100 = one core, 0 = unlimited"`
	CPUSet        string `json:"cpuset" maxLength:"64" doc:"pin to cores, e.g. 0-3 or 0,2; empty = any"`
	PidsLimit     int64  `json:"pids_limit" minimum:"16" maximum:"65536" doc:"max processes and threads"`
	IOWeight      int64  `json:"io_weight" minimum:"0" maximum:"1000" doc:"relative disk IO priority 10-1000, 0 = default"`
	DiskMB        int64  `json:"disk_mb" minimum:"0" maximum:"104857600" doc:"disk quota, 0 = unlimited"`
	BackupLimit   int64  `json:"backup_limit" minimum:"1" maximum:"100" doc:"how many backups this server may keep"`
	PortLimit     int64  `json:"port_limit" required:"false" minimum:"0" maximum:"100" doc:"ports the owner may add beyond the primary one (default 0)"`
	DatabaseLimit int64  `json:"database_limit" required:"false" minimum:"0" maximum:"100" doc:"databases the owner may create (default 0)"`
}

// DefaultLimits are used for fields a request leaves out.
func DefaultLimits() Limits {
	return Limits{MemoryMB: 1024, PidsLimit: 1024, BackupLimit: 20}
}

var cpusetRe = regexp.MustCompile(`^[0-9]+([-,][0-9]+)*$`)

// Normalize fills defaults for zero-valued fields that cannot be zero and
// validates ranges (the HTTP schema checks the same bounds; this guards every
// other caller).
func (l *Limits) Normalize() error {
	if l.PidsLimit == 0 {
		l.PidsLimit = 1024
	}
	if l.BackupLimit == 0 {
		l.BackupLimit = 20
	}
	switch {
	case l.MemoryMB < 64 || l.MemoryMB > 1<<20:
		return userErr("memory must be 64 to 1048576 MB")
	case l.SwapMB < -1 || l.SwapMB > 1<<20:
		return userErr("swap must be -1 (unlimited), 0 (none) or up to 1048576 MB")
	case l.CPUPct < 0 || l.CPUPct > 6400:
		return userErr("CPU limit must be 0 to 6400 percent")
	case l.CPUSet != "" && !cpusetRe.MatchString(l.CPUSet):
		return userErr("cpuset must look like 0-3 or 0,2")
	case l.PidsLimit < 16 || l.PidsLimit > 65536:
		return userErr("process limit must be 16 to 65536")
	case l.IOWeight != 0 && (l.IOWeight < 10 || l.IOWeight > 1000):
		return userErr("IO weight must be 0 (default) or 10 to 1000")
	case l.DiskMB < 0 || l.DiskMB > 100<<20:
		return userErr("disk limit must be 0 (unlimited) or a positive number of MB")
	case l.BackupLimit < 1 || l.BackupLimit > 100:
		return userErr("backup limit must be 1 to 100")
	case l.PortLimit < 0 || l.PortLimit > 100:
		return userErr("extra ports must be 0 to 100")
	case l.DatabaseLimit < 0 || l.DatabaseLimit > 100:
		return userErr("databases must be 0 to 100")
	}
	return nil
}

// ErrCapacity means a node would be over-allocated.
type ErrCapacity struct{ Msg string }

func (e ErrCapacity) Error() string { return e.Msg }

// checkCapacity verifies that adding (or resizing, via exclude) a server with
// limits l keeps the node within its configured capacity. A capacity of 0
// means "not tracked". On a node that tracks disk, servers must set a disk
// limit, otherwise one unlimited server defeats the accounting.
func checkCapacity(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, nodeID int64, exclude string, l Limits) error {
	var memCap, diskCap, over int64
	if err := q.QueryRowContext(ctx, `SELECT memory_mb,disk_mb,overallocate_pct FROM nodes WHERE id=?`, nodeID).Scan(&memCap, &diskCap, &over); err != nil {
		return nf(err)
	}
	var memUsed, diskUsed int64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(memory_mb),0),COALESCE(SUM(disk_mb),0) FROM servers WHERE node_id=? AND uuid<>?`, nodeID, exclude).Scan(&memUsed, &diskUsed); err != nil {
		return err
	}
	if memCap > 0 {
		if allowed := memCap * (100 + over) / 100; memUsed+l.MemoryMB > allowed {
			return ErrCapacity{fmt.Sprintf("this node has %d of %d MB memory left to allocate; %d MB requested", max(allowed-memUsed, 0), allowed, l.MemoryMB)}
		}
	}
	if diskCap > 0 {
		if l.DiskMB == 0 {
			return ErrCapacity{"this node tracks disk capacity, so the server needs a disk limit"}
		}
		if allowed := diskCap * (100 + over) / 100; diskUsed+l.DiskMB > allowed {
			return ErrCapacity{fmt.Sprintf("this node has %d of %d MB disk left to allocate; %d MB requested", max(allowed-diskUsed, 0), allowed, l.DiskMB)}
		}
	}
	return nil
}

// CheckLimits validates l and the node's capacity without saving anything.
func (s *Store) CheckLimits(ctx context.Context, uuid string, l Limits) error {
	if err := l.Normalize(); err != nil {
		return err
	}
	srv, err := s.GetServer(ctx, uuid)
	if err != nil {
		return err
	}
	return checkCapacity(ctx, s.DB, srv.NodeID, uuid, l)
}

// UpdateLimits changes a server's limits after checking node capacity.
func (s *Store) UpdateLimits(ctx context.Context, uuid string, l Limits) error {
	if err := l.Normalize(); err != nil {
		return err
	}
	srv, err := s.GetServer(ctx, uuid)
	if err != nil {
		return err
	}
	if err := checkCapacity(ctx, s.DB, srv.NodeID, uuid, l); err != nil {
		return err
	}
	_, err = s.exec(ctx, `UPDATE servers SET memory_mb=?,swap_mb=?,cpu_pct=?,cpuset=?,pids_limit=?,io_weight=?,disk_mb=?,backup_limit=?,port_limit=?,database_limit=? WHERE uuid=?`,
		l.MemoryMB, l.SwapMB, l.CPUPct, l.CPUSet, l.PidsLimit, l.IOWeight, l.DiskMB, l.BackupLimit, l.PortLimit, l.DatabaseLimit, uuid)
	return err
}

// SetNodeCapacity records what a node can hold (0 = not tracked).
func (s *Store) SetNodeCapacity(ctx context.Context, id, memMB, diskMB, overPct int64) error {
	if memMB < 0 || diskMB < 0 || overPct < 0 || overPct > 1000 {
		return userErr("capacity must not be negative; over-allocation 0 to 1000 percent")
	}
	res, err := s.exec(ctx, `UPDATE nodes SET memory_mb=?,disk_mb=?,overallocate_pct=? WHERE id=?`, memMB, diskMB, overPct, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
