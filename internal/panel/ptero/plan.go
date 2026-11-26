package ptero

import (
	"encoding/base32"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// Level says how much a note matters.
type Level int

const (
	// Info: not imported, or imported differently, with no effect on what
	// people can do.
	Info Level = iota
	// Changed: imported, but something behaves differently afterwards (a
	// permission is narrower, a limit was raised, 2FA was turned off).
	Changed
	// Blocking: the import cannot be applied until this is fixed.
	Blocking
)

// Note is one line of the report.
type Note struct {
	Level   Level
	Subject string // e.g. `server "Survival"`; empty for the whole import
	Msg     string
}

// Options change how the plan is made.
type Options struct {
	// AppKey decrypts two-factor secrets and database passwords. Without it
	// two-factor is turned off and database hosts are skipped.
	AppKey AppKey
	// UnlimitedMemoryMB is the memory limit given to servers that had none
	// (GamePanel always sets one).
	UnlimitedMemoryMB int64
}

// DefaultUnlimitedMemoryMB is Options.UnlimitedMemoryMB when it is 0.
const DefaultUnlimitedMemoryMB = 4096

// In the planned records below, ids that point at other records (owner,
// node, egg, host) are still Pterodactyl's; applying the plan maps them to
// the new GamePanel ids.

type PlanUser struct {
	PteroID      int64
	Email        string
	Admin        bool
	PasswordHash string // bcrypt, upgraded at the first sign-in
	TOTPSecret   string // base32, "" = two-factor off
	ExternalID   string
}

type PlanLocation struct {
	PteroID int64
	Short   string
	Long    string
}

type PlanNode struct {
	PteroID         int64
	LocationID      int64 // Pterodactyl's location id, kept as it is
	Name            string
	PortStart       int
	PortEnd         int
	MemoryMB        int64
	DiskMB          int64
	OverallocatePct int64
	SFTPHost        string
	SFTPPort        int
	WingsDir        string // where Wings kept server files on this node
}

type PlanEgg struct {
	PteroID int64
	Egg     store.Egg
}

type PlanPort struct {
	Port  int
	Notes string
}

type PlanServer struct {
	PteroID int64
	Server  store.Server // OwnerID, NodeID and EggID are Pterodactyl ids
	Extra   []PlanPort   // ports beyond the primary one
}

type PlanSubuser struct {
	ServerUUID  string
	UserPteroID int64
	Permissions []string
}

type PlanDBHost struct {
	PteroID  int64
	Host     store.DatabaseHost // NodeID is a Pterodactyl id, 0 = any
	Password string
}

type PlanDatabase struct {
	ServerUUID  string
	HostPteroID int64
	Name        string
	Username    string
	Remote      string
	Password    string
}

// Plan is everything an import would write, and why anything is left out.
type Plan struct {
	Users     []PlanUser
	Locations []PlanLocation
	Nodes     []PlanNode
	Eggs      []PlanEgg
	Servers   []PlanServer
	Subusers  []PlanSubuser
	Schedules []store.Schedule // ServerUUID set; CreatedBy 0 (runs as owner)
	DBHosts   []PlanDBHost
	Databases []PlanDatabase
	APIKeys   []store.ImportAPIKey // UserKey is a Pterodactyl user id
	Backups   []store.ImportBackup
	Notes     []Note
}

// Blocked reports whether any note stops the plan from being applied.
func (p *Plan) Blocked() bool {
	return slices.ContainsFunc(p.Notes, func(n Note) bool { return n.Level == Blocking })
}

func (p *Plan) note(l Level, subject, format string, args ...any) {
	p.Notes = append(p.Notes, Note{Level: l, Subject: subject, Msg: fmt.Sprintf(format, args...)})
}

// Build maps a Pterodactyl panel onto GamePanel. It only reads src.
func Build(src *Source, opt Options) *Plan {
	if opt.UnlimitedMemoryMB == 0 {
		opt.UnlimitedMemoryMB = DefaultUnlimitedMemoryMB
	}
	p := &Plan{}
	b := builder{src: src, opt: opt, p: p}
	b.planUsers()
	b.planLocations()
	b.planNodes()
	b.planEggs()
	b.planServers()
	b.planSubusers()
	b.planSchedules()
	b.planDatabases()
	b.planAPIKeys()
	b.planBackups()
	b.leftOut()
	return p
}

type builder struct {
	src *Source
	opt Options
	p   *Plan

	users    map[int64]bool
	nodes    map[int64]*PlanNode
	eggs     map[int64]*store.Egg
	eggVars  map[int64]EggVar // variable id -> variable
	servers  map[int64]*PlanServer
	srvOwner map[int64]int64
}

func quote(kind, name string) string { return fmt.Sprintf("%s %q", kind, name) }

func (b *builder) planUsers() {
	b.users = map[int64]bool{}
	seen := map[string]int64{}
	var noKey, dropped []string
	usernames := 0
	for _, u := range b.src.Users {
		subj := quote("user", u.Email)
		email := strings.ToLower(strings.TrimSpace(u.Email))
		if prev, dup := seen[email]; dup {
			b.p.note(Blocking, subj, "has the same email as user #%d once lower-cased; change one of them in Pterodactyl", prev)
			continue
		}
		seen[email] = u.ID
		pu := PlanUser{PteroID: u.ID, Email: email, Admin: u.Admin, PasswordHash: u.Password, ExternalID: u.ExternalID.String}
		if !strings.HasPrefix(u.Password, "$2y$") && !strings.HasPrefix(u.Password, "$2a$") && !strings.HasPrefix(u.Password, "$2b$") {
			pu.PasswordHash = ""
			b.p.note(Changed, subj, "password hash is not bcrypt; they will need a password reset link (panel reset-link)")
		}
		if u.UseTOTP {
			if b.opt.AppKey == nil {
				noKey = append(noKey, email)
			} else if secret, err := b.opt.AppKey.Decrypt(u.TOTPSecret.String); err == nil && validTOTPSecret(secret) {
				pu.TOTPSecret = secret
			} else {
				dropped = append(dropped, email)
			}
		}
		if u.Username != "" {
			usernames++
		}
		b.p.Users = append(b.p.Users, pu)
		b.users[u.ID] = true
	}
	if kept := len(slices.DeleteFunc(slices.Clone(b.p.Users), func(u PlanUser) bool { return u.TOTPSecret == "" })); kept > 0 {
		b.p.note(Info, "", "%d user(s) keep two-factor; their Pterodactyl recovery codes do not carry over, so they should make new ones (Account → Security)", kept)
	}
	if len(noKey) > 0 {
		b.p.note(Changed, "", "two-factor is turned off for %d user(s) because no --app-key was given: %s", len(noKey), strings.Join(noKey, ", "))
	}
	if len(dropped) > 0 {
		b.p.note(Changed, "", "two-factor secret could not be decrypted (wrong APP_KEY?), so it is turned off for: %s", strings.Join(dropped, ", "))
	}
	if usernames > 0 {
		b.p.note(Info, "", "usernames and first/last names are not kept; people sign in with their email")
	}
}

func validTOTPSecret(s string) bool {
	s = strings.TrimRight(strings.ToUpper(s), "=")
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	return err == nil && len(raw) >= 10
}

// planLocations keeps Pterodactyl's locations with their ids, so a billing
// module's products still name the right ones.
func (b *builder) planLocations() {
	seen := map[string]bool{}
	for _, l := range b.src.Locations {
		short := truncRunes(strings.TrimSpace(l.Short), 60)
		for i := 2; seen[strings.ToLower(short)]; i++ {
			suffix := fmt.Sprintf("-%d", i)
			short = truncRunes(strings.TrimSpace(l.Short), 60-len(suffix)) + suffix
		}
		if short != l.Short {
			b.p.note(Info, quote("location", l.Short), "renamed to %q (location codes are unique, case-insensitively, and at most 60 characters)", short)
		}
		seen[strings.ToLower(short)] = true
		b.p.Locations = append(b.p.Locations, PlanLocation{PteroID: l.ID, Short: short, Long: truncRunes(strings.TrimSpace(l.Long.String), 191)})
	}
}

func (b *builder) planNodes() {
	b.nodes = map[int64]*PlanNode{}
	names := map[string]bool{}
	ports := map[int64][]int{}
	for _, a := range b.src.Allocations {
		ports[a.NodeID] = append(ports[a.NodeID], a.Port)
	}
	for _, n := range b.src.Nodes {
		subj := quote("node", n.Name)
		base := truncRunes(strings.TrimSpace(n.Name), 64)
		name := base
		for i := 2; names[strings.ToLower(name)]; i++ {
			suffix := fmt.Sprintf(" (%d)", i)
			name = truncRunes(base, 64-len(suffix)) + suffix
		}
		if name != n.Name {
			b.p.note(Info, subj, "renamed to %q (GamePanel node names are unique and at most 64 characters)", name)
		}
		names[strings.ToLower(name)] = true
		pn := PlanNode{PteroID: n.ID, LocationID: n.LocationID, Name: name, SFTPHost: n.FQDN, SFTPPort: n.SFTPPort, WingsDir: n.Base, PortStart: 25565, PortEnd: 25665}
		if ps := ports[n.ID]; len(ps) > 0 {
			pn.PortStart, pn.PortEnd = slices.Min(ps), slices.Max(ps)
		}
		// Pterodactyl uses -1 to switch off the capacity check.
		if n.MemoryOverallocate >= 0 {
			pn.MemoryMB, pn.DiskMB = n.Memory, n.Disk
			pn.OverallocatePct = min(n.MemoryOverallocate, 1000)
			if n.DiskOverallocate != n.MemoryOverallocate {
				if n.DiskOverallocate < 0 {
					pn.DiskMB = 0
				}
				b.p.note(Info, subj, "has different memory (%d%%) and disk (%d%%) overallocation; GamePanel has one setting, set to %d%%", n.MemoryOverallocate, n.DiskOverallocate, pn.OverallocatePct)
			}
		}
		if n.Maintenance {
			b.p.note(Info, subj, "is in maintenance mode, which GamePanel does not have; its servers will be usable")
		}
		b.p.Nodes = append(b.p.Nodes, pn)
		b.nodes[n.ID] = &b.p.Nodes[len(b.p.Nodes)-1]
	}
	// Re-point the map at the final slice (append may have moved it).
	for i := range b.p.Nodes {
		b.nodes[b.p.Nodes[i].PteroID] = &b.p.Nodes[i]
	}
	if len(b.p.Nodes) > 0 {
		b.p.note(Info, "", "each node gets a new enrollment token; Wings is replaced by the GamePanel daemon (see Next steps)")
	}
}

// truncRunes cuts s to at most n characters, never inside a character.
func truncRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// ptdl is the PTDL egg JSON that api.ImportPTDL reads.
type ptdl struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	DockerImages json.RawMessage `json:"docker_images,omitempty"`
	Images       []string        `json:"images,omitempty"`
	Startup      string          `json:"startup"`
	Features     json.RawMessage `json:"features,omitempty"`
	Config       struct {
		Stop    string `json:"stop"`
		Startup string `json:"startup"`
		Files   string `json:"files"`
	} `json:"config"`
	Scripts struct {
		Installation struct {
			Script     string `json:"script"`
			Container  string `json:"container"`
			Entrypoint string `json:"entrypoint"`
		} `json:"installation"`
	} `json:"scripts"`
	Variables []ptdlVar `json:"variables"`
}

