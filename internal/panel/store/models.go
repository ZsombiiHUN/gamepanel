package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/argon2"
)

var ErrNotFound = errors.New("not found")

func nf(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// ---- users & sessions ----

type User struct {
	ID      int64  `json:"id"`
	Email   string `json:"email"`
	IsAdmin bool   `json:"is_admin"`
	hash    string
}

// argon2id with RFC 9106 parameters: t=3, m=64MiB, p=4.
const (
	argT, argM, argP, argKey = 3, 64 * 1024, 4, 32
)

func hashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argon2.IDKey([]byte(pw), salt, argT, argM, argP, argKey)
	return fmt.Sprintf("argon2id$%x$%x", salt, key)
}

func checkPassword(pw, enc string) bool {
	parts := splitN(enc, '$', 3)
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, err1 := hexDecode(parts[1])
	key, err2 := hexDecode(parts[2])
	if err1 != nil || err2 != nil || len(key) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, argT, argM, argP, uint32(len(key)))
	return constEq(got, key)
}

func (s *Store) CreateUser(ctx context.Context, email, password string, admin bool) (User, error) {
	if len(password) < 10 {
		return User{}, errors.New("password must be at least 10 characters")
	}
	res, err := s.exec(ctx, `INSERT INTO users(email,password_hash,is_admin,created_at) VALUES(?,?,?,?)`,
		email, hashPassword(password), admin, now())
	if err != nil {
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return User{ID: id, Email: email, IsAdmin: admin}, nil
}

func (s *Store) CountUsers(ctx context.Context) (n int) {
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return
}

// dummyHash lets Login spend the same time for unknown emails (no user enumeration by timing).
var dummyHash = hashPassword("dummy-password-for-timing")

func (s *Store) Login(ctx context.Context, email, password string) (User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx, `SELECT id,email,is_admin,password_hash FROM users WHERE email=?`, email).
		Scan(&u.ID, &u.Email, &u.IsAdmin, &u.hash)
	if err != nil {
		checkPassword(password, dummyHash)
		return User{}, errors.New("invalid email or password")
	}
	if !checkPassword(password, u.hash) {
		return User{}, errors.New("invalid email or password")
	}
	return u, nil
}

const SessionTTL = 14 * 24 * time.Hour

func (s *Store) NewSession(ctx context.Context, userID int64) (string, error) {
	tok, h := NewToken()
	_, err := s.exec(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)`,
		h, userID, time.Now().Add(SessionTTL).Unix())
	return tok, err
}

func (s *Store) UserBySession(ctx context.Context, token string) (User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx, `SELECT u.id,u.email,u.is_admin FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=? AND s.expires_at>?`, HashToken(token), now()).Scan(&u.ID, &u.Email, &u.IsAdmin)
	return u, nf(err)
}

func (s *Store) DeleteSession(ctx context.Context, token string) {
	s.exec(ctx, `DELETE FROM sessions WHERE token_hash=?`, HashToken(token))
}

// ---- nodes ----

type Node struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	PortStart int    `json:"port_start"`
	PortEnd   int    `json:"port_end"`
}

// CreateNode returns the node and its one-time enrollment token.
func (s *Store) CreateNode(ctx context.Context, name string, ps, pe int) (Node, string, error) {
	tok, h := NewToken()
	res, err := s.exec(ctx, `INSERT INTO nodes(name,token_hash,port_start,port_end,created_at) VALUES(?,?,?,?,?)`, name, h, ps, pe, now())
	if err != nil {
		return Node{}, "", err
	}
	id, _ := res.LastInsertId()
	return Node{ID: id, Name: name, PortStart: ps, PortEnd: pe}, tok, nil
}

func (s *Store) NodeByToken(ctx context.Context, token string) (Node, error) {
	var n Node
	err := s.DB.QueryRowContext(ctx, `SELECT id,name,port_start,port_end FROM nodes WHERE token_hash=?`, HashToken(token)).
		Scan(&n.ID, &n.Name, &n.PortStart, &n.PortEnd)
	return n, nf(err)
}

func (s *Store) GetNode(ctx context.Context, id int64) (Node, error) {
	var n Node
	err := s.DB.QueryRowContext(ctx, `SELECT id,name,port_start,port_end FROM nodes WHERE id=?`, id).
		Scan(&n.ID, &n.Name, &n.PortStart, &n.PortEnd)
	return n, nf(err)
}

func (s *Store) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,port_start,port_end FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Node{}
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.Name, &n.PortStart, &n.PortEnd); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) DeleteNode(ctx context.Context, id int64) error {
	_, err := s.exec(ctx, `DELETE FROM nodes WHERE id=?`, id)
	return err
}

// ---- eggs ----

type EggVar struct {
	Env         string `json:"env"`
	Name        string `json:"name"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

type Egg struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	DockerImage string   `json:"docker_image"`
	Startup     string   `json:"startup"`
	Stop        string   `json:"stop"`
	Variables   []EggVar `json:"variables"`
}

