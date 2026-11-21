// Package minecraft holds what the panel and the daemon know about Minecraft:
// the Server List Ping, the version lists of the server projects GamePanel's
// eggs install, and player name lookups.
package minecraft

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// ServerStatus is what a server answers to the Server List Ping, the request the
// game's multiplayer screen sends.
type ServerStatus struct {
	Version  string   `json:"version" doc:"the version name the server reports, such as \"Paper 1.21.11\""`
	Protocol int      `json:"protocol"`
	Online   int      `json:"online"`
	Max      int      `json:"max"`
	Players  []Player `json:"players" nullable:"false" doc:"a sample of who is online; servers send at most 12"`
	// MOTD is the description in legacy form: text with § colour and style
	// codes, newlines kept.
	MOTD string `json:"motd"`
	// Favicon is the server icon as a data:image/png URL, or "".
	Favicon   string `json:"favicon,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
}

type Player struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

const (
	maxStatusBytes = 256 << 10 // a status with a 64x64 icon is ~10 KB
	maxFavicon     = 128 << 10
)

// Ping asks the server at addr ("host:port") for its status.
func Ping(ctx context.Context, addr string) (ServerStatus, error) {
	var st ServerStatus
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return st, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return st, errors.New("bad port")
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return st, err
	}
	defer conn.Close()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(5 * time.Second)
	}
	_ = conn.SetDeadline(deadline)

	// Handshake (protocol -1: "whatever you speak"), next state 1 = status,
	// then the empty status request.
	var hs []byte
	hs = binary.AppendUvarint(hs, 0x00)
	hs = appendVarint(hs, -1)
	hs = appendString(hs, host)
	hs = binary.BigEndian.AppendUint16(hs, uint16(port))
	hs = binary.AppendUvarint(hs, 1)
	out := packet(hs)
	out = append(out, packet([]byte{0x00})...)
	start := time.Now()
	if _, err := conn.Write(out); err != nil {
		return st, err
	}
	r := bufio.NewReader(conn)
	n, err := binary.ReadUvarint(r)
	if err != nil {
		return st, fmt.Errorf("no answer: %w", err)
	}
	latency := time.Since(start)
	if n == 0 || n > maxStatusBytes {
		return st, errors.New("the answer is not a Minecraft status")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return st, err
	}
	return parseStatus(body, latency)
}

func parseStatus(body []byte, latency time.Duration) (ServerStatus, error) {
	var st ServerStatus
	br := bufio.NewReader(strings.NewReader(string(body)))
	id, err := binary.ReadUvarint(br)
	if err != nil || id != 0x00 {
		return st, errors.New("the answer is not a Minecraft status")
	}
	l, err := binary.ReadUvarint(br)
	if err != nil || l > maxStatusBytes {
		return st, errors.New("the answer is not a Minecraft status")
	}
	js := make([]byte, l)
	if _, err := io.ReadFull(br, js); err != nil {
		return st, err
	}
	var raw struct {
		Version struct {
			Name     string `json:"name"`
			Protocol int    `json:"protocol"`
		} `json:"version"`
		Players struct {
			Max    int      `json:"max"`
			Online int      `json:"online"`
			Sample []Player `json:"sample"`
		} `json:"players"`
		Description json.RawMessage `json:"description"`
		Favicon     string          `json:"favicon"`
	}
	if err := json.Unmarshal(js, &raw); err != nil {
		return st, fmt.Errorf("the status is not JSON: %w", err)
	}
	st = ServerStatus{
		Version: raw.Version.Name, Protocol: raw.Version.Protocol,
		Online: raw.Players.Online, Max: raw.Players.Max, Players: []Player{},
		MOTD: Legacy(raw.Description), LatencyMS: latency.Milliseconds(),
	}
	for _, p := range raw.Players.Sample {
		// Some servers put text lines in the sample with an all-zero id.
		if p.Name != "" && len(st.Players) < 24 {
			st.Players = append(st.Players, p)
		}
	}
	if strings.HasPrefix(raw.Favicon, "data:image/png;base64,") && len(raw.Favicon) <= maxFavicon {
		st.Favicon = raw.Favicon
	}
	return st, nil
}

func packet(b []byte) []byte {
	return append(binary.AppendUvarint(nil, uint64(len(b))), b...)
}

// appendVarint writes v as a protocol VarInt: negative numbers keep their two's-complement bits.
func appendVarint(b []byte, v int32) []byte { return binary.AppendUvarint(b, uint64(uint32(v))) } //nolint:gosec // intended reinterpretation

func appendString(b []byte, s string) []byte {
	return append(binary.AppendUvarint(b, uint64(len(s))), s...)
}

// colours maps chat colour names to their legacy codes.
var colours = map[string]byte{
	"black": '0', "dark_blue": '1', "dark_green": '2', "dark_aqua": '3', "dark_red": '4', "dark_purple": '5',
	"gold": '6', "gray": '7', "dark_gray": '8', "blue": '9', "green": 'a', "aqua": 'b', "red": 'c',
	"light_purple": 'd', "yellow": 'e', "white": 'f',
}

// Legacy turns a chat component (a string, an object or a list of them)
// into text with § codes. Hex colours have no legacy code and are dropped.
func Legacy(raw json.RawMessage) string {
	var b strings.Builder
	legacy(&b, raw, 0)
	return b.String()
}

func legacy(b *strings.Builder, raw json.RawMessage, depth int) {
	if depth > 16 || len(raw) == 0 {
		return
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		b.WriteString(s)
		return
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		for _, c := range list {
			legacy(b, c, depth+1)
		}
		return
	}
	var c struct {
		Text          string            `json:"text"`
		Color         string            `json:"color"`
		Bold          bool              `json:"bold"`
		Italic        bool              `json:"italic"`
		Underlined    bool              `json:"underlined"`
		Strikethrough bool              `json:"strikethrough"`
		Obfuscated    bool              `json:"obfuscated"`
		Extra         []json.RawMessage `json:"extra"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return
	}
	if code, ok := colours[c.Color]; ok {
		b.WriteString("§" + string(code))
	}
	for _, f := range []struct {
		on   bool
		code string
	}{{c.Obfuscated, "k"}, {c.Bold, "l"}, {c.Strikethrough, "m"}, {c.Underlined, "n"}, {c.Italic, "o"}} {
		if f.on {
			b.WriteString("§" + f.code)
		}
	}
	b.WriteString(c.Text)
	for _, e := range c.Extra {
		legacy(b, e, depth+1)
	}
	if c.Color != "" || c.Bold || c.Italic || c.Underlined || c.Strikethrough || c.Obfuscated {
		b.WriteString("§r")
	}
}
