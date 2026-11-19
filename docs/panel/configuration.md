# Panel configuration reference

The panel is configured only through environment variables. With systemd, put them in `/etc/gamepanel/panel.env` (see [Installing the panel](/panel/installing#_3-configure)). The panel reads them at start, so restart it after a change.

The panel **refuses to start** when a setting is invalid, and says which one. It **warns** (and starts) for combinations that work but are probably a mistake, such as SMTP without a public URL.

The only command-line flag is `--addr`, which overrides `GAMEPANEL_ADDR`.

## Core

| Variable | Default | Description |
|---|---|---|
| `GAMEPANEL_ADDR` | `127.0.0.1:8080` | Address and port to listen on. Keep it on loopback behind a reverse proxy |
| `GAMEPANEL_DB` | `./panel.db` | Path of the SQLite database. Created, and migrated to the current schema, at start. New files get mode `0600` |
| `GAMEPANEL_PUBLIC_URL` | unset | The panel's address as users see it, e.g. `https://panel.example.com`. Links in emails are built from it, never from a request's `Host` header. Passkeys and single sign-on need it; passkeys need a domain name (or `localhost`), not an IP address |
| `GAMEPANEL_SECRET_KEY` | key file `<db>.key` | 64 hex characters (32 bytes) that encrypt two-factor secrets at rest. If unset, a key file is created next to the database with mode `0600`. **Back it up with the database**: without it, enrolled authenticators cannot be checked |

Generate a key with `openssl rand -hex 32`. If you switch from the key file to `GAMEPANEL_SECRET_KEY`, use the key file's contents, or existing authenticators stop working.

## HTTPS and proxies

| Variable | Default | Description |
|---|---|---|
| `GAMEPANEL_TRUSTED_PROXIES` | unset | Comma-separated IPs or CIDRs of your reverse proxies, e.g. `127.0.0.1,::1`. Only requests from these may set `X-Forwarded-For` and `X-Forwarded-Proto`. The proxy must also keep the `Host` header |
| `GAMEPANEL_COOKIE_SECURE` | unset | `1` makes the panel behave as if every browser came over HTTPS. Not needed when HTTPS reaches the panel directly or through a trusted proxy |
| `GAMEPANEL_HSTS` | `host` | `host`: `Strict-Transport-Security` for the panel's host only. `subdomains` adds `includeSubDomains`. `preload` also adds `preload` with a two-year `max-age`. Only sent over HTTPS |

See [Reverse proxy and HTTPS](/panel/reverse-proxy).

## Accounts and sessions

| Variable | Default | Description |
|---|---|---|
| `GAMEPANEL_SIGNUP` | `invite` | `invite`: accounts are made only by admins, through invitations, or by a shop through the API. `open`: anyone can register on the sign-in page |
| `GAMEPANEL_REQUIRE_MFA` | `off` | `admins` or `all`: those accounts must use a second factor (authenticator code or passkey). Until they set one up they can only use their account page, and their API keys stop working. An account that has a second factor but signed in without it must sign in again with it |
| `GAMEPANEL_SESSION_IDLE` | `30m` | Sign out a session after this long without use. A Go duration, at least `1m` |
| `GAMEPANEL_SESSION_MAX` | `12h` | Sign out a session this long after sign-in, however busy. At least `1m` |

## Email

| Variable | Default | Description |
|---|---|---|
| `GAMEPANEL_SMTP_HOST` | unset | SMTP server. Setting it turns on email: password resets, invitations and account setup links. Needs `GAMEPANEL_PUBLIC_URL` |
| `GAMEPANEL_SMTP_PORT` | by TLS mode | 587 for `starttls`, 465 for `tls`, 25 for `none` |
| `GAMEPANEL_SMTP_USERNAME`, `GAMEPANEL_SMTP_PASSWORD` | unset | Login, if the server needs one |
| `GAMEPANEL_SMTP_FROM` | | Sender, like `GamePanel <panel@example.com>` |
| `GAMEPANEL_SMTP_TLS` | `starttls` | `starttls` (port 587), `tls` for implicit TLS (port 465), `none` only for a relay on the same machine |

See [Email](/panel/email).

## Single sign-on

| Variable | Description |
|---|---|
| `GAMEPANEL_SSO_GITHUB_CLIENT_ID`, `GAMEPANEL_SSO_GITHUB_CLIENT_SECRET` | Sign in with GitHub |
| `GAMEPANEL_SSO_DISCORD_CLIENT_ID`, `GAMEPANEL_SSO_DISCORD_CLIENT_SECRET` | Sign in with Discord |
| `GAMEPANEL_SSO_OIDC_ISSUER`, `GAMEPANEL_SSO_OIDC_CLIENT_ID`, `GAMEPANEL_SSO_OIDC_CLIENT_SECRET` | Any OpenID Connect provider |
| `GAMEPANEL_SSO_OIDC_NAME` | Text on the OIDC button. Default: `single sign-on` |

All of them need `GAMEPANEL_PUBLIC_URL`. See [Single sign-on](/panel/sso).

## Backups in S3

| Variable | Default | Description |
|---|---|---|
| `GAMEPANEL_S3_ENDPOINT` | unset | Setting it stores **new** backups in S3 instead of on the node, e.g. `https://s3.eu-central-1.amazonaws.com` |
| `GAMEPANEL_S3_BUCKET` | | Required with an endpoint |
| `GAMEPANEL_S3_ACCESS_KEY`, `GAMEPANEL_S3_SECRET_KEY` | | Required with an endpoint |
| `GAMEPANEL_S3_REGION` | `us-east-1` | |
| `GAMEPANEL_S3_PREFIX` | empty | Folder inside the bucket, e.g. `gamepanel/` |
| `GAMEPANEL_S3_PATH_STYLE` | unset | `1` puts the bucket in the path instead of the host name. Needed for MinIO and most self-hosted stores |

See [Backups in S3](/panel/s3-backups).

## A complete example

```ini
# /etc/gamepanel/panel.env
GAMEPANEL_ADDR=127.0.0.1:8080
GAMEPANEL_DB=/var/lib/gamepanel/panel.db
GAMEPANEL_PUBLIC_URL=https://panel.example.com
GAMEPANEL_TRUSTED_PROXIES=127.0.0.1,::1
GAMEPANEL_REQUIRE_MFA=admins

GAMEPANEL_SMTP_HOST=smtp.example.com
GAMEPANEL_SMTP_USERNAME=panel@example.com
GAMEPANEL_SMTP_PASSWORD=change-me
GAMEPANEL_SMTP_FROM=GamePanel <panel@example.com>

GAMEPANEL_S3_ENDPOINT=https://s3.eu-central-1.amazonaws.com
GAMEPANEL_S3_REGION=eu-central-1
GAMEPANEL_S3_BUCKET=example-gamepanel-backups
GAMEPANEL_S3_ACCESS_KEY=AKIA...
GAMEPANEL_S3_SECRET_KEY=...
GAMEPANEL_S3_PREFIX=backups/

GAMEPANEL_SSO_DISCORD_CLIENT_ID=...
GAMEPANEL_SSO_DISCORD_CLIENT_SECRET=...
```

## Logs

The panel logs to standard error in `key=value` form (Go's `slog` text format). Under systemd, read them with `journalctl -u gamepanel`.
