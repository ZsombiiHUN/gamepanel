package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

const (
	labelServer    = "gamepanel.server"
	labelInstaller = "gamepanel.installer"
	namePrefix     = "gp-"
	mountPoint     = "/home/container"
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

func (d *Docker) pull(ctx context.Context, image string) error {
	pull, err := d.cli.ImagePull(ctx, image, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pull %s: %w", image, err)
	}
	if err := pull.Wait(ctx); err != nil {
		return fmt.Errorf("pull %s: %w", image, err)
	}
	return nil
}

func (d *Docker) Create(ctx context.Context, s Spec) error {
	// Pull before touching the existing container: if the registry is down,
	// the server keeps the container it has instead of being left with none.
	if err := d.pull(ctx, s.Image); err != nil {
		return err
	}
	// Replace any previous container for this server (e.g. changed settings).
	if err := d.Remove(ctx, s.UUID); err != nil {
		return err
	}

	env := make([]string, 0, len(s.Env))
	for k, v := range s.Env {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)

	exposed := network.PortSet{}
	bindings := network.PortMap{}
	for _, p := range s.Ports {
		transport := p.Proto
		if transport == "" {
			transport = "tcp"
		}
		port, err := network.ParsePort(strconv.Itoa(p.Container) + "/" + transport)
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

	pids := s.Pids
	if pids <= 0 {
		pids = 1024
	}
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
		// MemorySwap is memory+swap combined: equal to memory means no swap.
		switch {
		case s.SwapMB < 0:
			hc.MemorySwap = -1
		default:
			hc.MemorySwap = (s.MemoryMB + s.SwapMB) << 20
		}
	}
	if s.CPUSet != "" {
		hc.CpusetCpus = s.CPUSet
	}
	if s.IOWeight > 0 {
		hc.BlkioWeight = uint16(min(s.IOWeight, 1000)) // the panel allows 10-1000
	}

	_, err = d.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name(s.UUID),
		Config: &container.Config{
			Image: s.Image,
			User:  user,
			// Override the image ENTRYPOINT: community images (e.g. Pterodactyl's
			// yolks) ship one that re-parses a STARTUP variable and would ignore
			// the command below. Our startup is already shell-safe.
			Entrypoint:   []string{"/bin/sh", "-c"},
			Cmd:          []string{s.Startup},
			Env:          env,
			WorkingDir:   mountPoint,
			Tty:          true,
			OpenStdin:    true,
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			ExposedPorts: exposed,
			Labels:       map[string]string{labelServer: s.UUID, labelBandwidth: strconv.FormatInt(s.BandwidthMbit, 10)},
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

// Remove deletes the server's container. A container that does not exist
// counts as removed.
func (d *Docker) Remove(ctx context.Context, uuid string) error {
	_, err := d.cli.ContainerRemove(ctx, name(uuid), client.ContainerRemoveOptions{Force: true})
	if cerrdefs.IsNotFound(err) {
		return nil
	}
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

// Attach returns the console of the server's current run, starting with
// at most AttachTail lines of what it already printed, and its stdin.
//
// Attaching with Logs would replay everything since the container was
// created, every earlier run included, which flooded the console after a
// daemon restart. The output therefore comes from the log stream instead,
// which can start at this run and be cut to a tail.
func (d *Docker) Attach(ctx context.Context, uuid string) (io.ReadCloser, io.WriteCloser, error) {
	in, err := d.cli.ContainerInspect(ctx, name(uuid), client.ContainerInspectOptions{})
	if err != nil {
		return nil, nil, err
	}
	logs, lerr := d.cli.ContainerLogs(ctx, name(uuid), client.ContainerLogsOptions{
		ShowStdout: true, ShowStderr: true, Follow: true,
		Since: in.Container.State.StartedAt, Tail: strconv.Itoa(AttachTail),
	})
	res, err := d.cli.ContainerAttach(ctx, name(uuid), client.ContainerAttachOptions{
		// Without a readable log (a log driver such as "none"), live output
		// comes from the attach stream instead.
		Stream: true, Stdin: true, Stdout: lerr != nil, Stderr: lerr != nil,
	})
	if err != nil {
		if lerr == nil {
			logs.Close()
		}
		return nil, nil, err
	}
	var r io.Reader = res.Reader // Tty=true, so the stream is raw (not multiplexed)
	closeAll := res.Close
	if lerr == nil {
		r = logs
		closeAll = func() { logs.Close(); res.Close() }
	}
	return hijacked{r: r, w: res.Conn, c: closeAll}, hijacked{r: res.Reader, w: res.Conn, c: closeAll}, nil
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

// WatchStats keeps one streaming stats connection open. A one-shot sample
// makes Docker wait a second for a second sample, which made a pass over N
// servers take N seconds.
func (d *Docker) WatchStats(ctx context.Context, uuid string, fn func(proto.Stats)) error {
	res, err := d.cli.ContainerStats(ctx, name(uuid), client.ContainerStatsOptions{Stream: true})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	dec := json.NewDecoder(res.Body)
	for {
		var s container.StatsResponse
		if err := dec.Decode(&s); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		// Docker keeps the stream open after the container stops and sends
		// empty samples (no read time); that is the end of this run.
		if s.Read.IsZero() {
			return nil
		}
		fn(statsFrom(uuid, s))
	}
}

func statsFrom(uuid string, s container.StatsResponse) proto.Stats {
	out := proto.Stats{UUID: uuid, MemBytes: s.MemoryStats.Usage}
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
	if sysDelta > 0 && cpuDelta > 0 {
		out.CPUPct = cpuDelta / sysDelta * float64(s.CPUStats.OnlineCPUs) * 100
	}
	return out
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
			case err := <-res.Err:
				_ = err // the closed channel tells the caller to subscribe again
				return
			case m, ok := <-res.Messages:
				if !ok {
					return
				}
				uuid := strings.TrimPrefix(m.Actor.Attributes["name"], namePrefix)
				if uuid == "" {
					continue
				}
				ev := Event{UUID: uuid}
				switch string(m.Action) {
				case "start":
					ev.State = StateRunning
				case "die":
					ev.State = StateOffline
					ev.Exit = d.exitOf(ctx, uuid, m.Actor.Attributes["exitCode"])
				case "stop", "kill", "oom":
					ev.State = StateOffline
				default:
					continue
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// exitOf reads how a container's process ended: the code from the event,
// and from an inspect whether the OOM killer was involved.
func (d *Docker) exitOf(ctx context.Context, uuid, code string) *Exit {
	x := &Exit{}
	x.Code, _ = strconv.Atoi(code)
	ictx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if res, err := d.cli.ContainerInspect(ictx, name(uuid), client.ContainerInspectOptions{}); err == nil && res.Container.State != nil {
		x.OOM = res.Container.State.OOMKilled
		if code == "" {
			x.Code = res.Container.State.ExitCode
		}
	}
	return x
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

// installTimeout bounds an install script; game downloads can be large but a
// hung script must not hold the server forever.
const installTimeout = 30 * time.Minute

var numericIDRe = regexp.MustCompile(`^\d+(:\d+)?$`)

// Install runs the egg's install script in a throwaway container.
//
// Unlike the game container it runs as root, because install scripts usually
// install packages, but it keeps only the capabilities that needs, can never
// gain more, has a pid and time limit, and is not labelled as a server (so it
// never shows up in state events). Afterwards the wrapper hands the data dir
// back to the user the game container runs as.
func (d *Docker) Install(ctx context.Context, r InstallRun, out io.Writer) error {
	owner, err := dirOwner(r.DataDir)
	if err != nil {
		return err
	}
	if !numericIDRe.MatchString(owner) || !proto.ValidEntrypoint(r.Entrypoint) {
		return errors.New("invalid installer settings")
	}
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()

	if err := d.pull(ctx, r.Image); err != nil {
		return err
	}

	env := make([]string, 0, len(r.Env))
	for k, v := range r.Env {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)

	pids := int64(4096)
	mem := max(r.MemoryMB, 1024) << 20
	script := r.Entrypoint + " /mnt/install/install.sh; rc=$?; chown -R " + owner + " /mnt/server; exit $rc"
	// A previous crashed run may have left its installer behind.
	_, _ = d.cli.ContainerRemove(ctx, name(r.UUID)+"-install", client.ContainerRemoveOptions{Force: true})
	created, err := d.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name(r.UUID) + "-install",
		Config: &container.Config{
			Image:      r.Image,
			Cmd:        []string{"/bin/sh", "-c", script},
			Env:        env,
			WorkingDir: "/mnt/server",
			Tty:        true,
			Labels:     map[string]string{labelInstaller: r.UUID},
		},
		HostConfig: &container.HostConfig{
			Binds:       []string{r.DataDir + ":/mnt/server", r.ScriptDir + ":/mnt/install:ro"},
			CapDrop:     []string{"ALL"},
			CapAdd:      []string{"CHOWN", "DAC_OVERRIDE", "FOWNER", "FSETID", "SETUID", "SETGID", "SETFCAP", "MKNOD"},
			SecurityOpt: []string{"no-new-privileges"},
			Resources:   container.Resources{Memory: mem, MemorySwap: mem, PidsLimit: &pids},
		},
	})
	if err != nil {
		return err
	}
	id := created.ID
	defer func() {
		rm, c := context.WithTimeout(context.Background(), 30*time.Second)
		defer c()
		_, _ = d.cli.ContainerRemove(rm, id, client.ContainerRemoveOptions{Force: true})
	}()

	// Attach before starting so no early output is lost.
	att, err := d.cli.ContainerAttach(ctx, id, client.ContainerAttachOptions{Stream: true, Stdout: true, Stderr: true})
	if err != nil {
		return err
	}
	defer att.Close()
	if _, err := d.cli.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
		return err
	}
	copied := make(chan struct{})
	go func() { _, _ = io.Copy(out, att.Reader); close(copied) }()

	wait := d.cli.ContainerWait(ctx, id, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case err := <-wait.Error:
		return err
	case res := <-wait.Result:
		select { // let the last output drain
		case <-copied:
		case <-time.After(2 * time.Second):
		}
		if res.StatusCode != 0 {
			return fmt.Errorf("installer exited with code %d", res.StatusCode)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("install: %w", ctx.Err())
	}
}
