package daemon

// The panel address: the forms people paste, and refusing unencrypted ones.

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// CheckPanelTransport refuses a plain ws:// panel address unless the panel
// is on this machine or a private network address, or allowInsecure. Over
// ws:// the node token, consoles and every file transfer cross the network
// unencrypted, and anyone holding the token can pose as the node.
func CheckPanelTransport(panelURL string, allowInsecure bool) error {
	u, err := url.Parse(panelURL)
	if err != nil {
		return err
	}
	if u.Scheme != "ws" || allowInsecure {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		return nil
	}
	return fmt.Errorf("GAMEPANEL_PANEL_URL %q is plain ws:// to an address that is not on this machine or a private network: "+
		"the node token, consoles and files would cross the internet unencrypted. Use wss:// (https://), "+
		"or set GAMEPANEL_ALLOW_INSECURE_PANEL_URL=1 if the path is protected some other way (a VPN)", panelURL)
}

// NormalizePanelURL accepts the panel's address in the forms people paste:
// http(s):// becomes ws(s)://, and a bare host gets the /daemon/connect path.
// Without this, a URL like https://panel.example.com only fails later with
// "expected handshake response status code 101 but got 200".
func NormalizePanelURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("GAMEPANEL_PANEL_URL: %w", err)
	}
	switch u.Scheme {
	case "ws", "wss":
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("GAMEPANEL_PANEL_URL %q: want wss://panel.example.com/daemon/connect", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("GAMEPANEL_PANEL_URL %q has no host", raw)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/daemon/connect"
	}
	return u.String(), nil
}