type ptdlVar struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Env          string `json:"env_variable"`
	Default      string `json:"default_value"`
	UserViewable bool   `json:"user_viewable"`
	UserEditable bool   `json:"user_editable"`
	Rules        string `json:"rules"`
}

// hasRules reports whether a JSON config value says anything ({} and []
// and "" do not).
func hasRules(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && s != "{}" && s != "[]" && s != "null"
}

func (b *builder) planEggs() {
	b.eggs = map[int64]*store.Egg{}
	b.eggVars = map[int64]EggVar{}
	byID := map[int64]Egg{}
	for _, e := range b.src.Eggs {
		byID[e.ID] = e
	}
	vars := map[int64][]EggVar{}
	for _, v := range b.src.EggVars {
		vars[v.EggID] = append(vars[v.EggID], v)
		b.eggVars[v.ID] = v
	}
	used := map[int64]bool{}
	for _, s := range b.src.Servers {
		used[s.EggID] = true
	}
	nameCount := map[string]int{}
	for _, e := range b.src.Eggs {
		nameCount[strings.ToLower(e.Name)]++
	}
	var xmlFiles []string
	for _, e := range b.src.Eggs {
		subj := quote("egg", e.Name)
		// config_from and copy_script_from inherit from another egg where
		// this one leaves the value empty, as Pterodactyl does.
		cfg := e
		if p, ok := byID[e.ConfigFrom.Int64]; ok && e.ConfigFrom.Valid {
			if !cfg.ConfigStop.Valid || cfg.ConfigStop.String == "" {
				cfg.ConfigStop = p.ConfigStop
			}
			if !hasRules(cfg.ConfigStartup.String) {
				cfg.ConfigStartup = p.ConfigStartup
			}
			if !hasRules(cfg.ConfigFiles.String) {
				cfg.ConfigFiles = p.ConfigFiles
			}
		}
		script := e
		if p, ok := byID[e.CopyScriptFrom.Int64]; ok && e.CopyScriptFrom.Valid {
			script = p
		}
		var j ptdl
		j.Name = e.Name
		if nameCount[strings.ToLower(e.Name)] > 1 {
			j.Name = fmt.Sprintf("%s (%s)", e.Name, b.src.Nests[e.NestID])
		}
		j.Description = e.Description.String
		img := strings.TrimSpace(e.DockerImages.String)
		if strings.HasPrefix(img, "[") {
			_ = json.Unmarshal([]byte(img), &j.Images)
		} else if img != "" {
			j.DockerImages = json.RawMessage(img)
		}
		j.Startup = e.Startup.String
		if f := strings.TrimSpace(e.Features.String); strings.HasPrefix(f, "[") {
			j.Features = json.RawMessage(f)
		}
		j.Config.Stop = cfg.ConfigStop.String
		j.Config.Startup = cfg.ConfigStartup.String
		j.Config.Files = cfg.ConfigFiles.String
		j.Scripts.Installation.Script = script.ScriptInstall.String
		j.Scripts.Installation.Container = script.ScriptImage
		j.Scripts.Installation.Entrypoint = script.ScriptEntry
		j.Variables = []ptdlVar{}
		for _, v := range vars[e.ID] {
			j.Variables = append(j.Variables, ptdlVar{
				Name: v.Name, Description: v.Description, Env: v.Env, Default: v.Default,
				UserViewable: v.UserViewable, UserEditable: v.UserEditable, Rules: v.Rules.String,
			})
		}
		raw, err := json.Marshal(j)
		if err != nil {
			b.p.note(Blocking, subj, "could not be read: %v", err)
			continue
		}
		egg, err := api.ImportPTDL(raw)
		if err != nil {
			if used[e.ID] {
				b.p.note(Blocking, subj, "cannot be imported (%v) and servers use it; fix the egg in Pterodactyl", err)
			} else {
				b.p.note(Changed, subj, "skipped: %v (no server uses it)", err)
			}
			continue
		}
		if j.Name != e.Name {
			b.p.note(Info, subj, "renamed to %q: another nest has an egg with the same name", j.Name)
		}
		for _, f := range egg.ConfigFiles {
			if f.Parser == "xml" {
				xmlFiles = append(xmlFiles, e.Name)
				break
			}
		}
		if hasRules(e.FileDenylist.String) {
			b.p.note(Changed, subj, "has a file denylist (%s); GamePanel does not enforce one yet, so owners can edit those files", strings.TrimSpace(e.FileDenylist.String))
		}
		if e.ForceOutgoing {
			b.p.note(Info, subj, "forces outgoing traffic through the allocation's IP; GamePanel uses the node's default route")
		}
		b.p.Eggs = append(b.p.Eggs, PlanEgg{PteroID: e.ID, Egg: egg})
	}
	for i := range b.p.Eggs {
		b.eggs[b.p.Eggs[i].PteroID] = &b.p.Eggs[i].Egg
	}
	if len(xmlFiles) > 0 {
		b.p.note(Changed, "", "%d egg(s) edit XML config files before each start, which GamePanel does not do yet (their other config files are edited). Imported servers keep their ports, so files already written stay right; new servers need the port set by hand: %s",
			len(xmlFiles), strings.Join(xmlFiles, ", "))
	}
}

