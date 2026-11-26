package netguard

import (
	"errors"
	"net"
	"testing"
)

func TestPublicIP(t *testing.T) {
	t.Parallel()
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "1.1.1.1": true, "2606:4700:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "0.0.0.0": false, "::1": false,
		"fe80::1": false, "fc00::1": false, "::ffff:127.0.0.1": false, "64:ff9b::a00:1": false,
		"224.0.0.1": false, "255.255.255.255": false, "198.18.0.1": false,
		"2002:7f00:1::1": false, "2002:a00:1::1": false, "2002:a9fe:a9fe::1": false, "2002:808:808::1": true,
		"::127.0.0.1": false, "::10.0.0.1": false, "::8.8.8.8": false,
	} {
		if got := Public(net.ParseIP(addr)); got != want {
			t.Errorf("Public(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestControl(t *testing.T) {
	if err := Control("tcp4", "8.8.8.8:443", nil); err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"127.0.0.1:80", "[::1]:80", "169.254.169.254:80", "10.0.0.1:8080", "host:80"} {
		if err := Control("tcp", a, nil); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s: %v", a, err)
		}
	}
	if err := Control("tcp", "no-port", nil); err == nil || errors.Is(err, ErrBlocked) {
		t.Errorf("bad address: %v", err)
	}
}
