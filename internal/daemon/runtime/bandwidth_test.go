package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// recorder runs nothing; it records the steps and fails those that match fail.
type recorder struct {
	ran  []string
	fail func(step) bool
}

func (r *recorder) run(_ context.Context, s step) error {
	r.ran = append(r.ran, s.String())
	if r.fail != nil && r.fail(s) {
		return errors.New("refused")
	}
	return nil
}

func TestBandwidthShapesBothWays(t *testing.T) {
	r := &recorder{}
	mode, err := applyLimit(context.Background(), r.run, 100)
	if err != nil || mode != IngressShaped {
		t.Fatalf("%v %v", mode, err)
	}
	want := []string{
		"tc qdisc replace dev eth0 root tbf rate 100mbit burst 131072 latency 50ms",
		"tc qdisc del dev eth0 ingress",
		"ip link del gp-ifb",
		"ip link add gp-ifb type ifb",
		"ip link set gp-ifb up",
		"tc qdisc add dev gp-ifb root tbf rate 100mbit burst 131072 latency 50ms",
		"tc qdisc add dev eth0 handle ffff: ingress",
		"tc filter add dev eth0 parent ffff: prio 1 matchall action mirred egress redirect dev gp-ifb",
	}
	if strings.Join(r.ran, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ran:\n%s\nwant:\n%s", strings.Join(r.ran, "\n"), strings.Join(want, "\n"))
	}
}

// Clean-up of an earlier run failing (nothing to remove) is fine, so applying
// the limit again on the same namespace works.
func TestBandwidthCleanUpMayFail(t *testing.T) {
	r := &recorder{fail: func(s step) bool { return s.mayFail }}
	if mode, err := applyLimit(context.Background(), r.run, 10); err != nil || mode != IngressShaped {
		t.Fatalf("%v %v", mode, err)
	}
}

// Without ifb in the kernel, incoming traffic is policed with a burst big
// enough for TCP to reach the limit.
func TestBandwidthFallsBackToPolicing(t *testing.T) {
	r := &recorder{fail: func(s step) bool { return strings.Contains(s.String(), "type ifb") }}
	mode, err := applyLimit(context.Background(), r.run, 100)
	if err != nil || mode != IngressPoliced {
		t.Fatalf("%v %v", mode, err)
	}
	last := r.ran[len(r.ran)-1]
	if last != "tc filter add dev eth0 parent ffff: prio 1 matchall action police rate 100mbit burst 3125000 conform-exceed drop" {
		t.Fatalf("police step: %s", last)
	}
	// The half-built shaping was removed before policing was set up.
	if r.ran[len(r.ran)-4] != "tc qdisc del dev eth0 ingress" {
		t.Fatalf("no clean-up between attempts:\n%s", strings.Join(r.ran, "\n"))
	}
}

func TestBandwidthFailsWithoutTC(t *testing.T) {
	r := &recorder{fail: func(step) bool { return true }}
	if _, err := applyLimit(context.Background(), r.run, 10); err == nil {
		t.Fatal("no error when nothing could be applied")
	}
	if len(r.ran) != 1 {
		t.Fatalf("kept going after egress failed: %v", r.ran)
	}
	// Egress works but neither kind of ingress does: an error, not a silent half limit.
	r = &recorder{fail: func(s step) bool { return strings.Contains(s.String(), "ingress") && !s.mayFail }}
	if _, err := applyLimit(context.Background(), r.run, 10); err == nil {
		t.Fatal("no error when ingress could not be limited")
	}
}

func TestBandwidthBursts(t *testing.T) {
	for _, c := range []struct {
		mbit        int64
		tbf, police string
	}{
		{1, "131072", "131072"},       // never below the largest packet
		{10, "131072", "312500"},      // 250 ms at 10 Mbit/s
		{1000, "1250000", "31250000"}, // 10 ms and 250 ms at 1 Gbit/s
	} {
		if got := tbfBurst(c.mbit); got != c.tbf {
			t.Errorf("tbf burst for %d mbit: %s want %s", c.mbit, got, c.tbf)
		}
		if got := policeBurst(c.mbit); got != c.police {
			t.Errorf("police burst for %d mbit: %s want %s", c.mbit, got, c.police)
		}
	}
}