// eggStartup is what Pterodactyl stores as the egg's startup, to tell a
// server's own startup from the default.
func (b *builder) eggStartup(eggID int64) string {
	for _, e := range b.src.Eggs {
		if e.ID == eggID {
			return e.Startup.String
		}
	}
	return ""
}

func (b *builder) planServers() {
	b.servers = map[int64]*PlanServer{}
	b.srvOwner = map[int64]int64{}
	allocByID := map[int64]Allocation{}
	extra := map[int64][]Allocation{}
	for _, a := range b.src.Allocations {
		allocByID[a.ID] = a
		if a.ServerID.Valid {
			extra[a.ServerID.Int64] = append(extra[a.ServerID.Int64], a)
		}
	}
	vals := map[int64]map[string]string{}
	for _, v := range b.src.ServerVars {
		ev, ok := b.eggVars[v.VariableID]
		if !ok {
			continue
		}
		if vals[v.ServerID] == nil {
			vals[v.ServerID] = map[string]string{}
		}
		vals[v.ServerID][ev.Env] = v.Value
	}
	// Primary ports first, so a duplicate extra port never takes a port
	// another server needs as its primary.
	taken := map[[2]int64]string{}
	for _, s := range b.src.Servers {
		if a, ok := allocByID[s.AllocationID]; ok {
			k := [2]int64{a.NodeID, int64(a.Port)}
			if other, dup := taken[k]; dup {
				b.p.note(Blocking, quote("server", s.Name), "uses port %d on the same node as server %q (on another IP); GamePanel listens on all addresses, so one of them needs another port", a.Port, other)
				continue
			}
			taken[k] = s.Name
		}
	}
	specificIP, oomOff := 0, 0
	for _, s := range b.src.Servers {
		subj := quote("server", s.Name)
		ps := PlanServer{PteroID: s.ID}
		v := &ps.Server
		v.UUID, v.Name, v.Description = s.UUID, s.Name, s.Description
		v.OwnerID, v.NodeID, v.EggID = s.OwnerID, s.NodeID, s.EggID
		v.ExternalID = s.ExternalID.String
		ok := true
		if !b.users[s.OwnerID] {
			b.p.note(Blocking, subj, "its owner (user #%d) is not imported", s.OwnerID)
			ok = false
		}
		if b.nodes[s.NodeID] == nil {
			b.p.note(Blocking, subj, "its node (#%d) does not exist", s.NodeID)
			ok = false
		}
		egg := b.eggs[s.EggID]
		if egg == nil {
			ok = false // the egg's own note says why
		}
		primary, found := allocByID[s.AllocationID]
		if !found {
			b.p.note(Blocking, subj, "its primary allocation (#%d) does not exist", s.AllocationID)
			ok = false
		}
		if !ok {
			continue
		}
		v.Port = primary.Port
		if primary.IP != "0.0.0.0" && primary.IP != "" && primary.IP != "::" {
			specificIP++
		}
		for _, a := range extra[s.ID] {
			if a.ID == s.AllocationID {
				continue
			}
			k := [2]int64{a.NodeID, int64(a.Port)}
			if other, dup := taken[k]; dup {
				b.p.note(Changed, subj, "extra port %s:%d is dropped: port %d is already used by server %q on another IP", a.IP, a.Port, a.Port, other)
				continue
			}
			taken[k] = s.Name
			ps.Extra = append(ps.Extra, PlanPort{Port: a.Port, Notes: a.Notes.String})
		}

		b.defaults(subj, s, egg, vals[s.ID], v)

		b.limits(subj, s, &v.Limits)
		if err := v.Normalize(); err != nil {
			b.p.note(Blocking, subj, "limits are not valid in GamePanel: %v", err)
			continue
		}

		b.status(subj, s.Status.String, v)
		if s.OOMDisabled {
			oomOff++
		}
		b.p.Servers = append(b.p.Servers, ps)
		b.srvOwner[s.ID] = s.OwnerID
	}
	for i := range b.p.Servers {
		b.servers[b.p.Servers[i].PteroID] = &b.p.Servers[i]
	}
	if oomOff > 0 {
		// Pterodactyl turns the OOM killer off by default, so this is most
		// servers: one line, not one per server.
		b.p.note(Info, "", "%d server(s) had the OOM killer disabled (Pterodactyl's default); in GamePanel a server that runs out of memory is stopped and restarted once", oomOff)
	}
	if specificIP > 0 {
		b.p.note(Info, "", "%d server(s) were bound to a specific IP; GamePanel publishes ports on all of the node's addresses", specificIP)
	}
	b.capacity()
}

