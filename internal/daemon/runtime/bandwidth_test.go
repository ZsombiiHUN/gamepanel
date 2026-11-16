package runtime

import (
	"strings"
	"testing"
)

func TestTCCommands(t *testing.T) {
	cmds := tcCommands("eth0", 100)
	got := make([]string, len(cmds))
	for i, c := range cmds {
		got[i] = strings.Join(c, " ")
	}
	want := []string{
		"qdisc replace dev eth0 root tbf rate 100mbit burst 131072 latency 50ms",
		"qdisc replace dev eth0 handle ffff: ingress",
		"filter replace dev eth0 parent ffff: prio 1 matchall action police rate 100mbit burst 131072 conform-exceed drop",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("command %d:\n got %s\nwant %s", i, got[i], want[i])
		}
	}
	// Slow links still get a burst bigger than any packet.
	if c := tcCommands("eth0", 1); c[0][9] != "131072" {
		t.Fatalf("burst for 1 mbit: %s", c[0][9])
	}
}
