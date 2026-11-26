// Package daemon is the node agent: it dials the panel, executes requests
// against a Runtime and the safe filesystem layer, and streams console output.
//
// Files: daemon.go (types, state on disk, claiming a server), session.go
// (the panel connection, states and usage), dispatch.go (requests), console.go,
// lifecycle.go (create, install, remove), power.go (start, stop, input),
// update.go (new settings), owner.go (the game's user), files.go (transfers),
// sftp.go, backup.go, adopt.go (taking over Wings), panelurl.go.
package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// Install states, reported through the normal state events.
const (
	StateInstalling    = "installing"
	StateInstallFailed = "install_failed"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Config struct {
	PanelURL string // ws(s)://host/daemon/connect
	Token    string
	DataDir  string
	// Optional tuning: how often stats/disk usage are reported (default 5 s)
	// and how long a measured disk size is trusted (default 30 s).
	StatsInterval time.Duration
	DiskCacheTTL  time.Duration
	// SFTPRecheck is how often an open SFTP session's access is re-checked
	// with the panel (default 30 s).
	SFTPRecheck time.Duration
	// SFTPKeepAlive is how often SFTP clients are pinged; a session that
	// misses three in a row is closed (default 30 s).
	SFTPKeepAlive time.Duration
	// PullAllowPrivate lets "pull from URL" fetch from private and loopback
	// addresses (off by default: see pull).
	PullAllowPrivate bool
	// NoResume keeps servers offline after the daemon starts, even those that
	// were running when it stopped (see resume).
	NoResume bool
	// ContainerUID and ContainerGID own new servers' files, and so are who
	// their games run as (see provision). 0 means LegacyContainerID.
	ContainerUID, ContainerGID int

	// Test-only: the SSH handshake deadline (30 s) and how long a login
	// waits for the panel (10 s).
	sftpHandshakeTimeout time.Duration
	sftpPanelWait        time.Duration
}

type managed struct {
	spec     proto.ServerSpec
	stdin    io.Writer
	attached bool // a console reader is active for the current run
	// attachGen counts console attachments. A reader only clears stdin and
	// attached if no later run has attached since, so an old run's stream
	// that ends late cannot take the current run's console with it.
	attachGen uint64
	install   string // "", StateInstalling or StateInstallFailed; "" = ready

	// op is the exclusive operation running on this server ("" = none), see
	// begin. backups counts archives being written, which may overlap with
	// most things but not with a restore or a delete.
	op      string
	backups int

	disk         diskCache
	diskStopping bool // a disk-limit stop is already in progress

	// stale: spec changed while the server was running; the container is
	// rebuilt from it at the next start (persisted as <uuid>.stale).
	stale bool

	// running mirrors the runtime's state as last reported (see noteState),
	// so the stats tick needs no container inspect. usage is the latest
	// CPU and memory sample, kept by a stats watcher while it runs.
	running   bool
	usage     proto.Stats
	stopWatch context.CancelFunc

	// stopping: a stop was asked for this run, so its exit is no crash.
	// crashed: it crashed and was left stopped; it reports "crashed" until
	// it starts again. lastCrash guards against restart loops.
	stopping  bool
	crashed   bool
	lastCrash time.Time

	// killed: a kill landed while a restart was stopping the server, so the
	// restart ends there instead of starting it again.
	killed bool

	// booted: this run printed one of the spec's Done lines (or the spec has
	// none), so it reports "running" rather than "starting".
	booted bool
}

// Operations that claim a server (begin).
const (
	opStart   = "start"
	opStop    = "stop"
	opRestart = "restart"
	opKill    = "kill"
	opUpdate  = "update"
	opInstall = "install"
	opRestore = "restore"
	opDelete  = "delete"
	opBackup  = "backup"
)

var errUnknownServer = proto.NotFound("this server is not on the node")

// opBusy describes a running operation for the person who has to wait for it.
func opBusy(op string) error {
	what := map[string]string{
		opStart: "the server is starting", opStop: "the server is stopping",
		opRestart: "the server is restarting", opKill: "the server is being killed",
		opUpdate:  "its limits are being changed",
		opInstall: "an install is running", opRestore: "a backup is being restored",
		opDelete: "the server is being deleted", opBackup: "a backup is being made",
	}[op]
	return proto.Conflict("try again in a moment: " + what)
}

// begin claims a server for op and returns the function that releases it.
//
// Requests run concurrently, and most operations first check the server's
// state and then act on it ("offline? then recreate the container"). Without a
// claim, a start could slip in between the check and the act. Exclusive
// operations therefore never overlap; a backup may run alongside anything but
// a restore or a delete. No operation starts while an install is running.
func (d *Daemon) begin(uuid, op string) (func(), error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.servers[uuid]
	if m == nil {
		return nil, errUnknownServer
	}
	if m.install == StateInstalling {
		return nil, opBusy(opInstall)
	}
	if op == opBackup {
		if m.op == opRestore || m.op == opDelete {
			return nil, opBusy(m.op)
		}
		m.backups++
		return func() { d.mu.Lock(); m.backups--; d.mu.Unlock() }, nil
	}
	if m.op != "" {
		return nil, opBusy(m.op)
	}
	if m.backups > 0 && (op == opRestore || op == opDelete) {
		return nil, opBusy(opBackup)
	}
	m.op = op
	return func() { d.mu.Lock(); m.op = ""; d.mu.Unlock() }, nil
}

// installState is the server's install state; ok is false for an unknown server.
func (d *Daemon) installState(uuid string) (st string, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if m := d.servers[uuid]; m != nil {
		return m.install, true
	}
	return "", false
}

type Daemon struct {
	cfg Config
	rt  runtime.Runtime
	log *slog.Logger

	mu      sync.Mutex
	servers map[string]*managed

	wmu  sync.Mutex
	conn *websocket.Conn

	relayClient *http.Client // transfers to and from the panel's relay

	sftp *sftpServer // set by StartSFTP; nil when SFTP is off

	walkHook func(uuid string) (int64, error) // tests: replaces the disk walk
	sendHook func(proto.Envelope)             // tests: receives what would be sent
}

func New(cfg Config, rt runtime.Runtime, log *slog.Logger) (*Daemon, error) {
	for _, d := range []string{"servers", "state"} {
		// Server data is handed to the game's own user (see provision), so its parents stay traversable.
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, d), 0o755); err != nil { //nolint:gosec // G301: see above
			return nil, err
		}
	}
	d := &Daemon{
		cfg: cfg, rt: rt, log: log, servers: map[string]*managed{},
		// No overall timeout: a large download runs as long as bytes flow.
		relayClient: &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 2 * time.Minute}},
	}
	d.loadState()
	return d, nil
}