// status maps Pterodactyl's server status (NULL once installed).
func (b *builder) status(subj, status string, v *store.Server) {
	v.InstallStatus = store.InstallInstalled
	switch status {
	case "":
	case "suspended":
		v.Suspended = true
	case "installing":
		v.InstallStatus = store.InstallInstalling
		b.p.note(Changed, subj, "was still installing; reinstall it after the import")
	case "install_failed", "reinstall_failed":
		v.InstallStatus = store.InstallFailed
		b.p.note(Info, subj, "its install had failed; it is imported as failed, reinstall it to retry")
	case "restoring_backup":
		b.p.note(Changed, subj, "was restoring a backup; check its files after the import")
	default:
		b.p.note(Info, subj, "has status %q, imported as installed", status)
	}
}

// defaults stores the server's startup and image as "" (the egg's own) when
// they are the egg's defaults, and the server's variable values.
func (b *builder) defaults(subj string, s Server, egg *store.Egg, vals map[string]string, v *store.Server) {
	if s.Startup != b.eggStartup(s.EggID) {
		v.Startup = s.Startup
		b.p.note(Info, subj, "keeps its own startup command")
	}
	if s.Image != egg.DockerImage {
		v.Image = s.Image
		if !slices.ContainsFunc(egg.DockerImages, func(i store.EggImage) bool { return i.Image == s.Image }) {
			b.p.note(Info, subj, "keeps its custom image %s", s.Image)
		}
	}
	v.Env = map[string]string{}
	for _, ev := range egg.Variables {
		if val, set := vals[ev.Env]; set {
			v.Env[ev.Env] = val
		}
	}
}

