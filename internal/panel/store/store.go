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
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	DB  *sql.DB
	key []byte // optional AES key for secrets at rest, see SetSecretKey

	// SessionMax is how long a session lasts from sign-in, however busy it
	// is; SessionIdle ends it sooner when it goes unused. Set before serving.
	SessionMax, SessionIdle time.Duration
}

// Session lifetimes (ASVS v5.0.0-7.3.1 and v5.0.0-7.3.2; values from the NIST
// SP 800-63B revision 3 AAL2 limits): sign in again after 12 hours, or after
// 30 minutes without activity. See docs/asvs.md.
const (
	DefaultSessionMax  = 12 * time.Hour
	DefaultSessionIdle = 30 * time.Minute
)

func Open(path string) (*Store, error) {
	// WAL + busy timeout + FK enforcement; one writer connection avoids
	// SQLITE_BUSY surprises at MVP scale.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	if testing.Testing() {
		if err := seedFromTemplate(path); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{DB: db, SessionMax: DefaultSessionMax, SessionIdle: DefaultSessionIdle}, nil
}

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

// template is an empty, fully migrated database, made once per test binary.
var template struct {
	once sync.Once
	data []byte
	err  error
}

// seedFromTemplate writes the migrated template to path when no database is
// there yet. The race detector makes the SQLite engine (C compiled to Go)
// slow, and running every migration took about 0.4s per store, which nearly
// every test opens. Databases that already exist, such as the ones the upgrade
// tests build at an old version, are left to migrate as usual.
func seedFromTemplate(path string) error {
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	template.once.Do(func() {
		dir, err := os.MkdirTemp("", "gamepanel-store-template-")
		if err != nil {
			template.err = err
			return
		}
		defer os.RemoveAll(dir)
		db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "t.db")+"?_pragma=foreign_keys(1)")
		if err != nil {
			template.err = err
			return
		}
		defer db.Close()
		out := filepath.Join(dir, "template.db")
		if err := migrate(db); err != nil {
			template.err = err
			return
		}
		if _, err := db.Exec(`VACUUM INTO ?`, out); err != nil {
			template.err = err
			return
		}
		template.data, template.err = os.ReadFile(out)
	})
	if template.err != nil {
		return fmt.Errorf("store template: %w", template.err)
	}
	return os.WriteFile(path, template.data, 0o644)
}

func (s *Store) Close() error { return s.DB.Close() }

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