// LegacyContainerID is who games ran as before ContainerUID existed, and
// still do when it is not set: usually the node's first login account.
const LegacyContainerID = 1000

func (d *Daemon) containerIDs() (uid, gid int) {
	uid, gid = d.cfg.ContainerUID, d.cfg.ContainerGID
	if uid <= 0 {
		uid = LegacyContainerID
	}
	if gid <= 0 {
		gid = uid
	}
	return uid, gid
}

func (d *Daemon) serverDir(uuid string) string { return filepath.Join(d.cfg.DataDir, "servers", uuid) }

func (d *Daemon) statePath(uuid string) string {
	return filepath.Join(d.cfg.DataDir, "state", uuid+".json")
}

func (d *Daemon) installedPath(uuid string) string {
	return filepath.Join(d.cfg.DataDir, "state", uuid+".installed")
}

func (d *Daemon) stalePath(uuid string) string {
	return filepath.Join(d.cfg.DataDir, "state", uuid+".stale")
}

// loadState restores specs persisted by earlier runs so a daemon restart does
// not forget its servers.
func (d *Daemon) loadState() {
	files, _ := filepath.Glob(filepath.Join(d.cfg.DataDir, "state", "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s proto.ServerSpec
		if json.Unmarshal(b, &s) == nil && uuidRe.MatchString(s.UUID) {
			m := &managed{spec: s}
			if _, err := os.Stat(d.stalePath(s.UUID)); err == nil {
				m.stale = true
			}
			// An install that was interrupted (daemon killed mid-run) never
			// finished, so it counts as failed until reinstalled.
			if s.Install != nil {
				if _, err := os.Stat(d.installedPath(s.UUID)); err != nil {
					m.install = StateInstallFailed
				}
			}
			d.servers[s.UUID] = m
		}
	}
}

// oneLine drops line breaks from a value the panel sent before it is logged.
// slog's text handler already escapes them; this also says so to scanners.
func oneLine(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", ""), "\r", "")
}

// removeAll deletes path (a file or a tree) and logs a failure: a leftover
// wastes disk but must not fail the operation that made it.
func (d *Daemon) removeAll(path string) {
	if err := os.RemoveAll(path); err != nil {
		d.log.Warn("could not remove a leftover file", "path", path, "err", err)
	}
}

// closeQuiet closes a handle no data was written through: one opened for
// reading, used only for metadata calls (stat, truncate), or abandoned before
// any write. Such a close cannot lose data, so its error is not worth reporting.
func closeQuiet(c io.Closer) { _ = c.Close() }
