package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/moby/moby/client"
)

// Network bandwidth limits. Docker has none, so the daemon shapes traffic
// inside the container's network namespace with tc (iproute2): a token
// bucket on what the server sends, and a policer on what it receives. Needs
// root, tc and nsenter on the node. Never fatal: a server whose limit cannot
// be applied still starts, and the daemon says so on its console.

const labelBandwidth = "gamepanel.bandwidth_mbit"

// tcCommands are the tc arguments that limit dev to mbit megabits per
// second each way. replace makes them safe to run again.
func tcCommands(dev string, mbit int64) [][]string {
	rate := strconv.FormatInt(mbit, 10) + "mbit"
	// Room for about 10 ms of traffic at full rate, and never less than
	// 128 KiB: a token bucket drops every packet larger than its burst, so
	// the burst must exceed the largest packet (64 KiB on loopback, 9000-byte
	// jumbo frames on some networks).
	burst := strconv.FormatInt(max(mbit*1_000_000/8/100, 128<<10), 10)
	return [][]string{
		{"qdisc", "replace", "dev", dev, "root", "tbf", "rate", rate, "burst", burst, "latency", "50ms"},
		{"qdisc", "replace", "dev", dev, "handle", "ffff:", "ingress"},
		{"filter", "replace", "dev", dev, "parent", "ffff:", "prio", "1", "matchall", "action", "police", "rate", rate, "burst", burst, "conform-exceed", "drop"},
	}
}

// LimitBandwidth applies the limit set at Create to a running container.
func (d *Docker) LimitBandwidth(ctx context.Context, uuid string) error {
	res, err := d.cli.ContainerInspect(ctx, name(uuid), client.ContainerInspectOptions{})
	if err != nil {
		return err
	}
	mbit, _ := strconv.ParseInt(res.Container.Config.Labels[labelBandwidth], 10, 64)
	if mbit <= 0 {
		return nil
	}
	pid := res.Container.State.Pid
	if pid <= 0 {
		return fmt.Errorf("the container is not running")
	}
	for _, args := range tcCommands("eth0", mbit) {
		cmd := exec.CommandContext(ctx, "nsenter", append([]string{"--target", strconv.Itoa(pid), "--net", "--", "tc"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("tc %s: %v: %s", strings.Join(args[:2], " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
