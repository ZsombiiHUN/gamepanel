package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Import is a whole panel to bring in at once (from Pterodactyl). Records
// point at each other by Key, the id they had in the other panel. Locations,
// nodes, eggs and servers keep their key as their id (the panel has none of
// them yet); accounts get new ids, since an admin may already exist.
type Import struct {
	Users     []ImportUser
	Locations []ImportLocation
	Nodes     []ImportNode
	Eggs      []ImportEgg
	Servers   []ImportServer // Server.OwnerID, NodeID and EggID are keys
	Subusers  []ImportSubuser
	Schedules []Schedule // ServerUUID set; run as the server's owner
	DBHosts   []ImportDBHost
	Databases []ImportDatabase
	APIKeys   []ImportAPIKey
	Backups   []ImportBackup
}

type ImportUser struct {
	Key   int64
	Email string
	Admin bool
	// PasswordHash is kept as it is (bcrypt is upgraded at the first
	// sign-in); "" leaves the account without a usable password until a
	// reset link is used.
	PasswordHash string
	TOTPSecret   string // base32; "" = two-factor off
	ExternalID   string
}

type ImportLocation struct {
	Key   int64
	Short string
	Long  string
}

type ImportNode struct {
	Key             int64
	LocationKey     int64 // 0 = none
	Name            string
	PortStart       int
	PortEnd         int
	MemoryMB        int64
	DiskMB          int64
	OverallocatePct int64
	SFTPHost        string
	SFTPPort        int
}

type ImportEgg struct {
	Key int64
	Egg Egg
}

type ImportServer struct {
	Key    int64 // its number in the application API; 0 = the next free one
	Server Server
	Extra  []ServerPort // ports beyond the primary one (Port and Notes)
}

type ImportSubuser struct {
	ServerUUID  string
	UserKey     int64
	Permissions []string
}

type ImportDBHost struct {
	Key      int64
	Host     DatabaseHost // NodeID is a key, 0 = any node
	Password string
}

type ImportDatabase struct {
	ServerUUID string
	HostKey    int64
	Name       string
	Username   string
	Remote     string
	Password   string
}

// ImportAPIKey is an API key that keeps working as it is: Token is the whole
// key a client sends, and only its hash is stored, as for keys made here.
type ImportAPIKey struct {
	UserKey    int64
	Name       string
	Token      string
	Prefix     string // shown to recognise the key
	CreatedAt  int64
	LastUsedAt int64
	ExpiresAt  int64 // 0 = never
}

// ImportBackup is a finished backup whose archive is on the server's node
// (Disk "local") or in S3 storage at the same key GamePanel uses.
type ImportBackup struct {
	ServerUUID  string
	UUID        string
	Name        string
	Size        int64
	Ignore      string // newline separated patterns
	Locked      bool
	Disk        string // local or s3
	CreatedAt   int64
	CompletedAt int64
}

// ImportResult says what an import created.
type ImportResult struct {
	// NodeTokens are the new nodes' enrollment tokens by key. They are shown
	// once; only their hashes are stored.
	NodeTokens map[int64]string
	NodeIDs    map[int64]int64
	UserIDs    map[int64]int64
}

// ImportNotEmptyError lists what is already in the panel.
type ImportNotEmptyError struct{ Found []string }

func (e *ImportNotEmptyError) Error() string {
	return "this panel is not empty (" + strings.Join(e.Found, ", ") + "); import into a new panel"
}

// importBlockers is what an import refuses to mix with: it only creates,
// and ids, names and ports from the other panel must not collide.
var importBlockers = []struct{ table, what string }{
	{"locations", "location"}, {"nodes", "node"}, {"servers", "server"}, {"eggs", "egg"}, {"database_hosts", "database host"},
}

