package daemon

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/minecraft"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// The ping goes to the server's primary port on the node, and only while it runs.
func TestPingAsksThePrimaryPort(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		for range 2 { // handshake, status request
			n, _ := binary.ReadUvarint(r)
			_, _ = io.CopyN(io.Discard, r, int64(n))
		}
		js := `{"version":{"name":"1.21.11","protocol":774},"players":{"max":5,"online":1},"description":"hi"}`
		body := append(binary.AppendUvarint([]byte{0}, uint64(len(js))), js...)
		_, _ = c.Write(append(binary.AppendUvarint(nil, uint64(len(body))), body...))
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	d := newTestDaemon(t, runtime.NewFake())
	id := uuidN(1)
	spec := proto.ServerSpec{UUID: id, Image: "img", Startup: "run", Stop: "stop", MemoryMB: 128, Env: map[string]string{"SERVER_PORT": strconv.Itoa(port)}}
	if err := d.create(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if !eventually(5*time.Second, func() bool { st, _ := d.installState(id); return st == "" }) {
		t.Fatal("never provisioned")
	}
	ping := func() (minecraft.ServerStatus, error) {
		raw, err := d.dispatch(context.Background(), req(proto.ReqServerPing, proto.UUIDOnly{UUID: id}))
		var st minecraft.ServerStatus
		if err == nil {
			err = json.Unmarshal(raw, &st)
		}
		return st, err
	}
	if _, err := ping(); proto.CodeOf(err) != proto.CodeConflict {
		t.Fatalf("ping of a stopped server: %v", err)
	}
	if err := power(d, id, "start"); err != nil {
		t.Fatal(err)
	}
	st, err := ping()
	if err != nil {
		t.Fatal(err)
	}
	if st.Online != 1 || st.Max != 5 || st.MOTD != "hi" {
		t.Fatalf("%+v", st)
	}
}
