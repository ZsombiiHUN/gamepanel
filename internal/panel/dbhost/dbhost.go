// Package dbhost creates and removes game server databases on a MySQL or
// MariaDB server, with an account the admin set up for the panel.
package dbhost

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Host is how to reach a database server as its admin account.
type Host struct {
	Host     string
	Port     int
	Username string
	Password string
}

var (
	// NameRe is what database and user names may contain. Identifiers cannot
	// be passed as query parameters, so this is what keeps them safe in SQL.
	NameRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
	// RemoteRe is a MySQL host pattern: an address, a name, or with % wildcards.
	RemoteRe = regexp.MustCompile(`^[A-Za-z0-9.%:_-]{1,60}$`)
)

// Privileges granted on a server's own database: everything a game or
// plugin needs, nothing that reaches beyond it (as Pterodactyl grants).
const privileges = "SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, INDEX, LOCK TABLES, CREATE TEMPORARY TABLES, " +
	"EXECUTE, CREATE VIEW, SHOW VIEW, CREATE ROUTINE, ALTER ROUTINE, EVENT, TRIGGER, REFERENCES"

func open(h Host) (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd = h.Username, h.Password
	cfg.Net, cfg.Addr = "tcp", net.JoinHostPort(h.Host, strconv.Itoa(h.Port))
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 10*time.Second, 30*time.Second, 30*time.Second
	// Values are escaped by the driver, so even statements MySQL cannot
	// prepare (CREATE USER ... IDENTIFIED BY) take them as parameters.
	cfg.InterpolateParams = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// Check connects and reports the server's version.
func Check(ctx context.Context, h Host) (string, error) {
	db, err := open(h)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var v string
	err = db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&v)
	return v, err
}

func checkNames(name, user, remote string) error {
	if !NameRe.MatchString(name) || !NameRe.MatchString(user) || len(user) > 32 || !RemoteRe.MatchString(remote) {
		return errors.New("invalid database name, user or remote")
	}
	return nil
}

// Create makes the database and a user that can use only it. If any step
// fails, what was made is removed again.
func Create(ctx context.Context, h Host, name, user, password, remote string) (err error) {
	if err := checkNames(name, user, remote); err != nil {
		return err
	}
	db, err := open(h)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "CREATE DATABASE `"+name+"`"); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			undo := context.WithoutCancel(ctx)
			_, uerr := db.ExecContext(undo, "DROP USER IF EXISTS ?@?", user, remote)
			_, derr := db.ExecContext(undo, "DROP DATABASE IF EXISTS `"+name+"`")
			err = errors.Join(err, uerr, derr)
		}
	}()
	if _, err = db.ExecContext(ctx, "CREATE USER ?@? IDENTIFIED BY ?", user, remote, password); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "GRANT "+privileges+" ON `"+name+"`.* TO ?@?", user, remote)
	return err
}

// Drop removes a database and its user. Already missing ones are fine.
func Drop(ctx context.Context, h Host, name, user, remote string) error {
	if err := checkNames(name, user, remote); err != nil {
		return err
	}
	db, err := open(h)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+name+"`"); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "DROP USER IF EXISTS ?@?", user, remote)
	return err
}

// SetPassword gives a database user a new password.
func SetPassword(ctx context.Context, h Host, user, remote, password string) error {
	if !NameRe.MatchString(user) || !RemoteRe.MatchString(remote) {
		return errors.New("invalid user")
	}
	db, err := open(h)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, "ALTER USER ?@? IDENTIFIED BY ?", user, remote, password)
	return err
}

const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

// Password returns a random password of n characters (letters and digits,
// so it survives being pasted into any config file).
func Password(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// Token returns n random lowercase letters and digits, for user names.
func Token(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	rand.Read(b)
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

// Friendly turns a driver error into something an admin can act on.
func Friendly(err error) string {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case 1045:
			return "the database server refused the panel's username or password"
		case 1007:
			return "a database with that name already exists on the host"
		case 1396:
			return "the database user already exists on the host"
		case 1044, 1227:
			return "the panel's database account lacks the privileges to create databases and users"
		}
		return fmt.Sprintf("the database server answered: %s", me.Message)
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return "could not reach the database server"
	}
	return "the database server could not do that"
}