func (b *builder) limits(subj string, s Server, l *store.Limits) {
	switch {
	case s.Memory == 0:
		l.MemoryMB = b.opt.UnlimitedMemoryMB
		b.p.note(Changed, subj, "had no memory limit; it gets %d MB (change with --unlimited-memory)", l.MemoryMB)
	case s.Memory < 64:
		l.MemoryMB = 64
		b.p.note(Changed, subj, "memory raised from %d to 64 MB, GamePanel's minimum", s.Memory)
	default:
		l.MemoryMB = s.Memory
	}
	l.SwapMB = max(s.Swap, -1)
	l.CPUPct = min(s.CPU, 6400)
	l.DiskMB = s.Disk
	// Pterodactyl's io (block IO weight, 500 by default) is left unset:
	// current Wings no longer applies it, and a kernel without the io.weight
	// controller would refuse to start a container that asks for one.
	if t := strings.ReplaceAll(s.Threads.String, " ", ""); t != "" {
		l.CPUSet = t
	}
	l.BackupLimit = min(s.BackupLimit, 100)
	if s.BackupLimit == 0 {
		l.BackupLimit = 1
		b.p.note(Changed, subj, "allowed no backups; GamePanel needs at least 1, so the owner can now keep 1")
	}
	// Pterodactyl's allocation limit counts the primary port too; NULL means
	// the owner cannot add any.
	if s.AllocationLimit.Valid {
		l.PortLimit = min(max(s.AllocationLimit.Int64-1, 0), 100)
	}
	l.DatabaseLimit = min(s.DatabaseLimit.Int64, 100)
}

