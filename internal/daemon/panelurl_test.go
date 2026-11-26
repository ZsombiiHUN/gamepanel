package daemon

import "testing"

func TestNormalizePanelURL(t *testing.T) {
	ok := []struct{ in, want string }{
		{"ws://127.0.0.1:8080/daemon/connect", "ws://127.0.0.1:8080/daemon/connect"},
		{"wss://panel.example.com/daemon/connect", "wss://panel.example.com/daemon/connect"},
		{"https://panel.example.com", "wss://panel.example.com/daemon/connect"},
		{"https://panel.example.com/", "wss://panel.example.com/daemon/connect"},
		{"wss://panel.example.com", "wss://panel.example.com/daemon/connect"},
		{" http://10.0.0.2:8080\n", "ws://10.0.0.2:8080/daemon/connect"}, // pasted with stray whitespace
	}
	for _, c := range ok {
		got, err := NormalizePanelURL(c.in)
		if err != nil || got != c.want {
			t.Errorf("NormalizePanelURL(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, in := range []string{"panel.example.com", "ftp://panel.example.com", "https://", ""} {
		if got, err := NormalizePanelURL(in); err == nil {
			t.Errorf("NormalizePanelURL(%q) = %q, want an error", in, got)
		}
	}
}

func TestCheckPanelTransport(t *testing.T) {
	for _, u := range []string{
		"wss://panel.example.com/daemon/connect", "wss://203.0.113.5/daemon/connect",
		"ws://127.0.0.1:8080/daemon/connect", "ws://localhost:8080/daemon/connect", "ws://[::1]:8080/daemon/connect",
		"ws://10.0.0.2:8080/daemon/connect", "ws://192.168.1.10/daemon/connect", "ws://[fd00::2]/daemon/connect",
	} {
		if err := CheckPanelTransport(u, false); err != nil {
			t.Errorf("%s refused: %v", u, err)
		}
	}
	for _, u := range []string{"ws://panel.example.com/daemon/connect", "ws://203.0.113.5/daemon/connect", "ws://panel/daemon/connect"} {
		if err := CheckPanelTransport(u, false); err == nil {
			t.Errorf("%s accepted", u)
		}
		if err := CheckPanelTransport(u, true); err != nil {
			t.Errorf("%s refused although allowed: %v", u, err)
		}
	}
}
