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
	"github.com/ZsombiiHUN/gamepanel/internal/version"
	"github.com/ZsombiiHUN/gamepanel/internal/web"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run is main with errors returned, so deferred cleanup (closing the
// database) happens on every exit path.
func run(args []string) error {
	if len(args) > 0 && version.IsFlag(args[0]) {
		fmt.Println(version.String("panel"))
		return nil
	}
	if len(args) > 0 && isHelp(args[0]) {
		// Answered before the database is opened, so asking for help
		// never creates panel.db or its key file.
		fmt.Print(usage)
		return nil
	}
	if len(args) > 0 && args[0] == "openapi" {
		// Prints the API description; needs no database (make openapi).
		b, err := api.OpenAPI()
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(b)
		return err
	}
	if len(args) > 0 && args[0] == "import-pterodactyl" {
		// A dry run reads only Pterodactyl's database; this panel's own
		// database is not opened (or created).
		return importPterodactyl(args[1:])
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	dbPath := env("GAMEPANEL_DB", "./panel.db")
	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Error("close database", "err", err)
		}
	}()
	st.Log = log
	if err := loadSecretKey(st, dbPath); err != nil {
		return err
	}
	commands := map[string]func(*store.Store, []string) error{
		"reset-2fa": reset2FA, "reset-link": resetLink, "create-admin": createAdmin,
	}
	if len(args) > 0 {
		if cmd, ok := commands[args[0]]; ok {
			return cmd(st, args[1:])
		}
	}
	return serve(st, log, args)
}

const usage = `Usage: panel [command]

Commands:
  (none)                      run the panel (-addr overrides GAMEPANEL_ADDR)
  create-admin <email>        create an admin account and print its password
  reset-2fa <email>           remove an account's second factors
  reset-link <email>          print a one-time password reset link
  import-pterodactyl          show what importing a Pterodactyl panel would do
                              (dry run; see import-pterodactyl -h)
  openapi                     print the OpenAPI description
  version                     print the version

Settings are environment variables (GAMEPANEL_DB, GAMEPANEL_ADDR, ...);
see https://github.com/ZsombiiHUN/gamepanel/blob/main/docs/panel/configuration.md
`

func isHelp(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "-help" || arg == "--help"
}

