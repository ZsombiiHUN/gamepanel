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
	// ReqServerUpdate applies a new spec without reinstalling: at once when the
	// server is offline, otherwise at its next start (see UpdateResult).
	ReqServerUpdate  = "server.update"
	ReqBackupCreate  = "backup.create"
	ReqBackupRestore = "backup.restore"
	ReqBackupDelete  = "backup.delete"
	ReqFilesList     = "files.list"
	ReqFilesRead     = "files.read"
	ReqFilesWrite    = "files.write"
	ReqFilesDelete   = "files.delete"
	ReqFilesMkdir    = "files.mkdir"
	// ReqFilesOp runs a file manager operation (FileOp.Op).
	ReqFilesOp = "files.op"
	// Transfers stream through the relay (see Transfer).
	ReqFilesUpload    = "files.upload"
	ReqFilesDownload  = "files.download"
	ReqFilesPull      = "files.pull"
	ReqBackupDownload = "backup.download"
)

// File manager operations carried by files.op.
const (
	OpMove       = "move"       // Moves
	OpCopy       = "copy"       // Path -> a free "<name> copy" next to it
	OpChmod      = "chmod"      // Paths, Mode
	OpDelete     = "delete"     // Paths
	OpCompress   = "compress"   // Root, Paths, Format
	OpDecompress = "decompress" // Path into Root
)

// FileOp is one file manager operation. Paths are relative to the server's
// directory, like every other file request.
type FileOp struct {
	UUID   string   `json:"uuid"`
	Op     string   `json:"op"`
	Root   string   `json:"root,omitempty"`
	Path   string   `json:"path,omitempty"`
	Paths  []string `json:"paths,omitempty"`
	Moves  []Move   `json:"moves,omitempty"`
	Mode   uint32   `json:"mode,omitempty"`
	Format string   `json:"format,omitempty"` // compress: zip | tar.gz
}

type Move struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// FileOpResult is the reply to files.op.
type FileOpResult struct {
	// Path of what was created (copy, compress), if anything.
	Path string `json:"path,omitempty"`
}

// Transfer asks the daemon to stream a file through the panel's relay. The
// daemon connects to <panel>/daemon/relay/<Ticket> with its node token:
// downloads POST the file there, uploads GET the bytes from there. Transfers
// thus need no inbound port on the node and never pass through the control
// websocket.
type Transfer struct {
	UUID     string `json:"uuid"`
	Path     string `json:"path,omitempty"`      // file to read or write
	BackupID string `json:"backup_id,omitempty"` // backup.download
	Ticket   string `json:"ticket,omitempty"`
	Size     int64  `json:"size,omitempty"` // upload: bytes the browser announced
	URL      string `json:"url,omitempty"`  // files.pull: where to fetch from
	// SHA512 (hex), when set, must match what was downloaded, or the file
	// is not kept (mod installs).
	SHA512 string `json:"sha512,omitempty"`
	// Max, when set, is the largest file a download may send; a bigger one
	// is refused before anything is relayed (the editor's limit).
	Max int64 `json:"max,omitempty"`
}

// TransferResult is the reply to a transfer once it finished.
type TransferResult struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// Relay headers a daemon sends with a download.
const (
	HeaderRelaySize = "X-Gamepanel-Size"
	HeaderRelayName = "X-Gamepanel-Name"
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
	// SFTPPort is the port the daemon's SFTP server listens on; 0 = SFTP off.
	SFTPPort int `json:"sftp_port,omitempty"`
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
	// BandwidthMbit limits network traffic each way (tc); 0 = unlimited.
	BandwidthMbit int64     `json:"bandwidth_mbit,omitempty"`
	Ports         []PortMap `json:"ports"`
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
	// Mode is the permission bits (0644); Symlink marks a link, which the
	// panel never follows.
	Mode    uint32 `json:"mode"`
	Symlink bool   `json:"symlink,omitempty"`
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
	// Pending: settings changed while the server ran; they apply at its next start.
	Pending bool `json:"pending,omitempty"`
}

func Marshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// UpdateResult is the reply to server.update.
type UpdateResult struct {
	// Pending: the server was running, so the change applies when it next starts.
	Pending bool `json:"pending"`
}

// BackupReq addresses one backup archive on a node.
type BackupReq struct {
	UUID     string   `json:"uuid"`      // server
	BackupID string   `json:"backup_id"` // backup uuid
	Ignore   []string `json:"ignore,omitempty"`
	Truncate bool     `json:"truncate,omitempty"` // restore: empty the server dir first
	// Remote: create uploads the archive through the panel's presigned URLs
	// (see BackupUpload) and keeps no local copy.
	Remote bool `json:"remote,omitempty"`
	// URL: restore downloads the archive from here (a presigned URL) instead
	// of reading a local file.
	URL string `json:"url,omitempty"`
}

// BackupUploadReq is what a node posts to <panel>/daemon/backups/upload to
// get presigned URLs for a finished archive.
type BackupUploadReq struct {
	Server   string `json:"server"`
	BackupID string `json:"backup_id"`
	Size     int64  `json:"size"`
}

// BackupUpload tells the node how to upload: one PUT per part, in order.
type BackupUpload struct {
	PartSize int64    `json:"part_size"`
	URLs     []string `json:"urls"`
}

// BackupCompleteReq is posted to <panel>/daemon/backups/complete once every
// part is up (or Failed, to abort the upload).
type BackupCompleteReq struct {
	Server   string       `json:"server"`
	BackupID string       `json:"backup_id"`
	Parts    []UploadPart `json:"parts"`
	Failed   bool         `json:"failed,omitempty"`
}

type UploadPart struct {
	Number int    `json:"number"`
	ETag   string `json:"etag"`
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
