package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/dbhost"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// dbTimeout bounds one conversation with a database host.
const dbTimeout = 30 * time.Second

var dbNameRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,48}$`)

type (
	hostListOut  struct{ Body []store.DatabaseHost }
	hostOut      struct{ Body store.DatabaseHost }
	hostCreateIn struct {
		Body struct {
			Name         string `json:"name" minLength:"1" maxLength:"64"`
			Host         string `json:"host" minLength:"1" maxLength:"255" doc:"address the panel and the game servers reach it on"`
			Port         int    `json:"port,omitempty" minimum:"1" maximum:"65535" default:"3306"`
			Username     string `json:"username" minLength:"1" maxLength:"64" doc:"an account that may create databases and users"`
			Password     string `json:"password" maxLength:"256"`
			MaxDatabases int64  `json:"max_databases,omitempty" minimum:"0" doc:"0 = no limit"`
			NodeID       int64  `json:"node_id,omitempty" doc:"prefer this host for servers on that node"`
		}
	}
)

type hostIDIn struct {
	ID int64 `path:"id"`
}

type databasesView struct {
	Databases []store.ServerDatabase `json:"databases" nullable:"false"`
	Limit     int64                  `json:"limit"`
	CanManage bool                   `json:"can_manage"`
	// Available: at least one database host exists.
	Available bool `json:"available"`
}
type (
	databasesOut     struct{ Body databasesView }
	databaseCreateIn struct {
		UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
		Body struct {
			Name   string `json:"name" pattern:"^[A-Za-z0-9_]{1,48}$" doc:"letters, digits and _; the server's prefix is added"`
			Remote string `json:"remote,omitempty" maxLength:"60" default:"%" doc:"where connections may come from: % = anywhere, or an address"`
		}
	}
)

type databaseIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	ID   int64  `path:"id"`
}
type databaseSecret struct {
	store.ServerDatabase
	Password string `json:"password"`
}
type databaseSecretOut struct{ Body databaseSecret }

func hostOf(h store.HostSecret) dbhost.Host {
	return dbhost.Host{Host: h.Host, Port: h.Port, Username: h.Username, Password: h.Password}
}

// dbErr shows a database host failure to the person, logged in full.
func dbErr(msg string, err error) error {
	ref := logRef(msg, err)
	return huma.NewError(http.StatusBadGateway, dbhost.Friendly(err)+". Reference: "+ref+".")
}

func (a *App) registerDatabases(api huma.API) {
	// ---- admin: database hosts ----
	huma.Register(api, huma.Operation{OperationID: "listDatabaseHosts", Method: "GET", Path: "/database-hosts", Summary: "List database hosts (admin)"},
		func(ctx context.Context, _ *struct{}) (*hostListOut, error) {
			if _, err := requireAdmin(ctx); err != nil {
				return nil, err
			}
			hs, err := a.Store.ListDatabaseHosts(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error", err)
			}
			return &hostListOut{Body: hs}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID: "createDatabaseHost", Method: "POST", Path: "/database-hosts",
		Summary: "Add a database host (admin; the connection is tested first)", DefaultStatus: 201,
	},
		func(ctx context.Context, in *hostCreateIn) (*hostOut, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			b := in.Body
			if b.Port == 0 {
				b.Port = 3306
			}
			c, cancel := context.WithTimeout(ctx, dbTimeout)
			defer cancel()
			if _, err := dbhost.Check(c, dbhost.Host{Host: b.Host, Port: b.Port, Username: b.Username, Password: b.Password}); err != nil {
				return nil, huma.Error422UnprocessableEntity("could not connect: " + dbhost.Friendly(err))
			}
			h, err := a.Store.CreateDatabaseHost(ctx, store.DatabaseHost{
				Name: strings.TrimSpace(b.Name), Host: b.Host, Port: b.Port,
				Username: b.Username, MaxDatabases: b.MaxDatabases, NodeID: b.NodeID,
			}, b.Password)
			if err != nil {
				return nil, userErr(http.StatusConflict, err)
			}
			a.audit(ctx, u, "database_host.create", strconv.FormatInt(h.ID, 10), h.Name+" "+h.Host)
			return &hostOut{Body: h}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteDatabaseHost", Method: "DELETE", Path: "/database-hosts/{id}", Summary: "Remove a database host with no databases (admin)", DefaultStatus: 204},
		func(ctx context.Context, in *hostIDIn) (*struct{}, error) {
			u, err := requireAdmin(ctx)
			if err != nil {
				return nil, err
			}
			if err := a.Store.DeleteDatabaseHost(ctx, in.ID); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return nil, huma.Error404NotFound("database host not found")
				}
				return nil, userErr(http.StatusConflict, err)
			}
			a.audit(ctx, u, "database_host.delete", strconv.FormatInt(in.ID, 10), "")
			return nil, nil
		})

	// ---- per server ----
	huma.Register(api, huma.Operation{OperationID: "listDatabases", Method: "GET", Path: "/servers/{uuid}/databases", Summary: "List a server's databases"},
		func(ctx context.Context, in *uuidIn) (*databasesOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermDatabasesRead)
			if err != nil {
				return nil, err
			}
			return a.databasesFor(ctx, u, s)
		})

	huma.Register(api, huma.Operation{
		OperationID: "createDatabase", Method: "POST", Path: "/servers/{uuid}/databases",
		Summary: "Create a database and its user", Description: "The reply holds the password; it can be shown again later.", DefaultStatus: 201,
	},
		func(ctx context.Context, in *databaseCreateIn) (*databaseSecretOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermDatabasesManage)
			if err != nil {
				return nil, err
			}
			remote := strings.TrimSpace(in.Body.Remote)
			if remote == "" {
				remote = "%"
			}
			if !dbhost.RemoteRe.MatchString(remote) {
				return nil, huma.Error422UnprocessableEntity("connections from: use %, an address, or a pattern such as 10.0.0.%")
			}
			hostID, err := a.Store.PickDatabaseHost(ctx, s.NodeID)
			if err != nil {
				return nil, userErr(http.StatusConflict, err)
			}
			prefix := "s" + strings.ReplaceAll(s.UUID, "-", "")[:8] + "_"
			d := store.ServerDatabase{HostID: hostID, Name: prefix + in.Body.Name, Username: "u" + prefix[1:] + dbhost.Token(8), Remote: remote}
			password := dbhost.Password(24)
			d, err = a.Store.ReserveDatabase(ctx, s.UUID, d, password, u.IsAdmin)
			if err != nil {
				return nil, userErr(http.StatusConflict, err)
			}
			h, err := a.Store.HostWithSecret(ctx, hostID)
			if err == nil {
				c, cancel := context.WithTimeout(ctx, dbTimeout)
				err = dbhost.Create(c, hostOf(h), d.Name, d.Username, password, remote)
				cancel()
			}
			if err != nil {
				a.Store.DeleteServerDatabase(ctx, d.ID)
				return nil, dbErr("creating a database failed", err)
			}
			a.audit(ctx, u, "database.create", s.UUID, d.Name)
			d, _ = a.Store.GetServerDatabase(ctx, s.UUID, d.ID)
			return &databaseSecretOut{Body: databaseSecret{ServerDatabase: d, Password: password}}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "databasePassword", Method: "GET", Path: "/servers/{uuid}/databases/{id}/password", Summary: "Show a database's password"},
		func(ctx context.Context, in *databaseIn) (*databaseSecretOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermDatabasesManage)
			if err != nil {
				return nil, err
			}
			d, err := a.Store.GetServerDatabase(ctx, s.UUID, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound("database not found")
			}
			pw, err := a.Store.DatabasePassword(ctx, d.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("could not read the password", err)
			}
			a.audit(ctx, u, "database.password.view", s.UUID, d.Name)
			return &databaseSecretOut{Body: databaseSecret{ServerDatabase: d, Password: pw}}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "rotateDatabasePassword", Method: "POST", Path: "/servers/{uuid}/databases/{id}/rotate", Summary: "Give a database a new password"},
		func(ctx context.Context, in *databaseIn) (*databaseSecretOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermDatabasesManage)
			if err != nil {
				return nil, err
			}
			d, err := a.Store.GetServerDatabase(ctx, s.UUID, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound("database not found")
			}
			h, err := a.Store.HostWithSecret(ctx, d.HostID)
			if err != nil {
				return nil, huma.Error500InternalServerError("could not load the database host", err)
			}
			pw := dbhost.Password(24)
			c, cancel := context.WithTimeout(ctx, dbTimeout)
			defer cancel()
			if err := dbhost.SetPassword(c, hostOf(h), d.Username, d.Remote, pw); err != nil {
				return nil, dbErr("rotating a database password failed", err)
			}
			if err := a.Store.SetDatabasePassword(ctx, d.ID, pw); err != nil {
				return nil, huma.Error500InternalServerError("the password changed but could not be saved; rotate it again", err)
			}
			a.audit(ctx, u, "database.password.rotate", s.UUID, d.Name)
			return &databaseSecretOut{Body: databaseSecret{ServerDatabase: d, Password: pw}}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteDatabase", Method: "DELETE", Path: "/servers/{uuid}/databases/{id}", Summary: "Delete a database and its user", DefaultStatus: 204},
		func(ctx context.Context, in *databaseIn) (*struct{}, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermDatabasesManage)
			if err != nil {
				return nil, err
			}
			d, err := a.Store.GetServerDatabase(ctx, s.UUID, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound("database not found")
			}
			if err := a.dropDatabase(ctx, d); err != nil {
				return nil, err
			}
			a.audit(ctx, u, "database.delete", s.UUID, d.Name)
			return nil, nil
		})
}

// dropDatabase removes a database from its host, then its record.
func (a *App) dropDatabase(ctx context.Context, d store.ServerDatabase) error {
	h, err := a.Store.HostWithSecret(ctx, d.HostID)
	if err != nil {
		return huma.Error500InternalServerError("could not load the database host", err)
	}
	c, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	if err := dbhost.Drop(c, hostOf(h), d.Name, d.Username, d.Remote); err != nil {
		return dbErr("deleting a database failed", err)
	}
	if err := a.Store.DeleteServerDatabase(ctx, d.ID); err != nil {
		return huma.Error500InternalServerError("db error", err)
	}
	return nil
}

// dropServerDatabases removes a deleted server's databases from their hosts.
func (a *App) dropServerDatabases(ctx context.Context, server string) {
	ds, err := a.Store.ListServerDatabases(ctx, server)
	if err != nil {
		a.Log.Warn("could not list a deleted server's databases", "server", server, "err", err)
		return
	}
	for _, d := range ds {
		if err := a.dropDatabase(ctx, d); err != nil {
			a.Log.Warn("could not drop a deleted server's database", "server", server, "database", d.Name, "err", err)
		}
	}
}

func (a *App) databasesFor(ctx context.Context, u store.User, s store.Server) (*databasesOut, error) {
	ds, err := a.Store.ListServerDatabases(ctx, s.UUID)
	if err != nil {
		return nil, huma.Error500InternalServerError("db error", err)
	}
	hosts, _ := a.Store.ListDatabaseHosts(ctx)
	perms, _, _ := a.access(ctx, u, s)
	return &databasesOut{Body: databasesView{Databases: ds, Limit: s.DatabaseLimit, CanManage: has(perms, PermDatabasesManage), Available: len(hosts) > 0}}, nil
}