// serve runs the panel until SIGINT or SIGTERM.
func serve(st *store.Store, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("panel", flag.ContinueOnError)
	addr := fs.String("addr", env("GAMEPANEL_ADDR", "127.0.0.1:8080"), "listen address")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // usage was printed
		}
		return err
	}
	deps, err := configure(st, log, *addr)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	deps.SchedulerCtx = ctx
	handler := api.New(deps)
	if n, err := st.CountUsers(ctx); err == nil && n == 0 {
		log.Warn("no users yet: run `panel create-admin you@example.com` to create the first admin")
	}
	srv := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	shutdown := make(chan error, 1)
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		shutdown <- srv.Shutdown(c)
	}()
	log.Info("panel listening", "addr", *addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	if err := <-shutdown; err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

// configure reads the GAMEPANEL_* settings into the API's dependencies,
// failing on any that are invalid and warning about risky combinations.
func configure(st *store.Store, log *slog.Logger, addr string) (api.Deps, error) {
	proxies, err := api.ParseTrustedProxies(env("GAMEPANEL_TRUSTED_PROXIES", ""))
	if err != nil {
		return api.Deps{}, fmt.Errorf("invalid GAMEPANEL_TRUSTED_PROXIES: %w", err)
	}
	if err := sessionLimits(st); err != nil {
		return api.Deps{}, err
	}
	cookieSecure := env("GAMEPANEL_COOKIE_SECURE", "") == "1"
	if host, _, err := net.SplitHostPort(addr); err == nil && !cookieSecure && len(proxies) == 0 && !isLoopback(host) {
		log.Warn("listening beyond this machine without HTTPS settings: session cookies are not marked Secure. " +
			"Put the panel behind an HTTPS proxy and set GAMEPANEL_TRUSTED_PROXIES (or GAMEPANEL_COOKIE_SECURE=1)")
	}
	backups, err := s3Backups(log)
	if err != nil {
		return api.Deps{}, err
	}
	publicURL := strings.TrimRight(os.Getenv("GAMEPANEL_PUBLIC_URL"), "/")
	mailer, err := smtpMailer(log, publicURL)
	if err != nil {
		return api.Deps{}, err
	}
	signup := env("GAMEPANEL_SIGNUP", "invite")
	if signup != "invite" && signup != "open" {
		return api.Deps{}, fmt.Errorf("GAMEPANEL_SIGNUP must be invite or open, not %q", signup)
	}
	requireMFA := os.Getenv("GAMEPANEL_REQUIRE_MFA")
	if !api.ValidMFAPolicy(requireMFA) {
		return api.Deps{}, fmt.Errorf("GAMEPANEL_REQUIRE_MFA must be off, admins or all, not %q", requireMFA)
	}
	hsts, err := hstsHeader(os.Getenv("GAMEPANEL_HSTS"))
	if err != nil {
		return api.Deps{}, err
	}
	deps := api.Deps{
		Store: st, Hub: hub.New(log), Log: log, Web: web.Handler(),
		TrustedProxies: proxies, S3: backups,
		PublicURL: publicURL, Signup: signup, SSO: ssoProviders(log, publicURL),
		CookieSecure: cookieSecure, RequireMFA: requireMFA, HSTS: hsts,
	}
	if mailer != nil { // never a typed nil in the interface
		deps.Mail = mailer
	}
	return deps, nil
}

// sessionLimits applies GAMEPANEL_SESSION_MAX and GAMEPANEL_SESSION_IDLE.
func sessionLimits(st *store.Store) error {
	for _, d := range []struct {
		name string
		dst  *time.Duration
	}{{"GAMEPANEL_SESSION_MAX", &st.SessionMax}, {"GAMEPANEL_SESSION_IDLE", &st.SessionIdle}} {
		v := os.Getenv(d.name)
		if v == "" {
			continue
		}
		dur, err := time.ParseDuration(v)
		if err != nil || dur < time.Minute {
			return fmt.Errorf("invalid %s %q: want a duration of at least 1m, such as 30m or 12h", d.name, v)
		}
		*d.dst = dur
	}
	return nil
}

// s3Backups returns the remote backup store, or nil when none is configured.
func s3Backups(log *slog.Logger) (*s3.Client, error) {
	ep := os.Getenv("GAMEPANEL_S3_ENDPOINT")
	if ep == "" {
		return nil, nil
	}
	backups, err := s3.New(s3.Config{
		Endpoint: ep, Region: os.Getenv("GAMEPANEL_S3_REGION"), Bucket: os.Getenv("GAMEPANEL_S3_BUCKET"),
		AccessKey: os.Getenv("GAMEPANEL_S3_ACCESS_KEY"), SecretKey: os.Getenv("GAMEPANEL_S3_SECRET_KEY"),
		Prefix: os.Getenv("GAMEPANEL_S3_PREFIX"), PathStyle: os.Getenv("GAMEPANEL_S3_PATH_STYLE") == "1",
	})
	if err != nil {
		return nil, fmt.Errorf("invalid S3 backup settings: %w", err)
	}
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := backups.Check(c); err != nil {
		// Not fatal: the store may be down for a moment. Backups fail with
		// a reason until it works.
		log.Warn("could not write to the S3 backup bucket; check the GAMEPANEL_S3_* settings", "err", err)
	}
	log.Info("new backups go to S3", "endpoint", ep, "bucket", os.Getenv("GAMEPANEL_S3_BUCKET"))
	return backups, nil
}

// smtpMailer returns the mail sender, or nil when none is configured.
func smtpMailer(log *slog.Logger, publicURL string) (*mail.SMTP, error) {
	h := os.Getenv("GAMEPANEL_SMTP_HOST")
	if h == "" {
		return nil, nil
	}
	port := 0
	if p := os.Getenv("GAMEPANEL_SMTP_PORT"); p != "" {
		var err error
		if port, err = strconv.Atoi(p); err != nil {
			return nil, fmt.Errorf("GAMEPANEL_SMTP_PORT must be a number, not %q", p)
		}
	}
	mailer, err := mail.New(mail.Config{
		Host: h, Port: port, Username: os.Getenv("GAMEPANEL_SMTP_USERNAME"), Password: os.Getenv("GAMEPANEL_SMTP_PASSWORD"),
		From: os.Getenv("GAMEPANEL_SMTP_FROM"), TLS: os.Getenv("GAMEPANEL_SMTP_TLS"),
	})
	if err != nil {
		return nil, fmt.Errorf("invalid SMTP settings: %w", err)
	}
	if publicURL == "" {
		log.Warn("GAMEPANEL_SMTP_HOST is set but GAMEPANEL_PUBLIC_URL is not: emails need the panel's address for their links, so none are sent")
	}
	return mailer, nil
}

// ssoProviders returns the configured single sign-on providers.
func ssoProviders(log *slog.Logger, publicURL string) []*api.SSOProvider {
	var sso []*api.SSOProvider
	if id := os.Getenv("GAMEPANEL_SSO_GITHUB_CLIENT_ID"); id != "" {
		sso = append(sso, api.GitHubSSO(id, os.Getenv("GAMEPANEL_SSO_GITHUB_CLIENT_SECRET")))
	}
	if id := os.Getenv("GAMEPANEL_SSO_DISCORD_CLIENT_ID"); id != "" {
		sso = append(sso, api.DiscordSSO(id, os.Getenv("GAMEPANEL_SSO_DISCORD_CLIENT_SECRET")))
	}
	if iss := os.Getenv("GAMEPANEL_SSO_OIDC_ISSUER"); iss != "" {
		sso = append(sso, api.OIDCSSO(os.Getenv("GAMEPANEL_SSO_OIDC_NAME"), iss, os.Getenv("GAMEPANEL_SSO_OIDC_CLIENT_ID"), os.Getenv("GAMEPANEL_SSO_OIDC_CLIENT_SECRET")))
	}
	if len(sso) > 0 && publicURL == "" {
		log.Warn("single sign-on is configured but GAMEPANEL_PUBLIC_URL is not: the provider needs the panel's address to send people back, so it stays off")
	}
	return sso
}

// hstsHeader maps GAMEPANEL_HSTS to the Strict-Transport-Security value
// ("" leaves the API's default for the host only).
func hstsHeader(v string) (string, error) {
	switch v {
	case "", "host":
		return "", nil
	case "subdomains":
		return "max-age=31536000; includeSubDomains", nil
	case "preload":
		return "max-age=63072000; includeSubDomains; preload", nil
	}
	return "", fmt.Errorf("GAMEPANEL_HSTS must be host, subdomains or preload, not %q", v)
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
	if err := st.Audit(ctx, store.AuditEntry{ActorEmail: "cli", Action: "totp.reset", Target: args[0], Detail: "reset from the command line"}); err != nil {
		fmt.Fprintln(os.Stderr, "warning: two-factor was reset, but writing the audit log failed:", err)
	}
	fmt.Println("Two-factor authentication disabled for", args[0])
	return nil
}
