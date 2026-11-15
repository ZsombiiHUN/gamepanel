// Package proto defines the wire protocol between the panel and node daemons.
//
// The daemon dials out to the panel over a single websocket, so nodes need no
// inbound ports for the control channel. All frames are JSON envelopes.
package proto

import "encoding/json"

// Envelope is the single frame type on the wire.
type Envelope struct {
	Type string          `json:"type"`
	ID   string          `json:"id,omitempty"` // request/reply correlation
	Data json.RawMessage `json:"data,omitempty"`
	Err  string          `json:"err,omitempty"` // set on failed replies
}

// Panel -> daemon request types.
const (
	ReqServerCreate = "server.create"
	ReqServerPower  = "server.power"
	ReqServerInput  = "server.input"
	ReqServerDelete = "server.delete"
	// ReqServerReinstall re-runs the egg install script for an offline server.
	ReqServerReinstall = "server.reinstall"
	ReqFilesList       = "files.list"
	ReqFilesRead       = "files.read"
	ReqFilesWrite      = "files.write"
	ReqFilesDelete     = "files.delete"
	ReqFilesMkdir      = "files.mkdir"
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
	CPUPct   int64             `json:"cpu_pct"` // 100 = one core, 0 = unlimited
	Ports    []PortMap         `json:"ports"`
	// Install, when set, must succeed once before the server can start.
	Install *InstallSpec `json:"install,omitempty"`
}

// InstallSpec describes a one-shot installer container. The script is
// mounted read-only and run as `<Entrypoint> <script>`.
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
	UUID     string  `json:"uuid"`
	CPUPct   float64 `json:"cpu_pct"`
	MemBytes uint64  `json:"mem_bytes"`
}

func Marshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
