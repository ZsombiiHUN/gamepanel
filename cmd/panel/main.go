package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/mail"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/s3"
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
	if len(os.Args) > 1 && os.Args[1] == "openapi" {
		// Prints the API description; needs no database (make openapi).
		b, err := api.OpenAPI()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		os.Stdout.Write(b)
		return
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	dbPath := env("GAMEPANEL_DB", "./panel.db")
	st, err := store.Open(dbPath)
	if err != nil {
		log.Error("open database", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	if err := loadSecretKey(st, dbPath); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if len(os.Args) > 1 && os.Args[1] == "reset-2fa" {
		if err := reset2FA(st, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "reset-link" {
		if err := resetLink(st, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
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
	for _, d := range []struct {
		name string
		dst  *time.Duration
	}{{"GAMEPANEL_SESSION_MAX", &st.SessionMax}, {"GAMEPANEL_SESSION_IDLE", &st.SessionIdle}} {
		if v := os.Getenv(d.name); v != "" {
			dur, err := time.ParseDuration(v)
			if err != nil || dur < time.Minute {
				log.Error("invalid "+d.name+": want a duration of at least 1m, such as 30m or 12h", "value", v)
				os.Exit(1)
			}
			*d.dst = dur
		}
	}
	cookieSecure := env("GAMEPANEL_COOKIE_SECURE", "") == "1"
	if host, _, err := net.SplitHostPort(*addr); err == nil && !cookieSecure && len(proxies) == 0 && !isLoopback(host) {
		log.Warn("listening beyond this machine without HTTPS settings: session cookies are not marked Secure. " +
			"Put the panel behind an HTTPS proxy and set GAMEPANEL_TRUSTED_PROXIES (or GAMEPANEL_COOKIE_SECURE=1)")
	}
	var backups *s3.Client
	if ep := os.Getenv("GAMEPANEL_S3_ENDPOINT"); ep != "" {
		backups, err = s3.New(s3.Config{
			Endpoint: ep, Region: os.Getenv("GAMEPANEL_S3_REGION"), Bucket: os.Getenv("GAMEPANEL_S3_BUCKET"),
			AccessKey: os.Getenv("GAMEPANEL_S3_ACCESS_KEY"), SecretKey: os.Getenv("GAMEPANEL_S3_SECRET_KEY"),
			Prefix: os.Getenv("GAMEPANEL_S3_PREFIX"), PathStyle: os.Getenv("GAMEPANEL_S3_PATH_STYLE") == "1",
		})
		if err != nil {
			log.Error("invalid S3 backup settings", "err", err)
			os.Exit(1)
		}
		c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if err := backups.Check(c); err != nil {
			// Not fatal: the store may be down for a moment. Backups fail with
			// a reason until it works.
			log.Warn("could not write to the S3 backup bucket; check the GAMEPANEL_S3_* settings", "err", err)
		}
		cancel()
		log.Info("new backups go to S3", "endpoint", ep, "bucket", os.Getenv("GAMEPANEL_S3_BUCKET"))
	}
	var mailer *mail.SMTP
	publicURL := strings.TrimRight(os.Getenv("GAMEPANEL_PUBLIC_URL"), "/")
	if h := os.Getenv("GAMEPANEL_SMTP_HOST"); h != "" {
		port, _ := strconv.Atoi(os.Getenv("GAMEPANEL_SMTP_PORT"))
		mailer, err = mail.New(mail.Config{Host: h, Port: port, Username: os.Getenv("GAMEPANEL_SMTP_USERNAME"), Password: os.Getenv("GAMEPANEL_SMTP_PASSWORD"),
			From: os.Getenv("GAMEPANEL_SMTP_FROM"), TLS: os.Getenv("GAMEPANEL_SMTP_TLS")})
		if err != nil {
			log.Error("invalid SMTP settings", "err", err)
			os.Exit(1)
		}
		if publicURL == "" {
			log.Warn("GAMEPANEL_SMTP_HOST is set but GAMEPANEL_PUBLIC_URL is not: emails need the panel's address for their links, so none are sent")
		}
	}
	signup := env("GAMEPANEL_SIGNUP", "invite")
	if signup != "invite" && signup != "open" {
		log.Error("GAMEPANEL_SIGNUP must be invite or open", "value", signup)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	h := hub.New(log)
	deps := api.Deps{
		Store: st, Hub: h, Log: log, Web: web.Handler(), SchedulerCtx: ctx,
		TrustedProxies: proxies, S3: backups,
		PublicURL: publicURL, Signup: signup,
		CookieSecure: cookieSecure,
	}
	if mailer != nil { // never a typed nil in the interface
		deps.Mail = mailer
	}
	handler := api.New(deps)
	if st.CountUsers(context.Background()) == 0 {
		log.Warn("no users yet: run `panel create-admin you@example.com` to create the first admin")
	}
	srv := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}

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

// isLoopback reports whether a listen host only accepts local connections.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// resetLink prints a password reset link for an account: the way to reset a
// password on a panel without email.
func resetLink(st *store.Store, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: panel reset-link you@example.com")
	}
	u, err := st.UserByEmail(context.Background(), args[0])
	if err != nil {
		return errors.New("no account with that email")
	}
	tok, err := st.NewResetToken(context.Background(), u.ID)
	if err != nil {
		return err
	}
	base := strings.TrimRight(os.Getenv("GAMEPANEL_PUBLIC_URL"), "/")
	if base == "" {
		base = "<panel address>"
	}
	fmt.Printf("Password reset link for %s (valid for %s, works once):\n%s/reset#token=%s\n", u.Email, store.ResetTTL, base, tok)
	return nil
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

// loadSecretKey sets the key that encrypts second-factor secrets. It comes
// from GAMEPANEL_SECRET_KEY (64 hex chars) or, if unset, a key file created
// next to the database with mode 0600. Back it up with the database: without
// it, enrolled authenticator secrets cannot be read.
func loadSecretKey(st *store.Store, dbPath string) error {
	if h := os.Getenv("GAMEPANEL_SECRET_KEY"); h != "" {
		k, err := hex.DecodeString(h)
		if err != nil || len(k) != 32 {
			return errors.New("GAMEPANEL_SECRET_KEY must be 64 hex characters (32 bytes)")
		}
		return st.SetSecretKey(k)
	}
	path := dbPath + ".key"
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		k := make([]byte, 32)
		rand.Read(k)
		if err := os.WriteFile(path, []byte(hex.EncodeToString(k)), 0o600); err != nil {
			return fmt.Errorf("create %s: %w", path, err)
		}
		return st.SetSecretKey(k)
	}
	if err != nil {
		return err
	}
	k, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(k) != 32 {
		return fmt.Errorf("%s is not a valid key file", path)
	}
	return st.SetSecretKey(k)
}

// reset2FA is the offline recovery path for a user who lost both their
// authenticator and recovery codes. It needs shell access to the panel host.
func reset2FA(st *store.Store, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: panel reset-2fa <email>")
	}
	ctx := context.Background()
	id, err := st.UserIDByEmail(ctx, args[0])
	if err != nil {
		return errors.New("no such user")
	}
	if err := st.DisableTOTP(ctx, id); err != nil {
		return err
	}
	st.Audit(ctx, store.AuditEntry{ActorEmail: "cli", Action: "totp.reset", Target: args[0], Detail: "reset from the command line"})
	fmt.Println("Two-factor authentication disabled for", args[0])
	return nil
}
