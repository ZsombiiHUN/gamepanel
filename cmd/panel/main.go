package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/web"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	dbPath := env("GAMEPANEL_DB", "./panel.db")
	st, err := store.Open(dbPath)
	if err != nil {
		log.Error("open database", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	if len(os.Args) > 1 && os.Args[1] == "create-admin" {
		if err := createAdmin(st, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	fs := flag.NewFlagSet("panel", flag.ExitOnError)
	addr := fs.String("addr", env("GAMEPANEL_ADDR", "127.0.0.1:8080"), "listen address")
	fs.Parse(os.Args[1:])

	proxies, err := api.ParseTrustedProxies(env("GAMEPANEL_TRUSTED_PROXIES", ""))
	if err != nil {
		log.Error("invalid GAMEPANEL_TRUSTED_PROXIES", "err", err)
		os.Exit(1)
	}
	h := hub.New(log)
	handler := api.New(api.Deps{
		Store: st, Hub: h, Log: log, Web: web.Handler(),
		TrustedProxies: proxies,
		CookieSecure:   env("GAMEPANEL_COOKIE_SECURE", "") == "1",
	})
	if st.CountUsers(context.Background()) == 0 {
		log.Warn("no users yet: run `panel create-admin you@example.com` to create the first admin")
	}
	srv := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(c)
	}()
	log.Info("panel listening", "addr", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}

// createAdmin makes an admin with a random password, printed once to the
// terminal that ran the command (never to logs or the database).
func createAdmin(st *store.Store, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: panel create-admin <email>")
	}
	b := make([]byte, 18)
	rand.Read(b)
	pw := base64.RawURLEncoding.EncodeToString(b)
	if _, err := st.CreateUser(context.Background(), args[0], pw, true); err != nil {
		return err
	}
	fmt.Printf("Admin created.\n  email:    %s\n  password: %s\nChange it after signing in.\n", args[0], pw)
	return nil
}