// ImportPanel writes a whole panel in one transaction: everything or
// nothing. It refuses a panel that has nodes, servers, eggs or database hosts,
// and accounts whose email or external id is already taken. Accounts already
// there (an admin made with create-admin) stay as they are.
func (s *Store) ImportPanel(ctx context.Context, in Import) (ImportResult, error) {
	res := ImportResult{NodeTokens: map[int64]string{}, NodeIDs: map[int64]int64{}, UserIDs: map[int64]int64{}}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	var found []string
	for _, b := range importBlockers {
		n, err := count(ctx, tx, `SELECT COUNT(*) FROM `+b.table)
		if err != nil {
			return res, err
		}
		if n > 0 {
			found = append(found, fmt.Sprintf("%d %s(s)", n, b.what))
		}
	}
	for _, u := range in.Users {
		n, err := count(ctx, tx, `SELECT COUNT(*) FROM users WHERE email=? OR (external_id IS NOT NULL AND external_id=?)`, u.Email, u.ExternalID)
		if err != nil {
			return res, err
		}
		if n > 0 {
			found = append(found, "an account for "+u.Email)
		}
	}
	if len(found) > 0 {
		return res, &ImportNotEmptyError{Found: found}
	}
	t := now()
	imp := importer{s: s, tx: tx, ctx: ctx, t: t, res: &res, eggIDs: map[int64]int64{}, hostIDs: map[int64]int64{}, locIDs: map[int64]bool{}}
	steps := []func(Import) error{imp.users, imp.locations, imp.nodes, imp.eggs, imp.servers, imp.subusers, imp.schedules, imp.databases, imp.apiKeys, imp.backups}
	for _, step := range steps {
		if err := step(in); err != nil {
			return res, err
		}
	}
	return res, tx.Commit()
}

type importer struct {
	s       *Store
	tx      *sql.Tx
	ctx     context.Context //nolint:containedctx // lives only for one ImportPanel call
	t       int64
	res     *ImportResult
	eggIDs  map[int64]int64
	hostIDs map[int64]int64
	locIDs  map[int64]bool
}

func (m *importer) exec(q string, a ...any) (sql.Result, error) {
	return m.tx.ExecContext(m.ctx, q, a...)
}

func (m *importer) users(in Import) error {
	for _, u := range in.Users {
		hash := u.PasswordHash
		if hash == "" {
			hash = "!" // matches no password: the owner sets one with a reset link
		}
		secret, enabled := "", false
		if u.TOTPSecret != "" {
			enc, err := m.s.seal(u.TOTPSecret)
			if err != nil {
				return err
			}
			secret, enabled = enc, true
		}
		r, err := m.exec(`INSERT INTO users(email,password_hash,is_admin,totp_secret,totp_enabled,external_id,created_at) VALUES(?,?,?,?,?,?,?)`,
			u.Email, hash, u.Admin, secret, enabled, nullable(u.ExternalID), m.t)
		if err != nil {
			return fmt.Errorf("account %s: %w", u.Email, err)
		}
		m.res.UserIDs[u.Key], _ = r.LastInsertId()
	}
	return nil
}

func (m *importer) locations(in Import) error {
	for _, l := range in.Locations {
		short, long, err := checkLocation(l.Short, l.Long)
		if err != nil {
			return fmt.Errorf("location %s: %w", l.Short, err)
		}
		if _, err := m.exec(`INSERT INTO locations(id,short,long,created_at) VALUES(?,?,?,?)`, l.Key, short, long, m.t); err != nil {
			return fmt.Errorf("location %s: %w", l.Short, locationUniqueErr(err))
		}
		m.locIDs[l.Key] = true
	}
	return nil
}

