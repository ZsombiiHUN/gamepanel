// Package netguard keeps outgoing connections that a user or an untrusted
// party chooses the address of (pull from URL, webhooks) away from private,
// loopback, link-local and other non-public addresses.
//
// The check runs in the dialer, after DNS resolution and for every
// connection, so neither a hostname that resolves inward (DNS rebinding) nor
// a redirect can get around it.
package netguard

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"syscall"
)

// ErrBlocked is wrapped by the error a blocked connection fails with.
var ErrBlocked = errors.New("refusing to connect to a non-public address")

// Control is a net.Dialer Control function that refuses non-public addresses.
func Control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !Public(ip) {
		return fmt.Errorf("%w: %s", ErrBlocked, host)
	}
	return nil
}

// Public reports whether ip is a globally routable unicast address.
func Public(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 0, // "this network"
			v4[0] == 100 && v4[1]&0xc0 == 64,         // carrier-grade NAT 100.64/10
			v4[0] == 192 && v4[1] == 0 && v4[2] == 0, // IETF protocol assignments
			v4[0] == 198 && v4[1]&0xfe == 18,         // benchmarking 198.18/15
			v4[0] >= 240:                             // reserved and broadcast
			return false
		}
		return true
	}
	if len(ip) != net.IPv6len {
		return false
	}
	switch {
	// NAT64 (64:ff9b::/96): the gateway connects to the IPv4 address inside.
	case ip[0] == 0x00 && ip[1] == 0x64 && ip[2] == 0xff && ip[3] == 0x9b:
		return Public(ip[12:16])
	// 6to4 (2002::/16): a relay connects to the IPv4 address inside.
	case ip[0] == 0x20 && ip[1] == 0x02:
		return Public(ip[2:6])
	// IPv4-compatible (::/96), deprecated since RFC 4291 and never a real
	// destination; a host with an old tunnel set up could route it to IPv4.
	case bytes.Equal(ip[:12], make([]byte, 12)):
		return false
	}
	return true
}
