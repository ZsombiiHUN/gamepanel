# Installing the panel

This sets up the panel on a Linux server as a systemd service, behind a reverse proxy. It takes about fifteen minutes.

::: danger Read this first
GamePanel is pre-alpha. Go through the [production checklist](/getting-started/production-checklist) before you rely on it.
:::

You need:

- a Linux server (see [Requirements](/getting-started/requirements)),
- a domain name pointing at it, for example `panel.example.com`,
- root access.

## The quick way: the installer

On Debian or Ubuntu, one command does every step on this page (and sets up Caddy for HTTPS):

```sh
curl -fsSL https://github.com/ZsombiiHUN/gamepanel/releases/latest/download/install.sh \
  | sudo bash -s -- panel --domain panel.example.com --email you@example.com
```

It downloads the newest release, checks it against the release's `checksums.txt`, and installs the same files, user and systemd unit described below. It prints the admin's password once. Add `--version v0.1.0` to pin a release, `--no-caddy` to bring your own proxy. Running it again is safe: it keeps an existing `panel.env` and database. Read the script first if you like; it is [`scripts/install.sh`](https://github.com/ZsombiiHUN/gamepanel/blob/main/scripts/install.sh).

Prefer containers? See [Docker Compose](/panel/docker). The rest of this page does it by hand.

## 1. Get the binaries

Download the panel for your CPU (`amd64` or `arm64`) from the [releases page](https://github.com/ZsombiiHUN/gamepanel/releases), and check it:

```sh
V=0.1.0 A=amd64
curl -fLO https://github.com/ZsombiiHUN/gamepanel/releases/download/v$V/gamepanel-panel_${V}_linux_$A.tar.gz
curl -fLO https://github.com/ZsombiiHUN/gamepanel/releases/download/v$V/checksums.txt
sha256sum --ignore-missing -c checksums.txt
tar xzf gamepanel-panel_${V}_linux_$A.tar.gz
sudo install -m 0755 panel /usr/local/bin/panel
panel version
```

The binary is static, with the interface built in; it runs on any Linux of that CPU type.

::: details Building from source instead
You need Go (see `go.mod`), Node 22 and pnpm 10.

```sh
git clone https://github.com/ZsombiiHUN/gamepanel && cd gamepanel
git checkout v0.1.0
make web                                  # builds the interface into the Go tree
CGO_ENABLED=0 go build -o bin/panel ./cmd/panel
CGO_ENABLED=0 go build -o bin/daemon ./cmd/daemon
sudo install -m 0755 bin/panel /usr/local/bin/panel
```

With `CGO_ENABLED=0` both are static. For an arm64 server, add `GOARCH=arm64`.
:::

## 2. Create a user and folders

The panel does not need root. Give it its own user:

```sh
sudo useradd --system --home /var/lib/gamepanel --shell /usr/sbin/nologin gamepanel
sudo install -d -o gamepanel -g gamepanel -m 0700 /var/lib/gamepanel
sudo install -d -o root -g gamepanel -m 0750 /etc/gamepanel
```

`/var/lib/gamepanel` holds the database and its key file. Nothing else needs to be writable.

## 3. Configure

Create `/etc/gamepanel/panel.env`:

```ini
GAMEPANEL_ADDR=127.0.0.1:8080
GAMEPANEL_DB=/var/lib/gamepanel/panel.db
GAMEPANEL_PUBLIC_URL=https://panel.example.com
GAMEPANEL_TRUSTED_PROXIES=127.0.0.1,::1
GAMEPANEL_REQUIRE_MFA=admins
```

```sh
sudo chown root:gamepanel /etc/gamepanel/panel.env
sudo chmod 0640 /etc/gamepanel/panel.env
```

The file will hold secrets once you add SMTP, S3 or single sign-on, so keep it readable only by root and the panel. Every setting is in the [configuration reference](/panel/configuration).

## 4. Create the first admin

Run the command as the panel's user, with the same settings the service will use:

```sh
sudo -u gamepanel sh -c 'set -a; . /etc/gamepanel/panel.env; exec /usr/local/bin/panel create-admin you@example.com'
```

It prints a random password **once**. The first run also creates the database and the key file `/var/lib/gamepanel/panel.db.key`.

## 5. Run it as a service

The release archive has this unit in `packaging/systemd/gamepanel.service`; copy it, or create `/etc/systemd/system/gamepanel.service`:

```ini
[Unit]
Description=GamePanel panel
After=network-online.target
Wants=network-online.target

[Service]
User=gamepanel
Group=gamepanel
EnvironmentFile=/etc/gamepanel/panel.env
ExecStart=/usr/local/bin/panel
WorkingDirectory=/var/lib/gamepanel
Restart=on-failure
RestartSec=5s

# Hardening: the panel only needs its own folder and the network.
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ReadWritePaths=/var/lib/gamepanel
CapabilityBoundingSet=
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
LockPersonality=yes
MemoryDenyWriteExecute=yes
RestrictNamespaces=yes
RestrictSUIDSGID=yes
SystemCallArchitectures=native

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now gamepanel
sudo systemctl status gamepanel
journalctl -u gamepanel -f        # the panel logs to stderr
```

The log should end with `panel listening addr=127.0.0.1:8080`. The panel stops cleanly on `SIGTERM`, waiting up to 5 seconds for requests in flight.

## 6. Put HTTPS in front

Follow [Reverse proxy and HTTPS](/panel/reverse-proxy). With Caddy this is two lines.

Then open `https://panel.example.com`, sign in, and change the admin password under **Account**. Set up two-factor or a passkey there too; with `GAMEPANEL_REQUIRE_MFA=admins` the panel will insist.

## 7. Add a node

Continue with [Installing a node](/nodes/installing). The panel's machine can be a node as well.

## Firewall

On the panel machine, only the reverse proxy's ports need to be open:

| Port | For |
|---|---|
| 80/tcp | HTTP, redirected to HTTPS (and certificate challenges) |
| 443/tcp | HTTPS: browsers and nodes |

Do **not** expose port 8080; it is plain HTTP.

## Next steps

- [Email](/panel/email), for password resets and invitations
- [Backups in S3](/panel/s3-backups)
- [Backing up the panel](/panel/backing-up)
- [Single sign-on](/panel/sso)
