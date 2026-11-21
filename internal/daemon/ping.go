package daemon

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/minecraft"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// pingHost is where a server's published ports are reached from the node
// itself (tests replace it).
var pingHost = "127.0.0.1"

// ping asks a running server for its Minecraft status on its primary port,
// which the container publishes on every address of the node.
func (d *Daemon) ping(ctx context.Context, uuid string) (minecraft.ServerStatus, error) {
	d.mu.Lock()
	m := d.servers[uuid]
	var port int
	running := false
	if m != nil {
		running = m.running
		port, _ = strconv.Atoi(m.spec.Env["SERVER_PORT"])
		if port == 0 && len(m.spec.Ports) > 0 {
			port = m.spec.Ports[0].Host
		}
	}
	d.mu.Unlock()
	if m == nil {
		return minecraft.ServerStatus{}, errUnknownServer
	}
	if !running {
		return minecraft.ServerStatus{}, proto.Conflict("the server is not running")
	}
	if port <= 0 {
		return minecraft.ServerStatus{}, proto.Invalid("the server has no port")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	st, err := minecraft.Ping(ctx, net.JoinHostPort(pingHost, strconv.Itoa(port)))
	if err != nil {
		return st, proto.Conflict("the server did not answer a status request: " + err.Error())
	}
	return st, nil
}
