package store

import (
	"context"
	"database/sql"
)

// DatabaseHost is a MySQL/MariaDB server the panel provisions databases on.
type DatabaseHost struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	MaxDatabases int64  `json:"max_databases" doc:"0 = no limit"`
	NodeID       int64  `json:"node_id" doc:"preferred for servers on this node; 0 = any"`
	Databases    int64  `json:"databases" doc:"databases on this host now"`
}

// HostSecret is a host with its admin password, for provisioning only.
type HostSecret struct {
	DatabaseHost
	Password string
}

const hostCols = `id,name,host,port,username,max_databases,COALESCE(node_id,0),(SELECT COUNT(*) FROM server_databases WHERE host_id=database_hosts.id)`

func scanHost(sc rowScanner) (DatabaseHost, error) {
	var h DatabaseHost
	err := sc.Scan(&h.ID, &h.Name, &h.Host, &h.Port, &h.Username, &h.MaxDatabases, &h.NodeID, &h.Databases)
	return h, nf(err)
}

func (s *Store) ListDatabaseHosts(ctx context.Context) ([]DatabaseHost, error) {
	return queryAll(ctx, s.RO, scanHost, `SELECT `+hostCols+` FROM database_hosts ORDER BY name`)
}

// CreateDatabaseHost saves a host; the password is sealed.
func (s *Store) CreateDatabaseHost(ctx context.Context, h DatabaseHost, password string) (DatabaseHost, error) {
	enc, err := s.seal(password)
	if err != nil {
		return h, err
	}
	var node any
	if h.NodeID != 0 {
		node = h.NodeID
	}
	res, err := s.exec(ctx, `INSERT INTO database_hosts(name,host,port,username,password_enc,max_databases,node_id,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		h.Name, h.Host, h.Port, h.Username, enc, h.MaxDatabases, node, now())
	if err != nil {
		return h, uniqueErr(err, "a database host with that name already exists")
	}
	h.ID, _ = res.LastInsertId()
	return h, nil
}

// HostWithSecret loads a host and opens its password.
func (s *Store) HostWithSecret(ctx context.Context, id int64) (HostSecret, error) {
	var enc string
	row := s.RO.QueryRowContext(ctx, `SELECT `+hostCols+`,password_enc FROM database_hosts WHERE id=?`, id)
	var h HostSecret
	if err := row.Scan(&h.ID, &h.Name, &h.Host, &h.Port, &h.Username, &h.MaxDatabases, &h.NodeID, &h.Databases, &enc); err != nil {
		return h, nf(err)
	}
	pw, err := s.open(enc)
	h.Password = pw
	return h, err
}

// ErrHostInUse means a host with databases on it cannot be removed.
var ErrHostInUse = userErr("delete the databases on this host first")

func (s *Store) DeleteDatabaseHost(ctx context.Context, id int64) error {
	n, err := count(ctx, s.RO, `SELECT COUNT(*) FROM server_databases WHERE host_id=?`, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrHostInUse
	}
	res, err := s.exec(ctx, `DELETE FROM database_hosts WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// PickDatabaseHost chooses where a server's new database goes: a host tied
// to the server's node if there is one with room, else any host with room.
func (s *Store) PickDatabaseHost(ctx context.Context, nodeID int64) (int64, error) {
	var id int64
	err := s.RO.QueryRowContext(ctx, `SELECT id FROM database_hosts h
		WHERE (max_databases = 0 OR (SELECT COUNT(*) FROM server_databases WHERE host_id=h.id) < max_databases)
		ORDER BY (COALESCE(node_id,0) = ?) DESC, (node_id IS NULL) DESC,
		         (SELECT COUNT(*) FROM server_databases WHERE host_id=h.id), id
		LIMIT 1`, nodeID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, userErr("no database host has room; ask an administrator to add one")
	}
	return id, err
}

// ServerDatabase is one database a server owns.
type ServerDatabase struct {
	ID       int64  `json:"id"`
	HostID   int64  `json:"host_id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Remote   string `json:"remote" doc:"where connections may come from; % = anywhere"`
	// Host and Port are where to connect.
	Host      string `json:"host"`
	Port      int    `json:"port"`
	CreatedAt int64  `json:"created_at"`
}

const sdbCols = `d.id,d.host_id,d.name,d.username,d.remote,h.host,h.port,d.created_at`

func scanSDB(sc rowScanner) (ServerDatabase, error) {
	var d ServerDatabase
	err := sc.Scan(&d.ID, &d.HostID, &d.Name, &d.Username, &d.Remote, &d.Host, &d.Port, &d.CreatedAt)
	return d, nf(err)
}

func (s *Store) ListServerDatabases(ctx context.Context, server string) ([]ServerDatabase, error) {
	return queryAll(ctx, s.RO, scanSDB, `SELECT `+sdbCols+` FROM server_databases d JOIN database_hosts h ON h.id=d.host_id WHERE d.server_uuid=? ORDER BY d.name`, server)
}

func (s *Store) GetServerDatabase(ctx context.Context, server string, id int64) (ServerDatabase, error) {
	return scanSDB(s.RO.QueryRowContext(ctx, `SELECT `+sdbCols+` FROM server_databases d JOIN database_hosts h ON h.id=d.host_id WHERE d.server_uuid=? AND d.id=?`, server, id))
}

// ErrDatabaseLimit means the server has as many databases as it may.
var ErrDatabaseLimit = userErr("this server cannot have more databases; ask an administrator to raise its limit")

// ReserveDatabase records a database before it is created on the host, so
// two requests cannot both take the last slot or the same name. The caller
// removes the record if provisioning fails.
func (s *Store) ReserveDatabase(ctx context.Context, server string, d ServerDatabase, password string, override bool) (ServerDatabase, error) {
	enc, err := s.seal(password)
	if err != nil {
		return d, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return d, err
	}
	defer tx.Rollback()
	var limit, have int64
	if err := tx.QueryRowContext(ctx, `SELECT database_limit,(SELECT COUNT(*) FROM server_databases WHERE server_uuid=servers.uuid) FROM servers WHERE uuid=?`, server).Scan(&limit, &have); err != nil {
		return d, nf(err)
	}
	if !override && have >= limit {
		return d, ErrDatabaseLimit
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO server_databases(server_uuid,host_id,name,username,password_enc,remote,created_at) VALUES(?,?,?,?,?,?,?)`,
		server, d.HostID, d.Name, d.Username, enc, d.Remote, now())
	if err != nil {
		return d, uniqueErr(err, "a database with that name already exists")
	}
	d.ID, _ = res.LastInsertId()
	return d, tx.Commit()
}

func (s *Store) DeleteServerDatabase(ctx context.Context, id int64) error {
	_, err := s.exec(ctx, `DELETE FROM server_databases WHERE id=?`, id)
	return err
}

// DatabasePassword opens a database's password.
func (s *Store) DatabasePassword(ctx context.Context, id int64) (string, error) {
	var enc string
	if err := s.RO.QueryRowContext(ctx, `SELECT password_enc FROM server_databases WHERE id=?`, id).Scan(&enc); err != nil {
		return "", nf(err)
	}
	return s.open(enc)
}

func (s *Store) SetDatabasePassword(ctx context.Context, id int64, password string) error {
	enc, err := s.seal(password)
	if err != nil {
		return err
	}
	_, err = s.exec(ctx, `UPDATE server_databases SET password_enc=? WHERE id=?`, enc, id)
	return err
}
