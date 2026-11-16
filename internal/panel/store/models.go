package store

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrBadPassword: the current password given to ChangePassword is wrong.
	ErrBadPassword = userErr("invalid current password")
	// ErrNodeInUse: a node cannot be deleted while servers live on it.
	ErrNodeInUse = userErr("node still has servers")
)

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
	// TOTPEnabled reports whether a second factor is required at sign-in.
	TOTPEnabled bool `json:"totp_enabled"`
	hash        string
}

// argon2id with RFC 9106 parameters: t=3, m=64MiB, p=4.
const (
	argT, argM, argP, argKey = 3, 64 * 1024, 4, 32
)

// Password length limits, in characters (not bytes, so non-Latin scripts
// get the same minimum). The cap stops a huge password being used to burn CPU.
const (
	MinPasswordLen = 12
	MaxPasswordLen = 256
)

// ValidatePassword enforces the password policy of OWASP ASVS §2.1: a length
// range, no composition rules, and no password that is known to be common or
// leaked, or that is built from the account's own email address.
func ValidatePassword(pw, email string) error {
	if n := utf8.RuneCountInString(pw); n < MinPasswordLen || n > MaxPasswordLen {
		return userErrf("password must be %d to %d characters", MinPasswordLen, MaxPasswordLen)
	}
	if isCommonPassword(pw) {
		return userErr("this password is too common (it appears in lists of leaked passwords); choose another")
	}
	// A password that is mostly the account name or the site's name is the
	// first thing a targeted guesser tries: refuse it when what is left after
	// removing that word would be too short to pass on its own.
	lower := strings.ToLower(pw)
	local, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	if mostly(lower, local) {
		return userErr("the password is too close to your email address")
	}
	if mostly(lower, "gamepanel") {
		return userErr("the password is too close to the name of this site")
	}
	return nil
}

func mostly(pw, word string) bool {
	if utf8.RuneCountInString(word) < 4 || !strings.Contains(pw, word) {
		return false
	}
	return utf8.RuneCountInString(strings.ReplaceAll(pw, word, "")) < MinPasswordLen
}

func hashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argon2.IDKey([]byte(pw), salt, argT, argM, argP, argKey)
	return fmt.Sprintf("argon2id$%x$%x", salt, key)
}

func checkPassword(pw, enc string) bool {
	parts := strings.SplitN(enc, "$", 3)
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, err1 := hex.DecodeString(parts[1])
	key, err2 := hex.DecodeString(parts[2])
	if err1 != nil || err2 != nil || len(key) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, argT, argM, argP, uint32(len(key)))
	return subtle.ConstantTimeCompare(got, key) == 1
}

