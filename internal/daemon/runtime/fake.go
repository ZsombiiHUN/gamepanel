package runtime

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// Fake is an in-memory Runtime. A fake "game" prints a boot log, echoes any
// console input and exits when it receives "stop". Used for tests and for
// developing without a Docker daemon (GAMEPANEL_RUNTIME=fake).
type Fake struct {
	mu      sync.Mutex
	servers map[string]*fakeServer
	subs    []chan Event
}

type fakeServer struct {
	spec   Spec
	state  string
	out    *io.PipeWriter // game -> console
	outR   *io.PipeReader
	inW    *io.PipeWriter // console -> game
	cancel context.CancelFunc
}

func NewFake() *Fake { return &Fake{servers: map[string]*fakeServer{}} }

func (f *Fake) get(uuid string) (*fakeServer, error) {
	s, ok := f.servers[uuid]
	if !ok {
		return nil, fmt.Errorf("server %s: not found", uuid)
	}
	return s, nil
}

func (f *Fake) emit(uuid, state string) { f.emitEvent(Event{UUID: uuid, State: state}) }

func (f *Fake) emitEvent(ev Event) {
	for _, c := range f.subs {
		select {
		case c <- ev:
		default:
		}
	}
}

func (f *Fake) Create(_ context.Context, s Spec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.servers[s.UUID] = &fakeServer{spec: s, state: StateOffline}
	return nil
}

// SpecOf returns the spec a server's container was last created with (tests).
func (f *Fake) SpecOf(uuid string) (Spec, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.servers[uuid]
	if !ok {
		return Spec{}, false
	}
	return s.spec, true
}

func (f *Fake) Start(ctx context.Context, uuid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.get(uuid)
	if err != nil {
		return err
	}
	if s.state != StateOffline {
		return errors.New("server is not offline")
	}
	outR, outW := io.Pipe()
	inR, inW := io.Pipe()
	gctx, cancel := context.WithCancel(context.Background())
	s.out, s.outR, s.inW, s.cancel = outW, outR, inW, cancel
	s.state = StateRunning
	f.emit(uuid, StateStarting)
	f.emit(uuid, StateRunning)
	go func() {
		fmt.Fprintf(outW, "[fake] booting %s\r\n[fake] ready\r\n", s.spec.Image)
		sc := bufio.NewScanner(inR)
		lines := make(chan string)
		go func() {
			for sc.Scan() {
				lines <- sc.Text()
			}
			close(lines)
		}()
		for {
			select {
			case <-gctx.Done():
				f.exit(uuid, Exit{Code: 137})
				return
			case l, ok := <-lines:
				if !ok || l == "stop" {
					fmt.Fprint(outW, "[fake] stopping\r\n")
					f.exit(uuid, Exit{})
					return
				}
				if l == "crash" {
					fmt.Fprint(outW, "[fake] crashing\r\n")
					f.exit(uuid, Exit{Code: 1})
					return
				}
				fmt.Fprintf(outW, "[fake] > %s\r\n", l)
			}
		}
	}()
	return nil
}

func (f *Fake) exit(uuid string, x Exit) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.servers[uuid]; ok && s.state != StateOffline {
		s.state = StateOffline
		s.out.Close()
		s.cancel()
		f.emitEvent(Event{UUID: uuid, State: StateOffline, Exit: &x})
	}
}

func (f *Fake) Stop(ctx context.Context, uuid string, timeoutSec int) error {
	f.mu.Lock()
	s, err := f.get(uuid)
	if err != nil || s.state == StateOffline {
		f.mu.Unlock()
		return err
	}
	in := s.inW
	f.mu.Unlock()
	go fmt.Fprintln(in, "stop")
	deadline := time.After(time.Duration(timeoutSec) * time.Second)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return f.Kill(ctx, uuid)
		case <-time.After(10 * time.Millisecond):
			if st, _ := f.State(ctx, uuid); st == StateOffline {
				return nil
			}
		}
	}
}

func (f *Fake) Kill(_ context.Context, uuid string) error {
	f.mu.Lock()
	s, err := f.get(uuid)
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (f *Fake) Remove(ctx context.Context, uuid string) error {
	_ = f.Kill(ctx, uuid)
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.servers, uuid)
	return nil
}

func (f *Fake) Attach(_ context.Context, uuid string) (io.ReadCloser, io.WriteCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.get(uuid)
	if err != nil {
		return nil, nil, err
	}
	if s.state == StateOffline {
		return nil, nil, errors.New("server is offline")
	}
	return s.outR, s.inW, nil
}

func (f *Fake) State(_ context.Context, uuid string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.get(uuid)
	if err != nil {
		return "", err
	}
	return s.state, nil
}

// WatchStats reports a fixed sample every 100 ms while the server runs.
func (f *Fake) WatchStats(ctx context.Context, uuid string, fn func(proto.Stats)) error {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		if st, err := f.State(ctx, uuid); err != nil || st != StateRunning {
			return err
		}
		fn(proto.Stats{UUID: uuid, CPUPct: 1.5, MemBytes: 256 << 20})
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func (f *Fake) Events(ctx context.Context) (<-chan Event, error) {
	ch := make(chan Event, 64)
	f.mu.Lock()
	f.subs = append(f.subs, ch)
	f.mu.Unlock()
	return ch, nil
}

func (f *Fake) List(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for id := range f.servers {
		out = append(out, id)
	}
	return out, nil
}

// Install pretends to run the script. A script containing "#fail" fails, so
// tests can exercise both outcomes.
func (f *Fake) Install(_ context.Context, r InstallRun, out io.Writer) error {
	b, err := os.ReadFile(filepath.Join(r.ScriptDir, "install.sh"))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "[fake-install] %s %s\r\n", r.Entrypoint, r.Image)
	if bytes.Contains(b, []byte("#fail")) {
		fmt.Fprint(out, "[fake-install] failing\r\n")
		return errors.New("installer exited with code 1")
	}
	return os.WriteFile(filepath.Join(r.DataDir, "installed.txt"), b, 0o644)
}

// LimitBandwidth records nothing: the fake has no network.
func (f *Fake) LimitBandwidth(context.Context, string) (IngressMode, error) {
	return IngressUnlimited, nil
}
