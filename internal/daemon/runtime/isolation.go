package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

// Server containers are untrusted code, and a node runs servers of different
// owners side by side. Isolation keeps them apart from each other and from
// the node:
//
//   - Every server (and installer) container joins one Docker network whose
//     bridge does not forward traffic between its containers
//     (com.docker.network.bridge.enable_icc=false). A server cannot reach
//     another server's unpublished ports (RCON, query, plugin web panels).
//
//   - Two iptables chains, kept by the daemon, cover what Docker does not:
//
//     GAMEPANEL-INPUT, jumped to from INPUT for traffic from the bridge: what
//     a server may open on the node itself. Ports that servers publish stay
//     reachable (a proxy such as Velocity reaches its backends at the node's
//     address), and so do HostPorts; everything else (SSH, the panel, SFTP,
//     a database bound to the node) is dropped.
//
//     GAMEPANEL-FORWARD, jumped to from DOCKER-USER: drops link-local
//     addresses (169.254.0.0/16: cloud metadata services hand out
//     credentials and boot scripts there) and, with BlockPrivate, private
//     networks. Replies to connections from those networks still go out.
//
// The chains are rebuilt whenever a container is created or removed, and
// checked every minute, so a firewall reload (ufw, firewalld) that drops the
// jumps is repaired.

// Docker network and bridge names used unless configured otherwise.
const (
	DefaultNetwork = "gamepanel0"
	DefaultBridge  = "gp0"
)

const (
	chainInput   = "GAMEPANEL-INPUT"
	chainForward = "GAMEPANEL-FORWARD"
	// labelLayout marks how a container was built. Containers with another
	// value predate a change to how they are built (here: the isolated
	// network and log limits) and are rebuilt at their next start.
	labelLayout   = "gamepanel.layout"
	currentLayout = "2"
	// labelPorts lists a server's published ports ("25565/tcp,25565/udp"),
	// which GAMEPANEL-INPUT lets servers reach.
	labelPorts = "gamepanel.ports"
	labelNet   = "gamepanel.network"
)

// privateRanges are dropped with BlockPrivate (link-local always is).
var privateRanges = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10"}

// PortRange is a range of node ports servers may connect to; Proto is "tcp",
// "udp" or "" for both.
type PortRange struct {
	From, To int
	Proto    string
}

// ParsePortRanges reads "3306,8000-8100/tcp" (GAMEPANEL_HOST_PORTS).
func ParsePortRanges(s string) ([]PortRange, error) {
	var out []PortRange
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var r PortRange
		spec, proto, hasProto := strings.Cut(part, "/")
		if hasProto {
			if proto != "tcp" && proto != "udp" {
				return nil, fmt.Errorf("%q: the protocol must be tcp or udp", part)
			}
			r.Proto = proto
		}
		lo, hi, isRange := strings.Cut(spec, "-")
		var err error
		if r.From, err = strconv.Atoi(lo); err != nil {
			return nil, fmt.Errorf("%q: not a port", part)
		}
		r.To = r.From
		if isRange {
			if r.To, err = strconv.Atoi(hi); err != nil {
				return nil, fmt.Errorf("%q: not a port range", part)
			}
		}
		if r.From < 1 || r.To > 65535 || r.From > r.To {
			return nil, fmt.Errorf("%q: ports go from 1 to 65535, low to high", part)
		}
		out = append(out, r)
	}
	return out, nil
}

// Isolation configures how server containers are kept apart.
type Isolation struct {
	// Network is the Docker network servers join; Bridge names its host
	// interface when the daemon creates it.
	Network, Bridge string
	// Firewall keeps the iptables chains. Off only where the daemon cannot
	// see Docker's network (Docker Desktop, rootless Docker) or another
	// firewall does the job.
	Firewall bool
	// HostPorts are node ports servers may connect to besides the ones
	// servers publish (a database for the servers, say).
	HostPorts []PortRange
	// BlockPrivate also drops traffic from servers to private networks.
	BlockPrivate bool
}

// iptablesRunner runs iptables(-restore); tests replace it.
type iptablesRunner func(ctx context.Context, stdin string, name string, args ...string) error

