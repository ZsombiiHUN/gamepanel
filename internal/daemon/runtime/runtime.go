// Package runtime abstracts the container engine so the daemon can be tested
// without Docker and ported to other engines later.
package runtime

import (
	"context"
	"io"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

const (
	StateOffline  = "offline"
	StateStarting = "starting"
	StateRunning  = "running"
	StateStopping = "stopping"
)

// AttachTail is how much earlier output a console attach replays, matching
// the panel's backlog.
const AttachTail = 500

type Spec struct {
	proto.ServerSpec
	DataDir string // absolute host path mounted at /home/container
}

// Event is emitted when a managed server changes state.
type Event struct {
	UUID  string
	State string
	// Exit is set on the event that reports the server's process ended.
	Exit *Exit
}

// Exit describes how a server's process ended.
type Exit struct {
	Code int
	OOM  bool // the kernel's out-of-memory killer ended it
}

// InstallRun is a one-shot installer job.
type InstallRun struct {
	UUID       string
	Image      string
	Entrypoint string
	ScriptDir  string // host dir holding install.sh, mounted read-only
	DataDir    string // server data dir, mounted at /mnt/server
	Env        map[string]string
	MemoryMB   int64
}

type Runtime interface {
	// Install runs the installer container to completion, streaming its
	// output to out. A non-nil error means it did not exit 0 (or could not run).
	Install(ctx context.Context, r InstallRun, out io.Writer) error
	// Create prepares (but does not start) the container for a server.
	Create(ctx context.Context, s Spec) error
	Start(ctx context.Context, uuid string) error
	// Stop asks the process to exit gracefully, escalating to kill after timeout.
	Stop(ctx context.Context, uuid string, timeoutSec int) error
	Kill(ctx context.Context, uuid string) error
	Remove(ctx context.Context, uuid string) error
	// Attach returns the combined console stream of the current run, starting
	// with at most AttachTail earlier lines, and a writer for stdin.
	Attach(ctx context.Context, uuid string) (io.ReadCloser, io.WriteCloser, error)
	State(ctx context.Context, uuid string) (string, error)
	// WatchStats calls fn with each CPU and memory sample of a running
	// server (about one a second) until the container stops or ctx ends.
	WatchStats(ctx context.Context, uuid string, fn func(proto.Stats)) error
	// Events streams state changes until ctx ends.
	Events(ctx context.Context) (<-chan Event, error)
	// LimitBandwidth applies the server's network limit (set at Create) to
	// its running container; a no-op without one.
	LimitBandwidth(ctx context.Context, uuid string) error
	// List returns the UUIDs of servers this runtime already manages.
	List(ctx context.Context) ([]string, error)
}
