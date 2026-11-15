package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

const (
	labelServer = "gamepanel.server"
	namePrefix  = "gp-"
	mountPoint  = "/home/container"
)

// Docker implements Runtime on the Docker Engine API.
type Docker struct{ cli *client.Client }

func NewDocker() (*Docker, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	return &Docker{cli: cli}, nil
}

func name(uuid string) string { return namePrefix + uuid }

func (d *Docker) Ping(ctx context.Context) error {
	_, err := d.cli.Ping(ctx, client.PingOptions{})
	return err
}

func (d *Docker) Create(ctx context.Context, s Spec) error {
	// Replace any previous container for this server (e.g. changed settings).
	_ = d.Remove(ctx, s.UUID)

	pull, err := d.cli.ImagePull(ctx, s.Image, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pull %s: %w", s.Image, err)
	}
	if err := pull.Wait(ctx); err != nil {
		return fmt.Errorf("pull %s: %w", s.Image, err)
	}

	env := make([]string, 0, len(s.Env))
	for k, v := range s.Env {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)

	exposed := network.PortSet{}
	bindings := network.PortMap{}
	for _, p := range s.Ports {
		proto := p.Proto
		if proto == "" {
			proto = "tcp"
		}
		port, err := network.ParsePort(strconv.Itoa(p.Container) + "/" + proto)
		if err != nil {
			return err
		}
		exposed[port] = struct{}{}
		bindings[port] = append(bindings[port], network.PortBinding{
			HostIP: netip.IPv4Unspecified(), HostPort: strconv.Itoa(p.Host),
		})
	}

	// Run as whoever owns the data directory. All capabilities are dropped, so
	// even root in the container could not write to a directory owned by
	// someone else; matching the owner avoids that without granting any cap.
	user, err := dirOwner(s.DataDir)
	if err != nil {
		return err
	}

	pids := int64(1024)
	useInit := true
	hc := &container.HostConfig{
		Binds:        []string{s.DataDir + ":" + mountPoint},
		PortBindings: bindings,
		// Defence in depth: game servers are untrusted code.
		CapDrop:     []string{"ALL"},
		SecurityOpt: []string{"no-new-privileges"},
		Init:        &useInit,
		Resources: container.Resources{
			Memory:    s.MemoryMB << 20,
			NanoCPUs:  s.CPUPct * 10_000_000, // 100% = 1e9 nano-CPUs
			PidsLimit: &pids,
		},
	}
	if s.MemoryMB > 0 {
		hc.Resources.MemorySwap = s.MemoryMB << 20 // no swap
	}

	_, err = d.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name(s.UUID),
		Config: &container.Config{
			Image:        s.Image,
			User:         user,
			Cmd:          []string{"/bin/sh", "-c", s.Startup},
			Env:          env,
			WorkingDir:   mountPoint,
			Tty:          true,
			OpenStdin:    true,
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			ExposedPorts: exposed,
			Labels:       map[string]string{labelServer: s.UUID},
		},
		HostConfig: hc,
	})
	return err
}

func dirOwner(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("cannot determine owner of %s", path)
	}
	return fmt.Sprintf("%d:%d", st.Uid, st.Gid), nil
}

func (d *Docker) Start(ctx context.Context, uuid string) error {
	_, err := d.cli.ContainerStart(ctx, name(uuid), client.ContainerStartOptions{})
	return err
}

func (d *Docker) Stop(ctx context.Context, uuid string, timeoutSec int) error {
	_, err := d.cli.ContainerStop(ctx, name(uuid), client.ContainerStopOptions{Timeout: &timeoutSec})
	return err
}

func (d *Docker) Kill(ctx context.Context, uuid string) error {
	_, err := d.cli.ContainerKill(ctx, name(uuid), client.ContainerKillOptions{Signal: "SIGKILL"})
	return err
}

func (d *Docker) Remove(ctx context.Context, uuid string) error {
	_, err := d.cli.ContainerRemove(ctx, name(uuid), client.ContainerRemoveOptions{Force: true})
	return err
}

type hijacked struct {
	r io.Reader
	w io.Writer
	c func()
}

func (h hijacked) Read(p []byte) (int, error)  { return h.r.Read(p) }
func (h hijacked) Write(p []byte) (int, error) { return h.w.Write(p) }
func (h hijacked) Close() error                { h.c(); return nil }

func (d *Docker) Attach(ctx context.Context, uuid string) (io.ReadCloser, io.WriteCloser, error) {
	res, err := d.cli.ContainerAttach(ctx, name(uuid), client.ContainerAttachOptions{
		Stream: true, Stdin: true, Stdout: true, Stderr: true, Logs: true,
	})
	if err != nil {
		return nil, nil, err
	}
	h := hijacked{r: res.Reader, w: res.Conn, c: res.Close}
	return h, h, nil // Tty=true, so the stream is raw (not multiplexed)
}

func (d *Docker) State(ctx context.Context, uuid string) (string, error) {
	res, err := d.cli.ContainerInspect(ctx, name(uuid), client.ContainerInspectOptions{})
	if err != nil {
		return "", err
	}
	return mapStatus(string(res.Container.State.Status)), nil
}

func mapStatus(s string) string {
	switch s {
	case "running":
		return StateRunning
	case "restarting":
		return StateStarting
	default: // created, exited, dead, paused, removing
		return StateOffline
	}
}

func (d *Docker) Stats(ctx context.Context, uuid string) (proto.Stats, error) {
	res, err := d.cli.ContainerStats(ctx, name(uuid), client.ContainerStatsOptions{IncludePreviousSample: true})
	if err != nil {
		return proto.Stats{}, err
	}
	defer res.Body.Close()
	var s container.StatsResponse
	if err := json.NewDecoder(res.Body).Decode(&s); err != nil {
		return proto.Stats{}, err
	}
	out := proto.Stats{UUID: uuid, MemBytes: s.MemoryStats.Usage}
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
	if sysDelta > 0 && cpuDelta > 0 {
		out.CPUPct = cpuDelta / sysDelta * float64(s.CPUStats.OnlineCPUs) * 100
	}
	return out, nil
}

// Events maps Docker container events onto server state changes. The caller
// must re-subscribe (with backoff) if the channel closes, e.g. when the
// Docker daemon restarts.
func (d *Docker) Events(ctx context.Context) (<-chan Event, error) {
	res := d.cli.Events(ctx, client.EventsListOptions{Filters: client.Filters{
		"type":  {"container": true},
		"label": {labelServer: true},
	}})
	out := make(chan Event, 64)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case <-res.Err:
				return
			case m, ok := <-res.Messages:
				if !ok {
					return
				}
				uuid := strings.TrimPrefix(m.Actor.Attributes["name"], namePrefix)
				if uuid == "" {
					continue
				}
				var st string
				switch string(m.Action) {
				case "start":
					st = StateRunning
				case "die", "stop", "kill", "oom":
					st = StateOffline
				default:
					continue
				}
				out <- Event{UUID: uuid, State: st}
			}
		}
	}()
	return out, nil
}

func (d *Docker) List(ctx context.Context) ([]string, error) {
	res, err := d.cli.ContainerList(ctx, client.ContainerListOptions{
		All: true, Filters: client.Filters{"label": {labelServer: true}},
	})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range res.Items {
		if id := c.Labels[labelServer]; id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}
