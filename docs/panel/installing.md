# Installing the panel

This sets up the panel on a Linux server as a systemd service, behind a reverse proxy. It takes about fifteen minutes.

::: danger Read this first
GamePanel is pre-alpha. Go through the [production checklist](/getting-started/production-checklist) before you rely on it.
:::

You need:

- a Linux server (see [Requirements](/getting-started/requirements)),
- a domain name pointing at it, for example `panel.example.com`,
- root access.

## 1. Get the binaries

There are no release downloads yet, so build them. You can do this on any machine with Go 1.27.4+, Node 22 and pnpm 10, and copy the result over.

```sh
git clone https://github.com/ZsombiiHUN/gamepanel && cd gamepanel
git checkout <commit or tag you want to run>
make web                                  # builds the interface into the Go tree
CGO_ENABLED=0 go build -o bin/panel ./cmd/panel
CGO_ENABLED=0 go build -o bin/daemon ./cmd/daemon
```

With `CGO_ENABLED=0` both are static binaries with no runtime dependencies, so they run on any Linux of the same CPU type. (`make build` works too, but links against the build machine's C library.) For an arm64 server, add `GOARCH=arm64`.

Install the panel:

```sh
sudo install -m 0755 bin/panel /usr/local/bin/panel
```

::: tip
Write down which commit you built. Until there are version numbers, it is the only way to know what you run.
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

Create `/etc/systemd/system/gamepanel.service`:

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
