// Package ptero reads a Pterodactyl panel's database and plans how it maps
// onto GamePanel: users, nodes, eggs, servers, subusers, schedules and
// databases. Reading never writes to Pterodactyl's database.
package ptero

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Source is what the import reads from Pterodactyl, as stored there.
type Source struct {
	Users       []User
	Locations   []Location
	Nodes       []Node
	Allocations []Allocation
	Nests       map[int64]string // id -> name
	Eggs        []Egg
	EggVars     []EggVar
	Servers     []Server
	ServerVars  []ServerVar
	Subusers    []Subuser
	Schedules   []Schedule
	Tasks       []Task
	DBHosts     []DBHost
	Databases   []Database
	// Counted only: not imported in this version.
	Backups, APIKeys, Mounts int
}

type Location struct {
	ID    int64
	Short string
	Long  sql.NullString
}

type User struct {
	ID         int64
	ExternalID sql.NullString
	Email      string
	Username   string
	Password   string // bcrypt
	Admin      bool
	UseTOTP    bool
	TOTPSecret sql.NullString // Laravel-encrypted
}

type Node struct {
	ID                 int64
	LocationID         int64
	Name               string
	Memory, Disk       int64
	MemoryOverallocate int64
	DiskOverallocate   int64
	Maintenance        bool
	SFTPPort           int
	Base               string // Wings' volume directory, e.g. /var/lib/pterodactyl/volumes
	FQDN               string // where people reach the node (SFTP)
}

type Allocation struct {
	ID       int64
	NodeID   int64
	IP       string
	Port     int
	ServerID sql.NullInt64
	Notes    sql.NullString
}

type Egg struct {
	ID             int64
	NestID         int64
	Name           string
	Description    sql.NullString
	Features       sql.NullString // JSON array
	DockerImages   sql.NullString // JSON object, label -> image, in order
	FileDenylist   sql.NullString // JSON array
	ConfigFiles    sql.NullString
	ConfigStartup  sql.NullString
	ConfigStop     sql.NullString
	ConfigFrom     sql.NullInt64
	Startup        sql.NullString
	ScriptImage    string
	ScriptEntry    string
	ScriptInstall  sql.NullString
	CopyScriptFrom sql.NullInt64
	ForceOutgoing  bool
}

type EggVar struct {
	ID           int64
	EggID        int64
	Name         string
	Description  string
	Env          string
	Default      string
	UserViewable bool
	UserEditable bool
	Rules        sql.NullString
}

type Server struct {
	ID              int64
	ExternalID      sql.NullString
	UUID            string
	NodeID          int64
	Name            string
	Description     string
	Status          sql.NullString // NULL (installed), installing, install_failed, reinstall_failed, suspended, restoring_backup
	OwnerID         int64
	Memory, Swap    int64
	Disk, IO, CPU   int64
	Threads         sql.NullString
	OOMDisabled     bool
	AllocationID    int64
	EggID           int64
	Startup         string
	Image           string
	AllocationLimit sql.NullInt64
	DatabaseLimit   sql.NullInt64
	BackupLimit     int64
}

type ServerVar struct {
	ServerID   int64
	VariableID int64
	Value      string
}

type Subuser struct {
	UserID      int64
	ServerID    int64
	Permissions sql.NullString // JSON array
}

type Schedule struct {
	ID                                 int64
	ServerID                           int64
	Name                               string
	DayOfWeek, Month, DayOfMonth, Hour string
	Minute                             string
	Active, OnlyWhenOnline             bool
}

type Task struct {
	ScheduleID        int64
	Sequence          int
	Action            string
	Payload           string
	Offset            int
	ContinueOnFailure bool
}

type DBHost struct {
	ID           int64
	Name         string
	Host         string
	Port         int
	Username     string
	Password     string // Laravel-encrypted
	MaxDatabases sql.NullInt64
	NodeID       sql.NullInt64
}

type Database struct {
	ServerID int64
	HostID   int64
	Name     string
	Username string
	Remote   string
	Password string // Laravel-encrypted
}

