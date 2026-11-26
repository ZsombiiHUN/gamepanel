package store

import (
	"errors"
	"io/fs"
	"os"
	"sync"
)

// The store's own tests cannot use storetest (it imports this package), so
// they seed the same way here.
var testTemplate struct {
	once sync.Once
	data []byte
	err  error
}

// openT is Open on a database seeded from the migrated template when path
// does not exist yet.
func openT(path string) (*Store, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		testTemplate.once.Do(func() { testTemplate.data, testTemplate.err = Template() })
		if testTemplate.err != nil {
			return nil, testTemplate.err
		}
		if err := os.WriteFile(path, testTemplate.data, 0o600); err != nil {
			return nil, err
		}
	}
	return Open(path)
}
