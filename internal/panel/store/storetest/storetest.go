// Package storetest opens panel databases for tests quickly: a new database
// starts as a copy of one migrated empty database, made once per test
// binary, instead of running every migration again.
package storetest

import (
	"errors"
	"io/fs"
	"os"
	"sync"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

var template struct {
	once sync.Once
	data []byte
	err  error
}

// Seed writes the migrated template to path when nothing is there yet.
// Databases that already exist (an upgrade test's old version, say) are
// left to migrate as usual.
func Seed(path string) error {
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	template.once.Do(func() { template.data, template.err = store.Template() })
	if template.err != nil {
		return template.err
	}
	return os.WriteFile(path, template.data, 0o600)
}

// Open is store.Open on a database seeded from the template.
func Open(path string) (*store.Store, error) {
	if err := Seed(path); err != nil {
		return nil, err
	}
	return store.Open(path)
}
