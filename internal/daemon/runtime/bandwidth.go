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
// inside the container's network namespace with iproute2: a token bucket on
// what the server sends, and on what it receives too, by redirecting
// incoming packets through an ifb device and shaping them there. Where the
// kernel has no ifb, incoming traffic is policed instead (dropped above the
// rate), which is coarser. Needs root, tc, ip and nsenter on the node. Never
// fatal: a server whose limit cannot be applied still starts, and the daemon
// says so on its console.
//
// Measured on a real Docker host (scripts/bandwidth-smoke.sh) with TCP:
// shaping holds both directions at about 96% of the limit. A policer whose
// burst is only a few milliseconds of traffic lets TCP reach half the limit
// at 10 Mbit/s and a tenth of it at 100 Mbit/s, because every drop halves
// TCP's sending rate; with a quarter of a second of burst it reaches the
// limit, so that is the fallback's burst.

const (
	labelBandwidth = "gamepanel.bandwidth_mbit"
	ifbDev         = "gp-ifb"
)

// IngressMode is how incoming traffic ended up limited.
type IngressMode string

const (
	IngressShaped    IngressMode = "shaped"    // ifb + token bucket
	IngressPoliced   IngressMode = "policed"   // no ifb in this kernel: drop above the rate
	IngressUnlimited IngressMode = "unlimited" // no limit set
)

// step is one command run in the container's network namespace.
type step struct {
	bin  string // "tc" or "ip"
	args []string
	// mayFail: clean-up of a previous run, which has nothing to remove the
	// first time.
	mayFail bool
}

func (s step) String() string { return s.bin + " " + strings.Join(s.args, " ") }

func rate(mbit int64) string { return strconv.FormatInt(mbit, 10) + "mbit" }

// tbfBurst is about 10 ms of traffic at full rate, and never less than 128
// KiB: a token bucket drops every packet larger than its burst, so the burst
// must exceed the largest packet (64 KiB with offloads, 9000-byte jumbo
// frames on some networks).
func tbfBurst(mbit int64) string { return strconv.FormatInt(max(mbit*1_000_000/8/100, 128<<10), 10) }

// policeBurst is a quarter of a second of traffic (see the measurements above).
func policeBurst(mbit int64) string { return strconv.FormatInt(max(mbit*1_000_000/8/4, 128<<10), 10) }

// egressSteps limit what dev sends. replace makes them safe to run again.
func egressSteps(dev string, mbit int64) []step {
	return []step{{bin: "tc", args: []string{"qdisc", "replace", "dev", dev, "root", "tbf", "rate", rate(mbit), "burst", tbfBurst(mbit), "latency", "50ms"}}}
}

// cleanIngress removes what an earlier run set up for incoming traffic, so
// either kind of ingress limit can be (re)applied on top.
func cleanIngress(dev string) []step {
	return []step{
		{bin: "tc", args: []string{"qdisc", "del", "dev", dev, "ingress"}, mayFail: true},
		{bin: "ip", args: []string{"link", "del", ifbDev}, mayFail: true},
	}
}

// shapedIngressSteps send what dev receives through an ifb device and shape
// it there with the same token bucket as egress.
func shapedIngressSteps(dev string, mbit int64) []step {
	return []step{
		{bin: "ip", args: []string{"link", "add", ifbDev, "type", "ifb"}},
		{bin: "ip", args: []string{"link", "set", ifbDev, "up"}},
		{bin: "tc", args: []string{"qdisc", "add", "dev", ifbDev, "root", "tbf", "rate", rate(mbit), "burst", tbfBurst(mbit), "latency", "50ms"}},
		{bin: "tc", args: []string{"qdisc", "add", "dev", dev, "handle", "ffff:", "ingress"}},
		{bin: "tc", args: []string{"filter", "add", "dev", dev, "parent", "ffff:", "prio", "1", "matchall", "action", "mirred", "egress", "redirect", "dev", ifbDev}},
	}
}

// policedIngressSteps drop what dev receives above the rate.
func policedIngressSteps(dev string, mbit int64) []step {
	return []step{
		{bin: "tc", args: []string{"qdisc", "add", "dev", dev, "handle", "ffff:", "ingress"}},
		{bin: "tc", args: []string{"filter", "add", "dev", dev, "parent", "ffff:", "prio", "1", "matchall", "action", "police", "rate", rate(mbit), "burst", policeBurst(mbit), "conform-exceed", "drop"}},
	}
}

// runner runs a step in a network namespace.
type runner func(ctx context.Context, s step) error

// applyLimit limits dev to mbit each way: egress always shaped, ingress
// shaped when the kernel has ifb and policed otherwise. Safe to run again on
// the same namespace.
func applyLimit(ctx context.Context, run runner, dev string, mbit int64) (IngressMode, error) {
	do := func(steps []step) error {
		for _, s := range steps {
			if err := run(ctx, s); err != nil && !s.mayFail {
				return err
			}
		}
		return nil
	}
	if err := do(egressSteps(dev, mbit)); err != nil {
		return "", err
	}
	do(cleanIngress(dev))
	shapeErr := do(shapedIngressSteps(dev, mbit))
	if shapeErr == nil {
		return IngressShaped, nil
	}
	do(cleanIngress(dev))
	if err := do(policedIngressSteps(dev, mbit)); err != nil {
		return "", fmt.Errorf("%w (shaping it first failed: %w)", err, shapeErr)
	}
	return IngressPoliced, nil
}

// LimitBandwidth applies the limit set at Create to a running container.
func (d *Docker) LimitBandwidth(ctx context.Context, uuid string) (IngressMode, error) {
	res, err := d.cli.ContainerInspect(ctx, name(uuid), client.ContainerInspectOptions{})
	if err != nil {
		return "", err
	}
	mbit, _ := strconv.ParseInt(res.Container.Config.Labels[labelBandwidth], 10, 64)
	if mbit <= 0 {
		return IngressUnlimited, nil
	}
	pid := res.Container.State.Pid
	if pid <= 0 {
		return "", fmt.Errorf("the container is not running")
	}
	return applyLimit(ctx, nsRunner(pid), "eth0", mbit)
}

// nsRunner runs steps with nsenter in the network namespace of pid.
func nsRunner(pid int) runner {
	return func(ctx context.Context, s step) error {
		args := append([]string{"--target", strconv.Itoa(pid), "--net", "--", s.bin}, s.args...)
		if out, err := exec.CommandContext(ctx, "nsenter", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w: %s", s, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
}