func execIPTables(ctx context.Context, stdin, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: fixed binaries, arguments built here
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Isolate creates the servers' network (if it does not exist) and, with
// Firewall, installs the firewall chains and keeps them in place until ctx
// ends. Containers created afterwards join the network.
func (d *Docker) Isolate(ctx context.Context, iso Isolation) error {
	if iso.Network == "" {
		iso.Network = DefaultNetwork
	}
	if iso.Bridge == "" {
		iso.Bridge = DefaultBridge
	}
	bridge, err := d.ensureNetwork(ctx, iso.Network, iso.Bridge)
	if err != nil {
		return err
	}
	iso.Bridge = bridge
	d.fwMu.Lock()
	d.iso = iso
	d.fwMu.Unlock()
	if !iso.Firewall {
		return nil
	}
	if _, err := net.InterfaceByName(bridge); err != nil {
		return fmt.Errorf("the servers' network bridge %s is not on this machine, so the daemon cannot firewall it "+
			"(Docker Desktop and rootless Docker run their network elsewhere); set GAMEPANEL_FIREWALL=off to run without it: %w", bridge, err)
	}
	bin, err := d.findIPTables(ctx)
	if err != nil {
		return err
	}
	d.fwMu.Lock()
	d.iptBin = bin
	d.fwMu.Unlock()
	if err := d.syncFirewall(ctx); err != nil {
		return err
	}
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := d.syncFirewall(ctx); err != nil && ctx.Err() == nil {
					slog.Error("could not restore the servers' firewall", "err", err)
				}
			}
		}
	}()
	return nil
}

