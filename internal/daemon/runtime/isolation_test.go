package runtime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/moby/moby/client"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

func TestParsePortRanges(t *testing.T) {
	got, err := ParsePortRanges(" 3306, 8000-8100/tcp ,27015/udp,")
	if err != nil {
		t.Fatal(err)
	}
	want := []PortRange{{3306, 3306, ""}, {8000, 8100, "tcp"}, {27015, 27015, "udp"}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	for _, bad := range []string{"x", "0", "70000", "10-5", "80/sctp", "1-x"} {
		if _, err := ParsePortRanges(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if r, err := ParsePortRanges(""); err != nil || len(r) != 0 {
		t.Fatalf("empty: %v %v", r, err)
	}
}

func TestFirewallRules(t *testing.T) {
	iso := Isolation{Bridge: "gp0", HostPorts: []PortRange{{3306, 3306, "tcp"}, {9000, 9010, ""}}}
	rules := firewallRules(iso, []string{"25565/tcp,25565/udp", "25566/tcp,25565/tcp", "bogus,0/tcp,70000/udp,1/icmp", ""})
	for _, want := range []string{
		"-F GAMEPANEL-INPUT",
		"-A GAMEPANEL-INPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT",
		"-A GAMEPANEL-INPUT -p tcp --dport 25565 -j ACCEPT",
		"-A GAMEPANEL-INPUT -p udp --dport 25565 -j ACCEPT",
		"-A GAMEPANEL-INPUT -p tcp --dport 25566 -j ACCEPT",
		"-A GAMEPANEL-INPUT -p tcp --dport 3306 -j ACCEPT",
		"-A GAMEPANEL-INPUT -p tcp --dport 9000:9010 -j ACCEPT",
		"-A GAMEPANEL-INPUT -p udp --dport 9000:9010 -j ACCEPT",
		"-A GAMEPANEL-FORWARD -d 169.254.0.0/16 -j DROP",
		"COMMIT",
	} {
		if !strings.Contains(rules, want+"\n") {
			t.Errorf("missing %q in\n%s", want, rules)
		}
	}
	for _, unwanted := range []string{"--dport 3306 -j ACCEPT\n-A GAMEPANEL-INPUT -p udp --dport 3306", "dport 0 ", "70000", "icmp", "10.0.0.0/8"} {
		if strings.Contains(rules, unwanted) {
			t.Errorf("unexpected %q in\n%s", unwanted, rules)
		}
	}
	if strings.Count(rules, "--dport 25565 -j ACCEPT") != 2 {
		t.Errorf("a port published twice should be listed once per protocol:\n%s", rules)
	}
	// The drop comes after every accept.
	if i, j := strings.LastIndex(rules, "-j ACCEPT"), strings.Index(rules, "-A GAMEPANEL-INPUT -j DROP"); j < i {
		t.Errorf("the input drop is not last:\n%s", rules)
	}
	// Replies to connections from private networks must still leave.
	priv := firewallRules(Isolation{BlockPrivate: true}, nil)
	if !strings.Contains(priv, "-d 10.0.0.0/8 -j DROP") || strings.Index(priv, "ESTABLISHED,RELATED -j RETURN") > strings.Index(priv, "10.0.0.0/8") {
		t.Errorf("private ranges not dropped after the established return:\n%s", priv)
	}
}

type iptCall struct{ stdin, cmd string }

type fakeIPT struct {
	mu    sync.Mutex
	calls []iptCall
	have  map[string]bool // rules that exist ("INPUT -i gp0 -j GAMEPANEL-INPUT")
	fail  string          // command prefix that fails
}

func (f *fakeIPT) run(_ context.Context, stdin, name string, args ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, iptCall{stdin, cmd})
	if f.fail != "" && strings.HasPrefix(cmd, f.fail) {
		return errors.New("iptables says no")
	}
	if rule, ok := strings.CutPrefix(cmd, "iptables -w -C "); ok {
		if !f.have[rule] {
			return errors.New("no such rule")
		}
	}
	if rule, ok := strings.CutPrefix(cmd, "iptables -w -I "); ok {
		chain, rest, _ := strings.Cut(rule, " 1 ")
		f.have[chain+" "+rest] = true
	}
	return nil
}

const containerList = `[{"Id":"a","Labels":{"gamepanel.server":"11111111-2222-4333-8444-555555555555","gamepanel.ports":"25565/tcp,25565/udp","gamepanel.layout":"2"}},
{"Id":"b","Labels":{"gamepanel.server":"22222222-2222-4333-8444-555555555555","gamepanel.ports":"25570/tcp"}}]`

func TestSyncFirewall(t *testing.T) {
	d, _ := newFakeDocker(t, nil)
	d.cli = newFakeDockerWith(t, map[string]string{"GET /containers/json": containerList})
	ipt := &fakeIPT{have: map[string]bool{}}
	d.ipt = ipt.run
	d.iso = Isolation{Network: DefaultNetwork, Bridge: "gp0", Firewall: true}
	ctx := context.Background()
	if err := d.syncFirewall(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ipt.calls[0].stdin, "--dport 25570 -j ACCEPT") || ipt.calls[0].cmd != "iptables-restore -w --noflush" {
		t.Fatalf("first call: %+v", ipt.calls[0])
	}
	if !ipt.have["INPUT -i gp0 -j GAMEPANEL-INPUT"] || !ipt.have["DOCKER-USER -i gp0 -j GAMEPANEL-FORWARD"] {
		t.Fatalf("jumps not inserted: %v", ipt.have)
	}
	// A second sync finds the jumps and does not insert them again.
	n := len(ipt.calls)
	if err := d.syncFirewall(ctx); err != nil {
		t.Fatal(err)
	}
	for _, c := range ipt.calls[n:] {
		if strings.Contains(c.cmd, " -I ") {
			t.Fatalf("inserted again: %s", c.cmd)
		}
	}
	// Failures are reported (Create fails on them).
	ipt.fail = "iptables-restore"
	if err := d.syncFirewall(ctx); err == nil {
		t.Fatal("a failed restore was not reported")
	}
	// Without Firewall nothing runs.
	d.iso.Firewall = false
	n = len(ipt.calls)
	if err := d.syncFirewall(ctx); err != nil || len(ipt.calls) != n {
		t.Fatalf("firewall off still ran iptables: %v", err)
	}
}

func TestOutdated(t *testing.T) {
	d, _ := newFakeDocker(t, nil)
	d.cli = newFakeDockerWith(t, map[string]string{"GET /containers/json": containerList})
	ids, err := d.Outdated(context.Background())
	if err != nil || len(ids) != 1 || ids[0] != "22222222-2222-4333-8444-555555555555" {
		t.Fatalf("outdated = %v, %v", ids, err)
	}
}

func TestEnsureNetwork(t *testing.T) {
	ctx := context.Background()
	// Missing: created with inter-container traffic off, then inspected again
	// (this engine still has none, so that fails).
	d, e := newFakeDocker(t, map[string]int{"GET /networks/gamepanel0": 404})
	if _, err := d.ensureNetwork(ctx, "gamepanel0", "gp0"); err == nil {
		t.Fatal("a network that is still missing after the create was accepted")
	}
	if !e.called("POST", "/networks/create") {
		t.Fatal("the network was not created")
	}

	ok := `{"Id":"0123456789abcdef","Driver":"bridge","Options":{"com.docker.network.bridge.enable_icc":"false","com.docker.network.bridge.name":"gp0"}}`
	d.cli = newFakeDockerWith(t, map[string]string{"GET /networks/gamepanel0": ok})
	if b, err := d.ensureNetwork(ctx, "gamepanel0", "gp0"); err != nil || b != "gp0" {
		t.Fatalf("existing network: %q %v", b, err)
	}
	unnamed := `{"Id":"0123456789abcdef","Driver":"bridge","Options":{"com.docker.network.bridge.enable_icc":"false"}}`
	d.cli = newFakeDockerWith(t, map[string]string{"GET /networks/gamepanel0": unnamed})
	if b, err := d.ensureNetwork(ctx, "gamepanel0", "gp0"); err != nil || b != "br-0123456789ab" {
		t.Fatalf("network without a bridge name: %q %v", b, err)
	}
	icc := `{"Id":"0123456789abcdef","Driver":"bridge","Options":{}}`
	d.cli = newFakeDockerWith(t, map[string]string{"GET /networks/gamepanel0": icc})
	if _, err := d.ensureNetwork(ctx, "gamepanel0", "gp0"); err == nil || !strings.Contains(err.Error(), "enable_icc") {
		t.Fatalf("a network with inter-container traffic was accepted: %v", err)
	}
	overlay := `{"Id":"0123456789abcdef","Driver":"overlay","Options":{"com.docker.network.bridge.enable_icc":"false"}}`
	d.cli = newFakeDockerWith(t, map[string]string{"GET /networks/gamepanel0": overlay})
	if _, err := d.ensureNetwork(ctx, "gamepanel0", "gp0"); err == nil {
		t.Fatal("an overlay network was accepted")
	}
}

// Containers are created only once the network is known, on it, with a
// bounded log and the layout and ports labels the firewall needs.
func TestCreateIsolated(t *testing.T) {
	ctx := context.Background()
	spec := Spec{ServerSpec: proto.ServerSpec{
		UUID: "11111111-2222-4333-8444-555555555555", Image: "example/game:1",
		Ports: []proto.PortMap{{Host: 25565, Container: 25565, Proto: "tcp"}, {Host: 25565, Container: 25565, Proto: "udp"}},
	}, DataDir: t.TempDir()}

	d, _ := newFakeDocker(t, nil)
	d.iso = Isolation{}
	if err := d.Create(ctx, spec); !errors.Is(err, errNoIsolation) {
		t.Fatalf("created without a network: %v", err)
	}
	if err := d.Install(ctx, InstallRun{UUID: spec.UUID, DataDir: spec.DataDir, Entrypoint: "bash"}, nil); !errors.Is(err, errNoIsolation) {
		t.Fatalf("installed without a network: %v", err)
	}

	rec := &recordingEngine{}
	d.cli = newFakeDockerHandler(t, rec)
	d.iso = Isolation{Network: DefaultNetwork}
	if err := d.Create(ctx, spec); err != nil {
		t.Fatal(err)
	}
	body := rec.createBody()
	for _, want := range []string{`"NetworkMode":"gamepanel0"`, `"Type":"local"`, `"max-size":"10m"`, `"gamepanel.layout":"2"`, `"gamepanel.ports":"25565/tcp,25565/udp"`, `"HOME=/home/container"`} {
		if !strings.Contains(body, want) {
			t.Errorf("container create lacks %s:\n%s", want, body)
		}
	}
}

// newFakeDockerWith is a client for an engine that answers the given
// "METHOD path-substring" with a body (and anything else with {}).
func newFakeDockerWith(t *testing.T, body map[string]string) *client.Client {
	t.Helper()
	return newFakeDockerHandler(t, &fakeEngine{body: body})
}

func newFakeDockerHandler(t *testing.T, h http.Handler) *client.Client {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	cli, err := client.New(client.WithHost("tcp://"+strings.TrimPrefix(ts.URL, "http://")), client.WithAPIVersion("1.47"))
	if err != nil {
		t.Fatal(err)
	}
	return cli
}

// recordingEngine keeps the body of the container create request.
type recordingEngine struct {
	mu     sync.Mutex
	create string
}

func (e *recordingEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Api-Version", "1.47")
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create") {
		b, _ := io.ReadAll(r.Body)
		e.mu.Lock()
		e.create = string(b)
		e.mu.Unlock()
		w.Write([]byte(`{"Id":"x"}`))
		return
	}
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json") {
		w.Write([]byte(`[]`))
		return
	}
	w.Write([]byte(`{}`))
}

func (e *recordingEngine) createBody() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.create
}

func TestFindIPTables(t *testing.T) {
	d, _ := newFakeDocker(t, nil)
	ipt := &fakeIPT{have: map[string]bool{}, fail: "iptables -w -n -L"}
	d.ipt = ipt.run
	if bin, err := d.findIPTables(context.Background()); err != nil || bin != "iptables-legacy" {
		t.Fatalf("got %q %v, want the legacy variant (the default one has no DOCKER-USER)", bin, err)
	}
	ipt.fail = "iptables"
	if _, err := d.findIPTables(context.Background()); err == nil || !strings.Contains(err.Error(), "DOCKER-USER") {
		t.Fatalf("no variant has the chain: %v", err)
	}
}