// capacity warns when a node's servers add up to more than the node allows,
// which GamePanel would refuse for a new server.
func (b *builder) capacity() {
	type sum struct{ mem, disk int64 }
	got := map[int64]*sum{}
	for _, s := range b.p.Servers {
		if got[s.Server.NodeID] == nil {
			got[s.Server.NodeID] = &sum{}
		}
		got[s.Server.NodeID].mem += s.Server.MemoryMB
		got[s.Server.NodeID].disk += s.Server.DiskMB
	}
	for _, n := range b.p.Nodes {
		g := got[n.PteroID]
		if g == nil {
			continue
		}
		f := float64(100+n.OverallocatePct) / 100
		if n.MemoryMB > 0 && float64(g.mem) > float64(n.MemoryMB)*f {
			b.p.note(Changed, quote("node", n.Name), "its servers need %d MB of memory, more than the %d MB (+%d%%) it allows; imported anyway, but new servers will be refused until you raise its capacity", g.mem, n.MemoryMB, n.OverallocatePct)
		}
		if n.DiskMB > 0 && float64(g.disk) > float64(n.DiskMB)*f {
			b.p.note(Changed, quote("node", n.Name), "its servers need %d MB of disk, more than the %d MB (+%d%%) it allows", g.disk, n.DiskMB, n.OverallocatePct)
		}
	}
}

// permMap maps Pterodactyl subuser permissions to GamePanel's. Power needs
// start, stop and restart together, and console.send needs control.console:
// where GamePanel's permission is broader, it is only granted when
// Pterodactyl granted all of what it covers.
var permMap = map[string][]string{
	"websocket.connect":      {"console.read"},
	"control.console":        {"console.read", "console.send"},
	"file.read":              {"files.read"},
	"file.read-content":      {"files.read"},
	"backup.read":            {"backups.read"},
	"backup.create":          {"backups.create"},
	"backup.restore":         {"backups.restore"},
	"backup.delete":          {"backups.delete"},
	"backup.download":        {"backups.download"},
	"allocation.read":        {"network.read"},
	"database.read":          {"databases.read"},
	"startup.read":           {"startup.read"},
	"startup.update":         {"startup.update"},
	"startup.docker-image":   {"startup.docker-image"},
	"settings.rename":        {"settings.rename"},
	"settings.reinstall":     {"settings.reinstall"},
	"activity.read":          {"activity.read"},
	"database.view_password": {"databases.read"},
}

// permGroups are GamePanel permissions granted only when every listed
// Pterodactyl permission was.
var permGroups = []struct {
	need  []string
	grant string
}{
	{[]string{"control.start", "control.stop", "control.restart"}, "power"},
	{[]string{"file.create", "file.update", "file.delete"}, "files.write"},
	{[]string{"schedule.create", "schedule.update", "schedule.delete"}, "schedules.manage"},
	{[]string{"allocation.create", "allocation.update", "allocation.delete"}, "network.manage"},
	{[]string{"database.create", "database.update", "database.delete"}, "databases.manage"},
}

// mapPermissions returns GamePanel's permissions and the Pterodactyl ones
// that had no equivalent.
func mapPermissions(in []string) (out, dropped []string) {
	have := map[string]bool{}
	for _, p := range in {
		have[p] = true
	}
	used := map[string]bool{}
	grant := map[string]bool{}
	for p := range have {
		if g, ok := permMap[p]; ok {
			for _, x := range g {
				grant[x] = true
			}
			used[p] = true
		}
	}
	for _, g := range permGroups {
		all := true
		for _, n := range g.need {
			all = all && have[n]
		}
		if all {
			grant[g.grant] = true
			for _, n := range g.need {
				used[n] = true
			}
		}
	}
	// SFTP follows the file permissions in GamePanel; nothing to drop.
	used["file.sftp"] = true
	// file.archive (compress, extract) is part of files.write.
	if grant["files.write"] {
		used["file.archive"] = true
	}
	for p := range have {
		if !used[p] {
			dropped = append(dropped, p)
		}
	}
	for p := range grant {
		out = append(out, p)
	}
	sort.Strings(out)
	sort.Strings(dropped)
	return out, dropped
}

func (b *builder) planSubusers() {
	emails := map[int64]string{}
	for _, u := range b.p.Users {
		emails[u.PteroID] = u.Email
	}
	for _, su := range b.src.Subusers {
		srv := b.servers[su.ServerID]
		if srv == nil || !b.users[su.UserID] {
			continue
		}
		subj := fmt.Sprintf("subuser %s on %q", emails[su.UserID], srv.Server.Name)
		if b.srvOwner[su.ServerID] == su.UserID {
			b.p.note(Info, subj, "is the server's owner; skipped")
			continue
		}
		var in []string
		if err := json.Unmarshal([]byte(su.Permissions.String), &in); err != nil {
			b.p.note(Changed, subj, "has unreadable permissions; skipped")
			continue
		}
		perms, dropped := mapPermissions(in)
		if len(dropped) > 0 {
			b.p.note(Changed, subj, "loses %s (no GamePanel equivalent, or only part of a broader permission was granted)", strings.Join(dropped, ", "))
		}
		if _, err := store.ValidatePermissions(perms); err != nil {
			b.p.note(Changed, subj, "has no permission left in GamePanel; skipped")
			continue
		}
		b.p.Subusers = append(b.p.Subusers, PlanSubuser{ServerUUID: srv.Server.UUID, UserPteroID: su.UserID, Permissions: perms})
	}
}