// findIPTables picks the iptables variant whose rules Docker keeps: the one
// with Docker's DOCKER-USER chain. A host can have both the legacy and the
// nftables variant, and "iptables" (in the daemon's image, say) need not be
// the one Docker uses; rules in the other would still filter, but there is
// no DOCKER-USER chain there to put ours ahead of Docker's.
func (d *Docker) findIPTables(ctx context.Context) (string, error) {
	run := d.ipt
	if run == nil {
		run = execIPTables
	}
	var errs []error
	for _, bin := range []string{"iptables", "iptables-legacy", "iptables-nft"} {
		if d.ipt == nil {
			if _, err := exec.LookPath(bin); err != nil {
				continue
			}
		}
		err := run(ctx, "", bin, "-w", "-n", "-L", "DOCKER-USER")
		if err == nil {
			return bin, nil
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return "", errors.New("iptables is not installed; the daemon needs it to keep game servers away from this machine's services " +
			"(or set GAMEPANEL_FIREWALL=off)")
	}
	return "", fmt.Errorf("found no iptables with Docker's DOCKER-USER chain; does Docker manage iptables here (\"iptables\": false in daemon.json turns that off)? "+
		"Set GAMEPANEL_FIREWALL=off if another firewall keeps game servers away from this machine: %w", errors.Join(errs...))
}

// ensureNetwork returns the host bridge of network name, creating the
// network if needed. An existing network must not forward traffic between
// containers.
func (d *Docker) ensureNetwork(ctx context.Context, name, bridge string) (string, error) {
	res, err := d.cli.NetworkInspect(ctx, name, client.NetworkInspectOptions{})
	if cerrdefs.IsNotFound(err) {
		no := false
		_, err = d.cli.NetworkCreate(ctx, name, client.NetworkCreateOptions{
			Driver: "bridge", EnableIPv6: &no,
			Options: map[string]string{
				"com.docker.network.bridge.name":       bridge,
				"com.docker.network.bridge.enable_icc": "false",
			},
			Labels: map[string]string{labelNet: "1"},
		})
		if err != nil && !cerrdefs.IsConflict(err) { // another daemon may have made it meanwhile
			return "", fmt.Errorf("create the servers' network %s: %w", name, err)
		}
		res, err = d.cli.NetworkInspect(ctx, name, client.NetworkInspectOptions{})
	}
	if err != nil {
		return "", fmt.Errorf("inspect the servers' network %s: %w", name, err)
	}
	n := res.Network
	if n.Driver != "bridge" {
		return "", fmt.Errorf("the servers' network %s uses the %s driver; it must be a bridge", name, n.Driver)
	}
	if n.Options["com.docker.network.bridge.enable_icc"] != "false" {
		return "", fmt.Errorf("the servers' network %s lets containers reach each other (enable_icc is not false); "+
			"remove it (docker network rm %s) and the daemon creates it again correctly", name, name)
	}
	if b := n.Options["com.docker.network.bridge.name"]; b != "" {
		return b, nil
	}
	if len(n.ID) < 12 {
		return "", fmt.Errorf("the servers' network %s has no usable id", name)
	}
	return "br-" + n.ID[:12], nil // Docker's name for a bridge it named itself
}

// syncFirewall rebuilds the chains from the containers that exist now and
// makes sure INPUT and DOCKER-USER jump to them. It is a no-op without
// Firewall.
func (d *Docker) syncFirewall(ctx context.Context) error {
	d.fwMu.Lock()
	defer d.fwMu.Unlock()
	if !d.iso.Firewall {
		return nil
	}
	res, err := d.cli.ContainerList(ctx, client.ContainerListOptions{
		All: true, Filters: client.Filters{"label": {labelServer: true}},
	})
	if err != nil {
		return err
	}
	var published []string
	for _, c := range res.Items {
		published = append(published, c.Labels[labelPorts])
	}
	run := d.ipt
	if run == nil {
		run = execIPTables
	}
	bin := d.iptBin
	if bin == "" {
		bin = "iptables"
	}
	if err := run(ctx, firewallRules(d.iso, published), bin+"-restore", "-w", "--noflush"); err != nil {
		return err
	}
	for _, j := range [][2]string{{"INPUT", chainInput}, {"DOCKER-USER", chainForward}} {
		rule := []string{j[0], "-i", d.iso.Bridge, "-j", j[1]}
		if run(ctx, "", bin, append([]string{"-w", "-C"}, rule...)...) == nil {
			continue
		}
		if err := run(ctx, "", bin, append([]string{"-w", "-I", j[0], "1"}, rule[1:]...)...); err != nil {
			return err
		}
	}
	return nil
}

// firewallRules is the iptables-restore input for both chains. published
// holds labelPorts values.
func firewallRules(iso Isolation, published []string) string {
	type port struct {
		n     int
		proto string
	}
	seen := map[port]bool{}
	var ports []port
	add := func(p port) {
		if !seen[p] {
			seen[p] = true
			ports = append(ports, p)
		}
	}
	for _, label := range published {
		for _, p := range strings.Split(label, ",") {
			num, proto, ok := strings.Cut(p, "/")
			n, err := strconv.Atoi(num)
			if ok && err == nil && n > 0 && n <= 65535 && (proto == "tcp" || proto == "udp") {
				add(port{n, proto})
			}
		}
	}
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].n != ports[j].n {
			return ports[i].n < ports[j].n
		}
		return ports[i].proto < ports[j].proto
	})

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w("*filter")
	w(":%s - [0:0]", chainInput)
	w(":%s - [0:0]", chainForward)
	w("-F %s", chainInput)
	w("-F %s", chainForward)
	w("-A %s -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT", chainInput)
	for _, p := range ports {
		w("-A %s -p %s --dport %d -j ACCEPT", chainInput, p.proto, p.n)
	}
	for _, r := range iso.HostPorts {
		dport := strconv.Itoa(r.From)
		if r.To != r.From {
			dport += ":" + strconv.Itoa(r.To)
		}
		for _, proto := range []string{"tcp", "udp"} {
			if r.Proto == "" || r.Proto == proto {
				w("-A %s -p %s --dport %s -j ACCEPT", chainInput, proto, dport)
			}
		}
	}
	w("-A %s -j DROP", chainInput)
	w("-A %s -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN", chainForward)
	w("-A %s -d 169.254.0.0/16 -j DROP", chainForward)
	if iso.BlockPrivate {
		for _, cidr := range privateRanges {
			w("-A %s -d %s -j DROP", chainForward, cidr)
		}
	}
	w("-A %s -j RETURN", chainForward)
	w("COMMIT")
	return b.String()
}

// portsLabel is the labelPorts value for a server's port bindings.
func portsLabel(ports []string) string {
	sort.Strings(ports)
	return strings.Join(ports, ",")
}

// Outdated lists servers whose containers were built by an earlier version
// of the daemon (a missing or older labelLayout).
func (d *Docker) Outdated(ctx context.Context) ([]string, error) {
	res, err := d.cli.ContainerList(ctx, client.ContainerListOptions{
		All: true, Filters: client.Filters{"label": {labelServer: true}},
	})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range res.Items {
		if id := c.Labels[labelServer]; id != "" && c.Labels[labelLayout] != currentLayout {
			out = append(out, id)
		}
	}
	return out, nil
}

// errNoIsolation is returned when a container would be created before
// Isolate ran: it would land on Docker's shared default network.
var errNoIsolation = errors.New("the servers' network is not set up (Isolate was not called)")
