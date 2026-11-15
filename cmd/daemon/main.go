package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon"
	"github.com/ZsombiiHUN/gamepanel/internal/daemon/runtime"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := daemon.Config{
		PanelURL: env("GAMEPANEL_PANEL_URL", "ws://127.0.0.1:8080/daemon/connect"),
		Token:    os.Getenv("GAMEPANEL_NODE_TOKEN"),
		DataDir:  env("GAMEPANEL_DATA_DIR", "./daemon-data"),
	}
	if cfg.Token == "" {
		log.Error("GAMEPANEL_NODE_TOKEN is required (create a node in the panel to get one)")
		os.Exit(1)
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