func (s *Store) CreateUser(ctx context.Context, email, password string, admin bool) (User, error) {
	if err := ValidatePassword(password, email); err != nil {
		return User{}, err
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
	err := s.DB.QueryRowContext(ctx, `SELECT id,email,is_admin,totp_enabled,password_hash FROM users WHERE email=?`, email).
		Scan(&u.ID, &u.Email, &u.IsAdmin, &u.TOTPEnabled, &u.hash)
	if err != nil {
		checkPassword(password, dummyHash)
		return User{}, userErr("invalid email or password")
	}
	if !checkPassword(password, u.hash) {
		return User{}, userErr("invalid email or password")
	}
	return u, nil
}

// touchEvery is how stale a session's last_seen_at may get before a request
// refreshes it, so busy sessions do not turn every request into a write.
const touchEvery = 60

// idleCutoff is the newest last_seen_at at which a session counts as idle.
func (s *Store) idleCutoff() int64 {
	return now() - int64(s.SessionIdle.Seconds())
}

// NewSession starts a session. Signing in is also when expired and idle
// sessions are cleared out, so the table does not grow forever.
func (s *Store) NewSession(ctx context.Context, userID int64) (string, error) {
	if _, err := s.exec(ctx, `DELETE FROM sessions WHERE expires_at<=? OR last_seen_at<=?`, now(), s.idleCutoff()); err != nil {
		return "", err
	}
	tok, h := NewToken()
	_, err := s.exec(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at,last_seen_at) VALUES(?,?,?,?)`,
		h, userID, time.Now().Add(s.SessionMax).Unix(), now())
	return tok, err
}

// UserBySession resolves a session cookie. A session ends at its absolute
// expiry, or earlier once it has gone unused for SessionIdle; each use pushes
// the idle deadline back (at most once a minute).
func (s *Store) UserBySession(ctx context.Context, token string) (User, error) {
	var u User
	var seen int64
	h := HashToken(token)
	err := s.DB.QueryRowContext(ctx, `SELECT u.id,u.email,u.is_admin,u.totp_enabled,s.last_seen_at FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=? AND s.expires_at>? AND s.last_seen_at>?`, h, now(), s.idleCutoff()).
		Scan(&u.ID, &u.Email, &u.IsAdmin, &u.TOTPEnabled, &seen)
	if err != nil {
		return User{}, nf(err)
	}
	if now()-seen >= touchEvery {
		s.exec(ctx, `UPDATE sessions SET last_seen_at=? WHERE token_hash=?`, now(), h)
	}
	return u, nil
}

// ChangePassword verifies the current password, sets the new one, and revokes
// every session except keepToken (the caller's own).
func (s *Store) ChangePassword(ctx context.Context, userID int64, current, next, keepToken string) error {
	var enc, email string
	if err := s.DB.QueryRowContext(ctx, `SELECT password_hash,email FROM users WHERE id=?`, userID).Scan(&enc, &email); err != nil {
		checkPassword(current, dummyHash)
		return ErrBadPassword
	}
	// Check the current password first, so the policy message for the new one
	// is only shown to someone who has proved who they are.
	if !checkPassword(current, enc) {
		return ErrBadPassword
	}
	if err := ValidatePassword(next, email); err != nil {
		return err
	}
	if _, err := s.exec(ctx, `UPDATE users SET password_hash=? WHERE id=?`, hashPassword(next), userID); err != nil {
		return err
	}
	return s.DeleteOtherSessions(ctx, userID, keepToken)
}

// DeleteOtherSessions revokes all of a user's sessions except keepToken.
func (s *Store) DeleteOtherSessions(ctx context.Context, userID int64, keepToken string) error {
	_, err := s.exec(ctx, `DELETE FROM sessions WHERE user_id=? AND token_hash<>?`, userID, HashToken(keepToken))
	return err
}

// GetUser loads an account by id.
func (s *Store) GetUser(ctx context.Context, id int64) (User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx, `SELECT id,email,is_admin,totp_enabled FROM users WHERE id=?`, id).Scan(&u.ID, &u.Email, &u.IsAdmin, &u.TOTPEnabled)
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
	// Capacity (0 = not tracked) and what servers have been allocated so far.
	MemoryMB        int64 `json:"memory_mb"`
	DiskMB          int64 `json:"disk_mb"`
	OverallocatePct int64 `json:"overallocate_pct"`
	AllocatedMemory int64 `json:"allocated_memory_mb"`
	AllocatedDisk   int64 `json:"allocated_disk_mb"`
}

const nodeCols = `id,name,port_start,port_end,memory_mb,disk_mb,overallocate_pct,
	(SELECT COALESCE(SUM(memory_mb),0) FROM servers WHERE node_id=nodes.id),
	(SELECT COALESCE(SUM(disk_mb),0) FROM servers WHERE node_id=nodes.id)`

func scanNode(sc interface{ Scan(...any) error }) (Node, error) {
	var n Node
	err := sc.Scan(&n.ID, &n.Name, &n.PortStart, &n.PortEnd, &n.MemoryMB, &n.DiskMB, &n.OverallocatePct, &n.AllocatedMemory, &n.AllocatedDisk)
	return n, err
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
	n, err := scanNode(s.DB.QueryRowContext(ctx, `SELECT `+nodeCols+` FROM nodes WHERE token_hash=?`, HashToken(token)))
	return n, nf(err)
}

func (s *Store) GetNode(ctx context.Context, id int64) (Node, error) {
	n, err := scanNode(s.DB.QueryRowContext(ctx, `SELECT `+nodeCols+` FROM nodes WHERE id=?`, id))
	return n, nf(err)
}

func (s *Store) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+nodeCols+` FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// DeleteNode removes a node that hosts no servers (ErrNodeInUse otherwise).
func (s *Store) DeleteNode(ctx context.Context, id int64) error {
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM servers WHERE node_id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrNodeInUse
	}
	res, err := s.exec(ctx, `DELETE FROM nodes WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- eggs ----

type EggVar struct {
	Env         string `json:"env"`
	Name        string `json:"name"`
	Default     string `json:"default"`
	Description string `json:"description"`
	// UserViewable and UserEditable decide whether server owners and
	// subusers see and change the value; admins always do both.
	UserViewable bool `json:"user_viewable"`
	UserEditable bool `json:"user_editable"`
	// Rules are Pterodactyl's validation rules, e.g. "required|string|max:20"
	// (see ValidateVar for the supported set).
	Rules string `json:"rules"`
}

// EggImage is one Docker image an egg offers, such as "Java 21".
type EggImage struct {
	Label string `json:"label"`
	Image string `json:"image"`
}

type Egg struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	DockerImage string `json:"docker_image" doc:"the default image (the first of docker_images)"`
	// DockerImages lists every image the egg offers, default first.
	DockerImages []EggImage `json:"docker_images" nullable:"false"`
	Startup      string     `json:"startup"`
	Stop         string     `json:"stop"`
	Variables    []EggVar   `json:"variables"`
	// Optional install step: Script runs once in a throwaway InstallImage
	// container with the server's data dir mounted at /mnt/server.
	InstallImage      string `json:"install_image"`
	InstallEntrypoint string `json:"install_entrypoint"`
	InstallScript     string `json:"install_script"`
}

func (s *Store) CreateEgg(ctx context.Context, e Egg) (Egg, error) {
	if len(e.DockerImages) == 0 && e.DockerImage != "" {
		e.DockerImages = []EggImage{{Label: e.DockerImage, Image: e.DockerImage}}
	}
	if e.DockerImage == "" && len(e.DockerImages) > 0 {
		e.DockerImage = e.DockerImages[0].Image
	}
	if e.Variables == nil {
		e.Variables = []EggVar{}
	}
	vars, _ := json.Marshal(e.Variables)
	imgs, _ := json.Marshal(e.DockerImages)
	res, err := s.exec(ctx, `INSERT INTO eggs(name,description,docker_image,docker_images,startup,stop,variables,install_image,install_entrypoint,install_script,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		e.Name, e.Description, e.DockerImage, string(imgs), e.Startup, e.Stop, string(vars), e.InstallImage, e.InstallEntrypoint, e.InstallScript, now())
	if err != nil {
		return Egg{}, err
	}
	e.ID, _ = res.LastInsertId()
	return e, nil
}

func scanEgg(sc interface{ Scan(...any) error }) (Egg, error) {
	var e Egg
	var vars, imgs string
	if err := sc.Scan(&e.ID, &e.Name, &e.Description, &e.DockerImage, &imgs, &e.Startup, &e.Stop, &vars, &e.InstallImage, &e.InstallEntrypoint, &e.InstallScript); err != nil {
		return e, nf(err)
	}
	json.Unmarshal([]byte(vars), &e.Variables)
	if e.Variables == nil {
		e.Variables = []EggVar{}
	}
	json.Unmarshal([]byte(imgs), &e.DockerImages)
	if len(e.DockerImages) == 0 {
		e.DockerImages = []EggImage{{Label: e.DockerImage, Image: e.DockerImage}}
	}
	return e, nil
}

const eggCols = `id,name,description,docker_image,docker_images,startup,stop,variables,install_image,install_entrypoint,install_script`

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
	UUID    string            `json:"uuid"`
	Name    string            `json:"name"`
	OwnerID int64             `json:"owner_id"`
	NodeID  int64             `json:"node_id"`
	EggID   int64             `json:"egg_id"`
	Port    int               `json:"port"`
	Env     map[string]string `json:"env"`
	// Image and Startup override the egg's; "" means use the egg's.
	Image   string `json:"image"`
	Startup string `json:"startup"`
	Limits
}

const srvCols = `uuid,name,owner_id,node_id,egg_id,memory_mb,cpu_pct,port,env,swap_mb,cpuset,pids_limit,io_weight,disk_mb,backup_limit,image,startup,port_limit`

func scanServer(sc interface{ Scan(...any) error }) (Server, error) {
	var v Server
	var env string
	if err := sc.Scan(&v.UUID, &v.Name, &v.OwnerID, &v.NodeID, &v.EggID, &v.MemoryMB, &v.CPUPct, &v.Port, &env, &v.SwapMB, &v.CPUSet, &v.PidsLimit, &v.IOWeight, &v.DiskMB, &v.BackupLimit, &v.Image, &v.Startup, &v.PortLimit); err != nil {
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

// ListServers returns every server for admins, otherwise the user's own and shared ones.
func (s *Store) ListServers(ctx context.Context, u User) ([]Server, error) {
	q, args := `SELECT `+srvCols+` FROM servers ORDER BY name`, []any{}
	if !u.IsAdmin {
		q, args = `SELECT `+srvCols+` FROM servers WHERE owner_id=? OR uuid IN (SELECT server_uuid FROM server_users WHERE user_id=?) ORDER BY name`, []any{u.ID, u.ID}
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
	if err := v.Limits.Normalize(); err != nil {
		return v, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if err := checkCapacity(ctx, tx, v.NodeID, "", v.Limits); err != nil {
		return v, err
	}
	port, err := pickPort(ctx, tx, v.NodeID, v.Port)
	if err != nil {
		return v, err
	}
	v.Port = port
	if v.Env == nil {
		v.Env = map[string]string{}
	}
	env, _ := json.Marshal(v.Env)
	if _, err := tx.ExecContext(ctx, `INSERT INTO servers(`+srvCols+`,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		v.UUID, v.Name, v.OwnerID, v.NodeID, v.EggID, v.MemoryMB, v.CPUPct, v.Port, string(env),
		v.SwapMB, v.CPUSet, v.PidsLimit, v.IOWeight, v.DiskMB, v.BackupLimit, v.Image, v.Startup, v.PortLimit, now()); err != nil {
		return v, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO server_ports(node_id,port,server_uuid,created_at) VALUES(?,?,?,?)`, v.NodeID, v.Port, v.UUID, now()); err != nil {
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

// ServerByPrefix finds the server on a node whose uuid starts with prefix
// (the 8 hex characters used in SFTP usernames). Ambiguous prefixes fail.
func (s *Store) ServerByPrefix(ctx context.Context, nodeID int64, prefix string) (Server, error) {
	for _, c := range prefix {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return Server{}, ErrNotFound
		}
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+srvCols+` FROM servers WHERE node_id=? AND uuid LIKE ? LIMIT 2`, nodeID, prefix+"%")
	if err != nil {
		return Server{}, err
	}
	defer rows.Close()
	var found []Server
	for rows.Next() {
		v, err := scanServer(rows)
		if err != nil {
			return Server{}, err
		}
		found = append(found, v)
	}
	if len(found) != 1 {
		return Server{}, ErrNotFound
	}
	return found[0], nil
}

// Labels returns egg and node names by id, so server lists can show them
// without loading install scripts or tokens.
func (s *Store) Labels(ctx context.Context) (eggs, nodes map[int64]string, err error) {
	eggs, nodes = map[int64]string{}, map[int64]string{}
	for table, dst := range map[string]map[int64]string{"eggs": eggs, "nodes": nodes} {
		rows, qerr := s.DB.QueryContext(ctx, `SELECT id,name FROM `+table)
		if qerr != nil {
			return nil, nil, qerr
		}
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return nil, nil, err
			}
			dst[id] = name
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, nil, err
		}
		rows.Close()
	}
	return eggs, nodes, nil
}
