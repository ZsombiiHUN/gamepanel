package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/fsx"
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
	defaultSFTPRecheck   = 30 * time.Second
	defaultSFTPKeepAlive = 30 * time.Second
)

// diskCache is a server's disk usage: the last measurement, adjusted by every
// change the daemon accounts for since (reserve adds, release subtracts).
//
// Measuring walks the whole data directory, which takes seconds on a large
// world, so there is at most one walk per server at a time and writers never
// wait for one once a figure exists: a stale figure is still used while a
// fresh one is measured in the background.
type diskCache struct {
	mu    sync.Mutex
	bytes int64
	at    time.Time    // when bytes was measured; zero = must measure before use
	gen   uint64       // bumped by invalidate
	run   *measurement // the walk in progress, if any
	// grown is what was reserved while run walks the directory. The walk may
	// or may not see those bytes, so they are added to its result: counting
	// them twice until the next walk is safe, missing them is not.
	grown int64
	// freed is set when space was released while run walks the directory.
	// Whether the walk counted the freed file depends on whether it got there
	// before the release, so its result is only an upper bound: it is used
	// (safe), and a second walk, started after the release, settles it.
	freed bool
	// moved is set when something was renamed while run walks the
	// directory. A file moved from a folder the walk has not reached into
	// one it already passed is never seen, so the result may be too low: the
	// figure keeps the larger of it and the accounted figure, and a second
	// walk settles it.
	moved bool
	// pending is what SFTP has reserved but not written yet. A walk that
	// starts meanwhile may pass the file before the bytes land, so they
	// count as grown for it.
	pending int64
	renames int // renames in progress (see renaming)

	// shape orders SFTP writes against the operations that shrink files
	// (truncate, remove, overwrite by rename). Writes hold it shared from the
	// size check until the bytes land, so a shrink never sees a size the
	// write was not measured against; shrinks hold it exclusively, so the
	// bytes of one file are released once however many sessions race to free
	// them. Uploads do not serialize against each other.
	shape sync.RWMutex
}

type measurement struct {
	done chan struct{}
	err  error
	// settles is set on a walk started because space was freed during the
	// previous one: a writer refused for lack of room waits for it.
	settles bool
}

// invalidate forces a new measurement before the figure is used again, for
// changes the daemon cannot account for exactly.
func (c *diskCache) invalidate() {
	c.mu.Lock()
	c.at = time.Time{}
	c.gen++
	c.mu.Unlock()
}

// reserve accounts for extra bytes about to be written, failing if they would
// pass limit. Check and add happen under one lock, so concurrent writers (two
// SFTP uploads, an upload and an editor save) cannot both spend the same room.
func (c *diskCache) reserve(extra, limit int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bytes+extra > limit {
		return ErrDiskFull{c.bytes, limit}
	}
	if extra < 0 { // a write that frees space first (an upload over a truncated file)
		c.credit(-extra)
		return nil
	}
	c.bytes += extra
	if c.run != nil {
		c.grown += extra
	}
	return nil
}

// release accounts for n bytes freed. It never fails: freeing space is always
// allowed, even over the limit.
func (c *diskCache) release(n int64) {
	c.mu.Lock()
	c.credit(n)
	c.mu.Unlock()
}

// credit subtracts n freed bytes; c.mu must be held. The figure never goes
// below zero, so a release larger than the figure (it was stale) cannot turn
// into room that does not exist.
func (c *diskCache) credit(n int64) {
	if n <= 0 {
		return
	}
	c.bytes = max(c.bytes-n, 0)
	if c.run != nil {
		c.freed = true
	}
}

// pend adjusts pending: +n before reserving bytes for a write, -n once it
// has landed (or was refused).
func (c *diskCache) pend(n int64) {
	c.mu.Lock()
	c.pending += n
	c.mu.Unlock()
}

// renaming marks a rename in progress until the returned func is called.
// Any walk that overlaps it, starting before, during or finishing after,
// may have missed the moved bytes (a rename done as link-then-unlink can
// even hide a file from a walk that runs entirely inside it).
func (c *diskCache) renaming() func() {
	c.mu.Lock()
	c.renames++
	if c.run != nil {
		c.moved = true
	}
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		c.renames--
		if c.run != nil {
			c.moved = true
		}
		c.mu.Unlock()
	}
}

// unrelease adds back space released for a change that then failed. Space
// is released before it is actually freed: a walk that runs in between
// counts the bytes still there (too high, safe), whereas releasing after
// would subtract them again from a walk that already missed them.
func (c *diskCache) unrelease(n int64) {
	if n <= 0 {
		return
	}
	c.mu.Lock()
	c.bytes += n
	if c.run != nil {
		c.grown += n
	}
	c.mu.Unlock()
}

// settling returns the walk a refused writer should wait for before giving
// up, if one is running because space was freed during the last walk.
func (c *diskCache) settling() *measurement {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.run != nil && c.run.settles {
		return c.run
	}
	return nil
}

func (d *Daemon) cacheFor(uuid string) *diskCache {
	d.mu.Lock()
	defer d.mu.Unlock()
	if m := d.servers[uuid]; m != nil {
		return &m.disk
	}
	return &diskCache{}
}

