// Package store owns the panel database. SQLite is the default; the schema
// sticks to portable SQL so a Postgres backend can be added later.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	// DB is the one connection that writes (SQLite allows one writer at a
	// time; queueing here instead of in SQLite avoids SQLITE_BUSY). RO is a
	// pool of read-only connections: WAL lets them read while DB writes, so
	// a long list or count does not hold up every request behind it. RO
	// refuses writes (query_only), so a misrouted one fails loudly.
	DB  *sql.DB
	RO  *sql.DB
	key []byte // optional AES key for secrets at rest, see SetSecretKey

	// SessionMax is how long a session lasts from sign-in, however busy it
	// is; SessionIdle ends it sooner when it goes unused. Set before serving.
	SessionMax, SessionIdle time.Duration

	// Log receives failures of best-effort writes (usage stamps, schedule
	// status) that must not fail the request that caused them.
	Log *slog.Logger
}

// Session lifetimes (ASVS v5.0.0-7.3.1 and v5.0.0-7.3.2; values from the NIST
// SP 800-63B revision 3 AAL2 limits): sign in again after 12 hours, or after
// 30 minutes without activity. See docs/asvs.md.
const (
	DefaultSessionMax  = 12 * time.Hour
	DefaultSessionIdle = 30 * time.Minute
)

func Open(path string) (*Store, error) {
	// WAL + busy timeout + FK enforcement; see Store for the two pools.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	// The database holds password hashes and node tokens: create it readable
	// by the panel's user only. SQLite gives its -wal and -shm files the same
	// mode as the database.
	if err := createPrivate(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	ro, err := sql.Open("sqlite", dsn+"&_pragma=query_only(1)")
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	ro.SetMaxOpenConns(readers)
	ro.SetMaxIdleConns(readers)
	return &Store{DB: db, RO: ro, SessionMax: DefaultSessionMax, SessionIdle: DefaultSessionIdle, Log: slog.Default()}, nil
}

// readers is the size of the read-only pool.
const readers = 4

// migrator applies the embedded migrations to db. It keeps no package state
// (unlike goose's global functions), so stores can be opened concurrently.
func migrator(db *sql.DB) (*goose.Provider, error) {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectSQLite3, db, sub)
}

func migrate(db *sql.DB) error {
	p, err := migrator(db)
	if err == nil {
		_, err = p.Up(context.Background())
	}
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// Template returns an empty, fully migrated database file's bytes. Tests
// copy it instead of running every migration for each store they open (see
// storetest), which under the race detector took about 0.4 s a store.
func Template() (data []byte, err error) {
	dir, err := os.MkdirTemp("", "gamepanel-store-template-")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "t.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	out := filepath.Join(dir, "template.db")
	if err := migrate(db); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(context.Background(), `VACUUM INTO ?`, out); err != nil {
		return nil, err
	}
	return os.ReadFile(out)
}

// createPrivate creates an empty file at path with mode 0600 unless something
// already exists there. Existing databases keep their mode (an operator may
// have chosen it).
func createPrivate(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return f.Close()
}

func (s *Store) Close() error { return errors.Join(s.RO.Close(), s.DB.Close()) }

func now() int64 { return time.Now().Unix() }

// NewToken returns a random URL-safe secret and its storage hash. Only the
// hash is persisted, so a database leak does not expose live tokens.
func NewToken() (token, hash string) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token)
}

func HashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func (s *Store) exec(ctx context.Context, q string, a ...any) (sql.Result, error) {
	return s.DB.ExecContext(ctx, q, a...)
}

// execBestEffort runs a write whose failure must not fail the caller, such as
// a last-used timestamp. The failure is logged rather than dropped.
func (s *Store) execBestEffort(ctx context.Context, what, q string, a ...any) {
	if _, err := s.DB.ExecContext(ctx, q, a...); err != nil {
		s.Log.WarnContext(ctx, "best-effort database write failed", "what", what, "err", err)
	}
}

// Ping checks that the database answers a query, on both pools.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	if err := s.RO.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
		return err
	}
	return s.DB.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

// Totals are counts for monitoring (/metrics).
type Totals struct {
	Users, Nodes, Servers, SuspendedServers, PendingDeliveries, FailedDeliveries int
}

// Totals counts what /metrics reports, in one query.
func (s *Store) Totals(ctx context.Context) (Totals, error) {
	var t Totals
	err := s.RO.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM users),
		(SELECT COUNT(*) FROM nodes),
		(SELECT COUNT(*) FROM servers),
		(SELECT COUNT(*) FROM servers WHERE suspended),
		(SELECT COUNT(*) FROM webhook_deliveries WHERE status='pending'),
		(SELECT COUNT(*) FROM webhook_deliveries WHERE status='failed')`).
		Scan(&t.Users, &t.Nodes, &t.Servers, &t.SuspendedServers, &t.PendingDeliveries, &t.FailedDeliveries)
	return t, err
}
