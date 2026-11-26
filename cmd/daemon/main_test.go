package main

import "testing"

func TestContainerUser(t *testing.T) {
	for in, want := range map[string][2]int{"": {0, 0}, "2500": {2500, 2500}, "2500:2600": {2500, 2600}} {
		u, g, err := containerUser(in)
		if err != nil || u != want[0] || g != want[1] {
			t.Errorf("%q: %d:%d %v", in, u, g, err)
		}
	}
	for _, bad := range []string{"0", "root", "-1", "2500:", "2500:0", "2500:x"} {
		if _, _, err := containerUser(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestIsolationSettings(t *testing.T) {
	t.Setenv("GAMEPANEL_FIREWALL", "")
	t.Setenv("GAMEPANEL_HOST_PORTS", "3306/tcp")
	iso, err := isolation()
	if err != nil || !iso.Firewall || iso.Network != "gamepanel0" || iso.Bridge != "gp0" || len(iso.HostPorts) != 1 || iso.BlockPrivate {
		t.Fatalf("defaults: %+v %v", iso, err)
	}
	t.Setenv("GAMEPANEL_FIREWALL", "off")
	t.Setenv("GAMEPANEL_BLOCK_PRIVATE", "1")
	if iso, _ := isolation(); iso.Firewall || !iso.BlockPrivate {
		t.Fatalf("off: %+v", iso)
	}
	t.Setenv("GAMEPANEL_HOST_PORTS", "nope")
	if _, err := isolation(); err == nil {
		t.Fatal("a bad port list was accepted")
	}
}
