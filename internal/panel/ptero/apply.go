package ptero

import "github.com/ZsombiiHUN/gamepanel/internal/panel/store"

// Import turns the plan into what store.ImportPanel writes. Records keep
// pointing at each other by their Pterodactyl ids, and locations, nodes, eggs
// and servers keep those ids in GamePanel too, so a billing module set up for
// Pterodactyl's application API still finds them.
func (p *Plan) Import() store.Import {
	var in store.Import
	for _, l := range p.Locations {
		in.Locations = append(in.Locations, store.ImportLocation{Key: l.PteroID, Short: l.Short, Long: l.Long})
	}
	for _, u := range p.Users {
		in.Users = append(in.Users, store.ImportUser{
			Key: u.PteroID, Email: u.Email, Admin: u.Admin, PasswordHash: u.PasswordHash, TOTPSecret: u.TOTPSecret, ExternalID: u.ExternalID,
		})
	}
	for _, n := range p.Nodes {
		in.Nodes = append(in.Nodes, store.ImportNode{
			Key: n.PteroID, LocationKey: n.LocationID, Name: n.Name, PortStart: n.PortStart, PortEnd: n.PortEnd,
			MemoryMB: n.MemoryMB, DiskMB: n.DiskMB, OverallocatePct: n.OverallocatePct, SFTPHost: n.SFTPHost, SFTPPort: n.SFTPPort,
		})
	}
	for _, e := range p.Eggs {
		in.Eggs = append(in.Eggs, store.ImportEgg{Key: e.PteroID, Egg: e.Egg})
	}
	for _, s := range p.Servers {
		is := store.ImportServer{Key: s.PteroID, Server: s.Server}
		// Wings restarts a server that crashes, and so does GamePanel when
		// this is on.
		is.Server.CrashRestart = true
		for _, x := range s.Extra {
			is.Extra = append(is.Extra, store.ServerPort{Port: x.Port, Notes: x.Notes})
		}
		in.Servers = append(in.Servers, is)
	}
	for _, su := range p.Subusers {
		in.Subusers = append(in.Subusers, store.ImportSubuser{ServerUUID: su.ServerUUID, UserKey: su.UserPteroID, Permissions: su.Permissions})
	}
	in.Schedules = append(in.Schedules, p.Schedules...)
	for _, h := range p.DBHosts {
		in.DBHosts = append(in.DBHosts, store.ImportDBHost{Key: h.PteroID, Host: h.Host, Password: h.Password})
	}
	for _, d := range p.Databases {
		in.Databases = append(in.Databases, store.ImportDatabase{
			ServerUUID: d.ServerUUID, HostKey: d.HostPteroID, Name: d.Name, Username: d.Username, Remote: d.Remote, Password: d.Password,
		})
	}
	return in
}
