package store

import (
	"context"
	"database/sql"
	"errors"
)

// ServerPort is one port a server has on its node.
type ServerPort struct {
	Port    int    `json:"port"`
	Notes   string `json:"notes"`
	Primary bool   `json:"primary" doc:"the port the game listens on (SERVER_PORT)"`
}

// ErrPortLimit means the server already has as many extra ports as it may.
var ErrPortLimit = userErr("this server cannot have more ports; ask an administrator to raise its limit")

// pickPort returns want if it is free and in the node's range, or the first
// free port in the range when want is 0.
func pickPort(ctx context.Context, q querier, nodeID int64, want int) (int, error) {
	var ps, pe int
	if err := q.QueryRowContext(ctx, `SELECT port_start,port_end FROM nodes WHERE id=?`, nodeID).Scan(&ps, &pe); err != nil {
		return 0, nf(err)
	}
	if want != 0 && (want < ps || want > pe) {
		return 0, userErrf("port must be within the node range %d-%d", ps, pe)
	}
	ports, err := queryAll(ctx, q, scanInt, `SELECT port FROM server_ports WHERE node_id=?`, nodeID)
	if err != nil {
		return 0, err
	}
	used := make(map[int]bool, len(ports))
	for _, p := range ports {
		used[p] = true
	}
	if want != 0 {
		if used[want] {
			return 0, userErrf("port %d is already in use on this node", want)
		}
		return want, nil
	}
	for p := ps; p <= pe; p++ {
		if !used[p] {
			return p, nil
		}
	}
	return 0, userErr("no free ports on this node")
}

// ListPorts returns a server's ports, the primary one first.
func (s *Store) ListPorts(ctx context.Context, uuid string) ([]ServerPort, error) {
	return queryAll(ctx, s.DB, func(sc rowScanner) (ServerPort, error) {
		var p ServerPort
		err := sc.Scan(&p.Port, &p.Notes, &p.Primary)
		return p, err
	}, `SELECT sp.port, sp.notes, sp.port = sv.port FROM server_ports sp JOIN servers sv ON sv.uuid = sp.server_uuid
		WHERE sp.server_uuid=? ORDER BY sp.port = sv.port DESC, sp.port`, uuid)
}

// AddPort gives a server another port: want, or any free one when want is 0.
// Unless override is set (admins), the server's port limit applies.
func (s *Store) AddPort(ctx context.Context, uuid string, want int, notes string, override bool) (ServerPort, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return ServerPort{}, err
	}
	defer tx.Rollback()
	var node, limit, have int64
	if err := tx.QueryRowContext(ctx, `SELECT node_id, port_limit, (SELECT COUNT(*) FROM server_ports WHERE server_uuid=servers.uuid) FROM servers WHERE uuid=?`, uuid).Scan(&node, &limit, &have); err != nil {
		return ServerPort{}, nf(err)
	}
	if !override && have > limit {
		return ServerPort{}, ErrPortLimit
	}
	port, err := pickPort(ctx, tx, node, want)
	if err != nil {
		return ServerPort{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO server_ports(node_id,port,server_uuid,notes,created_at) VALUES(?,?,?,?,?)`, node, port, uuid, notes, now()); err != nil {
		return ServerPort{}, err
	}
	return ServerPort{Port: port, Notes: notes}, tx.Commit()
}

// ErrPrimaryPort means the primary port cannot be removed.
var ErrPrimaryPort = userErr("the primary port cannot be removed; make another port primary first")

// RemovePort takes a port away from a server.
func (s *Store) RemovePort(ctx context.Context, uuid string, port int) error {
	var primary int
	if err := s.DB.QueryRowContext(ctx, `SELECT port FROM servers WHERE uuid=?`, uuid).Scan(&primary); err != nil {
		return nf(err)
	}
	if port == primary {
		return ErrPrimaryPort
	}
	res, err := s.exec(ctx, `DELETE FROM server_ports WHERE server_uuid=? AND port=?`, uuid, port)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdatePort sets a port's notes and, if primary, makes it the server's
// primary port.
func (s *Store) UpdatePort(ctx context.Context, uuid string, port int, notes *string, primary bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM server_ports WHERE server_uuid=? AND port=?`, uuid, port).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if notes != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE server_ports SET notes=? WHERE server_uuid=? AND port=?`, *notes, uuid, port); err != nil {
			return err
		}
	}
	if primary {
		if _, err := tx.ExecContext(ctx, `UPDATE servers SET port=? WHERE uuid=?`, port, uuid); err != nil {
			return err
		}
	}
	return tx.Commit()
}
