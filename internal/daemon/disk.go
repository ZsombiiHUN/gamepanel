package daemon

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// Disk limits are enforced by the daemon (a bind mount has no quota of its
// own): usage is measured by walking the data directory, cached, and checked
//   - before any write through the panel, SFTP or a backup restore,
//   - before a server starts, and
//   - periodically while it runs, stopping a server that grows past its limit.
// Between measurements a game can overshoot a little; the periodic check bounds it.

// Defaults; Config can override both (tests do).
const (
	defaultDiskCacheTTL  = 30 * time.Second
	defaultStatsInterval = 5 * time.Second
)

type diskCache struct {
	mu    sync.Mutex
	bytes int64
	at    time.Time
}

func (c *diskCache) get(maxAge time.Duration) (int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.at.IsZero() || time.Since(c.at) > maxAge {
		return 0, false
	}
	return c.bytes, true
}

func (c *diskCache) set(n int64) {
	c.mu.Lock()
	c.bytes, c.at = n, time.Now()
	c.mu.Unlock()
}

// invalidate forces the next check to measure again.
func (c *diskCache) invalidate() {
	c.mu.Lock()
	c.at = time.Time{}
	c.mu.Unlock()
}

// add accounts for bytes written since the last measurement.
func (c *diskCache) add(n int64) {
	c.mu.Lock()
	if !c.at.IsZero() {
		c.bytes += n
	}
	c.mu.Unlock()
}

func (d *Daemon) cacheFor(uuid string) *diskCache {
	d.mu.Lock()
	defer d.mu.Unlock()
	if m := d.servers[uuid]; m != nil {
		return &m.disk
	}
	return &diskCache{}
}

// diskUsage returns the data directory size, measuring if the cached figure is
// older than maxAge.
func (d *Daemon) diskUsage(uuid string, maxAge time.Duration) (int64, error) {
	c := d.cacheFor(uuid)
	if n, ok := c.get(maxAge); ok {
		return n, nil
	}
	dir, err := d.dir(uuid)
	if err != nil {
		return 0, err
	}
	defer dir.Close()
	n, err := dir.Usage()
	if err != nil {
		return 0, err
	}
	c.set(n)
	return n, nil
}

func (d *Daemon) diskLimitBytes(uuid string) int64 {
	spec, err := d.spec(uuid)
	if err != nil {
		return 0
	}
	return spec.DiskMB << 20
}

// ErrDiskFull is returned when a write would exceed the server's disk limit.
type ErrDiskFull struct{ Used, Limit int64 }

func (e ErrDiskFull) Error() string {
	return fmt.Sprintf("disk limit exceeded (%d MB used of %d MB)", e.Used>>20, e.Limit>>20)
}

// checkDisk fails if adding extra bytes would pass the limit.
func (d *Daemon) checkDisk(uuid string, extra int64) error {
	limit := d.diskLimitBytes(uuid)
	if limit == 0 {
		return nil
	}
	used, err := d.diskUsage(uuid, d.cfg.diskTTL())
	if err != nil {
		return err
	}
	if used+extra > limit {
		return ErrDiskFull{used, limit}
	}
	return nil
}

// diskHeadroom is how many more bytes may be written (-1 = unlimited).
func (d *Daemon) diskHeadroom(uuid string) int64 {
	limit := d.diskLimitBytes(uuid)
	if limit == 0 {
		return -1
	}
	used, err := d.diskUsage(uuid, d.cfg.diskTTL())
	if err != nil {
		return 0
	}
	return max(limit-used, 0)
}

// enforceDisk runs from the stats loop: it reports usage for every server and
// gracefully stops a running one that has outgrown its limit.
func (d *Daemon) enforceDisk(ctx context.Context, uuid string, running bool) (int64, bool) {
	used, err := d.diskUsage(uuid, d.cfg.diskTTL())
	if err != nil {
		return 0, false
	}
	limit := d.diskLimitBytes(uuid)
	if running && limit > 0 && used > limit {
		d.mu.Lock()
		m := d.servers[uuid]
		already := m == nil || m.diskStopping
		if m != nil {
			m.diskStopping = true
		}
		d.mu.Unlock()
		if !already {
			msg := fmt.Sprintf("\x1b[31m[gamepanel] %s - stopping the server. Free space or raise the limit, then start it again.\x1b[0m", ErrDiskFull{used, limit})
			_ = d.send(ctx, proto.Envelope{Type: proto.EvtConsole, Data: proto.Marshal(proto.Console{UUID: uuid, Line: msg})})
			spec, err := d.spec(uuid)
			if err == nil {
				go func() {
					_ = d.stop(ctx, spec)
					d.mu.Lock()
					if m := d.servers[uuid]; m != nil {
						m.diskStopping = false
					}
					d.mu.Unlock()
				}()
			}
		}
		return used, true
	}
	return used, false
}

func (c Config) diskTTL() time.Duration {
	if c.DiskCacheTTL > 0 {
		return c.DiskCacheTTL
	}
	return defaultDiskCacheTTL
}

func (c Config) statsEvery() time.Duration {
	if c.StatsInterval > 0 {
		return c.StatsInterval
	}
	return defaultStatsInterval
}
