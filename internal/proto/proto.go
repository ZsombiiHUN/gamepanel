// Package proto defines the wire protocol between the panel and node daemons.
//
// The daemon dials out to the panel over a single websocket, so nodes need no
// inbound ports for the control channel. All frames are JSON envelopes.
package proto

import (
	"encoding/json"
	"errors"
	"regexp"
)

// Envelope is the single frame type on the wire.
type Envelope struct {
	Type string          `json:"type"`
	ID   string          `json:"id,omitempty"` // request/reply correlation
	Data json.RawMessage `json:"data,omitempty"`
	Err  string          `json:"err,omitempty"` // set on failed replies
	// Code classifies a failed reply so the panel can pick a status without
	// parsing the message, which is meant for people.
	Code string `json:"code,omitempty"`
}

// Error codes carried in Envelope.Code.
const (
	// CodeConflict: the server is in the wrong state for the request (running,
	// installing, busy with another operation). Retrying later can succeed.
	CodeConflict = "conflict"
	// CodeDiskFull: the request would take the server past its disk limit.
	CodeDiskFull = "disk_full"
	// CodeInvalid: the request cannot work as asked (bad path, wrong kind of file).
	CodeInvalid = "invalid"
	// CodeNotFound: the file, folder, server or backup does not exist on the node.
	CodeNotFound = "not_found"
)

// Replies without a code carry internal detail (engine, file system, library
// messages). The panel logs those and shows people a generic message; only
// coded replies are shown as written.

// Error is a failed request: Msg for people, Code for programs.
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string   { return e.Msg }
func (e *Error) ErrCode() string { return e.Code }

// Conflict reports that the server is in the wrong state for a request.
func Conflict(msg string) error { return &Error{Code: CodeConflict, Msg: msg} }

// Invalid reports a request that cannot work as asked.
func Invalid(msg string) error { return &Error{Code: CodeInvalid, Msg: msg} }

// NotFound reports that the thing a request names does not exist.
func NotFound(msg string) error { return &Error{Code: CodeNotFound, Msg: msg} }

// CodeOf returns the code of err (or of an error it wraps), "" if none.
func CodeOf(err error) string {
	var c interface{ ErrCode() string }
	if errors.As(err, &c) {
		return c.ErrCode()
	}
	return ""
}

// Panel -> daemon request types.
const (
	ReqServerCreate = "server.create"
	ReqServerPower  = "server.power"
	ReqServerInput  = "server.input"
	ReqServerDelete = "server.delete"
	// ReqServerReinstall re-runs the egg install script for an offline server.
	ReqServerReinstall = "server.reinstall"
	// ReqServerUpdate applies new limits/spec to an offline server without reinstalling.
	ReqServerUpdate  = "server.update"
	ReqBackupCreate  = "backup.create"
	ReqBackupRestore = "backup.restore"
	ReqBackupDelete  = "backup.delete"
	ReqFilesList     = "files.list"
	ReqFilesRead     = "files.read"
	ReqFilesWrite    = "files.write"
	ReqFilesDelete   = "files.delete"
	ReqFilesMkdir    = "files.mkdir"
)

// Daemon -> panel event types (no reply expected).
const (
	EvtHello   = "hello"
	EvtConsole = "console"
	EvtState   = "state"
	EvtStats   = "stats"
	EvtReply   = "reply"
)

// Hello is the first frame a daemon sends after connecting.
type Hello struct {
	Version string   `json:"version"`
	Servers []string `json:"servers"` // UUIDs the daemon already manages
}

type ServerSpec struct {
	UUID     string            `json:"uuid"`
	Image    string            `json:"image"`
	Startup  string            `json:"startup"`
	Stop     string            `json:"stop"` // console command, or "^C"
	Env      map[string]string `json:"env"`
	MemoryMB int64             `json:"memory_mb"`
	CPUPct   int64             `json:"cpu_pct"`              // 100 = one core, 0 = unlimited
	SwapMB   int64             `json:"swap_mb,omitempty"`    // on top of memory; 0 = none, -1 = unlimited
	CPUSet   string            `json:"cpuset,omitempty"`     // "0-3"; empty = any core
	Pids     int64             `json:"pids_limit,omitempty"` // 0 = 1024
	IOWeight int64             `json:"io_weight,omitempty"`  // 10..1000, 0 = engine default
	DiskMB   int64             `json:"disk_mb,omitempty"`    // enforced by the daemon; 0 = unlimited
	Ports    []PortMap         `json:"ports"`
	// Install, when set, must succeed once before the server can start.
	Install *InstallSpec `json:"install,omitempty"`
}

// InstallSpec describes a one-shot installer container. The script is
// mounted read-only and run as `<Entrypoint> <script>`.
var entrypointRe = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

// ValidEntrypoint reports whether s is a plain program name or path. The
// daemon places it into a shell command line, so nothing else is allowed.
func ValidEntrypoint(s string) bool { return entrypointRe.MatchString(s) }

type InstallSpec struct {
	Image      string `json:"image"`
	Entrypoint string `json:"entrypoint"`
	Script     string `json:"script"`
}

type PortMap struct {
	Host      int    `json:"host"`
	Container int    `json:"container"`
	Proto     string `json:"proto"` // tcp or udp
}

type Power struct {
	UUID   string `json:"uuid"`
	Action string `json:"action"` // start | stop | restart | kill
}

type Input struct {
	UUID string `json:"uuid"`
	Line string `json:"line"`
}

type UUIDOnly struct {
	UUID string `json:"uuid"`
}

type FileReq struct {
	UUID    string `json:"uuid"`
	Path    string `json:"path"`
	Content []byte `json:"content,omitempty"`
}

type FileInfo struct {
	Name    string `json:"name"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	ModUnix int64  `json:"mod_unix"`
}

type Console struct {
	UUID string `json:"uuid"`
	Line string `json:"line"`
}

type State struct {
	UUID  string `json:"uuid"`
	State string `json:"state"` // offline | starting | running | stopping
}

type Stats struct {
	DiskBytes int64   `json:"disk_bytes"` // data directory size (sampled)
	UUID      string  `json:"uuid"`
	CPUPct    float64 `json:"cpu_pct"`
	MemBytes  uint64  `json:"mem_bytes"`
}

func Marshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// BackupReq addresses one backup archive on a node.
type BackupReq struct {
	UUID     string   `json:"uuid"`      // server
	BackupID string   `json:"backup_id"` // backup uuid
	Ignore   []string `json:"ignore,omitempty"`
	Truncate bool     `json:"truncate,omitempty"` // restore: empty the server dir first
}

// BackupResult is the reply to backup.create.
type BackupResult struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// SFTPAuthReq is posted by a daemon to the panel (HTTP, node token) to check
// an SFTP login. The password is an API key.
type SFTPAuthReq struct {
	Username string `json:"username"` // <email>.<first 8 hex of the server uuid>
	Password string `json:"password"`
	IP       string `json:"ip"` // the SFTP client's address, for rate limiting
	// Recheck marks a periodic re-validation of an open session: it is not a
	// new sign-in, so it is neither audited as one nor counted as a failed guess.
	Recheck bool `json:"recheck,omitempty"`
}

// SFTPAuthResp is the panel's decision.
type SFTPAuthResp struct {
	OK         bool   `json:"ok"`
	ServerUUID string `json:"server_uuid,omitempty"`
	Read       bool   `json:"read,omitempty"`
	Write      bool   `json:"write,omitempty"`
}