func (b *builder) planSchedules() {
	tasks := map[int64][]Task{}
	for _, t := range b.src.Tasks {
		tasks[t.ScheduleID] = append(tasks[t.ScheduleID], t)
	}
	for _, sc := range b.src.Schedules {
		srv := b.servers[sc.ServerID]
		if srv == nil {
			continue
		}
		subj := fmt.Sprintf("schedule %q on %q", sc.Name, srv.Server.Name)
		cron := strings.Join([]string{sc.Minute, sc.Hour, sc.DayOfMonth, sc.Month, sc.DayOfWeek}, " ")
		if _, err := store.NextRun(cron, time.Now()); err != nil {
			b.p.note(Changed, subj, "skipped: cron %q is not understood (%v)", cron, err)
			continue
		}
		v := store.Schedule{ServerUUID: srv.Server.UUID, Name: sc.Name, Cron: cron, Enabled: sc.Active, OnlyWhenOnline: sc.OnlyWhenOnline}
		ok := true
		for _, t := range tasks[sc.ID] {
			st := store.Task{Seq: t.Sequence, Action: t.Action, Payload: t.Payload, OffsetSeconds: t.Offset, ContinueOnFailure: t.ContinueOnFailure}
			if st.OffsetSeconds > 900 {
				st.OffsetSeconds = 900
				b.p.note(Changed, subj, "a task waited %d s; GamePanel allows at most 900, so it waits 900", t.Offset)
			}
			if err := store.ValidateTask(st); err != nil {
				b.p.note(Changed, subj, "skipped: task %d (%s) is not valid in GamePanel: %v", t.Sequence, t.Action, err)
				ok = false
				break
			}
			v.Tasks = append(v.Tasks, st)
		}
		if len(v.Tasks) > store.MaxTasksPerSchedule {
			b.p.note(Changed, subj, "skipped: it has %d tasks, GamePanel allows %d", len(v.Tasks), store.MaxTasksPerSchedule)
			ok = false
		}
		if ok {
			b.p.Schedules = append(b.p.Schedules, v)
		}
	}
	if len(b.p.Schedules) > 0 {
		b.p.note(Info, "", "schedules run in UTC and with the server owner's permissions")
	}
}

func (b *builder) planDatabases() {
	hosts := map[int64]bool{}
	for _, h := range b.src.DBHosts {
		subj := quote("database host", h.Name)
		if b.opt.AppKey == nil {
			b.p.note(Changed, subj, "skipped with its databases: its password is encrypted; give --app-key to import it")
			continue
		}
		pw, err := b.opt.AppKey.Decrypt(h.Password)
		if err != nil {
			b.p.note(Changed, subj, "skipped with its databases: password could not be decrypted (wrong APP_KEY?)")
			continue
		}
		dh := store.DatabaseHost{Name: h.Name, Host: h.Host, Port: h.Port, Username: h.Username, MaxDatabases: h.MaxDatabases.Int64}
		if h.NodeID.Valid && b.nodes[h.NodeID.Int64] != nil {
			dh.NodeID = h.NodeID.Int64
		}
		b.p.DBHosts = append(b.p.DBHosts, PlanDBHost{PteroID: h.ID, Host: dh, Password: pw})
		hosts[h.ID] = true
	}
	for _, d := range b.src.Databases {
		srv := b.servers[d.ServerID]
		if srv == nil || !hosts[d.HostID] {
			continue
		}
		pw, err := b.opt.AppKey.Decrypt(d.Password)
		if err != nil {
			b.p.note(Changed, fmt.Sprintf("database %q on %q", d.Name, srv.Server.Name), "skipped: password could not be decrypted")
			continue
		}
		b.p.Databases = append(b.p.Databases, PlanDatabase{ServerUUID: srv.Server.UUID, HostPteroID: d.HostID, Name: d.Name, Username: d.Username, Remote: d.Remote, Password: pw})
	}
}

func (b *builder) leftOut() {
	if n := b.src.Mounts; n > 0 {
		b.p.note(Changed, "", "%d mount(s) are not supported and not imported", n)
	}
	if len(b.p.Servers) > 0 {
		b.p.note(Changed, "", "SFTP logins change: the user name is <email>.<first 8 characters of the server id> and the password is an API key (Account → API keys), not the Pterodactyl user name and password")
	}
	if len(b.p.Servers) > 0 {
		b.p.note(Info, "", "block IO weight (io) is not imported: Wings does not apply it either; set it per server in GamePanel if you need it")
	}
	b.p.note(Info, "", "activity logs are not imported")
}

// Pterodactyl's api_keys.key_type values that people make.
const (
	keyAccount     = 1
	keyApplication = 2
)