func (m *importer) nodes(in Import) error {
	for _, n := range in.Nodes {
		if !validSFTPHost(n.SFTPHost) {
			n.SFTPHost = "" // the panel's own host name
		}
		if n.SFTPPort < 0 || n.SFTPPort > 65535 {
			n.SFTPPort = 0 // the port the daemon reports
		}
		var loc any
		if m.locIDs[n.LocationKey] {
			loc = n.LocationKey
		}
		tok, h := NewToken()
		if _, err := m.exec(`INSERT INTO nodes(id,name,token_hash,port_start,port_end,memory_mb,disk_mb,overallocate_pct,sftp_host,sftp_port,location_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			n.Key, n.Name, h, n.PortStart, n.PortEnd, n.MemoryMB, n.DiskMB, n.OverallocatePct, n.SFTPHost, n.SFTPPort, loc, m.t); err != nil {
			return fmt.Errorf("node %s: %w", n.Name, err)
		}
		m.res.NodeIDs[n.Key] = n.Key
		m.res.NodeTokens[n.Key] = tok
	}
	return nil
}

func (m *importer) eggs(in Import) error {
	for _, e := range in.Eggs {
		cols, err := eggRow(e.Egg)
		if err != nil {
			return err
		}
		if _, err := m.exec(`INSERT INTO eggs(id,`+eggInsertCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, append(append([]any{e.Key}, cols...), m.t)...); err != nil {
			return fmt.Errorf("egg %s: %w", e.Egg.Name, err)
		}
		m.eggIDs[e.Key] = e.Key
	}
	return nil
}

// ref maps a key to its new id, failing on a record the import does not have.
func ref(ids map[int64]int64, key int64, what, of string) (int64, error) {
	id, ok := ids[key]
	if !ok {
		return 0, fmt.Errorf("%s: its %s (#%d) is not imported", of, what, key)
	}
	return id, nil
}

func (m *importer) servers(in Import) error {
	for _, is := range in.Servers {
		v := is.Server
		var err error
		of := "server " + v.Name
		if v.OwnerID, err = ref(m.res.UserIDs, v.OwnerID, "owner", of); err != nil {
			return err
		}
		if v.NodeID, err = ref(m.res.NodeIDs, v.NodeID, "node", of); err != nil {
			return err
		}
		if v.EggID, err = ref(m.eggIDs, v.EggID, "egg", of); err != nil {
			return err
		}
		if err := v.Normalize(); err != nil {
			return fmt.Errorf("%s: %w", of, err)
		}
		if v.Env == nil {
			v.Env = map[string]string{}
		}
		env, err := json.Marshal(v.Env)
		if err != nil {
			return err
		}
		if is.Key != 0 {
			// Before the server row: its insert trigger then keeps this number.
			if _, err := m.exec(`INSERT INTO server_ids(id,uuid) VALUES(?,?)`, is.Key, v.UUID); err != nil {
				return fmt.Errorf("%s: number %d: %w", of, is.Key, err)
			}
		}
		if _, err := m.exec(`INSERT INTO servers(`+srvCols+`,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			v.UUID, v.Name, v.OwnerID, v.NodeID, v.EggID, v.MemoryMB, v.CPUPct, v.Port, string(env),
			v.SwapMB, v.CPUSet, v.PidsLimit, v.IOWeight, v.DiskMB, v.BackupLimit, v.Image, v.Startup, v.PortLimit, v.DatabaseLimit, v.Description, v.BandwidthMbit, v.CrashRestart,
			nullable(v.ExternalID), v.Suspended, v.InstallStatus, m.t); err != nil {
			return fmt.Errorf("%s: %w", of, err)
		}
		ports := append([]ServerPort{{Port: v.Port}}, is.Extra...)
		for _, p := range ports {
			if _, err := m.exec(`INSERT INTO server_ports(node_id,port,server_uuid,notes,created_at) VALUES(?,?,?,?,?)`, v.NodeID, p.Port, v.UUID, p.Notes, m.t); err != nil {
				return fmt.Errorf("%s port %d: %w", of, p.Port, err)
			}
		}
	}
	return nil
}

func (m *importer) subusers(in Import) error {
	for _, su := range in.Subusers {
		uid, err := ref(m.res.UserIDs, su.UserKey, "account", "subuser of "+su.ServerUUID)
		if err != nil {
			return err
		}
		perms, err := ValidatePermissions(su.Permissions)
		if err != nil {
			return err
		}
		if _, err := m.exec(`INSERT INTO server_users(server_uuid,user_id,permissions,created_at) VALUES(?,?,?,?)`, su.ServerUUID, uid, strings.Join(perms, ","), m.t); err != nil {
			return fmt.Errorf("subuser of %s: %w", su.ServerUUID, err)
		}
	}
	return nil
}

func (m *importer) schedules(in Import) error {
	for _, v := range in.Schedules {
		next, err := NextRun(v.Cron, time.Unix(m.t, 0))
		if err != nil {
			return fmt.Errorf("schedule %s: %w", v.Name, err)
		}
		if len(v.Tasks) > MaxTasksPerSchedule {
			return fmt.Errorf("schedule %s: too many tasks", v.Name)
		}
		nextRun := int64(0)
		if v.Enabled {
			nextRun = next.Unix()
		}
		r, err := m.exec(`INSERT INTO schedules(server_uuid,name,cron,enabled,only_when_online,next_run) VALUES(?,?,?,?,?,?)`,
			v.ServerUUID, v.Name, strings.TrimSpace(v.Cron), v.Enabled, v.OnlyWhenOnline, nextRun)
		if err != nil {
			return fmt.Errorf("schedule %s: %w", v.Name, err)
		}
		id, _ := r.LastInsertId()
		tasks := slices.Clone(v.Tasks)
		slices.SortStableFunc(tasks, func(a, b Task) int { return a.Seq - b.Seq })
		for i, t := range tasks {
			if err := ValidateTask(t); err != nil {
				return fmt.Errorf("schedule %s: %w", v.Name, err)
			}
			if _, err := m.exec(`INSERT INTO schedule_tasks(schedule_id,seq,action,payload,offset_seconds,continue_on_failure) VALUES(?,?,?,?,?,?)`,
				id, i, t.Action, t.Payload, t.OffsetSeconds, t.ContinueOnFailure); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *importer) databases(in Import) error {
	for _, h := range in.DBHosts {
		enc, err := m.s.seal(h.Password)
		if err != nil {
			return err
		}
		var node any
		if h.Host.NodeID != 0 {
			if id, ok := m.res.NodeIDs[h.Host.NodeID]; ok {
				node = id
			}
		}
		r, err := m.exec(`INSERT INTO database_hosts(name,host,port,username,password_enc,max_databases,node_id,created_at) VALUES(?,?,?,?,?,?,?,?)`,
			h.Host.Name, h.Host.Host, h.Host.Port, h.Host.Username, enc, h.Host.MaxDatabases, node, m.t)
		if err != nil {
			return fmt.Errorf("database host %s: %w", h.Host.Name, err)
		}
		m.hostIDs[h.Key], _ = r.LastInsertId()
	}
	for _, d := range in.Databases {
		host, err := ref(m.hostIDs, d.HostKey, "database host", "database "+d.Name)
		if err != nil {
			return err
		}
		enc, err := m.s.seal(d.Password)
		if err != nil {
			return err
		}
		if _, err := m.exec(`INSERT INTO server_databases(server_uuid,host_id,name,username,password_enc,remote,created_at) VALUES(?,?,?,?,?,?,?)`,
			d.ServerUUID, host, d.Name, d.Username, enc, d.Remote, m.t); err != nil {
			return fmt.Errorf("database %s: %w", d.Name, err)
		}
	}
	return nil
}

func (m *importer) apiKeys(in Import) error {
	for _, k := range in.APIKeys {
		uid, err := ref(m.res.UserIDs, k.UserKey, "account", "API key "+k.Prefix)
		if err != nil {
			return err
		}
		if !LooksLikeAPIKey(k.Token) {
			return fmt.Errorf("API key %s: not in a format the panel accepts", k.Prefix)
		}
		if _, err := m.exec(`INSERT INTO api_keys(user_id,name,token_hash,prefix,created_at,last_used_at,expires_at) VALUES(?,?,?,?,?,?,?)`,
			uid, k.Name, HashToken(k.Token), k.Prefix, k.CreatedAt, k.LastUsedAt, k.ExpiresAt); err != nil {
			return fmt.Errorf("API key %s: %w", k.Prefix, err)
		}
	}
	return nil
}

func (m *importer) backups(in Import) error {
	for _, b := range in.Backups {
		if b.Disk != "local" && b.Disk != "s3" {
			return fmt.Errorf("backup %s: unknown storage %q", b.UUID, b.Disk)
		}
		if _, err := m.exec(`INSERT INTO backups(uuid,server_uuid,name,status,size,ignore,locked,disk,created_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			b.UUID, b.ServerUUID, b.Name, BackupOK, b.Size, b.Ignore, b.Locked, b.Disk, b.CreatedAt, b.CompletedAt); err != nil {
			return fmt.Errorf("backup %s: %w", b.UUID, err)
		}
	}
	return nil
}
