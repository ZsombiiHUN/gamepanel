package minecraft

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer answers one Server List Ping with status, after checking the
// handshake asks for the status state.
func fakeServer(t *testing.T, status string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		read := func() []byte {
			n, err := binary.ReadUvarint(r)
			if err != nil {
				return nil
			}
			b := make([]byte, n)
			_, _ = io.ReadFull(r, b)
			return b
		}
		hs := read()
		if len(hs) == 0 || hs[0] != 0 || hs[len(hs)-1] != 1 {
			t.Errorf("handshake %x", hs)
			return
		}
		if req := read(); len(req) != 1 || req[0] != 0 {
			t.Errorf("status request %x", req)
			return
		}
		body := appendString([]byte{0x00}, status)
		_, _ = c.Write(packet(body))
	}()
	return ln.Addr().String()
}

func TestPing(t *testing.T) {
	t.Parallel()
	addr := fakeServer(t, `{"version":{"name":"Paper 1.21.11","protocol":774},
		"players":{"max":20,"online":2,"sample":[{"name":"Alex","id":"00000000-0000-0000-0000-000000000001"},{"name":"","id":"x"}]},
		"description":{"text":"Hello ","extra":[{"text":"world","color":"gold","bold":true}]},
		"favicon":"data:image/png;base64,AAAA"}`)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	st, err := Ping(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "Paper 1.21.11" || st.Protocol != 774 || st.Online != 2 || st.Max != 20 {
		t.Errorf("%+v", st)
	}
	if len(st.Players) != 1 || st.Players[0].Name != "Alex" {
		t.Errorf("players %+v", st.Players)
	}
	if st.MOTD != "Hello §6§lworld§r" {
		t.Errorf("motd %q", st.MOTD)
	}
	if st.Favicon != "data:image/png;base64,AAAA" {
		t.Errorf("favicon %q", st.Favicon)
	}
}

func TestPingRefusesNonMinecraft(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_, _ = c.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
			c.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := Ping(ctx, ln.Addr().String()); err == nil {
		t.Fatal("an HTTP answer passed as a status")
	}
}

func TestLegacy(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`"§aplain"`: "§aplain",
		`[{"text":"a"},{"text":"b","italic":true}]`:                "a§ob§r",
		`{"text":"","extra":["x",{"text":"y","color":"#ff0000"}]}`: "xy§r",
		`{}`: "",
	} {
		if got := Legacy(json.RawMessage(in)); got != want {
			t.Errorf("Legacy(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestOfflineUUID(t *testing.T) {
	t.Parallel()
	if got := OfflineUUID("Notch"); got != "b50ad385-829d-3141-a216-7e7d7539ba7f" {
		t.Fatal(got)
	}
}

func TestVersionHelpers(t *testing.T) {
	t.Parallel()
	for nv, want := range map[string]string{
		"21.1.77": "1.21.1", "21.0.5": "1.21", "20.4.237": "1.20.4", "26.3.0.4": "26.3", "26.3.1.2": "26.3.1",
		"21.1.0-beta": "", "19.2.1": "", "x.y.z": "",
	} {
		if got := neoForgeMinecraft(nv); got != want {
			t.Errorf("neoForgeMinecraft(%s) = %q, want %q", nv, got, want)
		}
	}
	v := []string{"1.21.2", "1.21.11", "26.1", "1.8.9", "1.21"}
	sortDesc(v)
	if strings.Join(v, " ") != "26.1 1.21.11 1.21.2 1.21 1.8.9" {
		t.Fatal(v)
	}
}

func TestVersionsAndLookup(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("User-Agent") == "" {
			t.Error("no User-Agent")
		}
		switch r.URL.Path {
		case "/mojang/mc/game/version_manifest_v2.json":
			_, _ = io.WriteString(w, `{"versions":[{"id":"26.3","type":"release"},{"id":"26w14a","type":"snapshot"},{"id":"1.21.11","type":"release"}]}`)
		case "/fill/paper/versions":
			_, _ = io.WriteString(w, `{"versions":[{"version":{"id":"1.21.11"}},{"version":{"id":"26.3"}},{"version":{"id":"26.4-rc1"}}]}`)
		case "/forge/promotions_slim.json":
			_, _ = io.WriteString(w, `{"promos":{"1.20.1-recommended":"47.4.0","1.20.1-latest":"47.4.1","1.12.2-latest":"14.23.5.2860","1.7.10_pre4-latest":"x"}}`)
		case "/neo":
			_, _ = io.WriteString(w, `{"versions":["20.4.237","21.1.77","21.1.200","26.3.0.4","21.2.0-beta"]}`)
		case "/profiles/Notch":
			_, _ = io.WriteString(w, `{"id":"069a79f444e94726a5befca90e38aaf5","name":"Notch"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := NewClient(Sources{Mojang: srv.URL + "/mojang", Fill: srv.URL + "/fill", Forge: srv.URL + "/forge", NeoForge: srv.URL + "/neo", Profiles: srv.URL + "/profiles"})
	ctx := context.Background()
	for flavor, want := range map[string]string{
		"vanilla":    "26.3 1.21.11",
		"paper":      "26.3 1.21.11",
		"forge":      "1.20.1 1.12.2",
		"neoforge":   "26.3 1.21.1 1.20.4",
		"bungeecord": "",
	} {
		got, err := c.Versions(ctx, flavor)
		if err != nil {
			t.Fatalf("%s: %v", flavor, err)
		}
		if strings.Join(got, " ") != want {
			t.Errorf("%s: %q, want %q", flavor, got, want)
		}
	}
	before := hits.Load()
	if _, err := c.Versions(ctx, "paper"); err != nil || hits.Load() != before {
		t.Error("version list not cached")
	}
	if _, err := c.Versions(ctx, "spigot"); err == nil {
		t.Error("unknown flavor accepted")
	}

	p, err := c.Lookup(ctx, "Notch")
	if err != nil || p.UUID != "069a79f4-44e9-4726-a5be-fca90e38aaf5" || p.Name != "Notch" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := c.Lookup(ctx, "Nobody_Here"); !errors.Is(err, ErrNoSuchPlayer) {
		t.Fatalf("unknown player: %v", err)
	}
	if _, err := c.Lookup(ctx, "../x"); err == nil {
		t.Fatal("bad name accepted")
	}
}
