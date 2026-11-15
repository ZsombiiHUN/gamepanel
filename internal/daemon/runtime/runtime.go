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

type Spec struct {
	proto.ServerSpec
	DataDir string // absolute host path mounted at /home/container
}

// Event is emitted when a managed server changes state.
type Event struct {
	UUID  string
	State string
}

type Runtime interface {
	// Create prepares (but does not start) the container for a server.
	Create(ctx context.Context, s Spec) error
	Start(ctx context.Context, uuid string) error
	// Stop asks the process to exit gracefully, escalating to kill after timeout.
	Stop(ctx context.Context, uuid string, timeoutSec int) error
	Kill(ctx context.Context, uuid string) error
	Remove(ctx context.Context, uuid string) error
	// Attach returns the combined console stream and a writer for stdin.
	Attach(ctx context.Context, uuid string) (io.ReadCloser, io.WriteCloser, error)
	State(ctx context.Context, uuid string) (string, error)
	Stats(ctx context.Context, uuid string) (proto.Stats, error)
	// Events streams state changes until ctx ends.
	Events(ctx context.Context) (<-chan Event, error)
	// List returns the UUIDs of servers this runtime already manages.
	List(ctx context.Context) ([]string, error)
}
