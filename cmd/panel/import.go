package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/ptero"
)

const importUsage = `Usage: panel import-pterodactyl [flags]

Reads a Pterodactyl 1.x panel's database (read-only) and prints what an
import into GamePanel would bring over, change or leave out. Nothing is
written anywhere.

Where to read from (one of):
  --env FILE       Pterodactyl's .env (e.g. /var/www/pterodactyl/.env): the
                   database login and APP_KEY are read from it
  --dsn DSN        user:password@tcp(host:3306)/panel, or GAMEPANEL_PTERO_DSN

Flags:
  --app-key KEY    Pterodactyl's APP_KEY (base64:...), or GAMEPANEL_PTERO_APP_KEY;
                   needed to keep two-factor and import database hosts
  --unlimited-memory MB
                   memory for servers that had no limit (default 4096)

Use a database user that can only read (GRANT SELECT).
`

func importPterodactyl(args []string) error {
	fs := flag.NewFlagSet("import-pterodactyl", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, importUsage) }
	envFile := fs.String("env", "", "")
	dsn := fs.String("dsn", os.Getenv("GAMEPANEL_PTERO_DSN"), "")
	appKey := fs.String("app-key", os.Getenv("GAMEPANEL_PTERO_APP_KEY"), "")
	unlimited := fs.Int64("unlimited-memory", ptero.DefaultUnlimitedMemoryMB, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *unlimited < 64 || *unlimited > 1<<20 {
		return errors.New("--unlimited-memory must be 64 to 1048576 MB")
	}
	cfg, key, err := importSource(*envFile, *dsn, *appKey)
	if err != nil {
		return err
	}
	var k ptero.AppKey
	if key != "" {
		if k, err = ptero.ParseAppKey(key); err != nil {
			return err
		}
	}
	conn, err := mysql.NewConnector(cfg)
	if err != nil {
		return err
	}
	db := sql.OpenDB(conn)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	src, err := ptero.Read(ctx, db)
	if err != nil {
		return fmt.Errorf("read Pterodactyl's database: %w", err)
	}
	plan := ptero.Build(src, ptero.Options{AppKey: k, UnlimitedMemoryMB: *unlimited})
	return ptero.WriteReport(os.Stdout, src, plan)
}

// importSource works out the database to read and the APP_KEY, from a
// Pterodactyl .env file or from a DSN. Flags win over the .env file.
func importSource(envFile, dsn, appKey string) (*mysql.Config, string, error) {
	var cfg *mysql.Config
	switch {
	case dsn != "":
		c, err := mysql.ParseDSN(dsn)
		if err != nil {
			return nil, "", fmt.Errorf("--dsn: %w", err)
		}
		cfg = c
	case envFile != "":
		env, err := readDotEnv(envFile)
		if err != nil {
			return nil, "", err
		}
		if env["DB_CONNECTION"] != "" && env["DB_CONNECTION"] != "mysql" && env["DB_CONNECTION"] != "mariadb" {
			return nil, "", fmt.Errorf("%s: DB_CONNECTION is %q; only MySQL/MariaDB is supported", envFile, env["DB_CONNECTION"])
		}
		cfg = mysql.NewConfig()
		cfg.User, cfg.Passwd, cfg.DBName = env["DB_USERNAME"], env["DB_PASSWORD"], env["DB_DATABASE"]
		host, port := env["DB_HOST"], env["DB_PORT"]
		if host == "" {
			host = "127.0.0.1"
		}
		if port == "" {
			port = "3306"
		}
		cfg.Net, cfg.Addr = "tcp", net.JoinHostPort(host, port)
		if appKey == "" {
			appKey = env["APP_KEY"]
		}
	default:
		return nil, "", errors.New("say where Pterodactyl's database is: --env /var/www/pterodactyl/.env or --dsn (see panel import-pterodactyl -h)")
	}
	cfg.ParseTime = false
	cfg.Timeout = 10 * time.Second
	cfg.ReadTimeout = time.Minute
	return cfg, appKey, nil
}

// readDotEnv reads KEY=value lines, as Laravel's .env has them. Quotes around
// a value are removed; ${...} references are not expanded.
func readDotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, sc.Err()
}