// ErrPelican means the database is Pelican's (eggs without nests), which
// this version does not read.
var ErrPelican = errors.New("this looks like a Pelican database (no nests table); only Pterodactyl 1.x is supported for now")

// Read loads everything the import needs in one read-only transaction, so a
// panel that is still running cannot hand over a half-changed picture.
func Read(ctx context.Context, db *sql.DB) (*Source, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() // read-only; nothing to keep

	var hasNests int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='nests'`).Scan(&hasNests); err != nil {
		return nil, err
	}
	if hasNests == 0 {
		var hasEggs int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='eggs'`).Scan(&hasEggs)
		if hasEggs > 0 {
			return nil, ErrPelican
		}
		return nil, errors.New("no Pterodactyl tables found in this database")
	}

	s := &Source{Nests: map[int64]string{}}
	q := func(name, query string, scan func(*sql.Rows) error) error {
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return fmt.Errorf("read %s: %w", name, err)
			}
		}
		return rows.Err()
	}
	steps := []struct {
		name, query string
		scan        func(*sql.Rows) error
	}{
		{"users", `SELECT id,external_id,email,username,password,root_admin,use_totp,totp_secret FROM users ORDER BY id`, func(r *sql.Rows) error {
			var u User
			err := r.Scan(&u.ID, &u.ExternalID, &u.Email, &u.Username, &u.Password, &u.Admin, &u.UseTOTP, &u.TOTPSecret)
			s.Users = append(s.Users, u)
			return err
		}},
		{"locations", "SELECT id,short,`long` FROM locations ORDER BY id", func(r *sql.Rows) error {
			var l Location
			err := r.Scan(&l.ID, &l.Short, &l.Long)
			s.Locations = append(s.Locations, l)
			return err
		}},
		{"nodes", "SELECT id,location_id,name,memory,memory_overallocate,disk,disk_overallocate,maintenance_mode,daemonSFTP,daemonBase,fqdn FROM nodes ORDER BY id", func(r *sql.Rows) error {
			var n Node
			err := r.Scan(&n.ID, &n.LocationID, &n.Name, &n.Memory, &n.MemoryOverallocate, &n.Disk, &n.DiskOverallocate, &n.Maintenance, &n.SFTPPort, &n.Base, &n.FQDN)
			s.Nodes = append(s.Nodes, n)
			return err
		}},
		{"allocations", `SELECT id,node_id,ip,port,server_id,notes FROM allocations ORDER BY id`, func(r *sql.Rows) error {
			var a Allocation
			err := r.Scan(&a.ID, &a.NodeID, &a.IP, &a.Port, &a.ServerID, &a.Notes)
			s.Allocations = append(s.Allocations, a)
			return err
		}},
		{"nests", `SELECT id,name FROM nests`, func(r *sql.Rows) error {
			var id int64
			var name string
			err := r.Scan(&id, &name)
			s.Nests[id] = name
			return err
		}},
		{"eggs", `SELECT id,nest_id,name,description,features,docker_images,file_denylist,config_files,config_startup,config_stop,config_from,
			startup,script_container,script_entry,script_install,copy_script_from,force_outgoing_ip FROM eggs ORDER BY id`, func(r *sql.Rows) error {
			var e Egg
			err := r.Scan(&e.ID, &e.NestID, &e.Name, &e.Description, &e.Features, &e.DockerImages, &e.FileDenylist, &e.ConfigFiles, &e.ConfigStartup,
				&e.ConfigStop, &e.ConfigFrom, &e.Startup, &e.ScriptImage, &e.ScriptEntry, &e.ScriptInstall, &e.CopyScriptFrom, &e.ForceOutgoing)
			s.Eggs = append(s.Eggs, e)
			return err
		}},
		{"egg variables", `SELECT id,egg_id,name,description,env_variable,default_value,user_viewable,user_editable,rules FROM egg_variables ORDER BY egg_id,id`, func(r *sql.Rows) error {
			var v EggVar
			err := r.Scan(&v.ID, &v.EggID, &v.Name, &v.Description, &v.Env, &v.Default, &v.UserViewable, &v.UserEditable, &v.Rules)
			s.EggVars = append(s.EggVars, v)
			return err
		}},
		{"servers", `SELECT id,external_id,uuid,node_id,name,description,status,owner_id,memory,swap,disk,io,cpu,threads,oom_disabled,
			allocation_id,egg_id,startup,image,allocation_limit,database_limit,backup_limit FROM servers ORDER BY id`, func(r *sql.Rows) error {
			var v Server
			err := r.Scan(&v.ID, &v.ExternalID, &v.UUID, &v.NodeID, &v.Name, &v.Description, &v.Status, &v.OwnerID, &v.Memory, &v.Swap, &v.Disk, &v.IO, &v.CPU,
				&v.Threads, &v.OOMDisabled, &v.AllocationID, &v.EggID, &v.Startup, &v.Image, &v.AllocationLimit, &v.DatabaseLimit, &v.BackupLimit)
			s.Servers = append(s.Servers, v)
			return err
		}},
		{"server variables", `SELECT server_id,variable_id,variable_value FROM server_variables WHERE server_id IS NOT NULL`, func(r *sql.Rows) error {
			var v ServerVar
			err := r.Scan(&v.ServerID, &v.VariableID, &v.Value)
			s.ServerVars = append(s.ServerVars, v)
			return err
		}},
		{"subusers", `SELECT user_id,server_id,permissions FROM subusers ORDER BY id`, func(r *sql.Rows) error {
			var v Subuser
			err := r.Scan(&v.UserID, &v.ServerID, &v.Permissions)
			s.Subusers = append(s.Subusers, v)
			return err
		}},
		{"schedules", `SELECT id,server_id,name,cron_day_of_week,cron_month,cron_day_of_month,cron_hour,cron_minute,is_active,only_when_online FROM schedules ORDER BY id`, func(r *sql.Rows) error {
			var v Schedule
			err := r.Scan(&v.ID, &v.ServerID, &v.Name, &v.DayOfWeek, &v.Month, &v.DayOfMonth, &v.Hour, &v.Minute, &v.Active, &v.OnlyWhenOnline)
			s.Schedules = append(s.Schedules, v)
			return err
		}},
		{"tasks", `SELECT schedule_id,sequence_id,action,payload,time_offset,continue_on_failure FROM tasks ORDER BY schedule_id,sequence_id`, func(r *sql.Rows) error {
			var v Task
			err := r.Scan(&v.ScheduleID, &v.Sequence, &v.Action, &v.Payload, &v.Offset, &v.ContinueOnFailure)
			s.Tasks = append(s.Tasks, v)
			return err
		}},
		{"database hosts", `SELECT id,name,host,port,username,password,max_databases,node_id FROM database_hosts ORDER BY id`, func(r *sql.Rows) error {
			var v DBHost
			err := r.Scan(&v.ID, &v.Name, &v.Host, &v.Port, &v.Username, &v.Password, &v.MaxDatabases, &v.NodeID)
			s.DBHosts = append(s.DBHosts, v)
			return err
		}},
		{"databases", "SELECT server_id,database_host_id,`database`,username,remote,password FROM `databases` ORDER BY id", func(r *sql.Rows) error {
			var v Database
			err := r.Scan(&v.ServerID, &v.HostID, &v.Name, &v.Username, &v.Remote, &v.Password)
			s.Databases = append(s.Databases, v)
			return err
		}},
	}
	for _, st := range steps {
		if err := q(st.name, st.query, st.scan); err != nil {
			return nil, err
		}
	}
	for _, c := range []struct {
		dst   *int
		query string
	}{
		{&s.Backups, `SELECT COUNT(*) FROM backups WHERE deleted_at IS NULL`},
		{&s.APIKeys, `SELECT COUNT(*) FROM api_keys`},
		{&s.Mounts, `SELECT COUNT(*) FROM mounts`},
	} {
		if err := tx.QueryRowContext(ctx, c.query).Scan(c.dst); err != nil {
			return nil, fmt.Errorf("count: %w", err)
		}
	}
	return s, nil
}
