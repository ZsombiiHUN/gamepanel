package store

import (
	"context"
	"fmt"
)

// Numeric server ids, for Pterodactyl's application API (see migration
// 00120). Servers are keyed by uuid everywhere else.

// ServerMeta is what the numeric API needs beyond Server.
type ServerMeta struct {
	ID        int64
	CreatedAt int64
}

// ServerMetas returns every server's number and creation time, by uuid.
func (s *Store) ServerMetas(ctx context.Context) (map[string]ServerMeta, error) {
	type row struct {
		uuid string
		m    ServerMeta
	}
	rows, err := queryAll(ctx, s.RO, func(sc rowScanner) (row, error) {
		var r row
		err := sc.Scan(&r.uuid, &r.m.ID, &r.m.CreatedAt)
		return r, err
	}, `SELECT s.uuid, i.id, s.created_at FROM servers s JOIN server_ids i ON i.uuid = s.uuid`)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ServerMeta, len(rows))
	for _, r := range rows {
		out[r.uuid] = r.m
	}
	return out, nil
}

// GetServerMeta returns one server's number and creation time.
func (s *Store) GetServerMeta(ctx context.Context, uuid string) (ServerMeta, error) {
	var m ServerMeta
	err := s.RO.QueryRowContext(ctx, `SELECT i.id, s.created_at FROM servers s JOIN server_ids i ON i.uuid = s.uuid WHERE s.uuid=?`, uuid).Scan(&m.ID, &m.CreatedAt)
	return m, nf(err)
}

// ServerByNumber loads a server by its number.
func (s *Store) ServerByNumber(ctx context.Context, id int64) (Server, error) {
	return scanServer(s.RO.QueryRowContext(ctx, `SELECT `+srvCols+` FROM servers WHERE uuid=(SELECT uuid FROM server_ids WHERE id=?)`, id))
}

// PortRange is an inclusive range of ports.
type PortRange struct{ From, To int }

// ErrNoPortInRange means none of the ports asked for is free on the node.
var ErrNoPortInRange = userErr("none of the requested ports is free on this node")

// FreePortIn returns the lowest port that lies in one of ranges and in the
// node's range, and that no server on the node uses.
func (s *Store) FreePortIn(ctx context.Context, nodeID int64, ranges []PortRange) (int, error) {
	n, err := s.GetNode(ctx, nodeID)
	if err != nil {
		return 0, err
	}
	used, err := s.UsedPorts(ctx, nodeID)
	if err != nil {
		return 0, err
	}
	best := 0
	for _, r := range ranges {
		for p := max(r.From, n.PortStart); p <= min(r.To, n.PortEnd); p++ {
			if _, taken := used[p]; !taken {
				if best == 0 || p < best {
					best = p
				}
				break
			}
		}
	}
	if best == 0 {
		return 0, ErrNoPortInRange
	}
	return best, nil
}

// UsedPorts returns the ports servers use on a node, with the server's uuid.
func (s *Store) UsedPorts(ctx context.Context, nodeID int64) (map[int]string, error) {
	type row struct {
		port int
		uuid string
	}
	rows, err := queryAll(ctx, s.RO, func(sc rowScanner) (row, error) {
		var r row
		err := sc.Scan(&r.port, &r.uuid)
		return r, err
	}, `SELECT port, server_uuid FROM server_ports WHERE node_id=?`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("ports of node %d: %w", nodeID, err)
	}
	out := make(map[int]string, len(rows))
	for _, r := range rows {
		out[r.port] = r.uuid
	}
	return out, nil
}
