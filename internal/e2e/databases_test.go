package e2e

import (
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

// TestServerDatabases runs against a real MySQL or MariaDB. It is skipped
// unless GAMEPANEL_TEST_MYSQL is set to host:port, with
// GAMEPANEL_TEST_MYSQL_USER and GAMEPANEL_TEST_MYSQL_PASSWORD for an account
// that may create databases and users, e.g.
//
//	docker run -d --name gp-mariadb -p 3306:3306 -e MARIADB_ROOT_PASSWORD=pw mariadb:11
func TestServerDatabases(t *testing.T) {
	t.Parallel()
	addr := os.Getenv("GAMEPANEL_TEST_MYSQL")
	if addr == "" {
		t.Skip("GAMEPANEL_TEST_MYSQL not set")
	}
	host, portS, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portS)
	s := newStack(t)
	srv := "/api/servers/" + s.server
	hostBody := map[string]any{"name": "main", "host": host, "port": port, "username": os.Getenv("GAMEPANEL_TEST_MYSQL_USER"), "password": os.Getenv("GAMEPANEL_TEST_MYSQL_PASSWORD")}

	// A host is only saved once the panel could log in to it.
	bad := map[string]any{}
	for k, v := range hostBody {
		bad[k] = v
	}
	bad["password"] = "wrong"
	if b := string(s.admin.do("POST", "/api/database-hosts", bad, 422)); !strings.Contains(b, "refused") {
		t.Fatalf("bad password: %s", b)
	}
	s.admin.do("POST", "/api/database-hosts", hostBody, 201)

	connect := func(user, pw, db string) (*sql.DB, error) {
		cfg := mysql.NewConfig()
		cfg.User, cfg.Passwd, cfg.Net, cfg.Addr, cfg.DBName = user, pw, "tcp", addr, db
		c, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			return nil, err
		}
		return c, c.Ping()
	}

	// The owner-side flow, as a subuser with databases.manage and a limit of 1.
	s.setLimits(map[string]any{"database_limit": 1}, 204)
	s.st.CreateUser(s.ctx, "bob@example.com", "another-long-password", false)
	jar, _ := cookiejar.New(nil)
	bob := &client{t, s.admin.base, &http.Client{Jar: jar}}
	bob.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "another-long-password"}, 200)
	s.admin.do("POST", srv+"/users", map[string]any{"email": "bob@example.com", "permissions": []string{"databases.read", "databases.manage"}}, 201)
	bob.do("GET", "/api/database-hosts", nil, 403)
	bob.do("POST", srv+"/databases", map[string]any{"name": "bad name; drop"}, 422)
	bob.do("POST", srv+"/databases", map[string]any{"name": "ok", "remote": "x'@'%"}, 422)

	var d struct {
		ID                       int64
		Name, Username, Password string
	}
	json.Unmarshal(bob.do("POST", srv+"/databases", map[string]any{"name": "luckperms"}, 201), &d)
	if !strings.HasSuffix(d.Name, "_luckperms") || d.Password == "" {
		t.Fatalf("created %+v", d)
	}
	bob.do("POST", srv+"/databases", map[string]any{"name": "second"}, 409) // limit 1

	db, err := connect(d.Username, d.Password, d.Name)
	if err != nil {
		t.Fatalf("connect with the new credentials: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INT)"); err != nil {
		t.Fatalf("the user cannot use its database: %v", err)
	}
	if _, err := db.Exec("CREATE DATABASE stolen"); err == nil {
		t.Fatal("the user could create other databases")
	}
	if _, err := db.Exec("SELECT * FROM mysql.user"); err == nil {
		t.Fatal("the user can read the server's user table")
	}
	db.Close()

	// The list never carries passwords; asking for one is audited.
	if b := string(bob.do("GET", srv+"/databases", nil, 200)); strings.Contains(b, d.Password) {
		t.Fatal("the list leaks the password")
	}
	var shown struct{ Password string }
	json.Unmarshal(bob.do("GET", srv+"/databases/"+strconv.FormatInt(d.ID, 10)+"/password", nil, 200), &shown)
	if shown.Password != d.Password {
		t.Fatal("shown password differs")
	}

	// Rotate: the old password stops working.
	var rot struct{ Password string }
	json.Unmarshal(bob.do("POST", srv+"/databases/"+strconv.FormatInt(d.ID, 10)+"/rotate", nil, 200), &rot)
	if _, err := connect(d.Username, d.Password, d.Name); err == nil {
		t.Fatal("old password still works")
	}
	if c, err := connect(d.Username, rot.Password, d.Name); err != nil {
		t.Fatalf("new password: %v", err)
	} else {
		c.Close()
	}

	// Read-only subusers see the list but cannot touch it.
	s.admin.do("PUT", srv+"/users/"+userID(t, s, "bob@example.com"), map[string]any{"permissions": []string{"databases.read"}}, 204)
	bob.do("GET", srv+"/databases", nil, 200)
	bob.do("GET", srv+"/databases/"+strconv.FormatInt(d.ID, 10)+"/password", nil, 403)
	bob.do("DELETE", srv+"/databases/"+strconv.FormatInt(d.ID, 10), nil, 403)

	// A host with databases cannot be removed; deleting the server drops them.
	s.admin.do("DELETE", "/api/database-hosts/1", nil, 409)
	s.admin.do("DELETE", srv, nil, 204)
	if _, err := connect(d.Username, rot.Password, d.Name); err == nil {
		t.Fatal("the database outlived its server")
	}
	s.admin.do("DELETE", "/api/database-hosts/1", nil, 204)
}
