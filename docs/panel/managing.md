# Repair, settings and uninstalling

If you installed with [the installer](/panel/installing#the-quick-way-the-installer), run it again on the machine:

```sh
curl -fsSL https://github.com/ZsombiiHUN/gamepanel/releases/latest/download/install.sh | sudo bash
```

It shows what runs there (the panel, a node, or both), whether each service is up, and whether a newer release is out. Then it offers:

| Choice | What it does |
|---|---|
| **Upgrade** | Only when a newer release is out. See [Upgrading](/panel/upgrading). |
| **Repair** | Reinstalls the version you run: programs and systemd services. The database, server files and settings stay. It comes first when a program is missing. |
| **Change settings** | The panel's domain, HTTPS with Caddy, a node's token or the panel it connects to. |
| **Add a node here** | On a machine with only the panel. |
| **Uninstall** | Removes GamePanel and keeps its data unless you ask. |

Every choice also works without questions, for scripts.

## Repair

```sh
curl -fsSL .../install.sh | sudo bash -s -- repair
```

A build without a release version (`panel version` says `dev`) is replaced by the newest release. If `panel.env` or `daemon.env` is gone, it asks for the domain, or the panel and token, and writes a new one; add `--domain`, or `--panel` and `--token`, to run it without questions.

## Change settings

```sh
curl -fsSL .../install.sh | sudo bash -s -- configure --domain new.example.com
curl -fsSL .../install.sh | sudo bash -s -- configure --https --accept-terms
curl -fsSL .../install.sh | sudo bash -s -- configure --token <token>
curl -fsSL .../install.sh | sudo bash -s -- configure --panel panel.example.com
```

- **Domain** sets `GAMEPANEL_PUBLIC_URL`, renames the site in Caddy if the installer set Caddy up, and restarts the panel. Passkeys belong to a domain: after a change, people sign in with their password or authenticator app and add their passkeys again. Point the new domain's DNS at the server first.
- **HTTPS** installs Caddy (Debian and Ubuntu) and serves the panel's domain. Its certificate comes from Let's Encrypt, so it asks you to accept the [Subscriber Agreement](https://letsencrypt.org/repository/), or takes `--accept-terms`.
- **Token** and **panel** change `daemon.env`, restart the daemon and wait until the panel accepts it. A refused token is an error. A node's token is shown once, at **Nodes → Create node**; if it is lost, delete the node and create it again (possible while it has no servers).

Installing over an existing node with `node --token <token>` also replaces the token in the kept `daemon.env`.

## Uninstall

```sh
curl -fsSL .../install.sh | sudo bash -s -- uninstall          # asks first
curl -fsSL .../install.sh | sudo bash -s -- uninstall --yes    # keeps the data
curl -fsSL .../install.sh | sudo bash -s -- uninstall --purge --yes
```

On a machine with both, the guided uninstall asks whether to remove everything, only the panel or only the node. It lists what goes and what stays, and asks before it starts.

It always removes:

- the services `gamepanel` and `gamepanel-daemon` and their programs in `/usr/local/bin`,
- the game server containers. The daemon goes first so it does not start them again; each server gets 30 seconds to save, as when you stop it in the panel,
- the GamePanel site in `/etc/caddy/Caddyfile`, if the installer added it. A copy of the file is kept as `Caddyfile.before-gamepanel-uninstall`. If Caddy served nothing else, it is stopped and disabled.

It keeps, unless you choose to delete the data (`--purge`):

- the panel's database and key in `/var/lib/gamepanel`, and `/etc/gamepanel/panel.env`,
- the game server files and their backups in `/var/lib/gamepanel-daemon`, and `/etc/gamepanel/daemon.env`.

Install again later and the kept data and settings are used: same accounts, same servers, same node token.

Deleting the data asks you to type `delete`. It copies the panel's database, key and `panel.env` to `/var/backups/gamepanel/uninstall-<time>/` first, then removes the folders above and the `gamepanel` system user. Game server files are not copied: download anything you want to keep first.

It never removes Docker, Caddy itself, the upgrade backups in `/var/backups/gamepanel`, or the log `/var/log/gamepanel-install.log`.
