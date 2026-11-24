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