// planAPIKeys keeps API keys working: the key a client sends is the
// identifier and the decrypted secret, and GamePanel stores its hash. A key
// GamePanel could not limit the same way (to IP addresses, or an
// application key to part of the API) is not imported, so no key can do
// more than it could.
func (b *builder) planAPIKeys() {
	admin := map[int64]bool{}
	for _, u := range b.p.Users {
		admin[u.PteroID] = u.Admin
	}
	var noKey, expired, other int
	for _, k := range b.src.APIKeys {
		if !b.users[k.UserID] {
			continue
		}
		id := k.Identifier.String
		subj := fmt.Sprintf("API key %s", id)
		if k.Type != keyAccount && k.Type != keyApplication {
			other++
			continue
		}
		if k.ExpiresAt != 0 && k.ExpiresAt <= time.Now().Unix() {
			expired++
			continue
		}
		if b.opt.AppKey == nil {
			noKey++
			continue
		}
		if ips := strings.TrimSpace(k.AllowedIPs.String); ips != "" && ips != "[]" && ips != "null" {
			b.p.note(Changed, subj, "not imported: it only works from some IP addresses, which a GamePanel key cannot be limited to; its owner makes a new one")
			continue
		}
		if k.Type == keyApplication {
			if !admin[k.UserID] {
				b.p.note(Changed, subj, "not imported: an application key of an account that is not an administrator")
				continue
			}
			if slices.ContainsFunc(k.Perms[:], func(p int) bool { return p != 3 }) {
				b.p.note(Changed, subj, "not imported: an application key limited to part of the API; a GamePanel administrator's key can do everything, so make a new one where full access is meant")
				continue
			}
		}
		secret, err := b.opt.AppKey.Decrypt(k.Token)
		tok := id + secret
		if err != nil || len(id) != 16 || len(secret) != 32 || !store.LooksLikeAPIKey(tok) {
			b.p.note(Changed, subj, "not imported: its secret could not be decrypted (wrong APP_KEY?)")
			continue
		}
		name := truncRunes(strings.TrimSpace(k.Memo.String), 64)
		if name == "" {
			name = "Pterodactyl key"
		}
		b.p.APIKeys = append(b.p.APIKeys, store.ImportAPIKey{
			UserKey: k.UserID, Name: name, Token: tok, Prefix: id, CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt, ExpiresAt: k.ExpiresAt,
		})
	}
	if noKey > 0 {
		b.p.note(Changed, "", "%d API key(s) are not imported because no --app-key was given (their secrets are encrypted); people make new ones (Account → API keys)", noKey)
	}
	if expired > 0 {
		b.p.note(Info, "", "%d expired API key(s) are not imported", expired)
	}
	if other > 0 {
		b.p.note(Info, "", "%d internal API key(s) (Wings and old daemon keys) are not imported", other)
	}
	if n := len(b.p.APIKeys); n > 0 {
		b.p.note(Info, "", "%d API key(s) keep working, so bots, scripts and billing modules need no new key; an application key (ptla_) works as its administrator's API key", n)
	}
}

// planBackups brings over finished backups. Wings' local archives move with
// adopt-wings; ones in S3 are where GamePanel looks for them
// (<server uuid>/<backup uuid>.tar.gz) once its backup storage is the same
// bucket.
func (b *builder) planBackups() {
	var unfinished, local, remote int
	for _, v := range b.src.Backups {
		srv := b.servers[v.ServerID]
		if srv == nil {
			continue
		}
		if !v.Successful || v.CompletedAt == 0 {
			unfinished++
			continue
		}
		disk := "local"
		switch v.Disk {
		case "wings":
			local++
		case "s3":
			disk = "s3"
			remote++
		default:
			b.p.note(Changed, fmt.Sprintf("backup %q of %q", v.Name, srv.Server.Name), "not imported: it is in %q storage, which GamePanel does not have", v.Disk)
			continue
		}
		var ignore []string
		if err := json.Unmarshal([]byte(v.IgnoredFiles), &ignore); err != nil {
			ignore = strings.Split(v.IgnoredFiles, "\n")
		}
		b.p.Backups = append(b.p.Backups, store.ImportBackup{
			ServerUUID: srv.Server.UUID, UUID: v.UUID, Name: v.Name, Size: v.Bytes, Ignore: strings.TrimSpace(strings.Join(ignore, "\n")),
			Locked: v.Locked, Disk: disk, CreatedAt: v.CreatedAt, CompletedAt: v.CompletedAt,
		})
	}
	if unfinished > 0 {
		b.p.note(Info, "", "%d failed or unfinished backup(s) are not imported", unfinished)
	}
	if local > 0 {
		b.p.note(Info, "", "%d backup(s) are on the nodes: adopt-wings takes their archives over from Wings' backup folder (/var/lib/pterodactyl/backups)", local)
	}
	if remote > 0 {
		b.p.note(Changed, "", "%d backup(s) are in S3 storage: set GamePanel's backup storage (GAMEPANEL_S3_*) to the same bucket, with GAMEPANEL_S3_PREFIX empty; until then they are listed but cannot be restored or deleted", remote)
	}
}