// measure starts a walk of the server's directory unless one is running.
// c.mu must be held.
func (d *Daemon) measure(uuid string, c *diskCache) *measurement {
	if c.run != nil {
		return c.run
	}
	m := &measurement{done: make(chan struct{})}
	c.run, c.grown, c.freed, c.moved = m, c.pending, false, c.renames > 0
	gen := c.gen
	go func() {
		n, err := d.walk(uuid)
		c.mu.Lock()
		if err == nil {
			if c.moved { // the walk may have missed what was moved
				n = max(n+c.grown, c.bytes) - c.grown
			}
			c.bytes = n + c.grown
			c.at = time.Now()
			if c.gen != gen { // invalidated mid-walk: the walk may predate the change
				c.at = time.Time{}
			}
		}
		m.err = err
		c.run = nil
		if err == nil && (c.freed || c.moved) {
			// The result may still count what was freed during the walk, or
			// miss what was moved; a walk starting now cannot.
			d.measure(uuid, c).settles = true
		}
		c.mu.Unlock()
		close(m.done)
	}()
	return m
}

// walk measures a server's directory; tests replace it to control timing.
func (d *Daemon) walk(uuid string) (int64, error) {
	if d.walkHook != nil {
		return d.walkHook(uuid)
	}
	return d.walkUsage(uuid)
}

// walkUsage never creates the directory: a walk racing a delete must not
// bring the server's folder back.
func (d *Daemon) walkUsage(uuid string) (int64, error) {
	if !uuidRe.MatchString(uuid) {
		return 0, proto.Invalid("invalid server id")
	}
	dir, err := fsx.OpenExisting(d.serverDir(uuid))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer dir.Close()
	return dir.Usage()
}

// diskUsage returns the data directory size, measuring (and waiting for the
// walk) if the figure is older than maxAge.
func (d *Daemon) diskUsage(uuid string, maxAge time.Duration) (int64, error) {
	return d.usage(uuid, maxAge, true)
}

// diskEstimate returns the current figure without waiting for a walk, unless
// there is no usable figure at all. A stale one starts a walk in the
// background.
func (d *Daemon) diskEstimate(uuid string) (int64, error) {
	return d.usage(uuid, d.cfg.diskTTL(), false)
}

func (d *Daemon) usage(uuid string, maxAge time.Duration, wait bool) (int64, error) {
	c := d.cacheFor(uuid)
	c.mu.Lock()
	if !c.at.IsZero() {
		n := c.bytes
		if time.Since(c.at) <= maxAge {
			c.mu.Unlock()
			return n, nil
		}
		if !wait {
			d.measure(uuid, c)
			c.mu.Unlock()
			return n, nil
		}
	}
	m := d.measure(uuid, c)
	c.mu.Unlock()
	<-m.done
	if m.err != nil {
		return 0, m.err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bytes, nil
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

func (ErrDiskFull) ErrCode() string { return proto.CodeDiskFull }

// reserveDisk fails if writing extra more bytes would pass the server's disk
// limit, and otherwise counts them as used until the next measurement.
// reserveDisk(uuid, 0) just checks that the server is not already over.
func (d *Daemon) reserveDisk(uuid string, extra int64) error {
	limit := d.diskLimitBytes(uuid)
	if limit == 0 {
		return nil
	}
	if _, err := d.diskEstimate(uuid); err != nil {
		return err
	}
	c := d.cacheFor(uuid)
	err := c.reserve(extra, limit)
	if err != nil {
		// Space freed during the last walk may not show until the next one;
		// wait for it rather than refuse a write that fits.
		if m := c.settling(); m != nil {
			<-m.done
			err = c.reserve(extra, limit)
		}
	}
	return err
}

// diskHeadroom is how many more bytes may be written (-1 = unlimited).
func (d *Daemon) diskHeadroom(uuid string) int64 {
	limit := d.diskLimitBytes(uuid)
	if limit == 0 {
		return -1
	}
	used, err := d.diskEstimate(uuid)
	if err != nil {
		return 0
	}
	return max(limit-used, 0)
}

// enforceDisk runs from the stats loop: it reports usage for every server and
// gracefully stops a running one that has outgrown its limit.
func (d *Daemon) enforceDisk(ctx context.Context, uuid string, running bool) (int64, bool) {
	// The estimate keeps the loop from stalling on one server's walk.
	used, err := d.diskEstimate(uuid)
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
			d.sendConsole(uuid, msg)
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

func (c Config) sftpRecheck() time.Duration {
	if c.SFTPRecheck > 0 {
		return c.SFTPRecheck
	}
	return defaultSFTPRecheck
}

func (c Config) statsEvery() time.Duration {
	if c.StatsInterval > 0 {
		return c.StatsInterval
	}
	return defaultStatsInterval
}

// Handshake and panel timeouts are fixed in production; tests shorten them.
func (c Config) sftpHandshake() time.Duration {
	if c.sftpHandshakeTimeout > 0 {
		return c.sftpHandshakeTimeout
	}
	return 30 * time.Second
}

func (c Config) sftpPanelTimeout() time.Duration {
	if c.sftpPanelWait > 0 {
		return c.sftpPanelWait
	}
	return 10 * time.Second
}

func (c Config) sftpKeepAlive() time.Duration {
	if c.SFTPKeepAlive > 0 {
		return c.SFTPKeepAlive
	}
	return defaultSFTPKeepAlive
}
