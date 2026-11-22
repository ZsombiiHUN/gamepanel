# Docker Compose

The panel and the daemon are published as images:

| Image | What it is |
|---|---|
| `ghcr.io/zsombiihun/gamepanel` | The panel. Runs as a non-root user; keeps `panel.db` and its key file in `/data` |
| `ghcr.io/zsombiihun/gamepanel-daemon` | The daemon (a node). Drives the host's Docker through its socket |

Both are built for `amd64` and `arm64`. Tags: `0.1.0` (one release), `0.1` (its newest patch release) and `latest`. Pin a version in production.

## Panel and node on one machine

[`compose.yml`](https://github.com/ZsombiiHUN/gamepanel/blob/main/compose.yml) runs the panel behind [Caddy](https://caddyserver.com/), which gets an HTTPS certificate by itself, and optionally a node.

1. Point your domain's DNS at the machine and open ports 80 and 443.
2. Get the files and set the domain:
   ```sh
   mkdir gamepanel && cd gamepanel
   curl -fLO https://raw.githubusercontent.com/ZsombiiHUN/gamepanel/main/compose.yml
   curl -fL -o .env https://raw.githubusercontent.com/ZsombiiHUN/gamepanel/main/.env.example
   nano .env                       # GAMEPANEL_DOMAIN, and pin GAMEPANEL_VERSION
   ```
3. Start the panel and create the first admin:
   ```sh
   docker compose up -d
   docker compose exec panel panel create-admin you@example.com
   ```
4. Sign in at `https://<your domain>`, open **Nodes → Create node**, and put the token in `.env` as `GAMEPANEL_NODE_TOKEN`.
5. Start the node:
   ```sh
   docker compose --profile node up -d
   docker compose logs -f daemon
   ```

Other settings (email, single sign-on, S3 backups) go in a `panel.env` file next to `compose.yml`, one `NAME=value` per line; see the [configuration reference](/panel/configuration).

The [command line](/panel/cli) works through `docker compose exec panel panel <command>`.

## How the daemon container works

The daemon creates game server containers on the **host's** Docker, so a few things differ from a normal container:

- **Same path inside and out.** Server folders are bind-mounted into game containers by the host's Docker, which only knows host paths. `compose.yml` mounts `/var/lib/gamepanel-daemon` at the same path in the daemon container; keep it that way if you change it.
- **Host networking.** Game ports and SFTP (2022) are published on the host; the daemon reaches the panel through its public `wss://` address.
- **`pid: host`, `NET_ADMIN` and `SYS_ADMIN`** are for [network limits](/nodes/network-limits): the daemon enters each server's network namespace to shape its traffic. Without them, servers run without bandwidth limits and their console says so.
- **The Docker socket** gives the daemon full control of the host's Docker, the same as running the daemon as root on the host.

A node on another machine needs only the `daemon` service: copy it into a `compose.yml` of its own, with `GAMEPANEL_PANEL_URL` pointing at the panel.

## Backups

The database lives in the `panel-data` volume. Stop the panel for a moment to copy it consistently:

```sh
docker compose stop panel
docker run --rm -v gamepanel_panel-data:/data -v "$PWD":/backup alpine \
  tar czf /backup/panel-$(date +%F).tar.gz -C /data .
docker compose start panel
```

More in [Backing up the panel](/panel/backing-up).

## Upgrading

Change `GAMEPANEL_VERSION` in `.env`, take a backup, then:

```sh
docker compose pull
docker compose --profile node up -d
```

The database migrates itself when the new panel starts. Read the [changelog](https://github.com/ZsombiiHUN/gamepanel/blob/main/CHANGELOG.md) first.