func (s *Store) CreateEgg(ctx context.Context, e Egg) (Egg, error) {
	vars, _ := json.Marshal(e.Variables)
	res, err := s.exec(ctx, `INSERT INTO eggs(name,description,docker_image,startup,stop,variables,created_at) VALUES(?,?,?,?,?,?,?)`,
		e.Name, e.Description, e.DockerImage, e.Startup, e.Stop, string(vars), now())
	if err != nil {
		return Egg{}, err
	}
	e.ID, _ = res.LastInsertId()
	return e, nil
}

func scanEgg(sc interface{ Scan(...any) error }) (Egg, error) {
	var e Egg
	var vars string
	if err := sc.Scan(&e.ID, &e.Name, &e.Description, &e.DockerImage, &e.Startup, &e.Stop, &vars); err != nil {
		return e, nf(err)
	}
	json.Unmarshal([]byte(vars), &e.Variables)
	if e.Variables == nil {
		e.Variables = []EggVar{}
	}
	return e, nil
}

const eggCols = `id,name,description,docker_image,startup,stop,variables`

func (s *Store) GetEgg(ctx context.Context, id int64) (Egg, error) {
	return scanEgg(s.DB.QueryRowContext(ctx, `SELECT `+eggCols+` FROM eggs WHERE id=?`, id))
}

func (s *Store) ListEggs(ctx context.Context) ([]Egg, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+eggCols+` FROM eggs ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Egg{}
	for rows.Next() {
		e, err := scanEgg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- servers ----

type Server struct {
	UUID     string            `json:"uuid"`
	Name     string            `json:"name"`
	OwnerID  int64             `json:"owner_id"`
	NodeID   int64             `json:"node_id"`
	EggID    int64             `json:"egg_id"`
	MemoryMB int64             `json:"memory_mb"`
	CPUPct   int64             `json:"cpu_pct"`
	Port     int               `json:"port"`
	Env      map[string]string `json:"env"`
}

const srvCols = `uuid,name,owner_id,node_id,egg_id,memory_mb,cpu_pct,port,env`

func scanServer(sc interface{ Scan(...any) error }) (Server, error) {
	var v Server
	var env string
	if err := sc.Scan(&v.UUID, &v.Name, &v.OwnerID, &v.NodeID, &v.EggID, &v.MemoryMB, &v.CPUPct, &v.Port, &env); err != nil {
		return v, nf(err)
	}
	json.Unmarshal([]byte(env), &v.Env)
	if v.Env == nil {
		v.Env = map[string]string{}
	}
	return v, nil
}

func (s *Store) GetServer(ctx context.Context, uuid string) (Server, error) {
	return scanServer(s.DB.QueryRowContext(ctx, `SELECT `+srvCols+` FROM servers WHERE uuid=?`, uuid))
}

// ListServers returns every server for admins, otherwise only the user's own.
func (s *Store) ListServers(ctx context.Context, u User) ([]Server, error) {
	q, args := `SELECT `+srvCols+` FROM servers ORDER BY name`, []any{}
	if !u.IsAdmin {
		q, args = `SELECT `+srvCols+` FROM servers WHERE owner_id=? ORDER BY name`, []any{u.ID}
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Server{}
	for rows.Next() {
		v, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// CreateServer allocates the first free port in the node's range.
func (s *Store) CreateServer(ctx context.Context, v Server) (Server, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	var ps, pe int
	if err := tx.QueryRowContext(ctx, `SELECT port_start,port_end FROM nodes WHERE id=?`, v.NodeID).Scan(&ps, &pe); err != nil {
		return v, nf(err)
	}
	if v.Port == 0 {
		rows, err := tx.QueryContext(ctx, `SELECT port FROM servers WHERE node_id=?`, v.NodeID)
		if err != nil {
			return v, err
		}
		used := map[int]bool{}
		for rows.Next() {
			var p int
			rows.Scan(&p)
			used[p] = true
		}
		rows.Close()
		for p := ps; p <= pe; p++ {
			if !used[p] {
				v.Port = p
				break
			}
		}
		if v.Port == 0 {
			return v, errors.New("no free ports on this node")
		}
	} else if v.Port < ps || v.Port > pe {
		return v, fmt.Errorf("port must be within the node range %d-%d", ps, pe)
	}
	if v.Env == nil {
		v.Env = map[string]string{}
	}
	env, _ := json.Marshal(v.Env)
	if _, err := tx.ExecContext(ctx, `INSERT INTO servers(`+srvCols+`,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		v.UUID, v.Name, v.OwnerID, v.NodeID, v.EggID, v.MemoryMB, v.CPUPct, v.Port, string(env), now()); err != nil {
		return v, err
	}
	return v, tx.Commit()
}

func (s *Store) DeleteServer(ctx context.Context, uuid string) error {
	_, err := s.exec(ctx, `DELETE FROM servers WHERE uuid=?`, uuid)
	return err
}

func (s *Store) ServersOnNode(ctx context.Context, nodeID int64) ([]Server, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+srvCols+` FROM servers WHERE node_id=?`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Server
	for rows.Next() {
		v, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
