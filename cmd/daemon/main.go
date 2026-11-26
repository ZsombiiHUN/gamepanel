package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon"
	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
	"github.com/ZsombiiHUN/gamepanel/internal/version"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	if len(os.Args) > 1 && version.IsFlag(os.Args[1]) {
		fmt.Println(version.String("daemon"))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "adopt-wings" {
		if err := adoptWings(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "help" || os.Args[1] == "-h" || os.Args[1] == "--help") {
		fmt.Print(usage)
		return
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	log.Info("starting", "version", version.Version)
	cfg := daemon.Config{
		PanelURL: env("GAMEPANEL_PANEL_URL", "ws://127.0.0.1:8080/daemon/connect"),
		Token:    os.Getenv("GAMEPANEL_NODE_TOKEN"),
		DataDir:  env("GAMEPANEL_DATA_DIR", "./daemon-data"),
		// Off by default: "pull from URL" must not reach the node's own network.
		PullAllowPrivate: os.Getenv("GAMEPANEL_PULL_ALLOW_PRIVATE") == "1",
		// On by default: servers running when the node went down start again.
		NoResume: os.Getenv("GAMEPANEL_RESUME") == "off",
	}
	panelURL, err := daemon.NormalizePanelURL(cfg.PanelURL)
	if err != nil {
		log.Error("bad panel address", "err", err)
		os.Exit(1)
	}
	cfg.PanelURL = panelURL
	if cfg.Token == "" {
		log.Error("GAMEPANEL_NODE_TOKEN is required (create a node in the panel to get one)")
		os.Exit(1)
	}

	if cfg.ContainerUID, cfg.ContainerGID, err = containerUser(os.Getenv("GAMEPANEL_CONTAINER_USER")); err != nil {
		log.Error("bad GAMEPANEL_CONTAINER_USER", "err", err)
		os.Exit(1)
	}
	if cfg.ContainerUID == 0 {
		if name, ok := loginAccount(daemon.LegacyContainerID); ok {
			log.Warn("game servers run as uid 1000, which is the login account "+name+" on this machine; "+
				"set GAMEPANEL_CONTAINER_USER to a user of their own (docs/nodes/configuration.md)", "uid", daemon.LegacyContainerID)
		}
	}
	var rt runtime.Runtime
	switch env("GAMEPANEL_RUNTIME", "docker") {
	case "fake":
		log.Warn("using the fake runtime: no real containers are started")
		rt = runtime.NewFake()
	default:
		d, err := runtime.NewDocker()
		if err != nil {
			log.Error("docker client", "err", err)
			os.Exit(1)
		}
		if err := d.Ping(context.Background()); err != nil {
			log.Error("cannot reach the Docker daemon (is it running? try GAMEPANEL_RUNTIME=fake for development)", "err", err)
			os.Exit(1)
		}
		iso, err := isolation()
		if err != nil {
			log.Error("bad network isolation settings", "err", err)
			os.Exit(1)
		}
		if !iso.Firewall {
			log.Warn("GAMEPANEL_FIREWALL=off: game servers can reach this machine's services and cloud metadata")
		}
		// The firewall is checked for as long as the process runs.
		if err := d.Isolate(context.Background(), iso); err != nil {
			log.Error("could not isolate game servers", "err", err)
			os.Exit(1)
		}
		rt = d
	}

	dm, err := daemon.New(cfg, rt, log)
	if err != nil {
		log.Error("init", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if addr := env("GAMEPANEL_SFTP_ADDR", "0.0.0.0:2022"); addr != "off" {
		// SFTP is optional: a busy port must not take the whole node down.
		if _, err := dm.StartSFTP(ctx, addr); err != nil {
			log.Warn("sftp disabled", "addr", addr, "err", err)
		}
	}
	dm.Run(ctx)
}

// isolation reads the network isolation settings (see runtime.Isolation).
func isolation() (runtime.Isolation, error) {
	iso := runtime.Isolation{
		Network:      env("GAMEPANEL_NETWORK", runtime.DefaultNetwork),
		Bridge:       env("GAMEPANEL_NETWORK_BRIDGE", runtime.DefaultBridge),
		Firewall:     os.Getenv("GAMEPANEL_FIREWALL") != "off",
		BlockPrivate: os.Getenv("GAMEPANEL_BLOCK_PRIVATE") == "1",
	}
	var err error
	iso.HostPorts, err = runtime.ParsePortRanges(os.Getenv("GAMEPANEL_HOST_PORTS"))
	return iso, err
}

// containerUser reads GAMEPANEL_CONTAINER_USER ("uid" or "uid:gid"); "" is 0, 0.
func containerUser(v string) (uid, gid int, err error) {
	if v == "" {
		return 0, 0, nil
	}
	us, gs, hasGID := strings.Cut(v, ":")
	if uid, err = strconv.Atoi(us); err != nil || uid <= 0 {
		return 0, 0, fmt.Errorf("%q: want a numeric uid other than 0, or uid:gid", v)
	}
	gid = uid
	if hasGID {
		if gid, err = strconv.Atoi(gs); err != nil || gid <= 0 {
			return 0, 0, fmt.Errorf("%q: want a numeric gid other than 0", v)
		}
	}
	return uid, gid, nil
}

// loginAccount reports the name of the account with uid in /etc/passwd when
// it can log in (a shell other than nologin or false).
func loginAccount(uid int) (string, bool) {
	b, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 7 || f[2] != strconv.Itoa(uid) {
			continue
		}
		shell := filepath.Base(f[6])
		return f[0], shell != "nologin" && shell != "false" && shell != ""
	}
	return "", false
}

const usage = `Usage: gamepanel-daemon [command]

Commands:
  (none)        run the daemon; settings are GAMEPANEL_* environment variables
                (see docs/nodes/configuration.md)
  adopt-wings   take over the server files of Pterodactyl's Wings on this node
                (see gamepanel-daemon adopt-wings -h)
  version       print the version
`

const adoptUsage = `Usage: gamepanel-daemon adopt-wings [flags]

Moves the server folders of Pterodactyl's Wings into this daemon's data
directory (GAMEPANEL_DATA_DIR), after the panel imported them with
panel import-pterodactyl --apply. Files keep their owner and nothing is
reinstalled. Run it before the daemon first connects to the panel.

Stop Wings first (systemctl disable --now wings); its servers may keep
running. When the daemon connects, it stops each one with its egg's stop
command and starts it again under GamePanel.

Flags:
  --from DIR       Wings' volumes directory (default /var/lib/pterodactyl/volumes)
  --states FILE    Wings' states.json; servers it lists as running start again
                   once the panel creates them (default: next to --from)
  --backups DIR    Wings' local backups; each server keeps its own once the
                   panel creates it (default: backups next to --from)
  --stop-running   if the folders must be copied (another file system), stop
                   servers Wings still runs (SIGINT, then killed) instead of
                   refusing; stopping them in Pterodactyl first is safer
  --stop-timeout S seconds a server gets to stop before it is killed (default 60)
`

func adoptWings(args []string) error {
	fs := flag.NewFlagSet("adopt-wings", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, adoptUsage) }
	from := fs.String("from", "/var/lib/pterodactyl/volumes", "")
	states := fs.String("states", "", "")
	backups := fs.String("backups", "", "")
	stop := fs.Bool("stop-running", false, "")
	timeout := fs.Int("stop-timeout", 60, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q (see adopt-wings -h)", fs.Arg(0))
	}
	if *states == "" {
		*states = filepath.Join(filepath.Dir(filepath.Clean(*from)), "states.json")
	}
	if *backups == "" {
		*backups = filepath.Join(filepath.Dir(filepath.Clean(*from)), "backups")
	}
	dataDir := env("GAMEPANEL_DATA_DIR", "./daemon-data")
	var dk daemon.WingsContainers
	if env("GAMEPANEL_RUNTIME", "docker") != "fake" {
		d, err := runtime.NewDocker()
		if err != nil {
			return fmt.Errorf("docker client: %w", err)
		}
		if err := d.Ping(context.Background()); err != nil {
			return fmt.Errorf("cannot reach the Docker daemon: %w", err)
		}
		dk = d
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	fmt.Printf("Taking over Wings' servers from %s into %s\n", *from, filepath.Join(dataDir, "servers"))
	res, err := daemon.AdoptWings(ctx, daemon.AdoptOptions{
		From: *from, DataDir: dataDir, States: *states, Backups: *backups, StopRunning: *stop, StopTimeout: *timeout, Docker: dk, Out: os.Stdout,
	})
	fmt.Printf("\nMoved %d, copied %d; %d backup archive(s).\n", len(res.Moved), len(res.Copied), res.Backups)
	if len(res.Copied) > 0 {
		fmt.Printf("Copied folders are still in %s; delete them once the servers work.\n", *from)
	}
	if err == nil {
		fmt.Println("Now start the daemon: systemctl enable --now gamepanel-daemon")
	}
	return err
}
