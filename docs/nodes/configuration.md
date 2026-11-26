# Node configuration reference

The daemon is configured only through environment variables. With systemd, put them in `/etc/gamepanel/daemon.env` (see [Installing a node](/nodes/installing#_4-install-the-daemon)) and restart the daemon after a change.

| Variable | Default | Description |
|---|---|---|
| `GAMEPANEL_NODE_TOKEN` | **required** | The token shown when the node was created. The daemon refuses to start without it |
| `GAMEPANEL_PANEL_URL` | `ws://127.0.0.1:8080/daemon/connect` | Where to reach the panel. Use `wss://panel.example.com/daemon/connect` for any node that is not on the panel's machine. `https://panel.example.com` works too: `http(s)` becomes `ws(s)` and a bare address gets `/daemon/connect` |
| `GAMEPANEL_ALLOW_INSECURE_PANEL_URL` | unset | `1` allows a plain `ws://` panel address that is not on this machine or a private network address. The daemon refuses one otherwise: the token, consoles and files would cross the network unencrypted |
| `GAMEPANEL_DATA_DIR` | `./daemon-data` | Server files, server settings, local backups and the SFTP host key. See [Data directory](/nodes/data-directory) |
| `GAMEPANEL_RUNTIME` | `docker` | `fake` runs no containers, for development and trying the panel out |
| `GAMEPANEL_SFTP_ADDR` | `0.0.0.0:2022` | Address and port of the SFTP server; `off` turns it off. If the port is taken, the daemon logs a warning and runs without SFTP |
| `GAMEPANEL_PULL_ALLOW_PRIVATE` | unset | `1` lets "download from a URL" fetch from private and local addresses. Off by default, so server owners cannot make the node reach its own network |
| `GAMEPANEL_CONTAINER_USER` | uid 1000 | `uid` or `uid:gid` that game servers run as and that owns their files. The installer creates a `gamepanel-game` system user for it. Unset, servers run as uid 1000, usually a person's login account, and the daemon warns. See [Data directory](/nodes/data-directory#server-files) |
| `GAMEPANEL_FIREWALL` | on | `off` runs without the servers' [firewall](/nodes/isolation): servers can then reach the node's own services and cloud metadata. Needed where the daemon cannot see Docker's network (Docker Desktop, rootless Docker) |
| `GAMEPANEL_HOST_PORTS` | unset | Ports on the node that game servers may connect to, besides the ones servers publish: `3306` or `3306,8000-8100/tcp`. For a database on the node, say |
| `GAMEPANEL_BLOCK_PRIVATE` | unset | `1` also keeps servers away from private networks (10/8, 172.16/12, 192.168/16, 100.64/10): other machines in a provider's private network, for example |
| `GAMEPANEL_NETWORK` | `gamepanel0` | The Docker network servers join. The daemon creates it; an existing one must have inter-container traffic off |
| `GAMEPANEL_NETWORK_BRIDGE` | `gp0` | The host interface name the daemon gives that network when creating it |
| `GAMEPANEL_RESUME` | on | `off` keeps servers offline when the daemon starts. Otherwise servers that were running when the node or Docker went down start again |

Docker's own variables (`DOCKER_HOST`, `DOCKER_API_VERSION`, `DOCKER_CERT_PATH`, `DOCKER_TLS_VERIFY`) are honoured, so the daemon can use a non-default socket.

## What the daemon uses the panel address for

All of these go to the host in `GAMEPANEL_PANEL_URL`:

| Path | For |
|---|---|
| `/daemon/connect` (websocket) | The control connection: orders, state, console, usage |
| `/daemon/...` (HTTP) | Upload and download streams, S3 backup URLs, SFTP login checks |

With `wss://` all of them use HTTPS; with `ws://`, plain HTTP. That is why a remote node must use `wss://`.

## Starting

At start the daemon:

1. checks it has a token, and that Docker answers (unless `GAMEPANEL_RUNTIME=fake`);
1. creates the servers' network and installs their [firewall](/nodes/isolation); a node that cannot do that does not start;
2. loads the servers it already knows from its data directory;
3. starts SFTP;
4. connects to the panel and reports its servers, any containers it has no record of, and its SFTP port.

It stops cleanly on `SIGTERM` or `SIGINT`. Game servers keep running.

## Logs

The daemon logs to standard error in `key=value` form. Under systemd: `journalctl -u gamepanel-daemon`. A lost connection is logged as `panel connection lost` with the time until the next try.
